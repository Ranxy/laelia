import { type ComponentType, useEffect, useMemo, useState } from "react";
import { matchRoutes, type RouteObject, useRoutes } from "react-router-dom";

// Path that matches no dashboard route; useRoutes returns null for it so the
// preview stays unmounted while no gesture is active.
const NO_PREVIEW_PATH = "/__swipe-preview-no-match__";

// Deep-clones the route tree so lazy Components loaded for the swipe-back
// preview never mutate the shared route table used by the data router.
function cloneRouteTree(routes: RouteObject[]): RouteObject[] {
  return routes.map((route) => ({
    ...route,
    children: route.children ? cloneRouteTree(route.children) : undefined,
  })) as RouteObject[];
}

// usePreviewRoutes renders the route tree at `path` (the swipe-back preview).
// Declarative useRoutes does not load lazy routes, so the matching routes'
// lazy modules are loaded first and their Components are attached to a private
// clone of the tree; the preview element appears once they are ready. Returns
// null when idle (path is null) or while the lazy modules are still loading.
export function usePreviewRoutes(routes: RouteObject[], path: string | null) {
  const previewRoutes = useMemo(() => cloneRouteTree(routes), [routes]);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    if (!path) {
      setReady(false);
      return;
    }
    // A new target path invalidates the previous preview (its lazy modules
    // may not be loaded yet), so drop the ready flag before loading.
    setReady(false);
    const matches = matchRoutes(previewRoutes, path);
    if (!matches) {
      setReady(true);
      return;
    }
    const loads = matches.map((m) =>
      typeof m.route.lazy === "function"
        ? m.route.lazy()
        : Promise.resolve(null)
    );
    Promise.all(loads)
      .then((results) => {
        if (cancelled) return;
        for (let i = 0; i < matches.length; i++) {
          const res = results[i] as { Component?: ComponentType } | null;
          if (res?.Component) matches[i].route.Component = res.Component;
        }
        setReady(true);
      })
      .catch(() => {
        if (!cancelled) setReady(true);
      });
    return () => {
      cancelled = true;
    };
  }, [previewRoutes, path]);

  const element = useRoutes(previewRoutes, path ?? NO_PREVIEW_PATH);
  return ready ? element : null;
}
