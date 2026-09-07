// Package client hosts the provisioner's manager-facing loop (design §7.2):
// connect with the provisioner token, send ProvisionerReady, receive
// provisioning jobs, drive the configured Backend, report backend events as
// ProvisionJobProgress, and reconnect with exponential backoff when the
// stream dies. The provisioner keeps no durable job state — the manager
// replays every non-terminal job on (re)connect, and backends upsert
// idempotently.
package client

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"golang.org/x/net/http2"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
	"github.com/Ranxy/laelia/backend/provisioner/version"
)

const (
	// pingInterval is the ProvisionerChannel keepalive cadence (design §4.3).
	pingInterval  = 30 * time.Second
	retryBaseWait = 2 * time.Second
	retryMaxWait  = 1 * time.Minute
)

// ErrShutdown is returned by Run when the manager told the provisioner to
// stop (token rotated or provisioner deleted): the process must exit instead
// of retrying with a dead credential.
var ErrShutdown = errors.New("provisioner shutdown requested by manager")

// Config is the client's runtime configuration.
type Config struct {
	ManagerURL string
	Token      string
	// Backend is the configured backend name, reported in ProvisionerReady.
	Backend string
	// AutoUpgrade is echoed in ProvisionerReady; the manager's auto-upgrade
	// loop honors it for this provisioner's machines.
	AutoUpgrade bool
	// RetainData is echoed in ProvisionerReady; the manager sends it back as
	// keep_data on teardown jobs so it can honor the configured retention.
	RetainData bool
	// ManagerURLOverride replaces job manager_url so pods reach the manager
	// through an in-cluster URL instead of the public one.
	ManagerURLOverride string
	// ConfigDigest is the short hash of the effective config, surfaced for
	// drift visibility in the provisioner status.
	ConfigDigest string
	Insecure     bool
	AllowHTTP    bool
}

// Client is the provisioner's manager client: one ProvisionerChannel bidi
// stream plus the configured Backend.
type Client struct {
	cfg          Config
	backend      backend.Backend
	streamClient *http.Client
	backoff      *ExponentialBackoff

	// sendMu serializes sends on the bidi stream: the ping ticker and the
	// event pump (backend events) both call sendStream, and connect's bidi
	// client is not safe for concurrent Send.
	sendMu sync.Mutex
	sender func(*v1pb.ProvisionerStreamMessage) error // nil while disconnected
}

// New builds a client around an already-constructed backend (the cmd layer
// resolves the registry; tests construct backends directly).
func New(cfg Config, be backend.Backend) (*Client, error) {
	cfg.ManagerURL = strings.TrimRight(cfg.ManagerURL, "/")
	if cfg.ManagerURL == "" {
		return nil, errors.New("manager_url is required")
	}
	if cfg.Token == "" {
		return nil, errors.New("token is required")
	}
	if cfg.Backend == "" {
		return nil, errors.New("backend is required")
	}
	if be == nil {
		return nil, errors.New("backend implementation is required")
	}
	if strings.HasPrefix(cfg.ManagerURL, "http://") {
		if !cfg.AllowHTTP {
			return nil, errors.New("plain HTTP connections are not allowed by default, use --allow-http or switch to https://")
		}
		slog.Warn("plain HTTP connection enabled, traffic will not be encrypted")
	}

	// The ProvisionerChannel is a long-lived bidi stream: no global timeout,
	// explicit HTTP/2 support (connect bidi requires it).
	streamClient := &http.Client{}
	if strings.HasPrefix(cfg.ManagerURL, "https://") {
		tlsCfg := &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: cfg.Insecure,
		}
		streamClient.Transport = &http.Transport{
			TLSClientConfig:       tlsCfg,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 60 * time.Second,
		}
	} else {
		// Plain HTTP still needs HTTP/2 for bidi; dial h2c directly, like the
		// machine client does for --allow-http.
		streamClient.Transport = &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		}
	}

	return &Client{
		cfg:          cfg,
		backend:      be,
		streamClient: streamClient,
		backoff:      NewExponentialBackoff(retryBaseWait, retryMaxWait),
	}, nil
}

// Run drives the client until ctx is done, the manager asks for shutdown, or
// the credential is permanently rejected. Backend controllers/watchers run
// for the lifetime of the call; their events flow through the event pump to
// whichever stream is live. Transient stream deaths reconnect with
// exponential backoff (the manager replays outstanding jobs).
func (c *Client) Run(ctx context.Context) error {
	eventCh := make(chan backend.Event, 64)
	if err := c.backend.Start(ctx, eventCh); err != nil {
		return errors.Wrap(err, "failed to start provisioner backend")
	}
	go c.pumpEvents(ctx, eventCh)

	for {
		err := c.runOnce(ctx)
		// A cancelled context is the normal shutdown path, not a failure.
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil // nolint:nilerr // cancellation is graceful shutdown
		}
		if errors.Is(err, ErrShutdown) || IsPermanentAuthFailure(err) {
			return err
		}
		if err != nil {
			slog.Warn("provisioner stream lost; reconnecting", "error", err)
		}
		if waitErr := c.backoff.Wait(ctx); waitErr != nil {
			return nil // nolint:nilerr // cancellation during backoff is shutdown
		}
	}
}

// runOnce lives for one stream: dial, announce readiness, pump receives, and
// ping. It returns when the stream ends (with the reason), ctx is cancelled,
// or a send fails.
func (c *Client) runOnce(ctx context.Context) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream := v1connect.NewProvisionerStreamServiceClient(c.streamClient, c.cfg.ManagerURL).ProvisionerChannel(streamCtx)
	stream.RequestHeader().Set("Authorization", "Bearer "+c.cfg.Token)

	// connect's bidi stream is a persistent HTTP/2 request: cancelling the
	// stream context does not tear the request down, so both halves must be
	// closed explicitly when this connection ends. CloseRequest half-closes
	// the request side (END_STREAM) so the manager's Receive unblocks with
	// EOF and its teardown stamps the provisioner offline; CloseResponse
	// unblocks this side's pending Receive.
	defer func() {
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
	}()

	c.setSender(stream.Send)
	defer c.setSender(nil)

	// Ready is the first frame; it also fires the (lazy) HTTP request.
	if err := c.sendStream(&v1pb.ProvisionerStreamMessage{
		Message: &v1pb.ProvisionerStreamMessage_Ready{Ready: &v1pb.ProvisionerReady{
			Version:       version.Version,
			Backend:       c.cfg.Backend,
			ConfigDigest:  c.cfg.ConfigDigest,
			AutoUpgrade:   c.cfg.AutoUpgrade,
			RetainData:    c.cfg.RetainData,
			MachineParams: machineParamsReady(c.backend.MachineParams()),
		}},
	}); err != nil {
		return err
	}
	slog.Info("provisioner stream opened", "backend", c.backend.Name(), "manager", c.cfg.ManagerURL)

	// res carries the receive pump's terminal error so the sender goroutine's
	// exit unblocks the loop below with a deterministic reason.
	res := &streamResult{}
	streamDone := make(chan struct{})

	go func() {
		defer close(streamDone)
		sawFrame := false
		for {
			msg, err := stream.Receive()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					res.set(err)
				}
				return
			}
			if !sawFrame {
				sawFrame = true
				c.backoff.Reset()
			}
			switch m := msg.Message.(type) {
			case *v1pb.ManagerProvisionerStreamMessage_ProvisionJob:
				go c.handleProvisionJob(streamCtx, m.ProvisionJob)

			case *v1pb.ManagerProvisionerStreamMessage_DeprovisionJob:
				go c.handleDeprovisionJob(streamCtx, m.DeprovisionJob)

			case *v1pb.ManagerProvisionerStreamMessage_DisconnectNotice:
				slog.Warn("manager requested provisioner shutdown", "reason", m.DisconnectNotice.GetReason())
				if m.DisconnectNotice.GetDeleted() {
					// The provisioner was permanently deleted: tear down our own
					// hosting (scale the operator Deployment to 0) so we stop
					// crash-looping with a dead credential. Best-effort.
					if err := c.backend.Shutdown(streamCtx); err != nil {
						slog.Warn("failed to tear down the operator after provisioner deletion", "error", err)
					}
				}
				res.set(ErrShutdown)
				return

			case *v1pb.ManagerProvisionerStreamMessage_Pong:
				// keepalive acknowledged

			default:
				slog.Warn("unknown message type from manager on provisioner stream")
			}
		}
	}()

	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	var pingSeq atomic.Int64
	for {
		select {
		case <-streamCtx.Done():
			return nil
		case <-streamDone:
			// nil = the manager closed cleanly; the caller reconnects.
			return res.get()
		case <-pingTicker.C:
			if err := c.sendStream(&v1pb.ProvisionerStreamMessage{
				Message: &v1pb.ProvisionerStreamMessage_Ping{
					Ping: &v1pb.Ping{Seq: pingSeq.Add(1), SentAt: time.Now().UnixMilli()},
				},
			}); err != nil {
				return err
			}
		}
	}
}

// pumpEvents forwards backend events to the manager as ProvisionJobProgress
// frames. Frames that race a dead stream are dropped: the job stays in a
// non-terminal phase on the manager side and is replayed on reconnect.
func (c *Client) pumpEvents(ctx context.Context, ch <-chan backend.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if err := c.sendStream(progressFrame(e)); err != nil {
				slog.Warn("failed to report provision progress; the manager replays non-terminal jobs",
					"machine", e.MachineID, "phase", e.Phase.String(), "error", err)
			}
		}
	}
}

// handleProvisionJob translates one ProvisionMachineJob into a backend
// upsert. A backend error becomes a FAILED progress frame so the machine row
// records the reason; a cancelled context (shutdown mid-flight) reports
// nothing — the manager replays the job on the next connect.
func (c *Client) handleProvisionJob(ctx context.Context, job *v1pb.ProvisionMachineJob) {
	if job == nil {
		return
	}
	machineID, err := common.GetMachineResourceID(job.GetMachine())
	if err != nil {
		slog.Error("malformed provision job machine name", "machine", job.GetMachine(), "error", err)
		return
	}
	spec := c.machineSpecFromJob(job, machineID)
	slog.Info("provisioning machine", "machine", machineID, "title", spec.Title, "workload_image", spec.RuntimeImage)
	if err := c.backend.Provision(ctx, spec, job.GetRefreshToken()); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Error("backend provision failed", "machine", machineID, "error", err)
		_ = c.sendStream(progressFrame(backend.Event{
			MachineID: machineID,
			Phase:     storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED,
			Error:     err.Error(),
		}))
	}
}

// handleDeprovisionJob translates one DeprovisionMachineJob into a backend
// teardown. Failures are logged only: the machine stays in DEPROVISIONING on
// the manager side and the job is replayed on the next connect.
func (c *Client) handleDeprovisionJob(ctx context.Context, job *v1pb.DeprovisionMachineJob) {
	if job == nil {
		return
	}
	machineID, err := common.GetMachineResourceID(job.GetMachine())
	if err != nil {
		slog.Error("malformed deprovision job machine name", "machine", job.GetMachine(), "error", err)
		return
	}
	slog.Info("deprovisioning machine", "machine", machineID, "keepData", job.GetKeepData())
	if err := c.backend.Deprovision(ctx, machineID, job.GetKeepData()); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Error("backend deprovision failed", "machine", machineID, "error", err)
	}
}

// machineParamsReady converts the backend's schema report into its wire form
// (store-shaped: the manager resolves the value types from its catalog).
func machineParamsReady(specs []*storepb.MachineParamSpec) []*v1pb.MachineParamSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]*v1pb.MachineParamSpec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, &v1pb.MachineParamSpec{
			Key:          spec.GetKey(),
			Required:     spec.GetRequired(),
			DefaultValue: spec.GetDefaultValue(),
			MinValue:     spec.GetMinValue(),
			MaxValue:     spec.GetMaxValue(),
			Options:      spec.GetOptions(),
		})
	}
	return out
}

// machineSpecFromJob builds the backend-neutral spec; manager_url_override
// redirects pods to an in-cluster manager URL (design §7.2).
func (c *Client) machineSpecFromJob(job *v1pb.ProvisionMachineJob, machineID string) backend.MachineSpec {
	managerURL := job.GetManagerUrl()
	if c.cfg.ManagerURLOverride != "" {
		managerURL = c.cfg.ManagerURLOverride
	}
	return backend.MachineSpec{
		MachineID:       machineID,
		Title:           job.GetTitle(),
		ManagerURL:      managerURL,
		Fingerprint:     job.GetFingerprint(),
		RuntimeImage:    job.GetRuntimeImage(),
		BinaryTarget:    job.GetBinaryTarget(),
		BootstrapScript: job.GetBootstrapScript(),
		Labels:          job.GetMachineLabels(),
		Params:          job.GetMachineParams(),
	}
}

// progressFrame maps a backend event to its stream frame.
func progressFrame(e backend.Event) *v1pb.ProvisionerStreamMessage {
	return &v1pb.ProvisionerStreamMessage{
		Message: &v1pb.ProvisionerStreamMessage_JobProgress{
			JobProgress: &v1pb.ProvisionJobProgress{
				Machine:      common.FormatMachineUID(e.MachineID),
				Phase:        v1pb.ProvisioningPhase(e.Phase),
				Error:        e.Error,
				WorkloadName: e.WorkloadName,
			},
		},
	}
}

func (c *Client) setSender(sender func(*v1pb.ProvisionerStreamMessage) error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.sender = sender
}

// sendStream sends one frame on the current stream; without a live stream the
// frame is dropped with an error (reportable, never fatal).
func (c *Client) sendStream(msg *v1pb.ProvisionerStreamMessage) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if c.sender == nil {
		return errors.New("provisioner stream is not connected")
	}
	return c.sender(msg)
}

// IsPermanentAuthFailure reports whether err means the provisioner's
// credential is permanently rejected (rotated/deleted provisioner) and
// retrying cannot help.
func IsPermanentAuthFailure(err error) bool {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return false
	}
	switch ce.Code() {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied:
		return true
	default:
		return false
	}
}

// DescribeAuthFailure maps a permanent (unauthenticated/permission-denied)
// credential rejection to a plain-language cause and the exact steps an
// operator should take. The manager rejects a provisioner token for a few
// distinct reasons (token signed by a different manager secret, deleted
// provisioner, rotated token, wrong environment); surfacing each with its fix
// turns a CrashLoopBackOff into an actionable message. Returns the raw error
// for non-auth errors so callers can always log something useful.
func DescribeAuthFailure(err error) string {
	if err == nil {
		return "unknown authentication error"
	}
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return err.Error()
	}
	msg := ce.Message()
	switch {
	case strings.Contains(msg, "signature is invalid"):
		return "the provisioner token was NOT issued by this manager — it was signed " +
			"with a different JWT secret than the one stored in the manager's database " +
			"(wrong environment, database reset, or a token pasted from another instance). " +
			"Fix: on the target manager open Settings → Provisioners, create or rotate a " +
			"provisioner, copy the new token into the k8s Secret token field " +
			"(deploy/deployment.yaml, 'laelia-provisioner-token'), apply it, and restart: " +
			"kubectl -n laelia-machines rollout restart deployment/laelia-provisioner"
	case strings.Contains(msg, "not exists"),
		strings.Contains(msg, "has been deactivated"),
		strings.Contains(msg, "failed to find provisioner"):
		return "the token's provisioner no longer exists on this manager (it was deleted or " +
			"deactivated, or was registered on a different instance). Fix: create a provisioner " +
			"under Settings → Provisioners and put its token into the k8s Secret, then restart " +
			"deployment/laelia-provisioner"
	case strings.Contains(msg, "token version mismatch"):
		return "the provisioner token was rotated on the manager; the version pinned in this " +
			"deployment is stale. Fix: copy the latest token from Settings → Provisioners into " +
			"the k8s Secret and restart deployment/laelia-provisioner"
	case strings.Contains(msg, "audience mismatch"),
		strings.Contains(msg, "invalid provisioner access token"):
		return "the manager does not recognize this token — it may have been minted for a " +
			"different release mode or environment. Fix: mint a fresh provisioner token on the " +
			"manager this provisioner is configured to connect to, put it into the k8s Secret, " +
			"and restart"
	default:
		return "the provisioner credential was rejected (" + msg + "). Recreate or rotate the " +
			"provisioner under Settings → Provisioners, put the new token into the k8s Secret, " +
			"and restart deployment/laelia-provisioner"
	}
}

// streamResult carries the receive pump's terminal error to the run loop.
type streamResult struct {
	mu  sync.Mutex
	err error
}

func (r *streamResult) set(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

func (r *streamResult) get() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// ExponentialBackoff spaces reconnect attempts: baseWait * 2^attempt capped
// at maxWait, reset whenever the stream proves alive.
type ExponentialBackoff struct {
	baseWait time.Duration
	maxWait  time.Duration
	attempt  int
}

func NewExponentialBackoff(baseWait, maxWait time.Duration) *ExponentialBackoff {
	return &ExponentialBackoff{baseWait: baseWait, maxWait: maxWait}
}

// Wait sleeps for the current backoff window (or until ctx is done).
func (eb *ExponentialBackoff) Wait(ctx context.Context) error {
	wait := time.Duration(math.Min(float64(eb.baseWait)*math.Pow(2, float64(eb.attempt)), float64(eb.maxWait)))
	eb.attempt++
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

// Reset restarts the backoff progression after a proven-alive stream.
func (eb *ExponentialBackoff) Reset() {
	eb.attempt = 0
}
