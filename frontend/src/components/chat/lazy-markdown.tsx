import {
  type ReactNode,
  type RefObject,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

// findClosestScrollContainer walks up from `el` to the first ancestor whose
// vertical overflow is auto/scroll. That ancestor is the chat list's scroll
// viewport and is used as the IntersectionObserver root so the lazy gate
// respects the list's real scroll position rather than the browser viewport.
//
// This is only a FALLBACK for callers that do not pass an explicit scrollRoot
// (e.g. the small DM chat). The channel chat passes its scroll container ref
// directly, because calling getComputedStyle per row across a 100-row list
// forces a style recalc for each row during mount and causes layout thrashing.
//
// We do NOT require the ancestor to be currently scrolling (scrollHeight >
// clientHeight): at the moment a row's effect runs the off-screen rows may not
// yet have their real heights applied, so the scroller can be transiently
// non-scrolling. Requiring scroll would make us fall back to the viewport root,
// and since the (short) scroller sits entirely within the viewport plus the
// 600px rootMargin, EVERY row would intersect and render immediately —
// defeating the gate. The overflow property alone is a stable signal of the
// scroll viewport regardless of current content size. When the list genuinely
// fits on screen (no scroll), every row intersects the scroller root and all
// render — which is correct, since they are all visible.
function findClosestScrollContainer(
  el: HTMLElement | null
): HTMLElement | null {
  let node = el?.parentElement ?? null;
  while (node) {
    const overflowY = getComputedStyle(node).overflowY;
    if (overflowY === "auto" || overflowY === "scroll") {
      return node;
    }
    node = node.parentElement;
  }
  return null;
}

export interface LazyMarkdownProps {
  // render is invoked only once the row is near the scroll viewport, so the
  // expensive MarkdownRender parse is skipped for off-screen history. Called at
  // most once per row (the result stays mounted afterward — we never unmount it
  // to avoid re-parsing on scroll-back).
  render: () => ReactNode;
  // Cheap placeholder shown until visible. Keeps the row's height roughly right
  // and gives instant readable text before the markdown renders.
  fallback: ReactNode;
  // eager skips the gate entirely, for rows that must render immediately (the
  // actively-streaming row at the bottom of the list).
  eager?: boolean;
  // scrollRoot is the chat list's scroll container, used as the
  // IntersectionObserver root. Passed explicitly by the channel chat so we don't
  // have to rediscover it per row (which would thrash layout via getComputedStyle
  // across a 100-row mount). When omitted, the container is found by walking the
  // DOM (fine for the small DM chat).
  scrollRoot?: RefObject<HTMLElement | null>;
  // rootMargin pre-renders rows slightly before they enter the viewport so the
  // markdown is ready by the time the user scrolls to it (no pop-in).
  rootMargin?: string;
}

// LazyMarkdown defers the heavy markdown render until its row is near the scroll
// viewport. The channel chat mounts up to 100 MessageRows at once; each agent
// row runs markstream-react's MarkdownRender (markdown parse + syntax
// highlight + DOM build), and doing that for all of them on entry is the single
// biggest contributor to the channel's slow first paint. Rendering a cheap
// raw-text placeholder until the row is visible cuts that to the handful of rows
// actually on screen.
//
// We deliberately do NOT layer `content-visibility: auto` on top of this any
// more. It used to skip layout/paint for the off-screen placeholders, but it
// also excluded those rows from the browser's scroll anchoring: when a row
// scrolled into view and swapped its 160px intrinsic placeholder for the real
// height (and again when the markdown replaced the raw-text fallback), scroll
// anchoring could not compensate, so the viewport snapped back down while
// scrolling up through history.
//
// Instead, the fallback row opts out of being a scroll-anchor candidate (see
// the layout effect below) while the real markdown is still pending. The
// browser then anchors to an already-rendered row below it, so the
// fallback→markdown height change is absorbed without moving the rows the user
// is reading. The scroll container must keep native `overflow-anchor` enabled
// outside of history-page transactions for this to work; chat-conversation.tsx
// only disables it for the brief prepend/append commit and restores it right
// after that commit has been laid out.
//
// Observer setup is deferred one animation frame so it runs AFTER the page's
// stick-to-bottom effect has scrolled the list to the latest message. React
// runs child effects before parent effects, so without the deferral the gate
// would observe while the list is still at the top and pre-render the top rows
// the user never lands on. With the deferral only the bottom (visible) rows and
// their overscan render on entry.
export function LazyMarkdown({
  render,
  fallback,
  eager = false,
  scrollRoot,
  rootMargin = "600px 0px",
}: LazyMarkdownProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(eager);
  // The message-row wrapper that contains this markdown. While the row is still
  // showing the raw-text fallback its height is not final, so it must not be
  // chosen as the browser's scroll-anchor node: if it were, the fallback→markdown
  // swap would keep the row's top edge fixed while the content below it is
  // pushed down (the exact "history squeezes the viewport down" jump). Excluding
  // the row forces the browser to anchor to an already-rendered row below it.
  const rowRef = useRef<HTMLElement | null>(null);
  const previousOverflowAnchorRef = useRef<string>("");

  useLayoutEffect(() => {
    if (visible) {
      // The fallback has just been replaced by the real markdown. The browser
      // must still see overflow-anchor:none on this row while it lays out the
      // height change and compensates the scroll position. A single rAF can run
      // before that layout, so restoration is deferred by two frames.
      const row = rowRef.current;
      if (!row) return;
      let raf2 = 0;
      const raf1 = requestAnimationFrame(() => {
        raf2 = requestAnimationFrame(() => {
          row.style.overflowAnchor = previousOverflowAnchorRef.current;
        });
      });
      return () => {
        cancelAnimationFrame(raf1);
        if (raf2) cancelAnimationFrame(raf2);
      };
    }

    const el = ref.current;
    if (!el) return;
    const row = el.closest<HTMLElement>("[data-msg-id]");
    if (!row) return;
    rowRef.current = row;
    previousOverflowAnchorRef.current = row.style.overflowAnchor;
    row.style.overflowAnchor = "none";
    // Intentionally no cleanup restore here: React runs effect cleanups before
    // the browser lays out the fallback→markdown swap, so restoring in cleanup
    // would make the growing row a valid anchor again and defeat the exclusion.
  }, [visible]);

  useEffect(() => {
    if (visible) return;
    const el = ref.current;
    if (!el) return;
    const root = scrollRoot?.current ?? findClosestScrollContainer(el);
    let io: IntersectionObserver | null = null;
    const raf = requestAnimationFrame(() => {
      if (!ref.current) return;
      io = new IntersectionObserver(
        (entries) => {
          if (entries.some((e) => e.isIntersecting)) setVisible(true);
        },
        { root, rootMargin }
      );
      io.observe(ref.current);
    });
    return () => {
      cancelAnimationFrame(raf);
      io?.disconnect();
    };
  }, [visible, rootMargin]);

  if (visible) return <>{render()}</>;
  return <span ref={ref}>{fallback}</span>;
}
