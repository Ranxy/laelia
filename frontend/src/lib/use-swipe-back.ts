import { useCallback, useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { ROUTE_INFO } from "@/router/route-info";
import { useCurrentRoute } from "@/router/use-current-route";
import { setSuppressLoadingFlags, useAppStore } from "@/stores";
import { useIsDesktop } from "./use-is-desktop";

// iOS-style interactive back gesture for mobile: drag from the left edge of
// the screen to the right. The current page follows the finger and the
// back-target route is rendered underneath (previewPath) so the destination is
// visible while dragging; releasing past the threshold slides the current page
// out and commits the navigation. The level stack is:
//   thread panel (full-screen overlay) -> current route -> its backTo target.
// The gesture is inert on desktop, on top-level tab routes (nothing to go
// back to), and over layer overlays (sheets/dialogs/previews dismiss on
// their own).
const EDGE_SIZE = 24; // px from the left edge where the gesture may start
const DIRECTION_LOCK = 10; // px of movement before the gesture is decided
const MAX_DRAG_RATIO = 0.5; // the page may slide up to half the viewport
const TRIGGER_RATIO = 0.25; // release past 25% of the viewport commits
const MIN_TRIGGER_PX = 80;
const SNAP_MS = 200; // spring-back animation
const COMMIT_MS = 250; // slide-out animation before the navigation commits
// Safety timeout: if the data router's navigation doesn't complete within this
// window, force a reset so the gesture state doesn't get stuck.
const RESET_TIMEOUT_MS = 1000;

export interface SwipeBackState {
  // Bind to the layout root (CSS variables for the thread panel live here).
  rootRef: (el: HTMLDivElement | null) => void;
  // Bind to the current page container; it is translated while dragging.
  currentPageRef: (el: HTMLDivElement | null) => void;
  // Back-target path rendered underneath while a route-level gesture is
  // active; null when idle (or when the gesture targets the thread panel).
  previewPath: string | null;
}

export function useSwipeBack(): SwipeBackState {
  const isDesktop = useIsDesktop();
  const navigate = useNavigate();
  const location = useLocation();
  const closeThread = useAppStore((s) => s.closeThread);
  const activeThreadRoot = useAppStore((s) => s.activeThreadRoot);
  const currentRoute = useCurrentRoute();

  const rootRef = useRef<HTMLDivElement | null>(null);
  const pageRef = useRef<HTMLDivElement | null>(null);
  const [previewPath, setPreviewPath] = useState<string | null>(null);
  // Set when a route-level commit's navigate() has been called; the
  // location-change effect clears the transform + preview once the data
  // router finishes the navigation (preventing a one-frame flash of the old
  // route before the new one renders).
  const pendingResetRef = useRef(false);

  const backTargetRef = useRef<string | null>(null);
  backTargetRef.current = currentRoute.name
    ? (ROUTE_INFO[currentRoute.name]?.backTo ?? null)
    : null;
  const threadActiveRef = useRef(false);
  threadActiveRef.current = activeThreadRoot != null;

  const setRoot = useCallback((el: HTMLDivElement | null) => {
    rootRef.current = el;
  }, []);
  const setPage = useCallback((el: HTMLDivElement | null) => {
    pageRef.current = el;
  }, []);

  useEffect(() => {
    if (isDesktop) return;
    const root = rootRef.current;
    if (!root) return;

    let startX = 0;
    let startY = 0;
    let dragging = false;
    let decided = false;
    let cancelled = false;
    let mode: "thread" | "route" | null = null;
    let maxDrag = 0;
    let timers: number[] = [];

    const clearTimers = () => {
      for (const t of timers) window.clearTimeout(t);
      timers = [];
    };

    const reset = () => {
      dragging = false;
      decided = false;
      cancelled = false;
      mode = null;
      clearTimers();
      if (pageRef.current) {
        pageRef.current.style.transition = "";
        pageRef.current.style.transform = "";
      }
      root.style.removeProperty("--swipe-offset");
      root.style.removeProperty("--swipe-transition");
      window.setTimeout(() => setSuppressLoadingFlags(false), 500);
      setPreviewPath(null);
    };

    const onTouchStart = (e: TouchEvent) => {
      const target = e.target as HTMLElement | null;
      if (target?.closest?.("[data-bb-layer-family]")) {
        cancelled = true;
        return;
      }
      const touch = e.touches[0];
      if (!touch || touch.clientX > EDGE_SIZE) {
        cancelled = true;
        return;
      }
      startX = touch.clientX;
      startY = touch.clientY;
      dragging = false;
      decided = false;
      cancelled = false;
      maxDrag = window.innerWidth * MAX_DRAG_RATIO;
      clearTimers();
      if (threadActiveRef.current) {
        mode = "thread";
        root.style.setProperty("--swipe-transition", "none");
      } else if (backTargetRef.current) {
        mode = "route";
        setSuppressLoadingFlags(true);
        setPreviewPath(backTargetRef.current);
        if (pageRef.current) pageRef.current.style.transition = "none";
      } else {
        cancelled = true;
        return;
      }
    };

    const onTouchMove = (e: TouchEvent) => {
      if (cancelled || !mode) return;
      const touch = e.touches[0];
      if (!touch) return;
      const dx = touch.clientX - startX;
      const dy = touch.clientY - startY;
      if (!decided) {
        if (Math.abs(dx) < DIRECTION_LOCK && Math.abs(dy) < DIRECTION_LOCK)
          return;
        decided = true;
        if (dy > dx || dx < 0) {
          cancelled = true;
          reset();
          return;
        }
        dragging = true;
      }
      if (!dragging) return;
      e.preventDefault();
      const offset = Math.min(Math.max(0, dx), maxDrag);
      if (mode === "route" && pageRef.current) {
        pageRef.current.style.transform = `translateX(${offset}px)`;
      } else if (mode === "thread") {
        root.style.setProperty("--swipe-offset", `${offset}px`);
      }
    };

    const finish = (commit: boolean) => {
      if (!dragging) return;
      dragging = false;
      const width = window.innerWidth;
      const ms = commit ? COMMIT_MS : SNAP_MS;
      if (mode === "route" && pageRef.current) {
        const page = pageRef.current;
        page.style.transition = `transform ${ms}ms ease-out`;
        page.style.transform = `translateX(${commit ? width : 0}px)`;
        timers.push(
          window.setTimeout(() => {
            if (commit) {
              const target = backTargetRef.current;
              if (target) {
                // Start the navigation but DON'T reset yet — the
                // location-change effect will reset once the data router
                // finishes the navigation. This prevents a one-frame flash
                // where the old route is visible at translateX(0) before the
                // new route renders.
                pendingResetRef.current = true;
                navigate(target, { replace: true });
                // Safety: force a reset if the navigation doesn't complete.
                timers.push(
                  window.setTimeout(() => {
                    if (pendingResetRef.current) reset();
                  }, RESET_TIMEOUT_MS)
                );
                return;
              }
            }
            reset();
          }, ms + 50)
        );
      } else if (mode === "thread") {
        root.style.setProperty(
          "--swipe-transition",
          `transform ${ms}ms ease-out`
        );
        root.style.setProperty("--swipe-offset", `${commit ? width : 0}px`);
        timers.push(
          window.setTimeout(() => {
            if (commit) closeThread();
            reset();
          }, ms + 50)
        );
      }
    };

    const onTouchEnd = (e: TouchEvent) => {
      if (!dragging) {
        if (mode) reset();
        return;
      }
      const touch = e.changedTouches[0];
      const dx = touch ? touch.clientX - startX : 0;
      const width = window.innerWidth;
      const commit = dx > Math.max(MIN_TRIGGER_PX, width * TRIGGER_RATIO);
      finish(commit);
    };

    const onTouchCancel = () => {
      if (!dragging) {
        if (mode) reset();
        return;
      }
      finish(false);
    };

    window.addEventListener("touchstart", onTouchStart, { passive: true });
    window.addEventListener("touchmove", onTouchMove, { passive: false });
    window.addEventListener("touchend", onTouchEnd, { passive: true });
    window.addEventListener("touchcancel", onTouchCancel, { passive: true });
    return () => {
      window.removeEventListener("touchstart", onTouchStart);
      window.removeEventListener("touchmove", onTouchMove);
      window.removeEventListener("touchend", onTouchEnd);
      window.removeEventListener("touchcancel", onTouchCancel);
      clearTimers();
    };
  }, [isDesktop, navigate, closeThread]);

  // When a route-level commit is pending, wait for the data router to finish
  // the navigation (location changes) before clearing the transform and
  // unmounting the preview. This prevents a one-frame flash where the old
  // route would be visible at translateX(0) before the new route renders.
  useEffect(() => {
    if (!pendingResetRef.current) return;
    pendingResetRef.current = false;
    if (pageRef.current) {
      pageRef.current.style.transition = "";
      pageRef.current.style.transform = "";
    }
    const root = rootRef.current;
    if (root) {
      root.style.removeProperty("--swipe-offset");
      root.style.removeProperty("--swipe-transition");
    }
    window.setTimeout(() => setSuppressLoadingFlags(false), 500);
    setPreviewPath(null);
  }, [location.pathname]);

  return { rootRef: setRoot, currentPageRef: setPage, previewPath };
}
