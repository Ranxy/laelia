// The backend turns `name.matches(q)` / `email.matches(q)` into
// `LOWER(principal.<col>) LIKE %q%`. The user's display name (title) lives in
// principal.name, so a search by email OR title is the union of the two
// matches() predicates. Strip CEL string delimiters and LIKE wildcards so the
// typed query can't break the parse or over-match.
export function escapeFilterQuery(raw: string): string {
  return raw.replace(/[\\"]|%|_/g, "").trim();
}

export function buildUserFilter(query: string): string {
  const q = escapeFilterQuery(query);
  if (q === "") return "";
  return `name.matches("${q}") || email.matches("${q}")`;
}
