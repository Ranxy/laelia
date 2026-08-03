import { Menu } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useState } from "react";
import { Outlet, useLocation, useNavigate } from "react-router-dom";
import { DesktopSidebar, MobileSidebar } from "@/components/sidebar";
import { UserMenu } from "@/components/user-menu";
import { toastManager } from "@/lib/toast";
import { reSubscribeIfEnabled, suppressRoute } from "@/lib/web-push";
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
  const open = useAppStore((s) => s.activePreview != null);
  return open ? <MarkdownPreviewOverlay /> : null;
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
  const [mobileOpen, setMobileOpen] = useState(false);
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

  // Close mobile sidebar on route change
  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname]);

  // Web Push: re-establish the push subscription on boot if the user previously
  // enabled it (handles browser-rotated subscriptions across reloads), tell the
  // service worker which conversation the page is currently viewing so pushes
  // for it are suppressed (the user is already looking at them), and listen for
  // PUSH_SUPPRESSED / NOTIFICATION_CLICK messages from the SW.
  useEffect(() => {
    void reSubscribeIfEnabled();
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
      {/* Store-driven preview overlays (lazy — load only when opened). */}
      <Suspense fallback={null}>
        <MarkdownPreviewGate />
        <ImagePreviewGate />
        {/* Admin onboarding: prompts admins to finish required config. */}
        <SetupChecklistGate />
      </Suspense>
    </div>
  );
}
