// Abort-aware sleep used by the long-poll watcher backoff (channel/thread
// watchers via chat-watcher.ts and the slices): sleep resolves after ms, or
// immediately when the signal aborts, so a stopped watcher exits its retry
// backoff without waiting out the delay. This is a sleep helper, not a polling
// subsystem — the watcher loops themselves live in chat-watcher.ts.
export function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      resolve();
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}
