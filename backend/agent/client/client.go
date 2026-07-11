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

	"github.com/Ranxy/laelia/backend/agent/chattools"
	"github.com/Ranxy/laelia/backend/agent/credential"
	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultConnectTimeout    = 30 * time.Second
	defaultRetryMaxWait      = 1 * time.Minute
	defaultRetryBaseWait     = 2 * time.Second
	// heartbeatTimeout bounds a single Heartbeat RPC. The agent's liveness
	// must not stall on a manager that accepts the connection but never
	// replies; a per-call timeout makes heartbeat failure (not the peer's
	// tcp keepalive) the detection signal.
	heartbeatTimeout = 10 * time.Second
)

type ConnState int

const (
	StateDisconnected ConnState = iota
	StateConnecting
	StateConnected
	StateDisconnecting
)

type Client struct {
	managerURL   string
	httpClient   *http.Client
	streamClient *http.Client
	client       v1connect.AgentServiceClient
	credential   *credential.Manager
	mu           sync.RWMutex

	connState   ConnState
	sessionID   string
	serverNonce string
	accessToken string
	backoff     *ExponentialBackoff
	cmdStream   *commandStream
	acpConfig   *executor.ACPConfig
	daemon      *daemonsrv.Server
	agentName   string
	// discoveredProviders is the cached result of probing the host for
	// installed LLM agent providers + their models. Refreshed once at startup
	// and on demand via the bidi DiscoverProviders control message.
	discoveredProviders []provider.Discovered
	discoveredAt        time.Time
	// resourceID is the agent's stable server-assigned UUID, parsed from the
	// bootstrap token. It keys the per-agent working dir and local state file.
	resourceID string
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

func New(managerURL, token string, insecure bool, allowHTTP bool, agentName string) (*Client, error) {
	managerURL = strings.TrimRight(managerURL, "/")

	// ACP config is always server-provided on connect (handleServerACPConfig).
	var acpConfig *executor.ACPConfig

	if strings.HasPrefix(managerURL, "http://") {
		if !allowHTTP {
			return nil, errors.New("plain HTTP connections are not allowed by default, use --allow-http flag or switch to https://")
		}
		slog.Warn("plain HTTP connection enabled, traffic will not be encrypted")
	}

	tokenDir := filepath.Join(os.Getenv("HOME"), ".laelia")
	resourceID, err := parseResourceIDFromBootstrapToken(token)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse agent identity from bootstrap token")
	}
	tokenFile := filepath.Join(tokenDir, "agent-token-"+resourceID)
	httpClient := &http.Client{Timeout: defaultConnectTimeout}

	// Separate HTTP client for the bidi command stream: no global timeout
	// (the stream is long-lived), but explicit HTTP/2 support to ensure
	// gRPC bidi streams work through proxies and TLS terminators.
	streamClient := &http.Client{}

	if strings.HasPrefix(managerURL, "https://") {
		tlsCfg := &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: insecure,
		}
		httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsCfg,
		}
		// Separate transport for the bidi command stream:
		// - ForceAttemptHTTP2 ensures gRPC bidi streams work through proxies
		// - ResponseHeaderTimeout bounds only the initial handshake, not the stream
		// - No http.Client.Timeout so the long-lived stream is not killed prematurely
		streamClient.Transport = &http.Transport{
			TLSClientConfig:       tlsCfg,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 60 * time.Second,
		}
	}

	client := v1connect.NewAgentServiceClient(httpClient, managerURL)

	return &Client{
		managerURL:   managerURL,
		httpClient:   httpClient,
		streamClient: streamClient,
		client:       client,
		credential:   credential.New(tokenFile, token),
		backoff:      NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		acpConfig:    acpConfig,
		agentName:    agentName,
		resourceID:   resourceID,
	}, nil
}

func (c *Client) Connect(ctx context.Context, info *v1pb.AgentInfo) error {
	c.mu.Lock()
	c.connState = StateConnecting
	c.mu.Unlock()

	fingerprint := computeFingerprint(info)

	refreshToken := c.credential.LoadRefreshToken()
	if refreshToken != "" {
		refreshResp, err := c.refreshToken(ctx, refreshToken, fingerprint)
		if err != nil {
			slog.Warn("refresh token failed, falling back to bootstrap token", "error", err)
		} else {
			c.accessToken = refreshResp.AccessToken
			c.credential.SaveRefreshToken(refreshResp.RefreshToken)

			connectResp, err := c.connectWithAccessToken(ctx, info, fingerprint)
			if err != nil {
				slog.Warn("connect with refreshed token failed, falling back", "error", err)
			} else {
				c.mu.Lock()
				c.connState = StateConnected
				c.sessionID = connectResp.SessionId
				c.serverNonce = connectResp.NextNonce
				c.mu.Unlock()
				c.backoff.Reset()
				c.handleServerACPConfig(connectResp)
				slog.Info("connected to manager via refresh token")
				return nil
			}
		}
	}

	bootstrapToken := c.credential.BootstrapToken()
	resp, err := c.connectWithBootstrapToken(ctx, bootstrapToken, info, fingerprint)
	if err != nil {
		c.mu.Lock()
		c.connState = StateDisconnected
		c.mu.Unlock()
		return errors.Wrapf(err, "failed to connect to manager")
	}

	c.accessToken = resp.AccessToken
	c.credential.SaveRefreshToken(resp.RefreshToken)
	c.mu.Lock()
	c.connState = StateConnected
	c.sessionID = resp.SessionId
	c.serverNonce = resp.NextNonce
	c.mu.Unlock()
	c.backoff.Reset()
	c.handleServerACPConfig(resp)
	slog.Info("connected to manager via bootstrap token")
	return nil
}

func (c *Client) handleServerACPConfig(connectResp *v1pb.ConnectAgentResponse) {
	cfg := executor.BuildACPConfig(connectResp.AcpConfig, c.resourceID)
	if cfg == nil {
		// Agent not configured yet (no executable). Stay inert until the admin
		// sets one via UpdateAgentACPConfig; the next connect will pick it up.
		c.mu.Lock()
		c.acpConfig = nil
		c.mu.Unlock()
		return
	}
	if err := os.MkdirAll(cfg.WorkingDir, 0o700); err != nil {
		slog.Warn("failed to create agent working dir", "dir", cfg.WorkingDir, "error", err)
		return
	}
	c.mu.Lock()
	c.acpConfig = cfg
	c.mu.Unlock()
	slog.Info("loaded ACP config from server", "workingDir", cfg.WorkingDir)
}

func (c *Client) Heartbeat(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.sessionID
	nonce := c.serverNonce
	token := c.accessToken
	c.mu.RUnlock()

	req := connect.NewRequest(&v1pb.AgentHeartbeatRequest{
		SessionId:     sessionID,
		PreviousNonce: nonce,
	})
	req.Header().Set("Authorization", "Bearer "+token)

	// Per-call timeout: a hung manager must not stall the heartbeat ticker
	// (and thus liveness detection) until the long-lived ctx is cancelled.
	hbCtx, cancel := context.WithTimeout(ctx, heartbeatTimeout)
	defer cancel()
	resp, err := c.client.AgentHeartbeat(hbCtx, req)
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

func (c *Client) Disconnect(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.sessionID
	token := c.accessToken
	c.mu.RUnlock()

	req := connect.NewRequest(&v1pb.AgentDisconnectRequest{
		SessionId: sessionID,
		Reason:    "shutdown",
	})
	req.Header().Set("Authorization", "Bearer "+token)

	_, err := c.client.AgentDisconnect(ctx, req)

	// Keep the persisted refresh token. The bootstrap token is single-use
	// (CONSUMED on the first successful connect), so the refresh token is the
	// only credential that can reconnect after a clean restart or a transient
	// stream/heartbeat failure. Wiping it here left the agent with nothing but
	// the already-consumed bootstrap token, so every reconnect failed with
	// "bootstrap token is not active". The on-disk token is always the latest
	// active one (SaveRefreshToken overwrites it on each rotation); permanent
	// decommission is handled server-side via RotateAgentToken/RevokeAgentToken,
	// which revoke the whole family and bump the token version, making any
	// lingering on-disk token useless.
	c.mu.Lock()
	c.connState = StateDisconnected
	c.mu.Unlock()

	return err
}

func (c *Client) Hello(ctx context.Context) (*v1pb.HelloResponse, error) {
	req := connect.NewRequest(&v1pb.HelloRequest{})
	resp, err := c.client.Hello(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *Client) State() ConnState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connState
}

func (c *Client) Run(ctx context.Context) error {
	slog.Info("connecting to manager", "url", c.managerURL)

	daemonSrv, err := daemonsrv.New(c.managerURL, c.agentName, c.resourceID, func() string {
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

	binaryDir := ""
	if exe, err := os.Executable(); err == nil {
		binaryDir = filepath.Dir(exe)
	}

	c.cmdStream = newCommandStream(c.streamClient, c.managerURL, daemonSrv.SocketPath(), daemonSrv.SessionToken(), binaryDir, c.agentName, c.resourceID)
	c.cmdStream.getToken = func() string {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.accessToken
	}
	c.cmdStream.getSessID = func() string {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.sessionID
	}
	c.cmdStream.getAcpConfig = func() *executor.ACPConfig {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.acpConfig
	}
	c.cmdStream.refreshProviders = func(ctx context.Context) []provider.Discovered {
		return c.refreshProviders(ctx)
	}
	c.cmdStream.buildTurnBatch = func(ctx context.Context) (string, error) {
		return chattools.BuildTurnBatch(ctx, daemonSrv.BatchDeps())
	}

	// Probe the host once for installed LLM agent providers + models so the
	// first AgentInfo report carries them. On-demand re-probing is driven by
	// the bidi DiscoverProviders control message (see command_stream).
	discoverCtx, discoverCancel := context.WithTimeout(ctx, 2*time.Minute)
	c.refreshProviders(discoverCtx)
	discoverCancel()

	for {
		select {
		case <-ctx.Done():
			slog.Info("agent stopping")
			disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = c.Disconnect(disconnectCtx)
			cancel()
			return nil
		default:
		}

		// Recompute AgentInfo each iteration so Capability reflects the latest
		// AcpConfig (the manager may update it on a reconnect via
		// handleServerACPConfig). Computing it once before the loop left
		// Capability stale for the agent's whole lifetime.
		info := c.collectAgentInfo()

		if err := c.Connect(ctx, info); err != nil {
			slog.Error("connect failed", "error", err)
			if err := c.backoff.Wait(ctx); err != nil {
				return err
			}
			continue
		}

		cmdCtx, cmdCancel := context.WithCancel(ctx)
		// Death fuse: the command stream is the authority on whether the agent
		// can actually receive work. The bidi stream and the heartbeat are
		// separate HTTP/2 streams, so a permanently dead command stream can
		// coexist with a healthy heartbeat — leaving the agent "Connected" but
		// deaf (never receiving BeginSession/Cancel/Permission). Start returns
		// its terminal error here (it no longer retries internally), and the
		// heartbeat loop watches streamErr to tear down and reconnect the whole
		// agent connection instead of going deaf.
		streamErr := make(chan error, 1)
		go func() {
			if err := c.cmdStream.Start(cmdCtx); err != nil {
				streamErr <- err
			}
		}()

		ticker := time.NewTicker(defaultHeartbeatInterval)

	heartbeatLoop:
		for {
			select {
			case <-ctx.Done():
				slog.Info("agent stopping")
				ticker.Stop()
				cmdCancel()
				disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.Disconnect(disconnectCtx)
				cancel()
				return nil
			case err := <-streamErr:
				slog.Warn("command stream died while heartbeat healthy, reconnecting", "error", err)
				c.mu.Lock()
				c.connState = StateDisconnected
				c.mu.Unlock()
				ticker.Stop()
				cmdCancel()
				disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.Disconnect(disconnectCtx)
				cancel()
				if err := c.backoff.Wait(ctx); err != nil {
					return err
				}
				break heartbeatLoop
			case <-ticker.C:
				if err := c.Heartbeat(ctx); err != nil {
					slog.Error("heartbeat failed", "error", err)
					c.mu.Lock()
					c.connState = StateDisconnected
					c.mu.Unlock()
					ticker.Stop()
					cmdCancel()
					break heartbeatLoop
				}
				slog.Debug("heartbeat sent")
			}
		}
	}
}

func (c *Client) connectWithBootstrapToken(ctx context.Context, bootstrapToken string, info *v1pb.AgentInfo, fingerprint string) (*v1pb.ConnectAgentResponse, error) {
	req := connect.NewRequest(&v1pb.ConnectAgentRequest{
		BootstrapToken: bootstrapToken,
		Info:           info,
		Fingerprint:    fingerprint,
	})
	resp, err := c.client.ConnectAgent(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *Client) connectWithAccessToken(ctx context.Context, info *v1pb.AgentInfo, fingerprint string) (*v1pb.ConnectAgentResponse, error) {
	c.mu.RLock()
	token := c.accessToken
	c.mu.RUnlock()

	req := connect.NewRequest(&v1pb.ConnectAgentRequest{
		Info:        info,
		Fingerprint: fingerprint,
	})
	req.Header().Set("Authorization", "Bearer "+token)

	resp, err := c.client.ConnectAgent(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func (c *Client) refreshToken(ctx context.Context, refreshToken string, fingerprint string) (*v1pb.RefreshAgentTokenResponse, error) {
	req := connect.NewRequest(&v1pb.RefreshAgentTokenRequest{
		RefreshToken: refreshToken,
		Fingerprint:  fingerprint,
	})
	resp, err := c.client.RefreshAgentToken(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func computeFingerprint(info *v1pb.AgentInfo) string {
	data := fmt.Sprintf("%s:%s:%s", info.Hostname, info.Os, info.Arch)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])[:16]
}

// acpConfigSnapshot returns the current ACP config under the read lock so
// AgentInfo can be recomputed from a consistent snapshot.
func (c *Client) acpConfigSnapshot() *executor.ACPConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.acpConfig
}

func (c *Client) collectAgentInfo() *v1pb.AgentInfo {
	hostname, _ := os.Hostname()
	c.mu.RLock()
	acpCfg := c.acpConfig
	providers := c.discoveredProviders
	discoveredAt := c.discoveredAt
	c.mu.RUnlock()

	info := &v1pb.AgentInfo{
		Hostname:           hostname,
		Os:                 runtime.GOOS,
		Arch:               runtime.GOARCH,
		Version:            "0.2.0",
		Ip:                 getOutboundIP(),
		Capability:         acpCfg.Capability(),
		AvailableProviders: discoveredToProto(providers, discoveredAt),
	}
	return info
}

// refreshProviders probes the host for installed LLM agent providers and their
// models, caching the result so subsequent AgentInfo reports carry it without
// re-spawning. Safe to call repeatedly; the cache is replaced atomically.
func (c *Client) refreshProviders(ctx context.Context) []provider.Discovered {
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
// reported in AgentInfo.available_providers.
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
		return "", errors.Wrap(err, "invalid bootstrap token format")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", errors.New("bootstrap token missing sub claim")
	}
	return sub, nil
}
