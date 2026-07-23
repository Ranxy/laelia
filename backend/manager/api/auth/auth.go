// Package auth handles the auth of gRPC server.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	errs "github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	issuer = "laelia"
	keyID  = "v1"

	AccessTokenAudienceFmt        = "ll.user.access.%s"
	AgentAccessTokenAudienceFmt   = "ll.agent.access.%s"
	MachineAccessTokenAudienceFmt = "ll.machine.access.%s"

	apiTokenDuration          = 1 * time.Hour
	DefaultTokenDuration      = 7 * 24 * time.Hour
	DefaultAgentTokenDuration = 365 * 24 * time.Hour
	// DefaultMachineTokenDuration matches the agent app: a long-lived machine
	// access token so the machine app stays connected without frequent reconnects.
	DefaultMachineTokenDuration = 365 * 24 * time.Hour

	AccessTokenCookieName = "access-token"

	GatewayMetadataAccessTokenKey   = "laelia-access-token"
	GatewayMetadataRequestOriginKey = "laelia-request-origin"

	// DeclaredAgentHeader is the HTTP header (and grpc-gateway metadata key)
	// a machine app sets on agent-callable RPCs to declare which agent it is
	// acting on behalf of (agents/{agent}). A machine authenticates once with
	// its access token; per-agent identity is carried per-request by this
	// header. The auth interceptor resolves it, verifies the machine owns the
	// agent (agent.machine_id == machine.id), and injects the agent under
	// AgentContextKey so existing handlers resolve the caller unchanged.
	DeclaredAgentHeader = "X-Laelia-Agent"

	TokenTypeBootstrap = "BOOTSTRAP"
	TokenTypeAccess    = "ACCESS"
	TokenTypeRefresh   = "REFRESH"
)

// APIAuthInterceptor is the auth interceptor for gRPC server.
type APIAuthInterceptor struct {
	store    *store.Store
	secret   string
	stateCfg *state.State
	profile  *config.Profile
}

// New returns a new API auth interceptor.
func New(
	store *store.Store,
	secret string,
	stateCfg *state.State,
	profile *config.Profile,
) *APIAuthInterceptor {
	return &APIAuthInterceptor{
		store:    store,
		secret:   secret,
		stateCfg: stateCfg,
		profile:  profile,
	}
}

type authResult struct {
	user                 *store.UserMessage
	agent                *store.AgentMessage
	machine              *store.MachineMessage
	accessTokenExpiresAt int64
}

// WrapUnary implements the ConnectRPC interceptor interface for unary RPCs.
func (in *APIAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		sourceIP := extractSourceIP(req.Header(), peerRemoteAddr(req.Peer()), in.profile.TrustProxy)
		ctx = context.WithValue(ctx, common.SourceIPContextKey, sourceIP)

		accessTokenStr, err := GetTokenFromHeaders(req.Header())
		if err != nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}

		authContext, err := getAuthContext(req.Spec().Procedure)
		if err != nil {
			return nil, err
		}
		ctx = context.WithValue(ctx, common.AuthContextKey, authContext)

		result, err := in.getUserOrAgentConnect(ctx, accessTokenStr)
		if err != nil {
			if IsAuthenticationAllowed(req.Spec().Procedure, authContext) {
				return next(ctx, req)
			}
			return nil, err
		}

		if result.user != nil {
			ctx = context.WithValue(ctx, common.UserContextKey, result.user)
		}
		if result.agent != nil {
			ctx = context.WithValue(ctx, common.AgentContextKey, result.agent)
		}
		if result.machine != nil {
			ctx = context.WithValue(ctx, common.MachineContextKey, result.machine)
			// A machine may act on behalf of an agent declared via the
			// X-Laelia-Agent header; resolve + ownership-check it here so
			// existing agent-callable handlers see the agent via
			// GetAgentFromContext unchanged.
			if declared, derr := in.resolveDeclaredAgent(ctx, result.machine, req.Header()); derr != nil {
				return nil, derr
			} else if declared != nil {
				ctx = context.WithValue(ctx, common.AgentContextKey, declared)
			}
		}
		if result.accessTokenExpiresAt > 0 {
			ctx = context.WithValue(ctx, common.AccessTokenExpiresAtContextKey, result.accessTokenExpiresAt)
		}
		return next(ctx, req)
	}
}

// WrapStreamingClient implements the ConnectRPC interceptor interface for streaming clients.
func (*APIAuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		return next(ctx, spec)
	}
}

// WrapStreamingHandler implements the ConnectRPC interceptor interface for streaming handlers.
func (in *APIAuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		sourceIP := extractSourceIP(conn.RequestHeader(), peerRemoteAddr(conn.Peer()), in.profile.TrustProxy)
		ctx = context.WithValue(ctx, common.SourceIPContextKey, sourceIP)

		accessTokenStr, err := GetTokenFromHeaders(conn.RequestHeader())
		if err != nil {
			return connect.NewError(connect.CodeUnauthenticated, err)
		}

		authContext, err := getAuthContext(conn.Spec().Procedure)
		if err != nil {
			return err
		}
		ctx = context.WithValue(ctx, common.AuthContextKey, authContext)

		result, err := in.getUserOrAgentConnect(ctx, accessTokenStr)
		if err != nil {
			if IsAuthenticationAllowed(conn.Spec().Procedure, authContext) {
				return next(ctx, conn)
			}
			return err
		}

		if result.user != nil {
			ctx = context.WithValue(ctx, common.UserContextKey, result.user)
		}
		if result.agent != nil {
			ctx = context.WithValue(ctx, common.AgentContextKey, result.agent)
		}
		if result.machine != nil {
			ctx = context.WithValue(ctx, common.MachineContextKey, result.machine)
			if declared, derr := in.resolveDeclaredAgent(ctx, result.machine, conn.RequestHeader()); derr != nil {
				return derr
			} else if declared != nil {
				ctx = context.WithValue(ctx, common.AgentContextKey, declared)
			}
		}
		if result.accessTokenExpiresAt > 0 {
			ctx = context.WithValue(ctx, common.AccessTokenExpiresAtContextKey, result.accessTokenExpiresAt)
		}

		return next(ctx, conn)
	}
}

func (in *APIAuthInterceptor) getUserOrAgentConnect(ctx context.Context, accessTokenStr string) (*authResult, error) {
	if accessTokenStr == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token not found"))
	}
	if _, ok := in.stateCfg.TokenExpireCache.Get(accessTokenStr); ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token expired"))
	}

	keyFunc := func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, errs.Errorf("unexpected access token signing method=%v, expect %v", t.Header["alg"], jwt.SigningMethodHS256)
		}
		if kid, ok := t.Header["kid"].(string); ok {
			if kid == "v1" {
				return []byte(in.secret), nil
			}
		}
		return nil, errs.Errorf("unexpected access token kid=%v", t.Header["kid"])
	}

	agentClaims := &agentClaimsMessage{}
	agentToken, agentErr := jwt.ParseWithClaims(accessTokenStr, agentClaims, keyFunc)

	userClaims := &claimsMessage{}
	userToken, userErr := jwt.ParseWithClaims(accessTokenStr, userClaims, keyFunc)

	machineClaims := &machineClaimsMessage{}
	machineToken, machineErr := jwt.ParseWithClaims(accessTokenStr, machineClaims, keyFunc)

	if agentErr == nil && agentToken != nil && agentToken.Valid {
		if audienceContains(agentClaims.Audience, fmt.Sprintf(AgentAccessTokenAudienceFmt, in.profile.Mode)) {
			agent, err := in.authenticateAgentByClaims(ctx, agentClaims)
			if err != nil {
				return nil, err
			}
			return &authResult{agent: agent, accessTokenExpiresAt: agentClaims.ExpiresAt.Unix()}, nil
		}
	}

	if machineErr == nil && machineToken != nil && machineToken.Valid {
		if audienceContains(machineClaims.Audience, fmt.Sprintf(MachineAccessTokenAudienceFmt, in.profile.Mode)) {
			machine, err := in.authenticateMachineByClaims(ctx, machineClaims)
			if err != nil {
				return nil, err
			}
			return &authResult{machine: machine, accessTokenExpiresAt: machineClaims.ExpiresAt.Unix()}, nil
		}
	}

	if userErr == nil && userToken != nil && userToken.Valid {
		if audienceContains(userClaims.Audience, fmt.Sprintf(AccessTokenAudienceFmt, in.profile.Mode)) {
			user, err := in.authenticateUserByClaims(ctx, userClaims)
			if err != nil {
				return nil, err
			}
			return &authResult{user: user, accessTokenExpiresAt: userClaims.ExpiresAt.Unix()}, nil
		}
	}

	if agentErr != nil && userErr != nil && machineErr != nil {
		if errors.Is(agentErr, jwt.ErrTokenExpired) || errors.Is(userErr, jwt.ErrTokenExpired) || errors.Is(machineErr, jwt.ErrTokenExpired) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token expired"))
		}
	}
	return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("invalid access token, audience mismatch, expected %q, %q or %q",
		fmt.Sprintf(AccessTokenAudienceFmt, in.profile.Mode),
		fmt.Sprintf(AgentAccessTokenAudienceFmt, in.profile.Mode),
		fmt.Sprintf(MachineAccessTokenAudienceFmt, in.profile.Mode),
	))
}

func (in *APIAuthInterceptor) authenticateUserByClaims(ctx context.Context, claims *claimsMessage) (*store.UserMessage, error) {
	principalID, err := strconv.Atoi(claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("malformed ID %s in the access token", claims.Subject))
	}
	user, err := in.store.GetUserByID(ctx, principalID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("failed to find user ID %d in the access token", principalID))
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("user ID %d not exists in the access token", principalID))
	}
	if user.MemberDeleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("user ID %d has been deactivated by administrators", user.ID))
	}

	in.profile.LastActiveTS.Store(time.Now().Unix())
	return user, nil
}

func (in *APIAuthInterceptor) authenticateAgentByClaims(ctx context.Context, claims *agentClaimsMessage) (*store.AgentMessage, error) {
	agent, err := in.store.GetAgentByResourceID(ctx, claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("failed to find agent %s", claims.Subject))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("agent %s not exists", claims.Subject))
	}
	if agent.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("agent %s has been deactivated", claims.Subject))
	}
	if agent.TokenVersion != claims.TokenVersion {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("agent token version mismatch"))
	}

	in.profile.LastActiveTS.Store(time.Now().Unix())
	return agent, nil
}

func (in *APIAuthInterceptor) authenticateMachineByClaims(ctx context.Context, claims *machineClaimsMessage) (*store.MachineMessage, error) {
	machine, err := in.store.GetMachineByResourceID(ctx, claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("failed to find machine %s", claims.Subject))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("machine %s not exists", claims.Subject))
	}
	if machine.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("machine %s has been deactivated", claims.Subject))
	}
	if machine.TokenVersion != claims.TokenVersion {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("machine token version mismatch"))
	}

	in.profile.LastActiveTS.Store(time.Now().Unix())
	return machine, nil
}

// resolveDeclaredAgent resolves the agent a machine caller is acting on behalf
// of, from the DeclaredAgentHeader (agents/{agent}). It verifies the machine
// owns the agent (agent.machine_id == machine.id) and that the agent is not
// deleted. Returns nil (no error) when the header is absent — the caller is a
// machine not acting on behalf of an agent (e.g. MachineHeartbeat). On a
// machine call to an agent-callable RPC the header is required, and the
// handler's GetAgentFromContext returning false yields Unauthenticated.
func (in *APIAuthInterceptor) resolveDeclaredAgent(ctx context.Context, machine *store.MachineMessage, headers http.Header) (*store.AgentMessage, error) {
	agentName := headers.Get(DeclaredAgentHeader)
	if agentName == "" {
		return nil, nil
	}
	resourceID, err := common.GetAgentResourceID(agentName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errs.Wrapf(err, "invalid %s header", DeclaredAgentHeader))
	}
	agent, err := in.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errs.Errorf("failed to find declared agent %s", resourceID))
	}
	if agent == nil || agent.Deleted {
		return nil, connect.NewError(connect.CodePermissionDenied, errs.Errorf("declared agent %s not found", resourceID))
	}
	if agent.MachineID != machine.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errs.Errorf("machine %s does not own agent %s", machine.ResourceID, resourceID))
	}
	return agent, nil
}

func GetTokenFromMetadata(md metadata.MD) (string, error) {
	authorizationHeaders := md.Get("Authorization")
	if len(md.Get("Authorization")) > 0 {
		authHeaderParts := strings.Fields(authorizationHeaders[0])
		if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
			return "", errs.Errorf("authorization header format must be Bearer {token}")
		}
		return authHeaderParts[1], nil
	}
	// check the HTTP cookie
	var accessToken string
	for _, t := range append(md.Get("grpcgateway-cookie"), md.Get("cookie")...) {
		header := http.Header{}
		header.Add("Cookie", t)
		request := http.Request{Header: header}
		if v, _ := request.Cookie(AccessTokenCookieName); v != nil {
			accessToken = v.Value
		}
	}
	return accessToken, nil
}

// GetTokenFromHeaders extracts the access token from HTTP headers for ConnectRPC.
func GetTokenFromHeaders(headers http.Header) (string, error) {
	// Check Authorization header first
	authHeader := headers.Get("Authorization")
	if authHeader != "" {
		authHeaderParts := strings.Fields(authHeader)
		if len(authHeaderParts) != 2 || strings.ToLower(authHeaderParts[0]) != "bearer" {
			return "", errs.Errorf("authorization header format must be Bearer {token}")
		}
		return authHeaderParts[1], nil
	}

	// Check HTTP cookies
	var accessToken string
	cookieHeaders := headers.Values("Cookie")
	for _, cookieHeader := range cookieHeaders {
		header := http.Header{}
		header.Add("Cookie", cookieHeader)
		request := http.Request{Header: header}
		if cookie, _ := request.Cookie(AccessTokenCookieName); cookie != nil {
			accessToken = cookie.Value
			break
		}
	}
	return accessToken, nil
}

func audienceContains(audience jwt.ClaimStrings, token string) bool {
	for _, v := range audience {
		if v == token {
			return true
		}
	}
	return false
}

// extractSourceIP resolves the client source IP for a request.
//
// When trustProxy is true, the leftmost X-Forwarded-For entry (or X-Real-IP) is
// trusted — but only when the server sits behind a trusted reverse proxy that
// overwrites client-supplied values. Otherwise the raw TCP peer address
// (remoteAddr) is used, so the source IP is always populated for downstream IP
// allowlists and rate limiters. Client-supplied forwarding headers are ignored
// entirely when trustProxy is false, preventing spoofing.
func extractSourceIP(headers http.Header, remoteAddr string, trustProxy bool) string {
	if trustProxy {
		if xff := headers.Get("X-Forwarded-For"); xff != "" {
			ips := strings.SplitN(xff, ",", 2)
			return strings.TrimSpace(ips[0])
		}
		if xri := headers.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	return stripPort(remoteAddr)
}

// peerRemoteAddr returns the connect peer's remote address (the raw TCP
// "host:port" of the client), or "" when unavailable (e.g. an in-process call).
func peerRemoteAddr(peer connect.Peer) string {
	return peer.Addr
}

// stripPort removes the port from a "host:port" / "[host]:port" address,
// returning the host as-is when it is not an authority form.
func stripPort(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

type claimsMessage struct {
	Name string `json:"name"`
	jwt.RegisteredClaims
}

type agentClaimsMessage struct {
	Name         string `json:"name"`
	TokenVersion int    `json:"token_version"`
	TokenType    string `json:"token_type"`
	SessionID    string `json:"session_id,omitempty"`
	TokenFamily  string `json:"token_family,omitempty"`
	jwt.RegisteredClaims
}

// machineClaimsMessage mirrors agentClaimsMessage for machine tokens. A machine
// authenticates once with its access token; per-agent identity is declared
// in-stream (AgentChannel's AgentReady.agent_name), validated against
// agent.machine_id.
type machineClaimsMessage struct {
	Name         string `json:"name"`
	TokenVersion int    `json:"token_version"`
	TokenType    string `json:"token_type"`
	SessionID    string `json:"session_id,omitempty"`
	TokenFamily  string `json:"token_family,omitempty"`
	jwt.RegisteredClaims
}

// AgentClaims is the verified, exported view of an agent token's claims. It is
// returned by ParseAgentToken so callers outside the auth package (e.g. the
// refresh-token handler) can bind token_version / token_type to the operation
// without re-implementing signature verification.
type AgentClaims struct {
	Name         string
	Subject      string
	TokenVersion int
	TokenType    string
	SessionID    string
	TokenFamily  string
}

// ParseAgentToken parses an agent JWT and verifies its HS256 signature against
// secret. It does NOT enforce token_type or token_version — callers bind those
// to the operation (refresh expects REFRESH; the version must equal the
// agent's current TokenVersion). Verifying the signature here means a token
// minted with a tampered claim set or a rotated/different secret is rejected
// before the store is consulted, so a refresh token whose version was forged
// cannot silently "upgrade" to the current token_version via a hash lookup.
func ParseAgentToken(tokenStr string, secret string) (*AgentClaims, error) {
	claims := &agentClaimsMessage{}
	parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, errs.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		if kid, ok := t.Header["kid"].(string); ok && kid == keyID {
			return []byte(secret), nil
		}
		return nil, errs.Errorf("unexpected kid %v", t.Header["kid"])
	})
	if err != nil {
		return nil, errs.Wrap(err, "invalid agent token")
	}
	if !parsed.Valid {
		return nil, errs.New("agent token is invalid")
	}
	return &AgentClaims{
		Name:         claims.Name,
		Subject:      claims.Subject,
		TokenVersion: claims.TokenVersion,
		TokenType:    claims.TokenType,
		SessionID:    claims.SessionID,
		TokenFamily:  claims.TokenFamily,
	}, nil
}

// MachineClaims is the verified, exported view of a machine token's claims,
// parallel to AgentClaims. Returned by ParseMachineToken for the machine-side
// auth handlers (ConnectMachine / RefreshMachineToken).
type MachineClaims struct {
	Name         string
	Subject      string
	TokenVersion int
	TokenType    string
	SessionID    string
	TokenFamily  string
}

// ParseMachineToken parses a machine JWT and verifies its HS256 signature
// against secret. Like ParseAgentToken it does not enforce token_type or
// token_version — callers bind those to the operation.
func ParseMachineToken(tokenStr string, secret string) (*MachineClaims, error) {
	claims := &machineClaimsMessage{}
	parsed, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, errs.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		if kid, ok := t.Header["kid"].(string); ok && kid == keyID {
			return []byte(secret), nil
		}
		return nil, errs.Errorf("unexpected kid %v", t.Header["kid"])
	})
	if err != nil {
		return nil, errs.Wrap(err, "invalid machine token")
	}
	if !parsed.Valid {
		return nil, errs.New("machine token is invalid")
	}
	return &MachineClaims{
		Name:         claims.Name,
		Subject:      claims.Subject,
		TokenVersion: claims.TokenVersion,
		TokenType:    claims.TokenType,
		SessionID:    claims.SessionID,
		TokenFamily:  claims.TokenFamily,
	}, nil
}

// GenerateAPIToken generates an API token.
func GenerateAPIToken(userName string, userID int, mode common.ReleaseMode, secret string) (string, error) {
	expirationTime := time.Now().Add(apiTokenDuration)
	return generateToken(userName, userID, fmt.Sprintf(AccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateAccessToken generates an access token for web.
func GenerateAccessToken(userName string, userID int, mode common.ReleaseMode, secret string, tokenDuration time.Duration) (string, error) {
	expirationTime := time.Now().Add(tokenDuration)
	return generateToken(userName, userID, fmt.Sprintf(AccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateAgentToken generates an agent token with the specified type and duration.
func GenerateAgentToken(agentName string, resourceID string, tokenVersion int, tokenType string, mode common.ReleaseMode, secret string, duration time.Duration) (string, error) {
	expirationTime := time.Now().Add(duration)
	return signAgentToken(agentName, resourceID, tokenVersion, tokenType, "", resourceID, fmt.Sprintf(AgentAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateAgentTokenWithFamily generates an agent token with a custom token family.
func GenerateAgentTokenWithFamily(agentName string, resourceID string, tokenVersion int, tokenType string, tokenFamily string, mode common.ReleaseMode, secret string, duration time.Duration) (string, error) {
	expirationTime := time.Now().Add(duration)
	return signAgentToken(agentName, resourceID, tokenVersion, tokenType, "", tokenFamily, fmt.Sprintf(AgentAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateAgentTokenWithSession generates an agent token with session ID.
func GenerateAgentTokenWithSession(agentName string, resourceID string, tokenVersion int, tokenType string, sessionID string, mode common.ReleaseMode, secret string, duration time.Duration) (string, error) {
	expirationTime := time.Now().Add(duration)
	return signAgentToken(agentName, resourceID, tokenVersion, tokenType, sessionID, resourceID, fmt.Sprintf(AgentAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateMachineToken generates a machine token with the specified type and duration.
func GenerateMachineToken(machineName string, resourceID string, tokenVersion int, tokenType string, mode common.ReleaseMode, secret string, duration time.Duration) (string, error) {
	expirationTime := time.Now().Add(duration)
	return signMachineToken(machineName, resourceID, tokenVersion, tokenType, "", resourceID, fmt.Sprintf(MachineAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateMachineTokenWithFamily generates a machine token with a custom token family.
func GenerateMachineTokenWithFamily(machineName string, resourceID string, tokenVersion int, tokenType string, tokenFamily string, mode common.ReleaseMode, secret string, duration time.Duration) (string, error) {
	expirationTime := time.Now().Add(duration)
	return signMachineToken(machineName, resourceID, tokenVersion, tokenType, "", tokenFamily, fmt.Sprintf(MachineAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

// GenerateMachineTokenWithSession generates a machine token with session ID.
func GenerateMachineTokenWithSession(machineName string, resourceID string, tokenVersion int, tokenType string, sessionID string, mode common.ReleaseMode, secret string, duration time.Duration) (string, error) {
	expirationTime := time.Now().Add(duration)
	return signMachineToken(machineName, resourceID, tokenVersion, tokenType, sessionID, resourceID, fmt.Sprintf(MachineAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

func signAgentToken(agentName string, resourceID string, tokenVersion int, tokenType string, sessionID string, tokenFamily string, aud string, expirationTime time.Time, secret []byte) (string, error) {
	claims := &agentClaimsMessage{
		Name:         agentName,
		TokenVersion: tokenVersion,
		TokenType:    tokenType,
		SessionID:    sessionID,
		TokenFamily:  tokenFamily,
		RegisteredClaims: jwt.RegisteredClaims{
			// jti makes every minted token unique. Without it, two tokens
			// minted for the same agent in the same second (e.g. a refresh
			// followed immediately by a connect) are byte-identical, hash to
			// the same idx_agent_token_hash, and violate the unique
			// constraint on insert.
			ID:        uuid.NewString(),
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    issuer,
			Subject:   resourceID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = keyID

	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func signMachineToken(machineName string, resourceID string, tokenVersion int, tokenType string, sessionID string, tokenFamily string, aud string, expirationTime time.Time, secret []byte) (string, error) {
	claims := &machineClaimsMessage{
		Name:         machineName,
		TokenVersion: tokenVersion,
		TokenType:    tokenType,
		SessionID:    sessionID,
		TokenFamily:  tokenFamily,
		RegisteredClaims: jwt.RegisteredClaims{
			// jti makes every minted token unique; see signAgentToken for why.
			ID:        uuid.NewString(),
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    issuer,
			Subject:   resourceID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = keyID

	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// Pay attention to this function. It holds the main JWT token generation logic.
func generateToken(userName string, userID int, aud string, expirationTime time.Time, secret []byte) (string, error) {
	// Create the JWT claims, which includes the username and expiry time.
	claims := &claimsMessage{
		Name: userName,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: jwt.ClaimStrings{aud},
			// In JWT, the expiry time is expressed as unix milliseconds.
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    issuer,
			Subject:   strconv.Itoa(userID),
		},
	}

	// Declare the token with the HS256 algorithm used for signing, and the claims.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = keyID

	// Create the JWT string.
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func getAuthContext(fullMethod string) (*common.AuthContext, error) {
	methodTokens := strings.Split(fullMethod, "/")
	if len(methodTokens) != 3 {
		return nil, errs.Errorf("invalid full method name %q", fullMethod)
	}
	rd, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(methodTokens[1]))
	if err != nil {
		return nil, errs.Wrapf(err, "invalid registry service descriptor, full method name %q", fullMethod)
	}
	sd, ok := rd.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, errs.Errorf("invalid service descriptor, full method name %q", fullMethod)
	}
	md, ok := sd.Methods().ByName(protoreflect.Name(methodTokens[2])).Options().(*descriptorpb.MethodOptions)
	if !ok {
		return nil, errs.Errorf("invalid method options, full method name %q", fullMethod)
	}
	allowWithoutCredentialAny := proto.GetExtension(md, v1pb.E_AllowWithoutCredential)
	allowWithoutCredential, ok := allowWithoutCredentialAny.(bool)
	if !ok {
		return nil, errs.Errorf("invalid allow without credential extension, full method name %q", fullMethod)
	}
	permissionAny := proto.GetExtension(md, v1pb.E_Permission)
	permission, ok := permissionAny.(string)
	if !ok {
		return nil, errs.Errorf("invalid permission extension, full method name %q", fullMethod)
	}
	authMethodAny := proto.GetExtension(md, v1pb.E_AuthMethod)
	am, ok := authMethodAny.(v1pb.AuthMethod)
	if !ok {
		return nil, errs.Errorf("invalid auth method extension, full method name %q", fullMethod)
	}
	var authMethod common.AuthMethod
	switch am {
	case v1pb.AuthMethod_AUTH_METHOD_UNSPECIFIED:
		authMethod = common.AuthMethodUnspecified
	case v1pb.AuthMethod_IAM:
		authMethod = common.AuthMethodIAM
	case v1pb.AuthMethod_CUSTOM:
		authMethod = common.AuthMethodCustom
	default:
		return nil, errs.Errorf("unknown auth method %v for full method name %q", am, fullMethod)
	}
	auditAny := proto.GetExtension(md, v1pb.E_Audit)
	audit, ok := auditAny.(bool)
	if !ok {
		return nil, errs.Errorf("invalid audit extension, full method name %q", fullMethod)
	}

	return &common.AuthContext{
		AllowWithoutCredential: allowWithoutCredential,
		Permission:             permission,
		AuthMethod:             authMethod,
		Audit:                  audit,
	}, nil
}
