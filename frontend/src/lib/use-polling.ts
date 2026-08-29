import { useEffect, useRef } from "react";

// usePolling is the shared component-level data-polling primitive: a
// fixed-cadence interval with visibility gating, replacing the hand-rolled
// setInterval effects previously inlined in page components (the pattern
// established in use-presence-heartbeat and activity-list).
//
// Contract:
// - Fixed cadence: fn fires every intervalMs regardless of how long the
//   previous call takes; slow ticks still overlap in wall-clock terms, so
//   callers that need it rely on silent refreshes / store seq guards.
// - Visibility gated: while document.hidden, every tick is a no-op, so a
//   background tab stops issuing requests without tearing down the timer.
//   A visibilitychange listener calls fn once immediately when the tab goes
//   hidden→visible (mirroring use-presence-heartbeat), instead of waiting up
//   to a full interval for the next tick.
// - Cleanup: on unmount — or whenever the interval restarts — both the
//   interval and the visibilitychange listener are removed.
// - enabled=false (opts) starts nothing: no interval, no listener. Flipping
//   enabled back to true recreates both.
// - fn is kept in a ref and never in the effect deps: callers may pass an
//   inline closure created every render — no useCallback needed — and each
//   tick always invokes the latest fn. Only intervalMs/enabled changes
//   restart the interval.
export function usePolling(
  fn: () => void | Promise<void>,
  intervalMs: number,
  opts?: { enabled?: boolean }
): void {
  const enabled = opts?.enabled ?? true;
  // Latest-ref pattern: sync the newest closure after each render without
  // making it a dependency of the polling effect.
  const fnRef = useRef(fn);
  useEffect(() => {
    fnRef.current = fn;
  }, [fn]);

  useEffect(() => {
    if (!enabled) return;
    const tick = () => {
      if (document.hidden) return;
      void fnRef.current();
    };
    const handle = setInterval(tick, intervalMs);
    const onVisible = () => {
      if (!document.hidden) tick();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      clearInterval(handle);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [enabled, intervalMs]);
}
