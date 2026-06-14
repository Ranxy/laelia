import type { RouteObject } from "react-router-dom";

let nameIndex = new Map<string, string>();

export function setRouteNameIndex(index: Map<string, string>): void {
  nameIndex = index;
}

function joinPath(parent: string, child: string): string {
  if (!child) return parent || "/";
  const left = parent.endsWith("/") ? parent.slice(0, -1) : parent;
  const right = child.startsWith("/") ? child : `/${child}`;
  return `${left}${right}` || "/";
}

export function buildRouteNameIndex(
  list: RouteObject[],
  parentPath = ""
): Map<string, string> {
  const index = new Map<string, string>();
  for (const route of list) {
    const segment = route.path ?? "";
    const fullPath = route.path?.startsWith("/")
      ? segment
      : joinPath(parentPath, segment);
    const name = (route.handle as { name?: string } | undefined)?.name;
    if (name && !index.has(name)) {
      index.set(name, fullPath || "/");
    }
    if (route.children) {
      for (const [childName, childPath] of buildRouteNameIndex(
        route.children,
        fullPath
      )) {
        if (!index.has(childName)) index.set(childName, childPath);
      }
    }
  }
  return index;
}

export type NavQuery = Record<string, unknown>;

export function buildSearchString(query: NavQuery): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item !== undefined && item !== null)
          search.append(key, String(item));
      }
    } else {
      search.set(key, String(value));
    }
  }
  return search.toString();
}

export function resolvePath(
  name: string,
  params?: Record<string, string | string[] | undefined>,
  query?: NavQuery
): string {
  const pattern = nameIndex.get(name);
  if (!pattern) {
    console.warn(`resolvePath: no route registered for name "${name}"`);
    return "/";
  }
  let path = pattern;
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value === undefined) continue;
      const single = Array.isArray(value) ? (value[0] ?? "") : value;
      path = path.replace(
        new RegExp(`:${key}(?![A-Za-z0-9_])`, "g"),
        encodeURIComponent(single)
      );
    }
  }
  if (query) {
    const qs = buildSearchString(query);
    if (qs) path = `${path}?${qs}`;
  }
  return path;
}
