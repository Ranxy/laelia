import { ChevronDown, ChevronRight } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RouterLink } from "@/components/router-link";
import { cn } from "@/lib/utils";
import {
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  MEMBERS_ROUTE,
} from "@/router/handles";
import { useCurrentRoute } from "@/router/use-current-route";
import {
  filterSidebarList,
  type SidebarItem,
  useSidebarItems,
} from "./sidebar-items";

// ---------------------------------------------------------------------------
// Active-route detection
// ---------------------------------------------------------------------------

function getItemClass(item: SidebarItem, currentRouteName: string): string {
  const isActive =
    item.name === currentRouteName ||
    currentRouteName.startsWith(`${item.name}.`);
  if (isActive) {
    return cn("router-link-active", "bg-link-hover");
  }
  // A section's list nav (e.g. machine.list) stays highlighted on that
  // section's detail/profile routes (machine.profile), since the list page is
  // the entry point for those sub-pages. We match the first path segment, so
  // only "*.list" items do this — leaf routes like settings.users are matched
  // exactly above and are not over-highlighted against their siblings.
  if (
    item.type === "route" &&
    item.name?.endsWith(".list") &&
    item.name.split(".")[0] === currentRouteName.split(".")[0]
  ) {
    return cn("router-link-active", "bg-link-hover");
  }
  // Opening an agent's commands view (reached from a member row or a machine
  // roster) highlights the Members nav item — Members is the flat contacts page
  // that replaced the old Agents list.
  if (
    item.name === MEMBERS_ROUTE &&
    (currentRouteName === COMMAND_ROUTE_LIST ||
      currentRouteName === COMMAND_ROUTE_DETAIL)
  ) {
    return cn("router-link-active", "bg-link-hover");
  }
  return "";
}

// ---------------------------------------------------------------------------
// Sidebar navigation (shared between desktop and mobile)
// ---------------------------------------------------------------------------

const routeClass =
  "group flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium text-main hover:bg-link-hover transition-colors";

export function SidebarNav({ collapsed }: { collapsed: boolean }) {
  const rawItems = useSidebarItems();
  const filteredItems = useMemo(() => filterSidebarList(rawItems), [rawItems]);
  const currentRoute = useCurrentRoute();
  const currentRouteName = currentRoute.name ?? "";

  const [expandedSet, setExpandedSet] = useState<Set<string>>(new Set());
  const manualToggledRef = useRef<Set<string>>(new Set());
  const autoExpandedRef = useRef<Set<string>>(new Set());

  const expandForActiveRoute = useCallback(
    (items: SidebarItem[]) => {
      setExpandedSet((prev) => {
        const next = new Set(prev);
        for (const key of autoExpandedRef.current) {
          next.delete(key);
        }
        autoExpandedRef.current = new Set();

        const walk = (list: SidebarItem[]) => {
          for (const item of list) {
            if (item.children && item.children.length > 0) {
              const hasActive = item.children.some(
                (child) =>
                  child.name === currentRouteName ||
                  currentRouteName.startsWith(`${child.name}.`)
              );
              if (hasActive && item.name) {
                next.add(item.name);
                autoExpandedRef.current.add(item.name);
              }
              walk(item.children);
            }
          }
        };
        walk(items);

        return next;
      });
    },
    [currentRouteName]
  );

  useEffect(() => {
    expandForActiveRoute(filteredItems);
  }, [expandForActiveRoute, filteredItems]);

  const toggleGroup = useCallback((name: string) => {
    setExpandedSet((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
        manualToggledRef.current.delete(name);
        autoExpandedRef.current.delete(name);
      } else {
        next.add(name);
        manualToggledRef.current.add(name);
      }
      return next;
    });
  }, []);

  const renderItem = (item: SidebarItem, _depth: number) => {
    if (item.type === "group") {
      const isExpanded =
        expandedSet.has(item.name ?? "") ||
        manualToggledRef.current.has(item.name ?? "");
      return (
        <div key={item.name}>
          <button
            type="button"
            aria-expanded={isExpanded}
            onClick={() => {
              if (item.name) toggleGroup(item.name);
            }}
            className={cn(
              "flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium text-control-light hover:bg-link-hover transition-colors",
              collapsed && "justify-center px-2"
            )}
          >
            {item.icon && <item.icon className="size-4 shrink-0" />}
            {!collapsed && (
              <>
                <span className="flex-1 text-left truncate">{item.title}</span>
                {isExpanded ? (
                  <ChevronDown className="size-4 shrink-0" />
                ) : (
                  <ChevronRight className="size-4 shrink-0" />
                )}
              </>
            )}
          </button>
          {isExpanded && item.children && !collapsed && (
            <div className="ml-3 mt-1 space-y-1 border-l border-control-border pl-3">
              {item.children.map((child) => renderItem(child, _depth + 1))}
            </div>
          )}
        </div>
      );
    }

    const activeClass = getItemClass(item, currentRouteName);
    return (
      <RouterLink
        key={item.name}
        name={item.name}
        className={cn(
          routeClass,
          activeClass,
          collapsed && "justify-center px-2"
        )}
      >
        {item.icon && <item.icon className="size-4 shrink-0" />}
        {!collapsed && <span className="truncate">{item.title}</span>}
      </RouterLink>
    );
  };

  return (
    <nav className="flex flex-col gap-1 px-2">
      {filteredItems.map((item) => renderItem(item, 0))}
    </nav>
  );
}
