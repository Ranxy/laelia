import {
  Activity as ActivityIcon,
  ChevronDown,
  ChevronRight,
  Home,
  type LucideIcon,
  Menu,
  Monitor,
  Settings,
  Users,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { RouterLink } from "@/components/router-link";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "@/components/ui/layer";
import { cn } from "@/lib/utils";
import {
  ACTIVITY_ROUTE,
  CHAT_ROUTE,
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  MACHINE_ROUTE_LIST,
  MEMBERS_ROUTE,
  SETTINGS_ROUTE,
  SETTINGS_ROUTE_CHAT,
  SETTINGS_ROUTE_IAM,
  SETTINGS_ROUTE_NOTIFICATIONS,
  SETTINGS_ROUTE_ROLES,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
} from "@/router/handles";
import { useCurrentRoute } from "@/router/use-current-route";
import { useHasPermission } from "@/stores/auth";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SidebarItem {
  title?: string;
  name?: string;
  icon?: LucideIcon;
  hide?: boolean;
  type: "route" | "group";
  children?: SidebarItem[];
}

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
// Sidebar item list builder
// ---------------------------------------------------------------------------

function useSidebarItems(): SidebarItem[] {
  const { t } = useTranslation();
  // Gate each Settings sub-item on the permission its page actually needs.
  // An ordinary workspace member (roles/workspaceMember baseline) holds none of
  // these, so every child is hidden for them and filterSidebarList then drops
  // the now-empty Settings group entirely — leaving no settings affordance in
  // the sidebar for non-admins. Per-resource perms are not considered here.
  const canViewStorage =
    useHasPermission("laelia.settings.get") ||
    useHasPermission("laelia.settings.update");
  const canViewUsers = useHasPermission("laelia.users.list");
  const canViewRoles = useHasPermission("laelia.roles.list");
  const canViewIam = useHasPermission("laelia.iam.getPolicy");

  return useMemo(
    (): SidebarItem[] => [
      {
        title: t("sidebar.home"),
        icon: Home,
        name: CHAT_ROUTE,
        type: "route",
      },
      {
        title: t("sidebar.activity"),
        icon: ActivityIcon,
        name: ACTIVITY_ROUTE,
        type: "route",
      },
      {
        title: t("sidebar.machines"),
        icon: Monitor,
        name: MACHINE_ROUTE_LIST,
        type: "route",
      },
      {
        title: t("sidebar.members"),
        icon: Users,
        name: MEMBERS_ROUTE,
        type: "route",
      },
      {
        title: t("sidebar.settings"),
        icon: Settings,
        name: SETTINGS_ROUTE,
        type: "group",
        children: [
          {
            title: t("sidebar.settings-storage"),
            name: SETTINGS_ROUTE_STORAGE,
            type: "route",
            hide: !canViewStorage,
          },
          {
            title: t("sidebar.settings-chat"),
            name: SETTINGS_ROUTE_CHAT,
            type: "route",
          },
          {
            title: t("sidebar.settings-notifications"),
            name: SETTINGS_ROUTE_NOTIFICATIONS,
            type: "route",
          },
          {
            title: t("sidebar.settings-users"),
            name: SETTINGS_ROUTE_USERS,
            type: "route",
            hide: !canViewUsers,
          },
          {
            title: t("sidebar.settings-roles"),
            name: SETTINGS_ROUTE_ROLES,
            type: "route",
            hide: !canViewRoles,
          },
          {
            title: t("sidebar.settings-iam"),
            name: SETTINGS_ROUTE_IAM,
            type: "route",
            hide: !canViewIam,
          },
        ],
      },
    ],
    [t, canViewStorage, canViewUsers, canViewRoles, canViewIam]
  );
}

// ---------------------------------------------------------------------------
// Filter logic
// ---------------------------------------------------------------------------

function filterSidebarList(items: SidebarItem[]): SidebarItem[] {
  return items
    .map((item) => ({
      ...item,
      children: (item.children ?? []).filter((child) => !child.hide),
    }))
    .filter((item) => {
      if (item.hide) return false;
      if (item.children && item.children.length > 0) return true;
      if (item.type === "group") return false;
      return !!item.name;
    });
}

// ---------------------------------------------------------------------------
// Sidebar navigation (shared between desktop and mobile)
// ---------------------------------------------------------------------------

const routeClass =
  "group flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium text-main hover:bg-link-hover transition-colors";

function SidebarNav({ collapsed }: { collapsed: boolean }) {
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

// ---------------------------------------------------------------------------
// Desktop sidebar
// ---------------------------------------------------------------------------

export function DesktopSidebar({
  collapsed,
  onToggleCollapse,
}: {
  collapsed: boolean;
  onToggleCollapse: () => void;
}) {
  return (
    <aside
      className={cn(
        "hidden lg:flex flex-col border-r border-control-border bg-background transition-all duration-300",
        collapsed ? "w-14" : "w-60"
      )}
    >
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-control-border px-4">
        {!collapsed && (
          <span className="text-sm font-semibold text-main truncate">
            Laelia AI
          </span>
        )}
        <button
          type="button"
          className={cn(
            "rounded-md p-1 text-control hover:bg-link-hover",
            collapsed && "mx-auto"
          )}
          onClick={onToggleCollapse}
        >
          <Menu className="size-4" />
        </button>
      </div>
      <div className="flex-1 overflow-y-auto py-2">
        <SidebarNav collapsed={collapsed} />
      </div>
    </aside>
  );
}

// ---------------------------------------------------------------------------
// Mobile sidebar overlay (portaled into overlay layer root)
// ---------------------------------------------------------------------------

export function MobileSidebar({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  return createPortal(
    <div
      className={cn(
        "fixed inset-0 lg:hidden",
        LAYER_SURFACE_CLASS,
        open ? "" : "pointer-events-none"
      )}
    >
      <button
        type="button"
        aria-label={t("common.close")}
        className={cn(
          "absolute inset-0 bg-overlay/50 transition-opacity",
          open ? "opacity-100" : "opacity-0"
        )}
        onClick={onClose}
      />
      <div
        className={cn(
          "absolute inset-y-0 left-0 w-60 bg-background shadow-lg transition-transform",
          open ? "translate-x-0" : "-translate-x-full"
        )}
      >
        <div className="flex h-12 shrink-0 items-center gap-2 border-b border-control-border px-4">
          <button
            type="button"
            className="rounded-md p-1 text-control hover:bg-link-hover"
            onClick={onClose}
          >
            <Menu className="size-4" />
          </button>
          <span className="text-sm font-semibold text-main">Laelia AI</span>
        </div>
        <div className="flex-1 overflow-y-auto py-2">
          <SidebarNav collapsed={false} />
        </div>
      </div>
    </div>,
    getLayerRoot("overlay")
  );
}
