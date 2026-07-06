import { type RefObject, useEffect, useRef } from "react";

// Keeps a scroll container pinned to the bottom while new content arrives,
// unless the user has scrolled away from the bottom (then it stays put and
// does not fight the user). Reused by CommandTerminal and CommandTimeline so
// both share the same auto-scroll-to-bottom behavior.
export function useAutoScroll<T>(
  scrollRef: RefObject<HTMLDivElement | null>,
  deps: T[]
) {
  const autoScrollRef = useRef(true);

  useEffect(() => {
    if (autoScrollRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  const onScroll = () => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 40;
  };

  return { onScroll, autoScrollRef };
}
