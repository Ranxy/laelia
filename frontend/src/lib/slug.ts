// Turns a free-form title into a resource-id slug (lowercase, alnum + dash).
// Roles derive their immutable resource id from the title at create time and
// identity providers theirs the same way — both previously carried private
// copies of this function (01-R17) that had drifted; the two spellings were
// behaviorally equivalent on every input shape (runs of non-alnum characters
// collapse to one dash, edges trim).
export function slugify(s: string): string {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}