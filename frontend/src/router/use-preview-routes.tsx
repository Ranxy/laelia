import {
  type ComponentType,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useSyncExternalStore,
} from "react";
import {
  matchRoutes,
  UNSAFE_RouteContext as RouteContext,
  type RouteObject,
  useRoutes,
} from "react-router-dom";

// ── Module cache ──────────────────────────────────────────────────────────
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

export function preloadPreviewRoute(routes: RouteObject[], path: string) {
  const matches = matchRoutes(routes, path);
  if (!matches) return;
  for (const m of matches) {
    if (typeof m.route.lazy === "function" && !lazyCache.has(m.route.lazy)) {
      void ensureRouteModule(m.route.lazy as () => Promise<unknown>);
    }
  }
}

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

// ── Preview route scope ───────────────────────────────────────────────────
//
// ROOT CAUSE: useRoutes() reads parentMatches from the outer RouteContext.
// Inside DashboardLayout (rendered by the data router), the RouteContext's
// matches include the :conversationId param — pathless parent routes inherit
// child params.  useRoutes merges these params into the preview's matches
// (Object.assign({}, parentParams, match.params)), so the preview's ChatLayout
// sees conversationId from useParams() and hides the conversation list.
//
// FIX: wrap the useRoutes call in a component tree that provides a RouteContext
// with stripped params (params: {}).  useRoutes then reads clean parentMatches
// and the preview's components get the correct params.

function PreviewRouteScope({ children }: { children: ReactNode }) {
  const outer = useContext(RouteContext);
  // Strip params from every match so useRoutes doesn't merge the real
  // route's conversationId (or any other param) into the preview.
  const value = useMemo(
    () => ({
      outlet: outer.outlet,
      isDataRoute: false as const,
      matches: outer.matches.map((m) => ({ ...m, params: {} })),
    }),
    [outer]
  ) as typeof outer & { matches: typeof outer.matches };
  return (
    <RouteContext.Provider value={value}>{children}</RouteContext.Provider>
  );
}

function PreviewRoutes({
  routes,
  path,
}: {
  routes: RouteObject[];
  path: string;
}) {
  return useRoutes(routes, path);
}

export function usePreviewRoutes(routes: RouteObject[], path: string | null) {
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
    ).catch(() => {});
  }, [previewRoutes, path]);

  if (!path) return null;

  // Render useRoutes inside PreviewRouteScope so it reads a RouteContext with
  // stripped params — preventing the real route's conversationId from leaking
  // into the preview's matches.
  return (
    <PreviewRouteScope>
      <PreviewRoutes routes={previewRoutes} path={path} />
    </PreviewRouteScope>
  );
}
