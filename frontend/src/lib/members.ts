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

// Resolves a workspace member reference ("users/<id>" | "groups/<email>") to
// a display label, falling back to the raw reference when the directory does
// not contain the principal (deleted / cross-source members).
export function memberLabel(
  member: string,
  users: User[],
  groups: Group[]
): string {
  if (member.startsWith("groups/")) {
    const email = member.slice("groups/".length);
    const group = groups.find((g) => g.email === email || g.name === member);
    return group ? groupDisplayName(group) : member;
  }
  const user = users.find((u) => u.name === member);
  return user ? displayName(user) : member;
}
