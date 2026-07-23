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
		tlsCfg := &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: insecure,
		}
		httpClient.Transport = &http.Transport{TLSClientConfig: tlsCfg}
		streamClient.Transport = &http.Transport{
			TLSClientConfig:       tlsCfg,
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

// Connect authenticates the machine. It first tries the persisted refresh
// token (machine access token), falling back to the single-use registration
// token. On success it stores the new credentials and spawns a runner for
// every agent in the response's assigned_agents list.
func (c *MachineClient) Connect(ctx context.Context, info *v1pb.MachineInfo) error {
	c.mu.Lock()
	c.connState = StateConnecting
	c.mu.Unlock()

	fingerprint := computeFingerprint(info)

	refreshToken := c.credential.LoadRefreshToken()
	if refreshToken != "" {
		refreshResp, err := c.refreshToken(ctx, refreshToken, fingerprint)
		if err != nil {
			slog.Warn("refresh token failed, falling back to registration token", "error", err)
		} else {
			c.mu.Lock()
			c.accessToken = refreshResp.AccessToken
			c.mu.Unlock()
			c.credential.SaveRefreshToken(refreshResp.RefreshToken)

			resp, err := c.connectWithAccessToken(ctx, info, fingerprint)
			if err != nil {
				slog.Warn("connect with refreshed token failed, falling back", "error", err)
			} else {
				c.applyConnectResponse(resp)
				slog.Info("connected to manager via refresh token", "agents", len(resp.AssignedAgents))
				c.spawnAssignedAgents(ctx, resp.AssignedAgents)
				return nil
			}
		}
	}

	registrationToken := c.credential.BootstrapToken()
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

// applyConnectResponse records the session + access token from a successful
// ConnectMachine. The refresh token is persisted by the caller via the
// credential manager.
func (c *MachineClient) applyConnectResponse(resp *v1pb.ConnectMachineResponse) {
	c.mu.Lock()
	c.connState = StateConnected
	c.sessionID = resp.SessionId
	c.accessToken = resp.AccessToken
	c.mu.Unlock()
	c.backoff.Reset()
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
	c.credential.SaveRefreshToken(resp.Msg.RefreshToken)
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
	c.credential.SaveRefreshToken(resp.Msg.RefreshToken)
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
