// Unified module-cleanup registry (audit 05 B7). Modules with file-scope side
// effects (module-level caches, recency lists, Query cache entries) used to be
// released by hand-maintained call sites on the logout/reset path — every new
// side-effectful module had to edit index.ts's reset() and/or auth.ts's
// logout(), and forgetting one left stale per-principal data across a
// logout-relogin. Contract: a module calls registerCleanup(fn) ONCE at its own
// file scope; reset() (and therefore logout) runs every registered callback
// before wiping slice state. New modules never touch index.ts/auth.ts.
//
// Callbacks must be synchronous and idempotent: they run on every reset of the
// tab. runCleanups() deliberately does not drain the registry — production
// registrations are recurring session-teardown tasks that must fire again on
// the next logout; draining would make the second logout of a tab leak. The
// unsubscribe function returned by registerCleanup() exists for temporary
// registrations (tests). A throwing callback is contained so one failing
// module cannot skip the others.

const cleanups = new Set<() => void>();

export function registerCleanup(fn: () => void): () => void {
  cleanups.add(fn);
  return () => {
    cleanups.delete(fn);
  };
}

// Runs every registered cleanup, then leaves the registry intact (see above).
export function runCleanups(): void {
  for (const fn of [...cleanups]) {
    try {
      fn();
    } catch (err) {
      console.error("cleanup registry: callback failed", err);
    }
  }
}
