//nolint:revive
package common

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// nolint:revive
const (
	WorkspacePrefix            = "workspaces/"
	ProjectNamePrefix          = "projects/"
	UserNamePrefix             = "users/"
	IdentityProviderNamePrefix = "idps/"
	SettingNamePrefix          = "settings/"
	RolePrefix                 = "roles/"
	GroupPrefix                = "groups/"
	AgentNamePrefix            = "agents/"
	MachineNamePrefix          = "machines/"
	ConversationNamePrefix     = "conversations/"
)

// GetUserID returns the user ID from a resource name.
func GetUserID(name string) (int, error) {
	return GetUIDFromName(name, UserNamePrefix)
}

// GetUIDFromName returns the UID from a resource name.
func GetUIDFromName(name, prefix string) (int, error) {
	tokens, err := GetNameParentTokens(name, prefix)
	if err != nil {
		return 0, err
	}
	uid, err := strconv.Atoi(tokens[0])
	if err != nil {
		return 0, errors.Errorf("invalid ID %q", tokens[0])
	}
	return uid, nil
}

// GetUserEmail returns the user email from a resource name.
func GetUserEmail(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, UserNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetSettingName returns the setting name from a resource name.
func GetSettingName(name string) (string, error) {
	token, err := GetNameParentTokens(name, SettingNamePrefix)
	if err != nil {
		return "", err
	}
	return token[0], nil
}

// GetIdentityProviderID returns the identity provider ID from a resource name.
func GetIdentityProviderID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, IdentityProviderNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetRoleID returns the role ID from a resource name.
func GetRoleID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, RolePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// GetGroupEmail returns the group email.
func GetGroupEmail(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, GroupPrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// TrimSuffix trims the suffix from the name and returns the trimmed name.
func TrimSuffix(name, suffix string) (string, error) {
	if !strings.HasSuffix(name, suffix) {
		return "", errors.Errorf("invalid request %q with suffix %q", name, suffix)
	}
	return strings.TrimSuffix(name, suffix), nil
}

// invalidTokenChars are characters that never appear in a legitimate resource
// name token (numeric IDs, emails, UUIDs, slugs) but are dangerous when a token
// value is interpolated into SQL or a path. Rejecting them here is defense in
// depth on top of query parameterization: callers that still interpolate a
// token into a SQL string literal cannot be broken out of it by these payloads.
var invalidTokenChars = "'\";\\()\x00"

// GetNameParentTokens returns the tokens from a resource name.
func GetNameParentTokens(name string, tokenPrefixes ...string) ([]string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2*len(tokenPrefixes) {
		return nil, errors.Errorf("invalid request %q", name)
	}

	var tokens []string
	for i, tokenPrefix := range tokenPrefixes {
		if fmt.Sprintf("%s/", parts[2*i]) != tokenPrefix {
			return nil, errors.Errorf("invalid prefix %q in request %q", tokenPrefix, name)
		}
		token := parts[2*i+1]
		if strings.ContainsAny(token, invalidTokenChars) {
			return nil, errors.Errorf("invalid token %q in request %q", token, name)
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// GetProjectID returns the project ID from a resource name.
func GetProjectID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, ProjectNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

func FormatUserEmail(email string) string {
	return fmt.Sprintf("%s%s", UserNamePrefix, email)
}

func FormatUserUID(uid int) string {
	return fmt.Sprintf("%s%d", UserNamePrefix, uid)
}

// avatarNameSuffix is the trailing segment that turns a user resource name into
// its avatar resource name: users/{id}/avatar.
const avatarNameSuffix = "/avatar"

// FormatUserAvatar returns the avatar resource name for a user: users/{id}/avatar.
func FormatUserAvatar(uid int) string {
	return fmt.Sprintf("%s%d%s", UserNamePrefix, uid, avatarNameSuffix)
}

// ParseUserAvatarName parses an avatar resource name (users/{id}/avatar) and
// returns the user id.
func ParseUserAvatarName(name string) (int, error) {
	trimmed, err := TrimSuffix(name, avatarNameSuffix)
	if err != nil {
		return 0, err
	}
	return GetUserID(trimmed)
}

// FormatAgentAvatar returns the avatar resource name for an agent:
// agents/{agent}/avatar.
func FormatAgentAvatar(resourceID string) string {
	return fmt.Sprintf("%s%s%s", AgentNamePrefix, resourceID, avatarNameSuffix)
}

// ParseAgentAvatarName parses an avatar resource name (agents/{agent}/avatar) and
// returns the agent's resource id.
func ParseAgentAvatarName(name string) (string, error) {
	trimmed, err := TrimSuffix(name, avatarNameSuffix)
	if err != nil {
		return "", err
	}
	return GetAgentResourceID(trimmed)
}

func FormatRole(role string) string {
	return fmt.Sprintf("%s%s", RolePrefix, role)
}

func FormatGroupEmail(email string) string {
	return fmt.Sprintf("%s%s", GroupPrefix, email)
}

func GetAgentResourceID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, AgentNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

func FormatAgentUID(uid string) string {
	return fmt.Sprintf("%s%s", AgentNamePrefix, uid)
}

// GetMachineResourceID returns the machine resource id (uuid) from a
// machines/{machine} resource name.
func GetMachineResourceID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, MachineNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}

// FormatMachineUID returns the machines/{machine} resource name for the given
// machine resource id.
func FormatMachineUID(uid string) string {
	return fmt.Sprintf("%s%s", MachineNamePrefix, uid)
}

// FormatConversationName returns the conversation resource name for the given
// conversation UUID.
func FormatConversationName(id string) string {
	return fmt.Sprintf("%s%s", ConversationNamePrefix, id)
}

// GetConversationResourceID returns the conversation UUID from a resource name.
func GetConversationResourceID(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, ConversationNamePrefix)
	if err != nil {
		return "", err
	}
	return tokens[0], nil
}
