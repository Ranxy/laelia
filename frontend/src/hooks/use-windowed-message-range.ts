// lib/use-windowed-message-range.ts
//
// ADR-3 step ③: the chat message list light-windowing. Only rows within
// roughly two viewports of the scroll position mount real MessageRow
// subtrees; rows outside the window render a height-memory placeholder —
// a bare div with the last-measured height (or the default estimate for
// rows never seen), so scroll geometry stays continuous. This keeps a
// multi-thousand-message history's DOM bounded without a virtualization
// library, and without fighting the Markdown renderer's height mutations:
// the streaming tail is always inside the window bottom margin, so its growth
// is always measured against real content.
//
// The window only engages after the scroll container has been measured by a
// ResizeObserver. jsdom reports zero-height containers and never fires
// observers, so tests and embeds render every row — no mocks anywhere.
//
// Pure DOM bookkeeping: no store access. The scroller (useMessageScroller)
// keeps owning anchors, jumps and pagination locks; this hook only decides
// which indices mount.

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

// Window margins in viewport units: rows start degrading two viewports above
// the visible band and three viewports below (the bottom margin also keeps
// newly streamed messages mounted together with the auto-stick path).
const WINDOW_VIEWPORTS_ABOVE = 2;
const WINDOW_VIEWPORTS_BELOW = 3;
// Fallback height for rows that have never been measured.
const DEFAULT_ROW_HEIGHT = 96;
// Rows adjacent to the visible window always stay real regardless of offset,
// so pagination interactions never reach a placeholder.
const OVERSCAN_ROWS = 8;

export interface WindowedRangeOptions {
  /** The scroll container the rows live in. */
  containerRef: React.RefObject<HTMLElement | null>;
  /** Stable row ids in render order. */
  rowIds: string[];
  /** When non-empty, degrade nothing and mount every row (jump in flight). */
  forceFullRender?: boolean;
}

export interface WindowedRange {
  /** Half-open index window [start, end) of rows to mount for real. */
  start: number;
  /** Exclusive end of the mount window. */
  end: number;
  /** Attach to a rendered row wrapper to keep its height remembered. */
  rowRef: (id: string) => (el: HTMLElement | null) => void;
  /** Height a placeholder row should occupy. */
  placeholderHeight: (id: string) => number;
}

export function useWindowedMessageRange(
  options: WindowedRangeOptions
): WindowedRange {
  const { containerRef, rowIds, forceFullRender } = options;
  // Engaged = the container has been measured; until then render everything.
  const [engaged, setEngaged] = useState(false);
  const [range, setRange] = useState({
    start: 0,
    end: Number.MAX_SAFE_INTEGER,
  });
  const heightsRef = useRef<Map<string, number>>(new Map());
  const rowRefCacheRef = useRef<Map<string, (el: HTMLElement | null) => void>>(
    new Map()
  );
  const rowIdsRef = useRef(rowIds);
  rowIdsRef.current = rowIds;

  const recompute = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;
    const viewport = container.clientHeight;
    if (viewport <= 0) return;
    const ids = rowIdsRef.current;
    const scrollTop = container.scrollTop;
    const bandTop = scrollTop - WINDOW_VIEWPORTS_ABOVE * viewport;
    const bandBottom = scrollTop + viewport + WINDOW_VIEWPORTS_BELOW * viewport;

    let offset = 0;
    let start = 0;
    let end = ids.length;
    let startFound = false;
    for (let index = 0; index < ids.length; index++) {
      const height =
        heightsRef.current.get(ids[index] ?? "") ?? DEFAULT_ROW_HEIGHT;
      if (!startFound && offset + height >= bandTop) {
        start = index;
        startFound = true;
      }
      if (offset >= bandBottom) {
        end = index;
        break;
      }
      offset += height;
    }
    if (!startFound) start = Math.max(0, ids.length - 1);
    setRange({
      start: Math.max(0, start - OVERSCAN_ROWS),
      end: Math.min(ids.length, end + OVERSCAN_ROWS),
    });
  }, [containerRef]);

  // rAF-coalesced scroll/resize handling.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let frame = 0;
    const schedule = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        recompute();
      });
    };
    schedule();
    container.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      container.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [containerRef, recompute]);

  // The engage gate: only a ResizeObserver's real measurement may turn the
  // window on. jsdom reports zero-height containers and never fires
  // observers, so tests and embeds keep the full render — no mocks anywhere.
  useEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      setEngaged(true);
      recompute();
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, [containerRef, recompute]);

  // Recompute before paint when pagination replaces the message window. Do
  // not clear the height cache or fall back to full rendering: doing so makes
  // prepend briefly mount a different DOM tree and can flash before the range
  // settles again.
  // biome-ignore lint/correctness/useExhaustiveDependencies: rerun only when the head row changes; the callback and container ref are stable.
  useLayoutEffect(() => {
    if (!engaged) return;
    recompute();
  }, [rowIds[0], engaged]);

  const rowRef = useCallback((id: string) => {
    // Stable per-id callback: a fresh closure every render would make React
    // detach + reattach (and re-measure) every row on every render.
    const cache = rowRefCacheRef.current;
    let fn = cache.get(id);
    if (!fn) {
      fn = (el: HTMLElement | null) => {
        // Only refresh on mount — placeholders must keep remembering the
        // last mounted height, so unmounts never clear the entry.
        if (el) heightsRef.current.set(id, el.offsetHeight);
      };
      cache.set(id, fn);
    }
    return fn;
  }, []);

  const placeholderHeight = useCallback(
    (id: string) => heightsRef.current.get(id) ?? DEFAULT_ROW_HEIGHT,
    []
  );

  if (forceFullRender || !engaged) {
    return {
      start: 0,
      end: Number.MAX_SAFE_INTEGER,
      rowRef,
      placeholderHeight,
    };
  }
  return {
    start: Math.min(range.start, rowIds.length),
    end: Math.min(range.end, rowIds.length),
    rowRef,
    placeholderHeight,
  };
}
