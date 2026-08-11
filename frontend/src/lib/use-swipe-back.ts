import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { ROUTE_INFO } from "@/router/route-info";
import { useCurrentRoute } from "@/router/use-current-route";
import { useAppStore } from "@/stores";
import { useIsDesktop } from "./use-is-desktop";

// iOS-style interactive back gesture for mobile: drag from the left edge of
// the screen to the right; the shell follows the finger and releasing past
// the threshold goes back exactly one level. The level stack is:
//   thread panel (full-screen overlay) -> current route -> its backTo target.
// The gesture is inert on desktop, on top-level tab routes (nothing to go
// back to), and over layer overlays (sheets/dialogs/previews dismiss on
// their own).
const EDGE_SIZE = 24; // px from the left edge where the gesture may start
const DIRECTION_LOCK = 10; // px of movement before the gesture is decided
const MAX_DRAG_RATIO = 0.5; // the shell may slide up to half the viewport
const TRIGGER_RATIO = 0.25; // release past 25% of the viewport commits
const MIN_TRIGGER_PX = 80;
const SNAP_MS = 200; // spring-back / commit animation

export function useSwipeBack() {
  const isDesktop = useIsDesktop();
  const navigate = useNavigate();
  const closeThread = useAppStore((s) => s.closeThread);
  const activeThreadRoot = useAppStore((s) => s.activeThreadRoot);
  const currentRoute = useCurrentRoute();

  const rootRef = useRef<HTMLDivElement | null>(null);
  // Latest back action, read by the window listeners without re-binding them
  // on every render (route/store changes would otherwise churn listeners).
  const backActionRef = useRef<() => void>(() => {});
  backActionRef.current = () => {
    if (activeThreadRoot) {
      closeThread();
      return;
    }
    const info = currentRoute.name ? ROUTE_INFO[currentRoute.name] : undefined;
    if (info?.backTo) navigate(info.backTo);
  };

  useEffect(() => {
    if (isDesktop) return;
    const el = rootRef.current;
    if (!el) return;

    let startX = 0;
    let startY = 0;
    let dragging = false;
    let decided = false;
    let cancelled = false;
    let maxDrag = 0;
    let snapTimer: number | undefined;

    const clearSnap = () => {
      if (snapTimer !== undefined) {
        window.clearTimeout(snapTimer);
        snapTimer = undefined;
      }
    };

    const onTouchStart = (e: TouchEvent) => {
      // Touches over layer overlays (sheets, dialogs, previews) keep their own
      // dismissal; the gesture must not navigate behind them.
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
      clearSnap();
      el.style.transition = "none";
    };

    const onTouchMove = (e: TouchEvent) => {
      if (cancelled) return;
      const touch = e.touches[0];
      if (!touch) return;
      const dx = touch.clientX - startX;
      const dy = touch.clientY - startY;
      if (!decided) {
        if (Math.abs(dx) < DIRECTION_LOCK && Math.abs(dy) < DIRECTION_LOCK)
          return;
        decided = true;
        // Vertical scrolls and leftward swipes (row actions) are not back.
        if (dy > dx || dx < 0) {
          cancelled = true;
          return;
        }
        dragging = true;
      }
      if (!dragging) return;
      e.preventDefault();
      const offset = Math.min(Math.max(0, dx), maxDrag);
      el.style.transform = `translateX(${offset}px)`;
    };

    const finish = (commit: boolean) => {
      if (!dragging) return;
      dragging = false;
      el.style.transition = `transform ${SNAP_MS}ms ease-out`;
      el.style.transform = "translateX(0px)";
      clearSnap();
      snapTimer = window.setTimeout(() => {
        el.style.transition = "";
        snapTimer = undefined;
      }, SNAP_MS + 50);
      if (commit) backActionRef.current();
    };

    const onTouchEnd = (e: TouchEvent) => {
      if (!dragging) return;
      const touch = e.changedTouches[0];
      const dx = touch ? touch.clientX - startX : 0;
      const width = window.innerWidth;
      const commit = dx > Math.max(MIN_TRIGGER_PX, width * TRIGGER_RATIO);
      finish(commit);
    };

    const onTouchCancel = () => finish(false);

    window.addEventListener("touchstart", onTouchStart, { passive: true });
    window.addEventListener("touchmove", onTouchMove, { passive: false });
    window.addEventListener("touchend", onTouchEnd, { passive: true });
    window.addEventListener("touchcancel", onTouchCancel, { passive: true });
    return () => {
      window.removeEventListener("touchstart", onTouchStart);
      window.removeEventListener("touchmove", onTouchMove);
      window.removeEventListener("touchend", onTouchEnd);
      window.removeEventListener("touchcancel", onTouchCancel);
      clearSnap();
    };
  }, [isDesktop]);

  return rootRef;
}
