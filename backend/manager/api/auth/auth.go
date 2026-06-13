// Package auth handles the auth of gRPC server.
package auth

import (
	"context"
	"errors"
	"fmt"
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
	errs "github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	issuer = "laelia"
	// Signing key section. For now, this is only used for signing, not for verifying since we only
	// have 1 version. But it will be used to maintain backward compatibility if we change the signing mechanism.
	keyID = "v1"
	// AccessTokenAudienceFmt is the format of the acccess token audience.
	AccessTokenAudienceFmt = "ll.user.access.%s"
	// AgentAccessTokenAudienceFmt is the format of the agent access token audience.
	AgentAccessTokenAudienceFmt = "ll.agent.access.%s"
	apiTokenDuration            = 1 * time.Hour
	// DefaultTokenDuration is the default token expiration duration.
	DefaultTokenDuration = 7 * 24 * time.Hour
	// DefaultAgentTokenDuration is the default agent token expiration duration.
	DefaultAgentTokenDuration = 365 * 24 * time.Hour

	// AccessTokenCookieName is the cookie name of access token.
	AccessTokenCookieName = "access-token"

	// GatewayMetadataAccessTokenKey is the gateway metadata key for access token.
	GatewayMetadataAccessTokenKey = "laelia-access-token"
	// GatewayMetadataRequestOriginKey is the gateway metadata key for the request origin header.
	GatewayMetadataRequestOriginKey = "laelia-request-origin"
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
	user  *store.UserMessage
	agent *store.AgentMessage
}

// WrapUnary implements the ConnectRPC interceptor interface for unary RPCs.
func (in *APIAuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
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

	if agentErr == nil && agentToken != nil && agentToken.Valid {
		if audienceContains(agentClaims.Audience, fmt.Sprintf(AgentAccessTokenAudienceFmt, in.profile.Mode)) {
			agent, err := in.authenticateAgentByClaims(ctx, agentClaims)
			if err != nil {
				return nil, err
			}
			return &authResult{agent: agent}, nil
		}
	}

	if userErr == nil && userToken != nil && userToken.Valid {
		if audienceContains(userClaims.Audience, fmt.Sprintf(AccessTokenAudienceFmt, in.profile.Mode)) {
			user, err := in.authenticateUserByClaims(ctx, userClaims)
			if err != nil {
				return nil, err
			}
			return &authResult{user: user}, nil
		}
	}

	if agentErr != nil && userErr != nil {
		if errors.Is(agentErr, jwt.ErrTokenExpired) || errors.Is(userErr, jwt.ErrTokenExpired) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errs.New("access token expired"))
		}
	}
	return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("invalid access token, audience mismatch, expected %q or %q",
		fmt.Sprintf(AccessTokenAudienceFmt, in.profile.Mode),
		fmt.Sprintf(AgentAccessTokenAudienceFmt, in.profile.Mode),
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
	agentID, err := strconv.Atoi(claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("malformed agent ID %s in the token", claims.Subject))
	}
	agent, err := in.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("failed to find agent ID %d", agentID))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("agent ID %d not exists", agentID))
	}
	if agent.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("agent ID %d has been deactivated", agent.ID))
	}
	if agent.TokenVersion != claims.TokenVersion {
		return nil, connect.NewError(connect.CodeUnauthenticated, errs.Errorf("agent token version mismatch"))
	}

	in.profile.LastActiveTS.Store(time.Now().Unix())
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

type claimsMessage struct {
	Name string `json:"name"`
	jwt.RegisteredClaims
}

type agentClaimsMessage struct {
	Name         string `json:"name"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
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

// GenerateAgentToken generates an agent connection token.
func GenerateAgentToken(agentName string, agentID int, tokenVersion int, mode common.ReleaseMode, secret string) (string, error) {
	expirationTime := time.Now().Add(DefaultAgentTokenDuration)
	return signAgentToken(agentName, agentID, tokenVersion, fmt.Sprintf(AgentAccessTokenAudienceFmt, mode), expirationTime, []byte(secret))
}

func signAgentToken(agentName string, agentID int, tokenVersion int, aud string, expirationTime time.Time, secret []byte) (string, error) {
	claims := &agentClaimsMessage{
		Name:         agentName,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    issuer,
			Subject:   strconv.Itoa(agentID),
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
