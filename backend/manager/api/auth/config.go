package auth

import (
	"strings"

	"github.com/Ranxy/laelia/backend/common"
)

// IsAuthenticationAllowed returns whether the method is exempted from authentication.
func IsAuthenticationAllowed(fullMethodName string, authContext *common.AuthContext) bool {
	// "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo" is used
	//  for reflection.
	if strings.HasPrefix(fullMethodName, "/grpc.reflection") {
		return true
	}
	return authContext.AllowWithoutCredential
}
