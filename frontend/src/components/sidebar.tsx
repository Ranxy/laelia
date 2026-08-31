import { Menu } from "lucide-react";
import { UserMenu } from "@/components/user-menu";
import { cn } from "@/lib/utils";
import { SidebarNav } from "./sidebar-nav";

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
      <div className="shrink-0 border-t border-control-border p-2">
        <UserMenu collapsed={collapsed} />
      </div>
    </aside>
  );
}
