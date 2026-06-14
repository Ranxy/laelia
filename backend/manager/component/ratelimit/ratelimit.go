package ratelimit

import (
	"context"
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
		APIRate:        1000.0 / 60.0,
		APIBurst:       100,
		TrustProxy:     false,
	}
}

type RateLimiter struct {
	cfg           Config
	globalLimiter *rate.Limiter
	ipLimiters    *lru.Cache[string, *rate.Limiter]
	agentLimiters *lru.Cache[string, *rate.Limiter]
	userLimiters  *lru.Cache[string, *rate.Limiter]
	mu            sync.Mutex
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
	userCache, err := lru.New[string, *rate.Limiter](10000)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create user rate limiter cache")
	}

	return &RateLimiter{
		cfg:           cfg,
		globalLimiter: rate.NewLimiter(rate.Limit(cfg.GlobalRate), cfg.GlobalBurst),
		ipLimiters:    ipCache,
		agentLimiters: agentCache,
		userLimiters:  userCache,
	}, nil
}

func (rl *RateLimiter) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !rl.globalLimiter.Allow() {
			return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("global rate limit exceeded"))
		}

		procedure := req.Spec().Procedure
		sourceIP := getSourceIP(req.Header(), rl.cfg.TrustProxy)

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
		case isLoginProcedure(procedure):
			limiter := rl.getIPLimiter(sourceIP)
			if !limiter.Allow() {
				return nil, connect.NewError(connect.CodeResourceExhausted, errors.New("login rate limit exceeded"))
			}
		default:
			userID := extractIdentifier(ctx, common.UserContextKey)
			if userID != "" {
				limiter := rl.getUserLimiter(userID)
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

func getSourceIP(headers http.Header, trustProxy bool) string {
	if trustProxy {
		if xff := headers.Get("X-Forwarded-For"); xff != "" {
			ips := strings.SplitN(xff, ",", 2)
			return strings.TrimSpace(ips[0])
		}
	}
	if xri := headers.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
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

func isLoginProcedure(procedure string) bool {
	return strings.HasSuffix(procedure, "Login")
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
