package auth

import (
	"strings"

	"github.com/Ranxy/laelia/backend/common"
)

// IsAuthenticationAllowed returns whether the method is exempted from authentication.
func IsAuthenticationAllowed(fullMethodName string, authContext *common.AuthContext) bool {
	if strings.HasPrefix(fullMethodName, "/grpc.reflection") {
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
