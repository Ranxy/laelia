package auth

import (
	"strings"

	"github.com/Ranxy/laelia/backend/common"
)

// IsAuthenticationAllowed returns whether the method is exempted from authentication.
// gRPC reflection is exempted only in dev mode; in production the routes are
// not even registered (see server/grpc_routes.go), and this gate is defense in
// depth so a stray reflection registration can never be probed anonymously.
func IsAuthenticationAllowed(fullMethodName string, authContext *common.AuthContext, devMode bool) bool {
	if devMode && strings.HasPrefix(fullMethodName, "/grpc.reflection") {
		return true
	}
	if authContext.AllowWithoutCredential {
		return true
	}
	if authContext.AuthMethod == common.AuthMethodCustom {
		return true
	}
	return false
}
