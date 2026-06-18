import { Menu } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { DesktopSidebar, MobileSidebar } from "@/react/components/sidebar";
import { UserMenu } from "@/react/components/user-menu";

const COLLAPSED_KEY = "laelia-sidebar-collapsed";

function loadCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === "true";
  } catch {
    return false;
  }
}

export function DashboardLayout() {
  const [collapsed, setCollapsed] = useState(loadCollapsed);
  const [mobileOpen, setMobileOpen] = useState(false);
  const location = useLocation();

  const toggleCollapsed = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(COLLAPSED_KEY, String(next));
      } catch {
        // ignore
      }
      return next;
    });
  }, []);

  // Close mobile sidebar on route change
  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname]);

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <DesktopSidebar
        collapsed={collapsed}
        onToggleCollapse={toggleCollapsed}
      />
      <MobileSidebar open={mobileOpen} onClose={() => setMobileOpen(false)} />
      <div className="flex flex-1 flex-col overflow-hidden">
        <header className="flex h-12 shrink-0 items-center gap-4 border-b border-control-border px-4">
          <button
            type="button"
            className="rounded-md p-1 text-control hover:bg-link-hover lg:hidden"
            onClick={() => setMobileOpen(true)}
          >
            <Menu className="size-4" />
          </button>
          <div className="flex-1" />
          <UserMenu />
        </header>
        <main className="flex-1 overflow-hidden">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
