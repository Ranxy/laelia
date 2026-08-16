import { useEffect, useRef } from "react";
import { useIsDesktop } from "./use-is-desktop";
import {
  SWIPE_BACK_COMMIT_MS,
  SWIPE_BACK_DIRECTION_LOCK,
  SWIPE_BACK_EDGE_SIZE,
  SWIPE_BACK_MAX_DRAG_RATIO,
  SWIPE_BACK_MIN_TRIGGER_PX,
  SWIPE_BACK_SNAP_MS,
  SWIPE_BACK_TRIGGER_RATIO,
} from "./use-swipe-back";

// Mobile swipe-to-close for right-edge sheets (currently the mention detail
// sheet). It mirrors the thread panel's swipe-back gesture: the drag starts at
// the left edge of the viewport, the sheet follows the finger to the right,
// and the backdrop fades out so the page underneath is visible while dragging.
// Releasing past the threshold slides the sheet out and closes it; otherwise
// it springs back. Inert on desktop.
interface UseSwipeToCloseSheetOptions {
  open: boolean;
  onClose: () => void;
  popup: HTMLDivElement | null;
  overlay: HTMLDivElement | null;
}

export function useSwipeToCloseSheet({
  open,
  onClose,
  popup,
  overlay,
}: UseSwipeToCloseSheetOptions) {
  const isDesktop = useIsDesktop();
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (isDesktop || !open || !popup || !overlay) return;

    let startX = 0;
    let startY = 0;
    let tracking = false;
    let dragging = false;
    let decided = false;
    let cancelled = false;
    let maxDrag = 0;
    let timers: number[] = [];

    const clearTimers = () => {
      for (const timer of timers) window.clearTimeout(timer);
      timers = [];
    };

    const reset = () => {
      tracking = false;
      dragging = false;
      decided = false;
      cancelled = false;
      clearTimers();
      popup.style.transition = "";
      popup.style.transform = "";
      overlay.style.transition = "";
      overlay.style.opacity = "";
    };

    const onTouchStart = (event: TouchEvent) => {
      const target = event.target as HTMLElement | null;
      if (!target || (!popup.contains(target) && !overlay.contains(target))) {
        cancelled = true;
        return;
      }

      const touch = event.touches[0];
      if (!touch || touch.clientX > SWIPE_BACK_EDGE_SIZE) {
        cancelled = true;
        return;
      }

      startX = touch.clientX;
      startY = touch.clientY;
      tracking = true;
      dragging = false;
      decided = false;
      cancelled = false;
      maxDrag = window.innerWidth * SWIPE_BACK_MAX_DRAG_RATIO;
      clearTimers();
      popup.style.transition = "none";
      overlay.style.transition = "none";
    };

    const onTouchMove = (event: TouchEvent) => {
      if (cancelled || !tracking) return;

      const touch = event.touches[0];
      if (!touch) return;

      const dx = touch.clientX - startX;
      const dy = touch.clientY - startY;

      if (!decided) {
        if (
          Math.abs(dx) < SWIPE_BACK_DIRECTION_LOCK &&
          Math.abs(dy) < SWIPE_BACK_DIRECTION_LOCK
        ) {
          return;
        }
        decided = true;
        if (dy > dx || dx < 0) {
          cancelled = true;
          reset();
          return;
        }
        dragging = true;
      }

      if (!dragging) return;

      event.preventDefault();
      const offset = Math.min(Math.max(0, dx), maxDrag);
      popup.style.transform = `translateX(${offset}px)`;
      // Fade the scrim as the sheet moves away so the page underneath is
      // visible during the drag, matching the thread panel (which has no
      // scrim at all).
      const progress = Math.min(1, offset / maxDrag);
      overlay.style.opacity = String(1 - progress);
    };

    const finish = (commit: boolean) => {
      if (!dragging) return;
      dragging = false;
      tracking = false;

      const ms = commit ? SWIPE_BACK_COMMIT_MS : SWIPE_BACK_SNAP_MS;
      popup.style.transition = `transform ${ms}ms ease-out`;
      overlay.style.transition = `opacity ${ms}ms ease-out`;

      if (commit) {
        popup.style.transform = "translateX(100%)";
        overlay.style.opacity = "0";
        timers.push(
          window.setTimeout(() => {
            onCloseRef.current();
          }, ms + 50)
        );
      } else {
        popup.style.transform = "translateX(0px)";
        overlay.style.opacity = "1";
        timers.push(window.setTimeout(reset, ms + 50));
      }
    };

    const onTouchEnd = (event: TouchEvent) => {
      if (!tracking) return;
      if (!dragging) {
        reset();
        return;
      }

      const touch = event.changedTouches[0];
      const dx = touch ? touch.clientX - startX : 0;
      const width = window.innerWidth;
      const commit =
        dx >
        Math.max(SWIPE_BACK_MIN_TRIGGER_PX, width * SWIPE_BACK_TRIGGER_RATIO);
      finish(commit);
    };

    const onTouchCancel = () => {
      if (!tracking) return;
      if (!dragging) {
        reset();
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
  }, [isDesktop, open, popup, overlay]);
}
