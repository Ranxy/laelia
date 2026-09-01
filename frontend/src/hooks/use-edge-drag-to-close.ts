import { useEffect, useRef, useState } from "react";
import { platformOwnsEdgeSwipe } from "@/lib/platform-edge-swipe";
import { useHistorySentinel } from "./use-history-sentinel";
import { useIsDesktop } from "./use-is-desktop";

// Shared mechanics for the mobile left-edge drag gestures (audit 06 P2: the
// route/thread back gesture and the sheet drag-to-close used to be two ~70%
// copies of the same state machine). The engine owns everything the gestures
// must feel identically: the window-level touch listeners, the left-edge
// start zone, the direction lock, the drag cap and commit threshold, and the
// settle timing. Each surface plugs in an EdgeDragHost that only decides
// whether a touch may start and where the drag visuals land:
//
// - useEdgeDragToClose (below): drawer-carrying sheets follow the finger and
//   dismiss on commit.
// - useSwipeBack (use-swipe-back.ts): the current page follows the finger and
//   commits a one-level navigation; full-screen panels (thread, tasks board)
//   slide via the --swipe-offset CSS variables instead.
//
// The gesture is inert on desktop; on real iOS/iPadOS browsers the system
// edge-swipe recognizer owns the edge touches (see platform-edge-swipe.ts),
// so each surface yields there and dismisses through its history sentinel
// (useHistorySentinel) instead — the browser back button gains the same
// dismiss-on-back behavior for free.
const EDGE_SIZE = 24; // px from the left edge where the gesture may start
const DIRECTION_LOCK = 10; // px of movement before the gesture is decided
const MAX_DRAG_RATIO = 0.5; // the surface may slide up to half the viewport
const TRIGGER_RATIO = 0.25; // release past 25% of the viewport commits
const MIN_TRIGGER_PX = 80;
export const EDGE_DRAG_SNAP_MS = 200; // spring-back animation
export const EDGE_DRAG_COMMIT_MS = 250; // slide-out animation before commit

// One gesture surface. All methods fire only between a successful begin() and
// the matching reset()/settle(), and read live state from their closure (the
// engine keeps the host in a ref, so closures refresh on every render).
export interface EdgeDragHost {
  // touchstart: may this touch start tracking? Returns false to yield the
  // touch (wrong target, another owner, nothing to dismiss). This is also
  // where a surface freezes its own transitions for the drag.
  begin(target: HTMLElement | null, clientX: number): boolean;
  // Follow the finger: `offset` is already clamped to the drag cap,
  // `progress` is offset / maxDrag (0..1).
  follow(offset: number, progress: number): void;
  // The touch released. Run the settle animation for the commit decision and
  // invoke `done()` when the gesture is fully over (the engine's reset —
  // call it from the scheduled callback, or not at all when the surface
  // unmounts instead, e.g. a committed sheet). `schedule` registers timers
  // the engine clears on cleanup and on the next touchstart.
  settle(
    commit: boolean,
    done: () => void,
    schedule: (fn: () => void, ms: number) => void
  ): void;
  // Clear all drag visuals instantly.
  reset(): void;
  // touchcancel policy. "instant" clears the gesture without animating —
  // used when a system gesture (iOS/Android edge swipe) may have claimed the
  // touch and our layers must not animate underneath its transition. "settle"
  // keeps the sheet feel: an in-progress drag springs back normally and an
  // already-settling commit runs to completion.
  touchCancel: "instant" | "settle";
}

export function useEdgeDrag(enabled: boolean, host: EdgeDragHost): void {
  const hostRef = useRef(host);
  hostRef.current = host;

  useEffect(() => {
    if (!enabled) return;

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
      hostRef.current.reset();
    };

    const onTouchStart = (event: TouchEvent) => {
      const target = event.target as HTMLElement | null;
      const touch = event.touches[0];
      if (
        !touch ||
        touch.clientX > EDGE_SIZE ||
        !hostRef.current.begin(target, touch.clientX)
      ) {
        cancelled = true;
        return;
      }
      startX = touch.clientX;
      startY = touch.clientY;
      tracking = true;
      dragging = false;
      decided = false;
      cancelled = false;
      maxDrag = window.innerWidth * MAX_DRAG_RATIO;
      clearTimers();
    };

    const onTouchMove = (event: TouchEvent) => {
      if (cancelled || !tracking) return;
      const touch = event.touches[0];
      if (!touch) return;
      const dx = touch.clientX - startX;
      const dy = touch.clientY - startY;
      if (!decided) {
        if (Math.abs(dx) < DIRECTION_LOCK && Math.abs(dy) < DIRECTION_LOCK) {
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
      hostRef.current.follow(offset, offset / maxDrag);
    };

    const finish = (commit: boolean) => {
      if (!dragging) return;
      dragging = false;
      tracking = false;
      hostRef.current.settle(commit, reset, (fn, ms) => {
        timers.push(window.setTimeout(fn, ms));
      });
    };

    const onTouchEnd = (event: TouchEvent) => {
      if (!tracking) return;
      if (!dragging) {
        reset();
        return;
      }
      const touch = event.changedTouches[0];
      const dx = touch ? touch.clientX - startX : 0;
      finish(dx > Math.max(MIN_TRIGGER_PX, window.innerWidth * TRIGGER_RATIO));
    };

    const onTouchCancel = () => {
      if (hostRef.current.touchCancel === "settle") {
        if (!tracking) return;
        if (!dragging) {
          reset();
          return;
        }
        finish(false);
        return;
      }
      // A touchcancel typically means a system gesture (e.g. the iOS edge
      // swipe) claimed the touch mid-drag: reset instantly instead of
      // animating a spring-back underneath the browser's own transition.
      reset();
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
  }, [enabled]);
}

interface UseEdgeDragToCloseOptions {
  open: boolean;
  onClose: () => void;
}

// Mobile edge drag-to-close for drawer-carrying sheets (ChatDrawerSheet,
// MentionDetailSheet, SidePanel). Owns the popup/overlay ref state the Sheet
// needs and composes the two halves of the gesture policy:
//
// - On browsers without a system edge-swipe (desktop devtools emulation,
//   Android in-page touches) the sheet follows the finger from the left edge
//   while the scrim fades to reveal the page underneath. Releasing past the
//   threshold slides the sheet out and closes it; otherwise it springs back.
// - On real iOS/iPadOS browsers the system edge-swipe owns those touches (see
//   platform-edge-swipe.ts), so the synthetic gesture yields (stays inert) and
//   dismissal goes through the history sentinel (use-history-sentinel): the
//   system swipe's transition reveals the sheet-free page underneath and its
//   commit closes the sheet instead of leaving the page. The browser back
//   button gains the same dismiss-on-back behavior for free.
export function useEdgeDragToClose({
  open,
  onClose,
}: UseEdgeDragToCloseOptions) {
  const [popup, setPopup] = useState<HTMLDivElement | null>(null);
  const [overlay, setOverlay] = useState<HTMLDivElement | null>(null);
  const isDesktop = useIsDesktop();
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEdgeDrag(!!open && !isDesktop && !platformOwnsEdgeSwipe(), {
    begin: (target) =>
      target != null &&
      ((popup !== null && popup.contains(target)) ||
        (overlay !== null && overlay.contains(target))),
    follow: (offset, progress) => {
      if (popup) popup.style.transform = `translateX(${offset}px)`;
      // Fade the scrim as the sheet moves away so the page underneath is
      // visible during the drag, matching the thread panel (which has no
      // scrim at all).
      if (overlay) overlay.style.opacity = String(1 - progress);
    },
    settle: (commit, done, schedule) => {
      if (!popup || !overlay) {
        done();
        return;
      }
      const ms = commit ? EDGE_DRAG_COMMIT_MS : EDGE_DRAG_SNAP_MS;
      popup.style.transition = `transform ${ms}ms ease-out`;
      overlay.style.transition = `opacity ${ms}ms ease-out`;
      if (commit) {
        popup.style.transform = "translateX(100%)";
        overlay.style.opacity = "0";
        schedule(() => {
          onCloseRef.current();
        }, ms + 50);
      } else {
        popup.style.transform = "translateX(0px)";
        overlay.style.opacity = "1";
        schedule(done, ms + 50);
      }
    },
    reset: () => {
      if (popup) {
        popup.style.transition = "";
        popup.style.transform = "";
      }
      if (overlay) {
        overlay.style.transition = "";
        overlay.style.opacity = "";
      }
    },
    touchCancel: "settle",
  });

  useHistorySentinel(!!open, () => onCloseRef.current());
  return { setPopup, setOverlay };
}
