import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { resolvePath } from "@/router/route-index";

export interface DetailTab {
  /** URL segment that identifies the tab; also used as the Tabs value. */
  key: string;
  icon: LucideIcon;
  /** i18n key of the tab label. */
  labelKey: string;
  /** Named route the trigger navigates to (see router/handles). */
  route: string;
  /** The trigger renders only while this holds (e.g. canEdit/canManage gates). */
  gate?: boolean;
}

interface DetailTabsLayoutProps {
  tabs: DetailTab[];
  /**
   * Name of the URL param holding the selected id (e.g. "agentId"); the URL
   * segment right after it picks the active tab, unknown segments fall back
   * to the first tab.
   */
  idParam: string;
  /** Optional row above the tab bar (e.g. the machine identity header). */
  header?: ReactNode;
  /** Optional trailing content in the tab-bar row (e.g. the agent message action). */
  tabsTrailing?: ReactNode;
  /** Optional content rendered after the tabs root (e.g. the mobile action FAB). */
  footer?: ReactNode;
}

// DetailTabsLayout is the shared tabbed detail skeleton (tab bar navigated via
// named route handles + an Outlet for the active child route). Data fetching
// and permission stays with the caller — pass a tab's `gate` to render its
// trigger only while the caller's freshly fetched gate holds, keeping the
// active-tab resolution itself gate-independent.
export function DetailTabsLayout({
  tabs,
  idParam,
  header,
  tabsTrailing,
  footer,
}: DetailTabsLayoutProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const params = useParams();
  const id = params[idParam];

  // Derive the active tab from the URL so deep links, refresh, and
  // back/forward keep the highlight in sync with the rendered child route.
  const activeTab = useMemo(() => {
    const segments = location.pathname.split("/").filter(Boolean);
    // /…/:<idParam>/<tab?> — the segment right after the id.
    const afterId = segments[segments.indexOf(id ?? "") + 1];
    return tabs.find((tab) => tab.key === afterId)?.key ?? tabs[0]?.key;
  }, [location.pathname, id, tabs]);

  return (
    <div className="flex h-full flex-col overflow-hidden">
      {header}
      <Tabs value={activeTab} className="flex h-full flex-col overflow-hidden">
        <div className="shrink-0 border-b border-control-border">
          <div className="flex items-end gap-2 px-4 pt-2 lg:px-6">
            <TabsList className="gap-x-6 border-b-0!">
              {tabs.map((tab) =>
                tab.gate === false ? null : (
                  <TabsTrigger
                    key={tab.key}
                    value={tab.key}
                    className="px-1"
                    onClick={() =>
                      navigate(resolvePath(tab.route, { [idParam]: id }))
                    }
                  >
                    <tab.icon className="size-4" />
                    {t(tab.labelKey)}
                  </TabsTrigger>
                )
              )}
            </TabsList>
            {tabsTrailing}
          </div>
        </div>
        <div className="flex-1 overflow-hidden">
          <Outlet />
        </div>
      </Tabs>
      {footer}
    </div>
  );
}
