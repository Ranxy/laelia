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
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/credential"
	"github.com/Ranxy/laelia/backend/agent/executor"
	mcpsrv "github.com/Ranxy/laelia/backend/agent/mcp"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultConnectTimeout    = 30 * time.Second
	defaultRetryMaxWait      = 1 * time.Minute
	defaultRetryBaseWait     = 2 * time.Second
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
	mcpServer   *mcpsrv.Server
	agentName   string
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

func New(managerURL, token string, insecure bool, allowHTTP bool, acpConfigPath, agentName string, acpConfigServer bool) (*Client, error) {
	managerURL = strings.TrimRight(managerURL, "/")

	var acpConfig *executor.ACPConfig
	var err error

	if !acpConfigServer {
		acpConfig, err = executor.LoadACPConfigFromFile(acpConfigPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Wrap(err, "failed to load ACP config")
		}
	}
	// acpConfigServer=true: acpConfig stays nil, will be populated from server connect response

	if strings.HasPrefix(managerURL, "http://") {
		if !allowHTTP {
			return nil, errors.New("plain HTTP connections are not allowed by default, use --allow-http flag or switch to https://")
		}
		slog.Warn("plain HTTP connection enabled, traffic will not be encrypted")
	}

	tokenDir := filepath.Join(os.Getenv("HOME"), ".laelia")
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
		credential:   credential.New(filepath.Join(tokenDir, "agent-token"), token),
		backoff:      NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		acpConfig:    acpConfig,
		agentName:    agentName,
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
	if connectResp.AcpConfigYaml == "" {
		return
	}
	cfg, err := executor.LoadACPConfigFromYAML(connectResp.AcpConfigYaml)
	if err != nil {
		slog.Warn("failed to load server-provided ACP config", "error", err)
		return
	}
	c.mu.Lock()
	c.acpConfig = cfg
	c.mu.Unlock()
	slog.Info("loaded ACP config from server (overrides local)")
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

	resp, err := c.client.AgentHeartbeat(ctx, req)
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
	c.credential.DeleteRefreshToken()

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
	info := collectAgentInfo(c.acpConfig)
	slog.Info("connecting to manager", "url", c.managerURL)

	mcpSrv, err := mcpsrv.New(c.managerURL, c.agentName, func() string {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.accessToken
	}, c.httpClient)
	if err != nil {
		return errors.Wrap(err, "failed to create MCP server")
	}
	if err := mcpSrv.Start(); err != nil {
		return errors.Wrap(err, "failed to start MCP server")
	}
	c.mcpServer = mcpSrv
	defer mcpSrv.Stop()

	c.cmdStream = newCommandStream(c.streamClient, c.managerURL, c.acpConfig, mcpSrv.Port(), c.agentName)
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

		if err := c.Connect(ctx, info); err != nil {
			slog.Error("connect failed", "error", err)
			if err := c.backoff.Wait(ctx); err != nil {
				return err
			}
			continue
		}

		cmdCtx, cmdCancel := context.WithCancel(ctx)
		go func() {
			if err := c.cmdStream.Start(cmdCtx); err != nil {
				slog.Error("command stream stopped", "error", err)
			}
		}()

		ticker := time.NewTicker(defaultHeartbeatInterval)

	heartbeatLoop:
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				cmdCancel()
				disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.Disconnect(disconnectCtx)
				cancel()
				return nil
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

func collectAgentInfo(acpConfig *executor.ACPConfig) *v1pb.AgentInfo {
	hostname, _ := os.Hostname()
	return &v1pb.AgentInfo{
		Hostname:   hostname,
		Os:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Version:    "0.2.0",
		Ip:         getOutboundIP(),
		Capability: acpConfig.Capability(),
	}
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
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
