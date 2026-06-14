package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/credential"
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

type AgentInfo struct {
	AgentType string            `json:"agent_type,omitempty"`
	Hostname  string            `json:"hostname,omitempty"`
	Os        string            `json:"os,omitempty"`
	Arch      string            `json:"arch,omitempty"`
	IP        string            `json:"ip,omitempty"`
	Version   string            `json:"version,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

type ConnectResponse struct {
	AccessToken          string `json:"accessToken"`
	RefreshToken         string `json:"refreshToken"`
	SessionID            string `json:"sessionId"`
	NextNonce            string `json:"nextNonce"`
	AccessTokenExpiresAt int64  `json:"accessTokenExpiresAt"`
}

type HeartbeatResponse struct {
	NextNonce            string `json:"nextNonce"`
	NextHeartbeatAt      int64  `json:"nextHeartbeatAt"`
	AccessToken          string `json:"accessToken,omitempty"`
	AccessTokenExpiresAt int64  `json:"accessTokenExpiresAt,omitempty"`
}

type RefreshResponse struct {
	AccessToken          string `json:"accessToken"`
	RefreshToken         string `json:"refreshToken"`
	AccessTokenExpiresAt int64  `json:"accessTokenExpiresAt"`
}

type HelloResponse struct {
	CurrentTime   int64  `json:"currentTime"`
	ServerVersion string `json:"serverVersion"`
}

type Client struct {
	managerURL string
	httpClient *http.Client
	credential *credential.Manager
	mu         sync.RWMutex

	connState   ConnState
	sessionID   string
	serverNonce string
	accessToken string
	backoff     *ExponentialBackoff
	insecure    bool
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

func New(managerURL, token string, insecure bool) *Client {
	managerURL = strings.TrimRight(managerURL, "/")
	tokenDir := filepath.Join(os.Getenv("HOME"), ".laelia")
	httpClient := &http.Client{Timeout: defaultConnectTimeout}

	if strings.HasPrefix(managerURL, "https://") {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS13,
				InsecureSkipVerify: insecure,
			},
		}
	}

	return &Client{
		managerURL: managerURL,
		httpClient: httpClient,
		credential: credential.New(filepath.Join(tokenDir, "agent-token"), token),
		backoff:    NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		insecure:   insecure,
	}
}

func (c *Client) Connect(ctx context.Context, info *AgentInfo) error {
	c.mu.Lock()
	c.connState = StateConnecting
	c.mu.Unlock()

	fingerprint := computeFingerprint(info)

	refreshToken := c.credential.LoadRefreshToken()
	if refreshToken != "" {
		resp, err := c.refreshToken(ctx, refreshToken, fingerprint)
		if err != nil {
			slog.Warn("refresh token failed, falling back to bootstrap token", "error", err)
		} else {
			c.accessToken = resp.AccessToken
			c.credential.SaveRefreshToken(resp.RefreshToken)

			connectResp, err := c.connectWithAccessToken(ctx, info, fingerprint)
			if err != nil {
				slog.Warn("connect with refreshed token failed, falling back", "error", err)
			} else {
				c.mu.Lock()
				c.connState = StateConnected
				c.sessionID = connectResp.SessionID
				c.serverNonce = connectResp.NextNonce
				c.mu.Unlock()
				c.backoff.Reset()
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
	c.sessionID = resp.SessionID
	c.serverNonce = resp.NextNonce
	c.mu.Unlock()
	c.backoff.Reset()
	slog.Info("connected to manager via bootstrap token")
	return nil
}

func (c *Client) Heartbeat(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.sessionID
	nonce := c.serverNonce
	c.mu.RUnlock()

	reqBody := map[string]any{
		"session_id":     sessionID,
		"previous_nonce": nonce,
	}

	var heartbeatResp HeartbeatResponse
	if err := c.doPostWithAuth(ctx, "/v1/agents:heartbeat", reqBody, &heartbeatResp); err != nil {
		return err
	}

	c.mu.Lock()
	c.serverNonce = heartbeatResp.NextNonce
	if heartbeatResp.AccessToken != "" {
		c.accessToken = heartbeatResp.AccessToken
	}
	c.mu.Unlock()

	return nil
}

func (c *Client) Disconnect(ctx context.Context) error {
	c.mu.RLock()
	sessionID := c.sessionID
	c.mu.RUnlock()

	reqBody := map[string]any{
		"session_id": sessionID,
		"reason":     "shutdown",
	}

	err := c.doPostWithAuth(ctx, "/v1/agents:disconnect", reqBody, nil)
	c.credential.DeleteRefreshToken()

	c.mu.Lock()
	c.connState = StateDisconnected
	c.mu.Unlock()

	return err
}

func (c *Client) Hello(ctx context.Context) (*HelloResponse, error) {
	var resp HelloResponse
	if err := c.doPostNoAuth(ctx, "/v1/agent/hello", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) State() ConnState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connState
}

func (c *Client) Run(ctx context.Context) error {
	info := collectAgentInfo()
	slog.Info("connecting to manager", "url", c.managerURL)

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

		ticker := time.NewTicker(defaultHeartbeatInterval)

	heartbeatLoop:
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
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
					break heartbeatLoop
				}
				slog.Debug("heartbeat sent")
			}
		}
	}
}

func (c *Client) connectWithBootstrapToken(ctx context.Context, bootstrapToken string, info *AgentInfo, fingerprint string) (*ConnectResponse, error) {
	reqBody := map[string]any{
		"bootstrap_token": bootstrapToken,
		"info":            info,
		"fingerprint":     fingerprint,
	}

	var resp ConnectResponse
	if err := c.doPostNoAuth(ctx, "/v1/agents:connect", reqBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) connectWithAccessToken(ctx context.Context, info *AgentInfo, fingerprint string) (*ConnectResponse, error) {
	reqBody := map[string]any{
		"info":        info,
		"fingerprint": fingerprint,
	}

	var resp ConnectResponse
	if err := c.doPostWithAuth(ctx, "/v1/agents:connect", reqBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) refreshToken(ctx context.Context, refreshToken string, fingerprint string) (*RefreshResponse, error) {
	reqBody := map[string]any{
		"refresh_token": refreshToken,
		"fingerprint":   fingerprint,
	}

	var resp RefreshResponse
	if err := c.doPostNoAuth(ctx, "/v1/agents:refreshToken", reqBody, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) doPostWithAuth(ctx context.Context, path string, body any, result any) error {
	c.mu.RLock()
	token := c.accessToken
	c.mu.RUnlock()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return errors.Wrapf(err, "failed to encode request")
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.managerURL+path, &buf)
	if err != nil {
		return errors.Wrapf(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("unauthorized: token may be expired or invalid")
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return errors.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return errors.Wrapf(err, "failed to decode response")
		}
	}
	return nil
}

func (c *Client) doPostNoAuth(ctx context.Context, path string, body any, result any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return errors.Wrapf(err, "failed to encode request")
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.managerURL+path, &buf)
	if err != nil {
		return errors.Wrapf(err, "failed to create request")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return errors.Errorf("unexpected status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return errors.Wrapf(err, "failed to decode response")
		}
	}
	return nil
}

func computeFingerprint(info *AgentInfo) string {
	data := fmt.Sprintf("%s:%s:%s", info.Hostname, info.Os, info.Arch)
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])[:16]
}

func collectAgentInfo() *AgentInfo {
	hostname, _ := os.Hostname()
	return &AgentInfo{
		Hostname: hostname,
		Os:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  "0.2.0",
		IP:       getOutboundIP(),
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
