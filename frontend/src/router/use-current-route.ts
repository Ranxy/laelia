import { useLocation, useMatches, useParams } from "react-router-dom";

export interface ReactRoute {
  name?: string;
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
  const leafHandle = matches.at(-1)?.handle as { name?: string } | undefined;
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
