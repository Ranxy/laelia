// Package client hosts the machine app's manager-facing client. A machine
// authenticates once (machine registration → access + refresh token) and then
// hosts many agents: it holds one MachineChannel control stream for roster
// changes + provider discovery, and opens one AgentChannel per assigned agent
// for that agent's drain loop. All agents share the machine's access token and
// a single local daemon socket; the daemon routes each CLI call to the agent
// named in LAELIA_AGENT.
package client

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/credential"
	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultConnectTimeout    = 30 * time.Second
	defaultRetryMaxWait      = 1 * time.Minute
	defaultRetryBaseWait     = 2 * time.Second
	// heartbeatTimeout bounds a single Heartbeat RPC. The machine's liveness
	// must not stall on a manager that accepts the connection but never replies;
	// a per-call timeout makes heartbeat failure (not the peer's tcp keepalive)
	// the detection signal.
	heartbeatTimeout = 10 * time.Second
	// machinePingInterval is the MachineChannel keepalive cadence. The manager
	// pings back (Pong) for liveness correlation; a dead control stream surfaces
	// to the Run loop alongside heartbeat failure so the whole machine reconnects.
	machinePingInterval = 15 * time.Second
)

type ConnState int

const (
	StateDisconnected ConnState = iota
	StateConnecting
	StateConnected
	StateDisconnecting
)

// MachineClient is the machine app's manager client. One instance per machine
// process; it owns the machine auth credentials, the shared local daemon, the
// MachineChannel control stream, and the set of per-agent runners.
type MachineClient struct {
	managerURL     string
	httpClient     *http.Client
	streamClient   *http.Client
	machineClient  v1connect.MachineServiceClient
	credential     *credential.Manager
	machineID      string // bare uuid, parsed from the registration token
	binaryDir      string
	daemon         *daemonsrv.Server
	backoff        *ExponentialBackoff
	machineVersion string

	mu          sync.RWMutex
	connState   ConnState
	sessionID   string
	serverNonce string
	accessToken string

	// discoveredProviders is the cached result of probing the host for installed
	// LLM agent providers + their models. Reported in MachineInfo on connect and
	// re-probed on demand via the MachineChannel DiscoverProviders control
	// message. Machine-scoped: every hosted agent selects from this list.
	discoveredProviders []provider.Discovered
	discoveredAt        time.Time

	// runners is the live set of per-agent drain loops, keyed by bare agent id.
	// The MachineChannel receive pump mutates this on AgentAssignment /
	// RemoveAgent / ReloadAgentAssignment. Guarded by runnersMu.
	runnersMu sync.Mutex
	runners   map[string]*agentRunner

	// streamSendMu serializes sends on the MachineChannel bidi stream. The
	// ping loop, the graceful-disconnect notice, and the DiscoverProviders
	// reply (sent from the receive pump's goroutine) all call stream.Send;
	// connect's bidi client is not safe for concurrent Send, so they go
	// through sendStream.
	streamSendMu sync.Mutex
}

type ExponentialBackoff struct {
	baseWait time.Duration
	maxWait  time.Duration
	attempt  int
}

func NewExponentialBackoff(baseWait, maxWait time.Duration) *ExponentialBackoff {
	return &ExponentialBackoff{baseWait: baseWait, maxWait: maxWait, attempt: 0}
}

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

func (eb *ExponentialBackoff) Reset() {
	eb.attempt = 0
}

// New creates a MachineClient. token is the machine registration token printed
// by CreateMachine; its JWT sub carries the machine's resource id, which keys
// the on-disk refresh-token file (~/.laelia/machine-token-<id>) and the daemon
// socket dir (~/.laelia/<id>/).
func New(managerURL, token string, insecure bool, allowHTTP bool) (*MachineClient, error) {
	managerURL = strings.TrimRight(managerURL, "/")

	if strings.HasPrefix(managerURL, "http://") {
		if !allowHTTP {
			return nil, errors.New("plain HTTP connections are not allowed by default, use --allow-http flag or switch to https://")
		}
		slog.Warn("plain HTTP connection enabled, traffic will not be encrypted")
	}

	machineID, err := parseResourceIDFromBootstrapToken(token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse machine identity from registration token")
	}
	tokenDir := filepath.Join(os.Getenv("HOME"), ".laelia")
	tokenFile := filepath.Join(tokenDir, "machine-token-"+machineID)
	httpClient := &http.Client{Timeout: defaultConnectTimeout}

	// Separate HTTP client for the bidi streams (MachineChannel + each
	// AgentChannel): no global timeout (the streams are long-lived), but
	// explicit HTTP/2 support so gRPC bidi works through proxies and TLS
	// terminators.
	streamClient := &http.Client{}

	if strings.HasPrefix(managerURL, "https://") {
		// Each transport gets its own TLS config: http2.ConfigureTransport
		// (triggered by ForceAttemptHTTP2) appends "h2" to the shared
		// *tls.Config's NextProtos in place. Sharing one pointer between the
		// h1-only httpClient and the h2 streamClient would leak "h2" into the
		// unary client's ALPN, so new unary connections negotiate h2 with the
		// proxy but the h1-only transport can't speak it — the proxy's HTTP/2
		// SETTINGS frame then parses as "malformed HTTP response" and corrupts
		// the connection pool under concurrent load (e.g. one message fanned
		// out to many agents at once). Clone() keeps the two configs
		// independent; enabling h2 on both lets ALPN pick h2 either way.
		tlsCfg := &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: insecure,
		}
		httpClient.Transport = &http.Transport{
			TLSClientConfig:   tlsCfg.Clone(),
			ForceAttemptHTTP2: true,
		}
		streamClient.Transport = &http.Transport{
			TLSClientConfig:       tlsCfg.Clone(),
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 60 * time.Second,
		}
	}

	binaryDir := ""
	if exe, err := os.Executable(); err == nil {
		binaryDir = filepath.Dir(exe)
	}

	return &MachineClient{
		managerURL:     managerURL,
		httpClient:     httpClient,
		streamClient:   streamClient,
		machineClient:  v1connect.NewMachineServiceClient(httpClient, managerURL),
		credential:     credential.New(tokenFile, token),
		machineID:      machineID,
		binaryDir:      binaryDir,
		backoff:        NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		runners:        make(map[string]*agentRunner),
		machineVersion: "0.2.0",
	}, nil
}

// Connect authenticates the machine. The refresh token is the machine's
// durable reconnection credential: the registration (bootstrap) token is
// single-use and consumed on the first successful ConnectMachine, so once a
// refresh token exists we reconnect through it. On success it stores the new
// credentials and spawns a runner for every agent in the response's
// assigned_agents list.
func (c *MachineClient) Connect(ctx context.Context, info *v1pb.MachineInfo) error {
	c.mu.Lock()
	c.connState = StateConnecting
	c.mu.Unlock()

	fingerprint := computeFingerprint(info)

	// Once a refresh token has been persisted, reconnect through it: the
	// single-use registration token is already consumed by this point.
	refreshToken := c.credential.LoadRefreshToken()
	if refreshToken != "" {
		err := c.connectViaRefresh(ctx, info, fingerprint, refreshToken)
		if err == nil {
			return nil
		}
		if !isPermanentAuthFailure(err) {
			// Transient (network/server) failure: keep the refresh token and let
			// the Run loop back off and retry. Falling back to the (consumed)
			// registration token would only yield "registration token is not
			// active" — a permanent-looking failure that would stop the machine
			// on a mere blip (e.g. a manager restart).
			c.mu.Lock()
			c.connState = StateDisconnected
			c.mu.Unlock()
			return err
		}
		// Permanent auth failure: the refresh token is dead — expired, or its
		// family was revoked / its version was bumped because an admin rotated
		// the token. Drop the stale refresh token and fall back to the
		// registration token the app was launched with. After a rotation that
		// is a fresh, unused bootstrap; without rotation it is the already
		// consumed bootstrap, so the registration path fails too and the Run
		// loop bails (admin must rotate) — which is the correct outcome.
		slog.Info("machine refresh token rejected; falling back to registration token", "error", err)
		c.credential.DeleteRefreshToken()
	}

	// First-ever connect (no refresh token) or recovery after a rotation: use
	// the registration token. This is the only path that consumes it.
	return c.connectViaRegistration(ctx, info, fingerprint, c.credential.BootstrapToken())
}

// connectViaRefresh reconnects using the persisted refresh token. The refresh
// token is a durable, multi-use reconnection credential: RefreshMachineToken
// reuses it across reconnects and only mints a replacement when it is near
// expiry (rolling renewal) — it does NOT consume it on every reconnect. So a
// lost refresh response (e.g. a manager hard-killed mid-request) is safely
// retryable: the same token is presented again and the server does not treat
// the retry as theft. Only when the server returns a non-empty RefreshToken
// (a rolling renewal) do we persist the replacement; otherwise we keep the
// existing token. The access token returned by the refresh response is the
// machine's bearer credential for the control stream + heartbeat; ConnectMachine
// on this path returns no access token (it only mints on the bootstrap path),
// so applyConnectResponse keeps the refresh-minted one.
func (c *MachineClient) connectViaRefresh(ctx context.Context, info *v1pb.MachineInfo, fingerprint, refreshToken string) error {
	refreshResp, err := c.refreshToken(ctx, refreshToken, fingerprint)
	if err != nil {
		c.mu.Lock()
		c.connState = StateDisconnected
		c.mu.Unlock()
		return errors.Wrap(err, "failed to refresh machine token")
	}
	c.mu.Lock()
	c.accessToken = refreshResp.AccessToken
	c.mu.Unlock()
	// Persist a replacement only when the server actually minted one (rolling
	// renewal near expiry). On the common reconnect the server reuses the same
	// refresh token and returns "" — saving that would wipe the durable
	// credential (the bug that made every manager restart unrecoverable).
	if refreshResp.RefreshToken != "" {
		c.credential.SaveRefreshToken(refreshResp.RefreshToken)
	}

	resp, err := c.connectWithAccessToken(ctx, info, fingerprint)
	if err != nil {
		c.mu.Lock()
		c.connState = StateDisconnected
		c.mu.Unlock()
		return errors.Wrap(err, "failed to connect with refreshed access token")
	}
	c.applyConnectResponse(resp)
	slog.Info("connected to manager via refresh token", "agents", len(resp.AssignedAgents))
	c.spawnAssignedAgents(ctx, resp.AssignedAgents)
	return nil
}

// connectViaRegistration performs the first-ever connect with the single-use
// registration token. ConnectMachine mints the initial access + refresh tokens
// (the refresh token is persisted by connectWithRegistrationToken).
func (c *MachineClient) connectViaRegistration(ctx context.Context, info *v1pb.MachineInfo, fingerprint, registrationToken string) error {
	resp, err := c.connectWithRegistrationToken(ctx, registrationToken, info, fingerprint)
	if err != nil {
		c.mu.Lock()
		c.connState = StateDisconnected
		c.mu.Unlock()
		return errors.Wrapf(err, "failed to connect to manager")
	}
	c.applyConnectResponse(resp)
	slog.Info("connected to manager via registration token", "agents", len(resp.AssignedAgents))
	c.spawnAssignedAgents(ctx, resp.AssignedAgents)
	return nil
}

// applyConnectResponse records the session from a successful ConnectMachine.
// The access token is only overwritten when ConnectMachine actually mints one
// (the bootstrap path); on the reconnect path ConnectMachine returns no access
// token, so we keep the refresh-minted token already stored by the caller. The
// refresh token is persisted by the caller via the credential manager.
func (c *MachineClient) applyConnectResponse(resp *v1pb.ConnectMachineResponse) {
	c.mu.Lock()
	c.connState = StateConnected
	c.sessionID = resp.SessionId
	if resp.AccessToken != "" {
		c.accessToken = resp.AccessToken
	}
	c.mu.Unlock()
	c.backoff.Reset()
}

// isPermanentAuthFailure reports whether err means the machine's credentials
// are permanently rejected and retrying cannot help. This is only reached with
// a refresh-path error (the durable credential) or a first-connect registration
// error: a revoked/rotated token family, a consumed registration token with no
// refresh token, a token-version mismatch, or a deleted machine. Transient
// network or server errors (e.g. 502 while the manager restarts) return false
// so the normal backoff retry continues and the machine auto-reconnects.
func isPermanentAuthFailure(err error) bool {
	var ce *connect.Error
	if !errors.As(err, &ce) {
		return false
	}
	switch ce.Code() {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied:
		return true
	}
	return false
}

func (c *MachineClient) connectWithRegistrationToken(ctx context.Context, registrationToken string, info *v1pb.MachineInfo, fingerprint string) (*v1pb.ConnectMachineResponse, error) {
	req := connect.NewRequest(&v1pb.ConnectMachineRequest{
		RegistrationToken: registrationToken,
		Info:              info,
		Fingerprint:       fingerprint,
	})
	resp, err := c.machineClient.ConnectMachine(ctx, req)
	if err != nil {
		return nil, err
	}
	// ConnectMachine mints a refresh token only on the bootstrap (registration)
	// path; on the access-token reconnect path it returns "". Persisting that
	// empty value would overwrite the durable refresh token on disk and make
	// the next reconnect unrecoverable — so only persist a non-empty token.
	if resp.Msg.RefreshToken != "" {
		c.credential.SaveRefreshToken(resp.Msg.RefreshToken)
	}
	return resp.Msg, nil
}

func (c *MachineClient) connectWithAccessToken(ctx context.Context, info *v1pb.MachineInfo, fingerprint string) (*v1pb.ConnectMachineResponse, error) {
	c.mu.RLock()
	token := c.accessToken
	c.mu.RUnlock()

	req := connect.NewRequest(&v1pb.ConnectMachineRequest{
		Info:        info,
		Fingerprint: fingerprint,
	})
	req.Header().Set("Authorization", "Bearer "+token)

	resp, err := c.machineClient.ConnectMachine(ctx, req)
	if err != nil {
		return nil, err
	}
	// ConnectMachine mints a refresh token only on the bootstrap (registration)
	// path; on the access-token reconnect path it returns "". Persisting that
	// empty value would overwrite the durable refresh token on disk and make
	// the next reconnect unrecoverable — so only persist a non-empty token.
	if resp.Msg.RefreshToken != "" {
		c.credential.SaveRefreshToken(resp.Msg.RefreshToken)
	}
	return resp.Msg, nil
}

func (c *MachineClient) refreshToken(ctx context.Context, refreshToken, fingerprint string) (*v1pb.RefreshMachineTokenResponse, error) {
	req := connect.NewRequest(&v1pb.RefreshMachineTokenRequest{
		RefreshToken: refreshToken,
		Fingerprint:  fingerprint,
	})
	resp, err := c.machineClient.RefreshMachineToken(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *MachineClient) Heartbeat(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.sessionID
	nonce := c.serverNonce
	token := c.accessToken
	c.mu.RUnlock()

	req := connect.NewRequest(&v1pb.MachineHeartbeatRequest{
		SessionId:     sessionID,
		PreviousNonce: nonce,
	})
	req.Header().Set("Authorization", "Bearer "+token)

	hbCtx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
	defer cancel()
	resp, err := c.machineClient.MachineHeartbeat(hbCtx, req)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.serverNonce = resp.Msg.NextNonce
	if resp.Msg.AccessToken != "" {
		c.accessToken = resp.Msg.AccessToken
	}
	c.mu.Unlock()
	return nil
}

func (c *MachineClient) Disconnect(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.sessionID
	token := c.accessToken
	c.mu.RUnlock()

	req := connect.NewRequest(&v1pb.MachineDisconnectRequest{
		SessionId: sessionID,
		Reason:    "shutdown",
	})
	req.Header().Set("Authorization", "Bearer "+token)

	_, err := c.machineClient.MachineDisconnect(ctx, req)

	// Keep the persisted refresh token: the registration token is single-use
	// (consumed on first connect), so the refresh token is the only credential
	// that can reconnect after a restart or transient failure. Permanent
	// decommission is handled server-side via Rotate/RevokeMachineToken, which
	// revoke the family and bump the token version.
	c.mu.Lock()
	c.connState = StateDisconnected
	c.mu.Unlock()
	return err
}

func (c *MachineClient) Hello(ctx context.Context) (*v1pb.HelloResponse, error) {
	// Hello is on AgentService; reuse a throwaway client to probe the manager.
	agentClient := v1connect.NewAgentServiceClient(c.httpClient, c.managerURL)
	req := connect.NewRequest(&v1pb.HelloRequest{})
	resp, err := agentClient.Hello(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *MachineClient) State() ConnState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connState
}

// Run is the machine app's main loop: start the shared daemon, probe providers,
// then repeatedly connect → open the MachineChannel control stream + heartbeat
// → tear down and reconnect on death. It returns only when ctx is cancelled.
func (c *MachineClient) Run(ctx context.Context) error {
	slog.Info("connecting to manager", "url", c.managerURL, "machineID", c.machineID)

	daemonSrv, err := daemonsrv.New(c.managerURL, c.machineID, func() string {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.accessToken
	}, c.httpClient)
	if err != nil {
		return errors.Wrap(err, "failed to create local daemon server")
	}
	if err := daemonSrv.Start(); err != nil {
		return errors.Wrap(err, "failed to start local daemon server")
	}
	c.daemon = daemonSrv
	defer daemonSrv.Stop()

	// Probe the host once for installed LLM agent providers + models so the
	// first MachineInfo report carries them. On-demand re-probing is driven by
	// the MachineChannel DiscoverProviders control message.
	discoverCtx, discoverCancel := context.WithTimeout(ctx, 2*time.Minute)
	c.refreshProviders(discoverCtx)
	discoverCancel()

	for {
		select {
		case <-ctx.Done():
			c.shutdown()
			return nil
		default:
		}

		// Recompute MachineInfo each iteration so Capability reflects the latest
		// provider probe.
		info := c.collectMachineInfo()

		if err := c.Connect(ctx, info); err != nil {
			// A permanent credential failure — the refresh token family was
			// revoked/rotated, the token version mismatched, the machine was
			// deleted, or the single-use registration token is consumed with no
			// refresh token to fall back on — will never succeed by retrying.
			// The admin must rotate the token and restart the machine app with
			// the new registration token. A transient failure (e.g. 502 while the
			// manager restarts) is not permanent: back off and retry so the
			// machine auto-reconnects once the manager is back.
			if isPermanentAuthFailure(err) {
				slog.Error("machine credentials are no longer valid; an admin must rotate the token and restart with the new registration token", "error", err)
				return errors.Wrap(err, "machine credentials rejected by manager; stopping (rotate token and restart)")
			}
			slog.Error("connect failed", "error", err)
			if err := c.backoff.Wait(ctx); err != nil {
				return err
			}
			continue
		}

		ctrlCtx, ctrlCancel := context.WithCancel(ctx)
		streamErr := make(chan error, 1)
		go func() {
			if err := c.runControlStream(ctrlCtx, daemonSrv); err != nil {
				streamErr <- err
			}
		}()

		ticker := time.NewTicker(defaultHeartbeatInterval)

	heartbeatLoop:
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				ctrlCancel()
				c.shutdown()
				return nil
			case err := <-streamErr:
				slog.Warn("machine control stream died while heartbeat healthy, reconnecting", "error", err)
				ticker.Stop()
				ctrlCancel()
				c.teardownRunners()
				c.markDisconnected()
				c.disconnectWithTimeout()
				if err := c.backoff.Wait(ctx); err != nil {
					return err
				}
				break heartbeatLoop
			case <-ticker.C:
				if err := c.Heartbeat(ctx); err != nil {
					slog.Error("heartbeat failed", "error", err)
					ticker.Stop()
					ctrlCancel()
					c.teardownRunners()
					c.markDisconnected()
					break heartbeatLoop
				}
				slog.Debug("heartbeat sent")
			}
		}
	}
}

// shutdown stops runners and notifies the manager of a graceful disconnect.
func (c *MachineClient) shutdown() {
	slog.Info("machine stopping")
	c.teardownRunners()
	c.markDisconnected()
	disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = c.Disconnect(disconnectCtx)
	cancel()
}

func (c *MachineClient) markDisconnected() {
	c.mu.Lock()
	c.connState = StateDisconnected
	c.mu.Unlock()
}

func (c *MachineClient) disconnectWithTimeout() {
	disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = c.Disconnect(disconnectCtx)
	cancel()
}

func computeFingerprint(info *v1pb.MachineInfo) string {
	data := fmt.Sprintf("%s:%s:%s", info.Hostname, info.Os, info.Arch)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])[:16]
}

func (c *MachineClient) collectMachineInfo() *v1pb.MachineInfo {
	hostname, _ := os.Hostname()
	c.mu.RLock()
	providers := c.discoveredProviders
	discoveredAt := c.discoveredAt
	c.mu.RUnlock()

	return &v1pb.MachineInfo{
		Hostname:           hostname,
		Os:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		Version:            c.machineVersion,
		Ip:                 getOutboundIP(),
		AvailableProviders: discoveredToProto(providers, discoveredAt),
	}
}

// refreshProviders probes the host for installed LLM agent providers and their
// models, caching the result so subsequent MachineInfo reports carry it without
// re-spawning. Safe to call repeatedly; the cache is replaced atomically. The
// returned slice lets the MachineChannel reply to DiscoverProviders with the
// fresh list in one probe.
func (c *MachineClient) refreshProviders(ctx context.Context) []provider.Discovered {
	discovered := provider.Default().Discover(ctx)
	c.mu.Lock()
	c.discoveredProviders = discovered
	c.discoveredAt = time.Now()
	c.mu.Unlock()
	if len(discovered) > 0 {
		ids := make([]string, 0, len(discovered))
		for _, d := range discovered {
			ids = append(ids, d.ProviderID)
		}
		slog.Info("discovered LLM agent providers", "providers", ids)
	} else {
		slog.Info("no LLM agent providers discovered on host")
	}
	return discovered
}

// discoveredToProto converts the internal discovery result to the proto form
// reported in MachineInfo.available_providers.
func discoveredToProto(in []provider.Discovered, at time.Time) []*v1pb.AgentProviderInfo {
	if len(in) == 0 {
		return nil
	}
	var ts *timestamppb.Timestamp
	if !at.IsZero() {
		ts = timestamppb.New(at)
	}
	out := make([]*v1pb.AgentProviderInfo, 0, len(in))
	for _, d := range in {
		models := make([]*v1pb.AgentModelOption, 0, len(d.Models))
		for _, m := range d.Models {
			models = append(models, &v1pb.AgentModelOption{
				Value:       m.Value,
				Name:        m.Name,
				Description: m.Description,
			})
		}
		out = append(out, &v1pb.AgentProviderInfo{
			ProviderId:                d.ProviderID,
			DisplayName:               d.DisplayName,
			Version:                   d.Version,
			ExecutablePath:            d.ExecutablePath,
			Models:                    models,
			SupportsModelConfigOption: d.SupportsModelConfigOption,
			DetectedAt:                ts,
		})
	}
	return out
}

func getOutboundIP() string {
	// Best-effort: bound the dial so a missing default route cannot stall
	// startup. The UDP "dial" only selects a source address; no packets flow.
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", 5*time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()
	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return localAddr.IP.String()
}

func parseResourceIDFromBootstrapToken(tokenStr string) (string, error) {
	parser := jwt.Parser{}
	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(tokenStr, claims)
	if err != nil {
		return "", errors.Wrap(err, "invalid registration token format")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", errors.New("registration token missing sub claim")
	}
	return sub, nil
}

// bareAgentID strips the agents/ prefix from a full agent resource name,
// returning the bare uuid. A value that is already bare is returned unchanged.
func bareAgentID(agentName string) string {
	if i := strings.LastIndex(agentName, "/"); i >= 0 {
		return agentName[i+1:]
	}
	return agentName
}
