import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { MobileHeader } from "@/components/mobile-header";
import { MobileTabBar } from "@/components/mobile-tab-bar";
import { DesktopSidebar } from "@/components/sidebar";
import { UserMenu } from "@/components/user-menu";
import { toastManager } from "@/lib/toast";
import { reconcilePushSubscription, suppressRoute } from "@/lib/web-push";
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
    <div className="flex h-screen overflow-hidden bg-background">
      <DesktopSidebar
        collapsed={collapsed}
        onToggleCollapse={toggleCollapsed}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Desktop header. */}
        <header className="hidden lg:flex h-12 shrink-0 items-center gap-4 border-b border-control-border px-4">
          <div className="flex-1" />
          <UserMenu />
        </header>
        {/* Mobile header. */}
        <div className="fixed left-0 right-0 top-0 z-chrome lg:hidden">
          <MobileHeader />
        </div>
        <main className="flex-1 overflow-hidden pt-[var(--mobile-header-height)] pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom))] lg:pt-0 lg:pb-0">
          <Outlet />
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
