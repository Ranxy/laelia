package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// GatewayResponseModifier is the response modifier for grpc gateway.
type GatewayResponseModifier struct {
	Store *store.Store
}

// Modify is the mux option for modifying response header.
func (*GatewayResponseModifier) Modify(ctx context.Context, response http.ResponseWriter, _ proto.Message) error {
	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return errors.Errorf("failed to get ServerMetadata from context in the gateway response modifier")
	}

	if vs := md.HeaderMD.Get("Set-Cookie"); len(vs) > 0 {
		for _, v := range vs {
			response.Header().Add("Set-Cookie", v)
		}
	}
	return nil
}

// token="" => unset
func GetTokenCookie(ctx context.Context, stores *store.Store, origin, token string) *http.Cookie {
	if token == "" {
		return &http.Cookie{
			Name:    AccessTokenCookieName,
			Value:   "",
			Expires: time.Unix(0, 0),
			Path:    "/",
		}
	}
	isHTTPS := strings.HasPrefix(origin, "https")
	sameSite := http.SameSiteStrictMode
	if isHTTPS {
		sameSite = http.SameSiteNoneMode
	}
	tokenDuration := GetTokenDuration(ctx, stores)
	return &http.Cookie{
		Name:  AccessTokenCookieName,
		Value: token,
		// CookieExpDuration expires slightly earlier than the jwt expiration. Client would be logged out if the user
		// cookie expires, thus the client would always logout first before attempting to make a request with the expired jwt.
		// Suppose we have a valid refresh token, we will refresh the token in 2 cases:
		// 1. The access token is about to expire in <<refreshThresholdDuration>>
		// 2. The access token has already expired, we refresh the token so that the ongoing request can pass through.
		Expires: time.Now().Add(tokenDuration - 1*time.Second),
		Path:    "/",
		// Http-only helps mitigate the risk of client side script accessing the protected cookie.
		HttpOnly: true,
		Secure:   isHTTPS,
		SameSite: sameSite,
	}
}

func GetTokenDuration(_ context.Context, _ *store.Store) time.Duration {
	tokenDuration := DefaultTokenDuration
	// maybe we can add a setting for token duration in the future

	return tokenDuration
}
