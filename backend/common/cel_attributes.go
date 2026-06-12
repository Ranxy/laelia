//nolint:revive
package common

// CEL attribute names for resource scope.
const (
	// CELAttributeResourceEnvironmentID is the environment ID of the resource.
	CELAttributeResourceEnvironmentID = "resource.environment_id"
	// CELAttributeResourceProjectID is the project ID of the resource.
	CELAttributeResourceProjectID = "resource.project_id"
)

// CEL attribute names for request scope.
const (
	// CELAttributeRequestExpirationDays is the number of days until the request expires.
	CELAttributeRequestExpirationDays = "request.expiration_days"
	// CELAttributeRequestRole is the requested role.
	CELAttributeRequestRole = "request.role"
	// CELAttributeRequestTime is the timestamp of the request.
	CELAttributeRequestTime = "request.time"
)

// CEL attribute names for approval scope (deprecated, kept for backward compatibility).
const (
	// CELAttributeLevel is the risk level (deprecated).
	CELAttributeLevel = "level"
	// CELAttributeSource is the risk source (deprecated).
	CELAttributeSource = "source"
)
