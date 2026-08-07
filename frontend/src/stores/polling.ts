// sleep resolves after ms, or immediately when the signal aborts, so a stopped
// watcher exits its retry backoff without waiting out the delay. Used by the
// self-scheduling long-poll loops (channel/thread watchers) between a failed
// request and the next attempt.
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
