import { ChevronLeft } from "lucide-react";
import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { MobileHeader } from "@/components/mobile-header";
import { MobileTabBar } from "@/components/mobile-tab-bar";
import { DesktopSidebar } from "@/components/sidebar";
import { usePresenceHeartbeat } from "@/composables/use-presence-heartbeat";
import { toastManager } from "@/lib/toast";
import { useSwipeBack } from "@/lib/use-swipe-back";
import { reconcilePushSubscription, suppressRoute } from "@/lib/web-push";
import { routeNameForPath } from "@/router/route-index";
import { ROUTE_INFO } from "@/router/route-info";
import { useCurrentRoute } from "@/router/use-current-route";
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
  // Presence heartbeat: marks the signed-in user online every 30s and
  // refreshes the agent roster, so the chat page's green online badges stay
  // current. App-wide (not chat-route-scoped) on purpose — see the hook.
  usePresenceHeartbeat();
  // Mobile swipe-back: drag from the left edge to go back one level (thread
  // panel first, then the route's backTo target). Inert on desktop.
  const { rootRef, currentPageRef } = useSwipeBack();
  const currentRoute = useCurrentRoute();
  const { t } = useTranslation();

  // Peek title for the static swipe-back surface: the back-target page's
  // title resolved from route metadata (the target route is NOT mounted —
  // see the preview-retirement decision).
  const peekTitleKey = useMemo(() => {
    const backTo = currentRoute.name
      ? ROUTE_INFO[currentRoute.name]?.backTo
      : undefined;
    if (!backTo) return undefined;
    const targetName = routeNameForPath(backTo);
    return targetName ? ROUTE_INFO[targetName]?.titleKey : undefined;
  }, [currentRoute.name]);

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
    <div ref={rootRef} className="flex h-dvh overflow-hidden bg-background">
      <DesktopSidebar
        collapsed={collapsed}
        onToggleCollapse={toggleCollapsed}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Mobile header. */}
        <div className="fixed left-0 right-0 top-0 z-chrome lg:hidden">
          <MobileHeader />
        </div>
        <main className="relative flex-1 overflow-hidden pt-[var(--mobile-header-height)] pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))] lg:pt-0 lg:pb-0">
          {/* Static swipe-back peek surface: always mounted underneath the
              (opaque) page, revealed only while the page slides right during
              the gesture. Shows the back-target's title via route metadata —
              the target route is deliberately not mounted. */}
          <div
            aria-hidden
            className="absolute inset-0 z-0 flex flex-col items-center justify-center gap-2 bg-background pt-[var(--mobile-header-height)] pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))] lg:pt-0 lg:pb-0"
          >
            <ChevronLeft className="size-5" />
            {peekTitleKey && (
              <span className="max-w-full truncate px-6 text-sm text-control-light">
                {t(peekTitleKey)}
              </span>
            )}
          </div>
          <div
            ref={currentPageRef}
            className="relative z-10 h-full bg-background will-change-transform"
          >
            <Outlet />
          </div>
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
