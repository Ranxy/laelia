// AIP resource-name construction/parsing helpers. Every caller that builds or
// splits a `types/{id}` resource name goes through this module so the string
// shapes have one source of truth (06 R-02/B-08).

export function agentResourceName(agentId: string | undefined): string {
  return `agents/${agentId ?? ""}`;
}

// commandIdFromName extracts the bare command id from
// "agents/{agent}/commands/{command}".
export function commandIdFromName(
  name: string | undefined
): string | undefined {
  if (!name) return undefined;
  return name.split("/").pop();
}

// roleIDFromName extracts the bare id from `roles/{id}`.
export function roleIDFromName(name: string | undefined): string {
  if (!name) return "";
  return name.startsWith("roles/") ? name.slice("roles/".length) : name;
}

// avatarNameForUserId builds the avatar resource name for a user from their
// mention handle (the {user} segment of "users/{user}").
export function avatarNameForUserId(handle: string): string {
  return `users/${handle}/avatar`;
}

// avatarNameForAgentId builds the avatar resource name for an agent from its
// resource id (the {agent} segment of "agents/{agent}").
export function avatarNameForAgentId(agentResourceId: string): string {
  return `agents/${agentResourceId}/avatar`;
}
