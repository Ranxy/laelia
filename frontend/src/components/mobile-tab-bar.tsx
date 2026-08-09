import { Activity, Home, Settings, Users } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate } from "react-router-dom";
import { cn } from "@/lib/utils";

interface TabItem {
  path: string;
  labelKey: string;
  icon: typeof Home;
}

const TABS: TabItem[] = [
  { path: "/", labelKey: "sidebar.home", icon: Home },
  { path: "/activity", labelKey: "sidebar.activity", icon: Activity },
  { path: "/members", labelKey: "sidebar.members", icon: Users },
  { path: "/settings", labelKey: "sidebar.settings", icon: Settings },
];

function isTabActive(tab: TabItem, pathname: string): boolean {
  if (tab.path === "/") {
    return (
      pathname === "/" ||
      (!pathname.startsWith("/activity") &&
        !pathname.startsWith("/members") &&
        !pathname.startsWith("/agents") &&
        !pathname.startsWith("/settings") &&
        !pathname.startsWith("/machines"))
    );
  }
  return pathname === tab.path || pathname.startsWith(`${tab.path}/`);
}

export function MobileTabBar() {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const pathname = location.pathname;

  return (
    <nav
      aria-label={t("mobile.tab-bar")}
      className="shrink-0 border-t border-control-border bg-background lg:hidden"
      style={{
        height: "calc(var(--mobile-tab-height) + var(--mobile-safe-bottom))",
        paddingBottom: "var(--mobile-safe-bottom)",
      }}
    >
      <ul className="grid h-[var(--mobile-tab-height)] grid-cols-4">
        {TABS.map((tab) => {
          const Icon = tab.icon;
          const active = isTabActive(tab, pathname);
          return (
            <li key={tab.path} className="flex">
              <button
                type="button"
                onClick={() => navigate(tab.path)}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex flex-1 flex-col items-center justify-center gap-0.5",
                  "text-[11px] font-medium transition-colors",
                  active
                    ? "text-accent"
                    : "text-control-light hover:text-control"
                )}
              >
                <Icon className="size-5" strokeWidth={active ? 2.25 : 2} />
                <span>{t(tab.labelKey)}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
