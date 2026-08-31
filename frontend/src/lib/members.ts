import type { Group } from "@/types/proto-es/v1/group_service_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

// Workspace member resolution helpers shared by the settings CRUD pages
// (member editors / IAM label rows) and MemberPicker. One implementation —
// previously four near-copies drifted (01-R11, machine-profile variant).

export function displayName(user: User): string {
  return user.title || user.email || user.name || "";
}

export function groupDisplayName(group: Group): string {
  return group.title || group.email || group.name || "";
}

// Resolves a workspace member reference to a display label — semantics of
// the byte-identical copies formerly in the mcp-servers / api-providers
// pages: user members show the email, group members the group title,
// "allUsers" passes through. Falls back to the raw reference when the
// directory does not contain the principal (deleted / cross-source members).
export function memberLabel(
  member: string,
  users: User[],
  groups: Group[]
): string {
  if (member === "allUsers") return "allUsers";
  if (member.startsWith("users/")) {
    return users.find((u) => u.name === member)?.email ?? member;
  }
  if (member.startsWith("groups/")) {
    const token = member.slice("groups/".length);
    return (
      groups.find((g) => g.email === token || g.name === `groups/${token}`)
        ?.title ?? token
    );
  }
  return member;
}
