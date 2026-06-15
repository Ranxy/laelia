//nolint:revive
package common

import "context"

// ContextKey is the key type of context value.
type ContextKey int

const (
	// UserContextKey is the key name used to store user message in the context.
	UserContextKey ContextKey = iota
	AuthContextKey
	ServiceDataKey
	AgentContextKey
	SessionContextKey
	SourceIPContextKey
	AccessTokenExpiresAtContextKey
)

type AuthMethod int

const (
	AuthMethodUnspecified AuthMethod = iota
	AuthMethodIAM
	AuthMethodCustom
)

type Resource struct {
	Type      string
	Name      string
	ProjectID string
	Workspace bool
}

type AuthContext struct {
	Audit                  bool
	AllowWithoutCredential bool
	Permission             string
	AuthMethod             AuthMethod
	Resources              []*Resource
}

func GetAuthContextFromContext(ctx context.Context) (*AuthContext, bool) {
	authCtx, ok := ctx.Value(AuthContextKey).(*AuthContext)
	return authCtx, ok
}

func (c *AuthContext) HasWorkspaceResource() bool {
	for _, r := range c.Resources {
		if r.Workspace {
			return true
		}
	}
	return false
}

func (c *AuthContext) GetProjectResources() []string {
	projectIDMap := make(map[string]bool)
	for _, r := range c.Resources {
		if r.ProjectID != "" {
			projectIDMap[r.ProjectID] = true
		}
	}
	var projectIDs []string
	for projectID := range projectIDMap {
		projectIDs = append(projectIDs, projectID)
	}
	return projectIDs
}

func GetSessionIDFromContext(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(SessionContextKey).(string)
	return sessionID, ok
}

func GetSourceIPFromContext(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(SourceIPContextKey).(string)
	return ip, ok
}

func GetAccessTokenExpiresAtFromContext(ctx context.Context) (int64, bool) {
	exp, ok := ctx.Value(AccessTokenExpiresAtContextKey).(int64)
	return exp, ok
}
