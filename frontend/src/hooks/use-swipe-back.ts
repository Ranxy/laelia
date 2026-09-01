import { useCallback, useEffect, useRef } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { platformOwnsEdgeSwipe } from "@/lib/platform-edge-swipe";
import { resolvePath } from "@/router/route-index";
import { ROUTE_INFO } from "@/router/route-info";
import { useCurrentRoute } from "@/router/use-current-route";
import { useAppStore } from "@/stores";
import {
  EDGE_DRAG_COMMIT_MS,
  EDGE_DRAG_SNAP_MS,
  useEdgeDrag,
} from "./use-edge-drag-to-close";
import { useIsDesktop } from "./use-is-desktop";

// iOS-style interactive back gesture for mobile: drag from the left edge of
// the screen to the right. The current page follows the finger and a static
// peek surface underneath is revealed (the back target is NOT mounted — the
// peek shows the destination's title only, per the preview-retirement
// decision); releasing past the threshold slides the current page out and
// commits the navigation. The level stack is:
//   thread panel (full-screen overlay) -> current route -> its backTo target.
// The gesture is inert on desktop, on top-level tab routes (nothing to go
// back to), over layer overlays (sheets/dialogs/previews dismiss on their
// own), and for route-level back on real iOS/iPadOS browsers whose system
// edge-swipe recognizer owns those touches (see platform-edge-swipe.ts).
//
// The touch mechanics (edge zone, direction lock, thresholds, settle timing)
// live in the shared edge-drag engine (use-edge-drag-to-close.ts); this hook
// only supplies the surface host below.
// Touches that BEGIN on the device bezel report their first position at the
// viewport edge (clientX ≈ 0-2px). On Android, gesture navigation claims those
// touches below the browser, and on any browser with a previous history entry
// a bezel-originated rightward pan is the system back gesture — running the
// synthetic gesture on it would race the platform transition (see
// platform-edge-swipe.ts for the full artifact description). Bezel touches are
// therefore yielded whenever the browser has somewhere to swipe back to.
const BEZEL_GUARD = 3;

// Safety timeout: if the data router's navigation doesn't complete within this
// window, force a reset so the gesture state doesn't get stuck.
const RESET_TIMEOUT_MS = 1000;

type SwipeBackMode = "thread" | "route" | null;

export interface SwipeBackState {
  // Bind to the layout root (CSS variables for the thread panel live here).
  rootRef: (el: HTMLDivElement | null) => void;
  // Bind to the current page container; it is translated while dragging.
  currentPageRef: (el: HTMLDivElement | null) => void;
}

export function useSwipeBack(): SwipeBackState {
  const isDesktop = useIsDesktop();
  const navigate = useNavigate();
  const location = useLocation();
  const closeThread = useAppStore((s) => s.closeThread);
  const activeThreadRoot = useAppStore((s) => s.activeThreadRoot);
  // The tasks board panel is the other full-screen overlay driven by this
  // gesture (mode "thread" below): its close action needs the conversation
  // whose board is open.
  const openTasksConv = useAppStore((s) => {
    for (const [conv, open] of Object.entries(s.tasksPanelOpen)) {
      if (open) return conv;
    }
    return null;
  });
  const closeTasksPanel = useAppStore((s) => s.closeTasksPanel);
  const currentRoute = useCurrentRoute();

  const rootRef = useRef<HTMLDivElement | null>(null);
  const pageRef = useRef<HTMLDivElement | null>(null);
  // Set when a route-level commit's navigate() has been called; the
  // location-change effect clears the transform once the data router
  // finishes the navigation (preventing a one-frame flash of the old
  // route before the new one renders).
  const pendingResetRef = useRef(false);
  // Decided per gesture in begin(); read by follow/settle. Lives in a ref so
  // a re-render mid-drag (store update, etc.) cannot lose the mode.
  const modeRef = useRef<SwipeBackMode>(null);

  const backTargetRef = useRef<string | null>(null);
  // backTo is a route name; the gesture commits its navigation by path.
  const currentBackTo = currentRoute.name
    ? ROUTE_INFO[currentRoute.name]?.backTo
    : undefined;
  backTargetRef.current = currentBackTo ? resolvePath(currentBackTo) : null;
  const threadActiveRef = useRef(false);
  threadActiveRef.current = activeThreadRoot != null;
  const tasksPanelConvRef = useRef<string | null>(null);
  tasksPanelConvRef.current = openTasksConv;

  const setRoot = useCallback((el: HTMLDivElement | null) => {
    rootRef.current = el;
  }, []);
  const setPage = useCallback((el: HTMLDivElement | null) => {
    pageRef.current = el;
  }, []);

  useEdgeDrag(!isDesktop, {
    begin(target, clientX) {
      if (target?.closest?.("[data-bb-layer-family]")) {
        return false;
      }
      const root = rootRef.current;
      if (!root) return false;
      if (
        (threadActiveRef.current || tasksPanelConvRef.current != null) &&
        !platformOwnsEdgeSwipe()
      ) {
        // Full-screen panel overlays (thread panel, tasks board) driven via
        // the --swipe-offset CSS variables. On real iOS/iPadOS browsers the
        // system edge-swipe owns the touch (see platform-edge-swipe.ts); both
        // panels dismiss through their history sentinels there
        // (useHistorySentinel) instead of this synthetic gesture, which would
        // stack the browser's back-transition snapshot underneath them.
        modeRef.current = "thread";
        root.style.setProperty("--swipe-transition", "none");
        return true;
      }
      if (backTargetRef.current) {
        // Only race the platform edge swipe when it cannot engage. On real
        // iOS/iPadOS browsers the system recognizer's zone covers the whole
        // edge area (not just the bezel), so ANY synthetic route gesture
        // there stacks the browser's own back-transition snapshot underneath
        // our layers — the three-layer artifact. Route-level back is
        // delegated to the platform there; its native transition reveals the
        // same destination (the previous history entry == backTo in the
        // standard flows). The same yield applies to bezel-originated touches
        // elsewhere (Android gesture nav claims them below the browser). When
        // the browser has no previous entry at all (fresh deep link,
        // history.state.idx === 0) the platform gesture has nothing to do and
        // the synthetic gesture keeps the full edge zone.
        const historyIdx =
          (window.history.state as { idx?: number } | null)?.idx ?? 0;
        if (
          historyIdx > 0 &&
          (platformOwnsEdgeSwipe() || clientX <= BEZEL_GUARD)
        ) {
          return false;
        }
        modeRef.current = "route";
        const page = pageRef.current;
        if (page) page.style.transition = "none";
        return true;
      }
      return false;
    },

    follow(offset) {
      const mode = modeRef.current;
      if (mode === "route" && pageRef.current) {
        pageRef.current.style.transform = `translateX(${offset}px)`;
      } else if (mode === "thread") {
        rootRef.current?.style.setProperty("--swipe-offset", `${offset}px`);
      }
    },

    settle(commit, done, schedule) {
      const mode = modeRef.current;
      const width = window.innerWidth;
      const ms = commit ? EDGE_DRAG_COMMIT_MS : EDGE_DRAG_SNAP_MS;
      if (mode === "route" && pageRef.current) {
        const page = pageRef.current;
        page.style.transition = `transform ${ms}ms ease-out`;
        page.style.transform = `translateX(${commit ? width : 0}px)`;
        schedule(() => {
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
              schedule(() => {
                if (pendingResetRef.current) done();
              }, RESET_TIMEOUT_MS);
              return;
            }
          }
          done();
        }, ms + 50);
      } else if (mode === "thread" && rootRef.current) {
        const root = rootRef.current;
        root.style.setProperty(
          "--swipe-transition",
          `transform ${ms}ms ease-out`
        );
        root.style.setProperty("--swipe-offset", `${commit ? width : 0}px`);
        schedule(() => {
          if (commit) {
            if (threadActiveRef.current) {
              closeThread();
            } else if (tasksPanelConvRef.current) {
              closeTasksPanel(tasksPanelConvRef.current);
            }
          }
          done();
        }, ms + 50);
      } else {
        done();
      }
    },

    reset() {
      modeRef.current = null;
      const page = pageRef.current;
      if (page) {
        page.style.transition = "";
        page.style.transform = "";
      }
      const root = rootRef.current;
      if (root) {
        root.style.removeProperty("--swipe-offset");
        root.style.removeProperty("--swipe-transition");
      }
    },

    touchCancel: "instant",
  });

  // When a route-level commit is pending, wait for the data router to finish
  // the navigation (location changes) before clearing the transform. This
  // prevents a one-frame flash where the old route would be visible at
  // translateX(0) before the new route renders.
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed on the location commit; the body reads refs only.
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
  }, [location.pathname]);

  return { rootRef: setRoot, currentPageRef: setPage };
}
