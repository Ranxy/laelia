package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"

	"connectrpc.com/connect"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
	"golang.org/x/time/rate"

	"github.com/Ranxy/laelia/backend/common"
)

type Config struct {
	GlobalRate     float64
	GlobalBurst    int
	ConnectRate    float64
	ConnectBurst   int
	HeartbeatRate  float64
	HeartbeatBurst int
	LoginRate      float64
	LoginBurst     int
	DeviceRate     float64
	DeviceBurst    int
	PublicRate     float64
	PublicBurst    int
	APIRate        float64
	APIBurst       int
	TrustProxy     bool
}

func DefaultConfig() Config {
	return Config{
		GlobalRate:     10000.0 / 60.0,
		GlobalBurst:    5000,
		ConnectRate:    10.0 / 60.0,
		ConnectBurst:   5,
		HeartbeatRate:  120.0 / 60.0,
		HeartbeatBurst: 10,
		LoginRate:      5.0 / 60.0,
		LoginBurst:     3,
		// Device flow: anonymous per-IP bucket for StartDeviceLogin /
		// PollDeviceLogin / GetDeviceLoginStatus. The CLI polls every 5s and
		// the approval page every 3s, so the bucket must be generous enough
		// that a handful of concurrent flows never trip 429s, while still
		// bounding a single source's fan-out.
		DeviceRate:  1.0,
		DeviceBurst: 30,
		// Public read endpoints (GetWorkspaceInfo, ListIdentityProviders) are
		// called by the unauthenticated login page on every render. They get
		// their own per-IP bucket so a few page refreshes don't exhaust the
		// tight "connect" bucket (burst 5) used for agent registration.
		PublicRate:  1.0,
		PublicBurst: 30,
		APIRate:     1000.0 / 60.0,
		APIBurst:    100,
		TrustProxy:  false,
	}
}

type RateLimiter struct {
	cfg              Config
	globalLimiter    *rate.Limiter
	ipLimiters       *lru.Cache[string, *rate.Limiter]
	agentLimiters    *lru.Cache[string, *rate.Limiter] // heartbeat bucket
	agentAPILimiters *lru.Cache[string, *rate.Limiter] // agent API call bucket
	userLimiters     *lru.Cache[string, *rate.Limiter]
	deviceLimiters   *lru.Cache[string, *rate.Limiter] // device flow bucket
	loginLimiters    *lru.Cache[string, *rate.Limiter] // password login bucket
	publicLimiters   *lru.Cache[string, *rate.Limiter] // public read bucket
	mu               sync.Mutex
}

func New(cfg Config) (*RateLimiter, error) {
	ipCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create IP rate limiter cache")
	}
	agentCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create agent rate limiter cache")
	}
	agentAPICache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create agent API rate limiter cache")
	}
	userCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create user rate limiter cache")
	}
	deviceCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create device rate limiter cache")
	}
	loginCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create login rate limiter cache")
	}
	publicCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create public rate limiter cache")
	}

	return &RateLimiter{
		cfg:              cfg,
		globalLimiter:    rate.NewLimiter(rate.Limit(cfg.GlobalRate), cfg.GlobalBurst),
		ipLimiters:       ipCache,
		agentLimiters:    agentCache,
		agentAPILimiters: agentAPICache,
		userLimiters:     userCache,
		deviceLimiters:   deviceCache,
		loginLimiters:    loginCache,
		publicLimiters:   publicCache,
	}, nil
}

func (rl *RateLimiter) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !rl.globalLimiter.Allow() {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("global rate limit exceeded"))
		}

		procedure := req.Spec().Procedure
		sourceIP := getSourceIP(req.Header(), req.Peer().Addr, rl.cfg.TrustProxy)

		switch {
		case isConnectProcedure(procedure):
			limiter := rl.getIPLimiter(sourceIP)
			if !limiter.Allow() {
				return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("connect rate limit exceeded"))
			}
		case isHeartbeatProcedure(procedure):
			agentID := extractIdentifier(ctx, common.AgentContextKey)
			if agentID != "" {
				limiter := rl.getAgentLimiter(agentID)
				if !limiter.Allow() {
					return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("heartbeat rate limit exceeded"))
				}
			}
		case isDeviceProcedure(procedure):
			limiter := rl.getDeviceLimiter(sourceIP)
			if !limiter.Allow() {
				return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("device login rate limit exceeded"))
			}
		case isLoginProcedure(procedure):
			limiter := rl.getLoginLimiter(sourceIP)
			if !limiter.Allow() {
				return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("login rate limit exceeded"))
			}
		case isPublicReadProcedure(procedure):
			limiter := rl.getPublicLimiter(sourceIP)
			if !limiter.Allow() {
				return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("public read rate limit exceeded"))
			}
		default:
			// Authenticated callers are keyed on the principal the auth
			// interceptor (which runs before us) injected into the context:
			// human users by their user resource id, agents by their agent
			// resource id. Anonymous callers — no principal in context, e.g.
			// CreateUser brute-force — fall back to per-IP so a single source
			// cannot fan out anonymous requests while relying solely on the
			// shared global budget.
			if id := extractIdentifier(ctx, common.UserContextKey); id != "" {
				if !rl.getUserLimiter(id).Allow() {
					return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("API rate limit exceeded"))
				}
			} else if id := extractIdentifier(ctx, common.AgentContextKey); id != "" {
				// Agent API calls (ListPeerAgents, GetOrCreateAgentDM,
				// SendMessage, ...) get their own APIRate/APIBurst bucket —
				// distinct from the heartbeat bucket. Sharing the heartbeat
				// bucket (2/s, burst 10) made a single agent's burst of
				// peer-discovery RPCs during message processing both trip 429s
				// and starve its own heartbeats.
				if !rl.getAgentAPILimiter(id).Allow() {
					return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("API rate limit exceeded"))
				}
			} else {
				limiter := rl.getIPLimiter(sourceIP)
				if !limiter.Allow() {
					return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("API rate limit exceeded"))
				}
			}
		}

		return next(ctx, req)
	}
}

func (*RateLimiter) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

func (*RateLimiter) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(ctx, conn)
	}
}

func (rl *RateLimiter) getIPLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.ipLimiters.Get(ip); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.ConnectRate), rl.cfg.ConnectBurst)
	rl.ipLimiters.Add(ip, limiter)
	return limiter
}

func (rl *RateLimiter) getAgentLimiter(agentID string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.agentLimiters.Get(agentID); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.HeartbeatRate), rl.cfg.HeartbeatBurst)
	rl.agentLimiters.Add(agentID, limiter)
	return limiter
}

// getAgentAPILimiter returns the per-agent API call bucket (APIRate/APIBurst),
// separate from the heartbeat bucket so a burst of agent RPCs cannot starve
// the agent's heartbeats or trip on the tiny heartbeat burst.
func (rl *RateLimiter) getAgentAPILimiter(agentID string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.agentAPILimiters.Get(agentID); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.APIRate), rl.cfg.APIBurst)
	rl.agentAPILimiters.Add(agentID, limiter)
	return limiter
}

// getDeviceLimiter returns the per-IP device-flow bucket. The device RPCs
// are anonymous (the CLI has no credential yet), so they are keyed on the
// source IP like the login bucket.
func (rl *RateLimiter) getDeviceLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.deviceLimiters.Get(ip); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.DeviceRate), rl.cfg.DeviceBurst)
	rl.deviceLimiters.Add(ip, limiter)
	return limiter
}

// getLoginLimiter returns the per-IP password-login bucket (LoginRate/
// LoginBurst), separate from the connect and device buckets so a burst of
// failed password attempts cannot throttle agent registration or the device
// flow (and vice versa).
func (rl *RateLimiter) getLoginLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.loginLimiters.Get(ip); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.LoginRate), rl.cfg.LoginBurst)
	rl.loginLimiters.Add(ip, limiter)
	return limiter
}

func (rl *RateLimiter) getPublicLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.publicLimiters.Get(ip); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.PublicRate), rl.cfg.PublicBurst)
	rl.publicLimiters.Add(ip, limiter)
	return limiter
}

func (rl *RateLimiter) getUserLimiter(userID string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if limiter, ok := rl.userLimiters.Get(userID); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Limit(rl.cfg.APIRate), rl.cfg.APIBurst)
	rl.userLimiters.Add(userID, limiter)
	return limiter
}

// getSourceIP resolves the client source IP for rate-limit keying. Forwarding
// headers are only honored when trustProxy is true (server behind a trusted
// reverse proxy that overwrites them); otherwise the raw TCP peer address is
// used. Client-supplied X-Real-IP is never trusted when trustProxy is false,
// preventing IP spoofing to dodge or pin rate limits.
func getSourceIP(headers http.Header, remoteAddr string, trustProxy bool) string {
	if trustProxy {
		if xff := headers.Get("X-Forwarded-For"); xff != "" {
			ips := strings.SplitN(xff, ",", 2)
			return strings.TrimSpace(ips[0])
		}
		if xri := headers.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil && host != "" {
		return host
	}
	if remoteAddr != "" {
		return remoteAddr
	}
	return "unknown"
}

func isConnectProcedure(procedure string) bool {
	return strings.HasSuffix(procedure, "ConnectAgent") ||
		strings.HasSuffix(procedure, "RefreshAgentToken")
}

func isHeartbeatProcedure(procedure string) bool {
	return strings.HasSuffix(procedure, "AgentHeartbeat")
}

// isLoginProcedure matches only the real password login RPC. A suffix match
// on "Login" would also catch the device-flow RPCs (StartDeviceLogin,
// PollDeviceLogin, ApproveDeviceLogin), routing them into the tiny login
// bucket and leaving the CLI stuck in "login rate limit exceeded" while it
// polls for approval.
func isLoginProcedure(procedure string) bool {
	return procedure == "/laelia.v1.AuthService/Login"
}

func isDeviceProcedure(procedure string) bool {
	return strings.HasSuffix(procedure, "StartDeviceLogin") ||
		strings.HasSuffix(procedure, "PollDeviceLogin") ||
		strings.HasSuffix(procedure, "GetDeviceLoginStatus")
}

// isPublicReadProcedure matches anonymous read-only endpoints that the
// unauthenticated login page calls on every render. They are deliberately
// given a more generous per-IP bucket than the "connect" bucket so normal page
// refreshes don't trip 429s.
func isPublicReadProcedure(procedure string) bool {
	return procedure == "/laelia.v1.SettingService/GetWorkspaceInfo" ||
		procedure == "/laelia.v1.IdentityProviderService/ListIdentityProviders"
}

func extractIdentifier(ctx context.Context, key common.ContextKey) string {
	val := ctx.Value(key)
	if val == nil {
		return ""
	}
	type identifier interface {
		GetResourceID() string
	}
	if id, ok := val.(identifier); ok {
		return id.GetResourceID()
	}
	type emailIdentifier interface {
		GetEmail() string
	}
	if id, ok := val.(emailIdentifier); ok {
		return id.GetEmail()
	}
	return ""
}
