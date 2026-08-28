import { useEffect, useRef } from "react";
import { platformOwnsEdgeSwipe } from "./platform-edge-swipe";

// Marks the duplicate history entry an overlay pushes while it is open (see
// useHistorySentinel). The value is a per-push token so several stacked
// overlays (e.g. the member detail sheet on top of the members drawer) each
// recognize their own sentinel.
const SENTINEL_KEY = "laelia.historySentinel";

let sentinelSeq = 0;

function sentinelToken(state: unknown): string | undefined {
  if (typeof state !== "object" || state === null) return undefined;
  const token = (state as Record<string, unknown>)[SENTINEL_KEY];
  return typeof token === "string" ? token : undefined;
}

function pushSentinel(token: string) {
  window.history.pushState(
    {
      ...(window.history.state as Record<string, unknown> | null),
      [SENTINEL_KEY]: token,
    },
    ""
  );
}

// While `active`, keeps a duplicate history entry on top of the stack so the
// browser's back affordances dismiss the overlay instead of leaving the page:
// the iOS system edge swipe's transition reveals this overlay-free entry
// underneath (no extra layer), and its commit pops the sentinel — the
// resulting popstate dismisses the overlay via `onClose`. The browser back
// button and Android's back gesture gain the same dismiss-on-back behavior.
//
// Stacked overlays each push their own tokenized sentinel; a pop dismisses
// exactly the overlay whose sentinel it consumed. The cleanup defers its own
// balancing pop by one task so a StrictMode remount (or a quick close and
// reopen) cancels it and reuses the sentinel instead of racing it.
export function useHistorySentinel(active: boolean, onClose: () => void): void {
  const yieldsToSystem = platformOwnsEdgeSwipe();
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const tokenRef = useRef<string | null>(null);
  const cleanupPopRef = useRef<number | null>(null);

  useEffect(() => {
    if (!active || !yieldsToSystem) return;
    // A pop scheduled by a just-cleaned-up previous activation is cancelled
    // when the overlay re-activates before it fired: the existing sentinel is
    // still on top and gets reused as-is.
    if (cleanupPopRef.current != null) {
      window.clearTimeout(cleanupPopRef.current);
      cleanupPopRef.current = null;
    }
    const existing = sentinelToken(window.history.state);
    if (existing == null || existing !== tokenRef.current) {
      tokenRef.current = String(++sentinelSeq);
      pushSentinel(tokenRef.current);
    }
    const onPop = () => {
      if (sentinelToken(window.history.state) === tokenRef.current) {
        // A sentinel stacked ABOVE ours was popped (an overlay stacked on top
        // was dismissed); ours is still on top — keep waiting.
        return;
      }
      // Our sentinel was consumed: dismiss the overlay instead of leaving
      // the page.
      tokenRef.current = null;
      onCloseRef.current();
    };
    window.addEventListener("popstate", onPop);
    return () => {
      window.removeEventListener("popstate", onPop);
      const myToken = tokenRef.current;
      tokenRef.current = null;
      // The overlay closed through its own UI (close button, synthetic
      // gesture commit): consume our sentinel so the stack stays balanced. A
      // navigation away from the page leaves the current entry unmarked and
      // skips the pop.
      if (myToken != null && sentinelToken(window.history.state) === myToken) {
        cleanupPopRef.current = window.setTimeout(() => {
          cleanupPopRef.current = null;
          if (sentinelToken(window.history.state) === myToken) {
            window.history.back();
          }
        }, 0);
      }
    };
  }, [active, yieldsToSystem]);
}
