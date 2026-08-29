// Shared "content unchanged" helpers for list-shaped store slices.
//
// Poll-driven fetches run every few seconds; writing a fresh array/object
// reference on every tick would re-render every component subscribed to the
// list even when nothing changed. Returning the previous reference lets
// zustand's Object.is check bail the re-render.

// sameList reports whether two arrays hold the same entries in the same
// order. Order participates: callers render the returned order directly
// (e.g. pinned channels), so a reordered-but-content-identical list counts as
// changed. Pair with proto-es `equals(...)` for element comparison — it gives
// field-level equality (unset vs default included) for free.
export function sameList<T>(
  a: readonly T[] | undefined,
  b: readonly T[] | undefined,
  eq: (x: T, y: T) => boolean
): boolean {
  if (a === b) return true;
  if (!a || !b) return !a && !b;
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (!eq(a[i], b[i])) return false;
  }
  return true;
}

// sameUnreadMap compares unread-count records by key set and value. The map
// is rebuilt from the response on every poll; an unchanged snapshot must not
// invalidate selectors that read it.
export function sameUnreadMap(
  a: Record<string, number> | undefined,
  b: Record<string, number> | undefined
): boolean {
  if (a === b) return true;
  if (!a || !b) return !a && !b;
  const keys = Object.keys(a);
  if (keys.length !== Object.keys(b).length) return false;
  for (const k of keys) {
    if (a[k] !== b[k]) return false;
  }
  return true;
}
