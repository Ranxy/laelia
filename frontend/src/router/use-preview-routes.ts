import {
  type ComponentType,
  useEffect,
  useMemo,
  useSyncExternalStore,
} from "react";
import { matchRoutes, type RouteObject, useRoutes } from "react-router-dom";

// Path that matches no dashboard route; useRoutes returns null for it so the
// preview stays unmounted while no gesture is active.
const NO_PREVIEW_PATH = "/__swipe-preview-no-match__";

// ---------------------------------------------------------------------------
// Module cache
//
// useRoutes (declarative) does not load lazy routes, so the preview must
// resolve the matching routes' lazy modules itself.  The previous approach
// gated the preview behind a `ready` state that only flipped true inside a
// useEffect → microtask → setState cycle — even when the bundler had already
// cached the chunk, this cost at least one rendered frame of blank preview,
// which the user saw as "data not loaded until the gesture finishes".
//
// The cache below maps each route's `lazy` function to its resolved module.
// cloneRouteTree reads it synchronously and sets `Component` on the clone, so
// once a module is cached the preview renders on the very first frame.  A
// useSyncExternalStore subscription re-renders the host the moment a module
// finishes loading, and preloadPreviewRoute (called at gesture start) kicks
// off the import() before React even re-renders — by the time the preview's
// clone runs the module is usually already in the cache.
// ---------------------------------------------------------------------------
const lazyCache = new Map<
  () => Promise<unknown>,
  { Component?: ComponentType }
>();
const cacheListeners = new Set<() => void>();
let cacheVersion = 0;

function subscribeCache(listener: () => void): () => void {
  cacheListeners.add(listener);
  return () => {
    cacheListeners.delete(listener);
  };
}

function getCacheSnapshot(): number {
  return cacheVersion;
}

function getServerCacheSnapshot(): number {
  return 0;
}

// Loads a route's lazy module (if not already cached) and notifies subscribers
// when the cache is updated so useSyncExternalStore re-renders the host.
function ensureRouteModule(
  lazyFn: () => Promise<unknown>
): Promise<{ Component?: ComponentType } | null> {
  const cached = lazyCache.get(lazyFn);
  if (cached) return Promise.resolve(cached);
  return lazyFn().then((mod) => {
    const resolved = (mod as { Component?: ComponentType } | null) ?? {};
    lazyCache.set(lazyFn, resolved);
    cacheVersion++;
    cacheListeners.forEach((l) => l());
    return resolved;
  });
}

// Preload the lazy modules for the route matching `path`.  Called at gesture
// start (onTouchStart) so the dynamic import begins before React re-renders —
// by the time the preview's cloneRouteTree runs, the module is often already
// cached and the preview renders on the first frame.
export function preloadPreviewRoute(routes: RouteObject[], path: string) {
  const matches = matchRoutes(routes, path);
  if (!matches) return;
  for (const m of matches) {
    if (typeof m.route.lazy === "function" && !lazyCache.has(m.route.lazy)) {
      void ensureRouteModule(m.route.lazy as () => Promise<unknown>);
    }
  }
}

// Deep-clones the route tree so lazy Components loaded for the swipe-back
// preview never mutate the shared route table used by the data router.  Routes
// whose lazy modules are already cached get Component set (and lazy cleared)
// so useRoutes renders them immediately.
function cloneRouteTree(routes: RouteObject[]): RouteObject[] {
  return routes.map((route) => {
    const clone: RouteObject = { ...route };
    if (typeof clone.lazy === "function") {
      const mod = lazyCache.get(clone.lazy as () => Promise<unknown>);
      if (mod?.Component) {
        clone.Component = mod.Component;
        clone.lazy = undefined;
      }
    }
    clone.children = route.children
      ? cloneRouteTree(route.children)
      : undefined;
    return clone;
  }) as RouteObject[];
}

// usePreviewRoutes renders the route tree at `path` (the swipe-back preview).
// Returns null when idle (path is null).  When the matching routes' lazy
// modules are already cached the preview renders synchronously on the first
// frame; otherwise the effect loads them and useSyncExternalStore re-renders
// the moment they resolve.
export function usePreviewRoutes(routes: RouteObject[], path: string | null) {
  // Re-render when the module cache changes (e.g. after preloadPreviewRoute
  // resolves a module at gesture start).
  const version = useSyncExternalStore(
    subscribeCache,
    getCacheSnapshot,
    getServerCacheSnapshot
  );
  const previewRoutes = useMemo(
    () => cloneRouteTree(routes),
    [routes, version]
  );

  useEffect(() => {
    if (!path) return;
    const matches = matchRoutes(previewRoutes, path);
    if (!matches) return;
    // Load any routes whose modules aren't cached yet.  ensureRouteModule
    // updates the cache and notifies useSyncExternalStore, which re-renders
    // and re-clones with Component set.
    const uncached = matches.filter(
      (m) =>
        typeof m.route.lazy === "function" &&
        !lazyCache.has(m.route.lazy as () => Promise<unknown>)
    );
    if (uncached.length === 0) return;
    Promise.all(
      uncached.map((m) =>
        ensureRouteModule(m.route.lazy as () => Promise<unknown>)
      )
    ).catch(() => {
      // Swallow load errors — the route renders as an empty Outlet.
    });
  }, [previewRoutes, path]);

  const element = useRoutes(previewRoutes, path ?? NO_PREVIEW_PATH);
  return path ? element : null;
}
