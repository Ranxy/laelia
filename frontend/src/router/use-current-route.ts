import { useLocation, useMatches, useParams } from "react-router-dom";
import type { RouteName } from "./route-info";

export interface ReactRoute {
  // The leaf route's handle name. Sourced from the route tree whose handles
  // are `satisfies RouteHandle`, so it is a known route name (or undefined
  // for routes without a handle, e.g. the legacy redirect stubs).
  name?: RouteName;
  fullPath: string;
  hash: string;
  params: Record<string, string | string[] | undefined>;
  query: Record<string, unknown>;
}

function assembleRoute(
  location: ReturnType<typeof useLocation>,
  matches: ReturnType<typeof useMatches>,
  params: ReturnType<typeof useParams>
): ReactRoute {
  const leafHandle = matches.at(-1)?.handle as { name?: RouteName } | undefined;
  return {
    name: leafHandle?.name,
    fullPath: `${location.pathname}${location.search}${location.hash}`,
    hash: location.hash,
    params: params as Record<string, string | string[] | undefined>,
    query: Object.fromEntries(new URLSearchParams(location.search)),
  };
}

export function useCurrentRoute(): ReactRoute {
  const location = useLocation();
  const params = useParams();
  const matches = useMatches();
  return assembleRoute(location, matches, params);
}
