import { sleep } from "./polling";

// Shared cadences for the chat-domain watchers: the channel message watcher
// (channel.ts) and the thread reply watcher (thread.ts) run the same shape of
// self-rescheduling long-poll loop, so their cadence constants live together
// here (ChatGateway consolidation).
//
// LONG_POLL_MS — Long-poll hold time. The server caps wait_ms at 30000; 25000
//   leaves headroom for network/proxy latency so the client re-issues before
//   the server would time out the request.
// RETRY_DELAY_MS — Backoff between a failed long poll and the next attempt, so
//   a network blip does not turn into a tight retry loop.
export const LONG_POLL_MS = 25000;
export const RETRY_DELAY_MS = 1000;

// Badge/activity cadence for the channel watcher: thread reply counts, task
// badges, and agent activity are not covered by the message delta
// (server-side), so they keep their own 5s poll while the message watcher
// long-polls.
export const BADGE_INTERVAL_MS = 5000;

// BadgeIntervalHandle owns the timers/listeners of one badge-refresh interval.
// stop() must be called on watcher teardown (stopWatchingChannel/reset) so the
// visibilitychange listener does not outlive the loop.
export interface BadgeIntervalHandle {
  stop: () => void;
}

// startLongPollLoop runs one round of `round`, then re-issues it immediately —
// the shape shared by the channel and thread watchers: hold one request open
// until new messages land or the 25s server timeout elapses, then re-issue.
// A failed round backs off RETRY_DELAY_MS before retrying (abort-aware, so a
// stopped watcher exits its backoff instead of waiting it out). Between
// re-issues the loop pauses while the tab is hidden (and resumes immediately
// on visible), so a background tab stops issuing long polls entirely.
//
// The first round starts synchronously (up to its first await): callers and
// tests rely on the watcher issuing its initial request during
// startWatchingChannel, not on the next microtask.
//
// `round` owns every store write; it must check its own abort signal after the
// await before applying results (the established watcher guard, unchanged).
export function startLongPollLoop(opts: {
  signal: AbortSignal;
  round: () => Promise<void>;
}): void {
  const loop = async () => {
    try {
      await opts.round();
    } catch {
      if (opts.signal.aborted) return; // stopped — exit the loop
      // Network error — back off briefly, then re-issue the long poll.
      await sleep(RETRY_DELAY_MS, opts.signal);
    }
    if (opts.signal.aborted) return;
    // Visibility gate: a background tab stops issuing long polls and resumes
    // the moment the tab is visible again. Skipped when already visible so
    // re-issue stays synchronous (an await would add a microtask hop between
    // rounds).
    if (document.hidden) {
      await waitForVisible(opts.signal);
      if (opts.signal.aborted) return;
    }
    void loop();
  };
  void loop();
}

// waitForVisible resolves immediately when the tab is visible (or the watcher
// is stopping); otherwise it resolves on the next visibilitychange or abort.
function waitForVisible(signal: AbortSignal): Promise<void> {
  if (!document.hidden || signal.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const settle = () => {
      document.removeEventListener("visibilitychange", onVisible);
      signal.removeEventListener("abort", onAbort);
      resolve();
    };
    const onVisible = () => {
      if (!document.hidden) settle();
    };
    const onAbort = () => settle();
    document.addEventListener("visibilitychange", onVisible);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

// startBadgeInterval runs `fn` every intervalMs with the same visibility
// gating as usePolling: while document.hidden every tick is a no-op, and
// returning to the foreground fires once immediately instead of waiting up to
// a full interval. Failures must be swallowed inside fn so one bad tick never
// kills the surrounding watcher.
export function startBadgeInterval(
  fn: () => void,
  intervalMs: number
): BadgeIntervalHandle {
  const tick = () => {
    if (document.hidden) return;
    fn();
  };
  const timer = setInterval(tick, intervalMs);
  const onVisible = () => {
    if (!document.hidden) fn();
  };
  document.addEventListener("visibilitychange", onVisible);
  return {
    stop: () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    },
  };
}
