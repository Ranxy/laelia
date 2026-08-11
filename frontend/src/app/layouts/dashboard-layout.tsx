import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  matchRoutes,
  Outlet,
  useLocation,
  useNavigate,
} from "react-router-dom";
import { MobileHeader } from "@/components/mobile-header";
import { MobileTabBar } from "@/components/mobile-tab-bar";
import { DesktopSidebar } from "@/components/sidebar";
import { toastManager } from "@/lib/toast";
import { useSwipeBack } from "@/lib/use-swipe-back";
import { reconcilePushSubscription, suppressRoute } from "@/lib/web-push";
import { ROUTE_INFO } from "@/router/route-info";
import { dashboardChildrenRoutes } from "@/router/routes/dashboard";
import {
  preloadPreviewRoute,
  usePreviewRoutes,
} from "@/router/use-preview-routes";
import { useAppStore } from "@/stores";

// The overlays/dialog are code-split so markstream-react (and the
// stream-markdown grammar registry it pulls in) stays out of the initial entry
// chunk: the shell only loads a chunk when a preview/lightbox is actually
// open, or when an admin loads the setup checklist. Chat pages pull markstream
// in their own lazy route chunks, so it is never part of first paint.
const MarkdownPreviewOverlay = lazy(() =>
  import("@/components/preview/markdown-preview-overlay").then((m) => ({
    default: m.MarkdownPreviewOverlay,
  }))
);
const HtmlPreviewOverlay = lazy(() =>
  import("@/components/preview/html-preview-overlay").then((m) => ({
    default: m.HtmlPreviewOverlay,
  }))
);
const ImagePreviewOverlay = lazy(() =>
  import("@/components/preview/image-preview-overlay").then((m) => ({
    default: m.ImagePreviewOverlay,
  }))
);
const SetupChecklistDialog = lazy(() =>
  import("@/components/setup-checklist-dialog").then((m) => ({
    default: m.SetupChecklistDialog,
  }))
);

// Each gate renders the lazy overlay only while its store state is active, so
// the underlying chunk loads on first use instead of on boot.
function MarkdownPreviewGate() {
  const open = useAppStore((s) => s.activePreview?.kind === "markdown");
  return open ? <MarkdownPreviewOverlay /> : null;
}

function HtmlPreviewGate() {
  const open = useAppStore((s) => s.activePreview?.kind === "html");
  return open ? <HtmlPreviewOverlay /> : null;
}

function ImagePreviewGate() {
  const open = useAppStore((s) => s.activeImage != null);
  return open ? <ImagePreviewOverlay /> : null;
}

function SetupChecklistGate() {
  const isAdmin = useAppStore(
    (s) => s.currentUser?.permissions?.includes("laelia.settings.get") ?? false
  );
  return isAdmin ? <SetupChecklistDialog /> : null;
}

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
  const location = useLocation();
  const navigate = useNavigate();
  // Mobile swipe-back: drag from the left edge to go back one level (thread
  // panel first, then the route's backTo target). Inert on desktop.
  const { rootRef, currentPageRef, previewPath } = useSwipeBack();
  // The back-target route rendered underneath the current page while the
  // gesture is active, so the destination is visible during the drag.
  const previewElement = usePreviewRoutes(dashboardChildrenRoutes, previewPath);

  // While previewing, the mobile header shows the destination page's title
  // (the header visually belongs to the page underneath the drag).
  const previewTitleKey = useMemo(() => {
    if (!previewPath) return undefined;
    const matches = matchRoutes(dashboardChildrenRoutes, previewPath);
    const leaf = matches?.at(-1);
    const name = (leaf?.route.handle as { name?: string } | undefined)?.name;
    return name ? ROUTE_INFO[name]?.titleKey : undefined;
  }, [previewPath]);

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

  // Preload the lazy modules for every swipe-back target route at mount so
  // the preview renders on the very first frame when a gesture starts — the
  // module cache is already populated, cloneRouteTree sets Component
  // synchronously, and useRoutes renders the component without waiting for a
  // microtask/useSyncExternalStore re-render cycle.
  useEffect(() => {
    const backTargets = new Set<string>();
    for (const info of Object.values(ROUTE_INFO)) {
      if (info.backTo) backTargets.add(info.backTo);
    }
    for (const target of backTargets) {
      preloadPreviewRoute(dashboardChildrenRoutes, target);
    }
  }, []);

  // Web Push: on boot, refresh the server-side keys for this browser's push
  // subscription when it is already registered (browsers rotate keys across
  // reloads), tell the service worker which conversation the page is currently
  // viewing so pushes for it are suppressed (the user is already looking at
  // them), and listen for PUSH_SUPPRESSED / NOTIFICATION_CLICK messages.
  useEffect(() => {
    void reconcilePushSubscription();
  }, []);

  useEffect(() => {
    // The conversation route is "/{conversationId}"; sending any pathname is
    // safe — the SW only suppresses when a push's route matches it exactly.
    void suppressRoute(location.pathname);
  }, [location.pathname]);

  useEffect(() => {
    function onMessage(event: MessageEvent) {
      const data = event.data;
      if (!data || typeof data !== "object") return;
      if (data.type === "PUSH_SUPPRESSED" && data.payload) {
        const payload = data.payload as {
          title?: string;
          body?: string;
        };
        toastManager.add({
          type: "info",
          title: payload.title,
          description: payload.body,
        });
      } else if (data.type === "NOTIFICATION_CLICK" && data.route) {
        navigate(data.route);
      }
    }
    navigator.serviceWorker?.addEventListener("message", onMessage);
    return () => {
      navigator.serviceWorker?.removeEventListener("message", onMessage);
    };
  }, [navigate]);

  return (
    <div ref={rootRef} className="flex h-screen overflow-hidden bg-background">
      <DesktopSidebar
        collapsed={collapsed}
        onToggleCollapse={toggleCollapsed}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Mobile header. */}
        <div className="fixed left-0 right-0 top-0 z-chrome lg:hidden">
          <MobileHeader previewTitleKey={previewTitleKey} />
        </div>
        <main className="relative flex-1 overflow-hidden pt-[var(--mobile-header-height)] pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))] lg:pt-0 lg:pb-0">
          <div
            ref={currentPageRef}
            className="relative z-10 h-full bg-background will-change-transform"
          >
            <Outlet />
          </div>
          {previewPath && (
            <div className="absolute inset-0 z-0 bg-background will-change-transform pt-[var(--mobile-header-height)] pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))] lg:pt-0 lg:pb-0">
              <Suspense fallback={null}>{previewElement}</Suspense>
            </div>
          )}
        </main>
        <div className="fixed bottom-0 left-0 right-0 z-chrome lg:hidden">
          <MobileTabBar />
        </div>
      </div>
      {/* Store-driven preview overlays (lazy — load only when opened). */}
      <Suspense fallback={null}>
        <MarkdownPreviewGate />
        <HtmlPreviewGate />
        <ImagePreviewGate />
        {/* Admin onboarding: prompts admins to finish required config. */}
        <SetupChecklistGate />
      </Suspense>
    </div>
  );
}
