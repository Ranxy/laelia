import {
  type RefObject,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

// Messages only need to expose what the scroller reads: the DOM row id and,
// for the embedded read-position scroll, the room version. Chronological,
// oldest first.
interface ScrollerMessage {
  id: string;
  roomVersion?: bigint;
}

interface UseMessageScrollerOptions {
  // Conversation whose messages are scrolled; keys the paging requests.
  conversationName: string;
  // Latest messages, mirrored into the scroll handler without re-creating the
  // handler every time the watcher appends a message.
  messages: ScrollerMessage[];
  // Store-backed incremental paging for the open conversation.
  loadOlderMessages: (conversationName: string) => Promise<void> | void;
  loadNewerMessages: (conversationName: string) => Promise<void> | void;
  // Fresh-read predicates: the scroll handler must see the current store
  // state at scroll time, not a value captured at render.
  hasOlderMessages: () => boolean;
  hasNewerMessages: () => boolean;
  isJumpLoading: () => boolean;
  // Focused jump window (jumpToMessage's anchor): the scroller behaves as a
  // history view while set, never auto-sticking to the bottom.
  jumpTarget: { messageId: string } | null;
  // True while the jump window's load is still committing.
  jumpLoading: boolean;
  // Store action that exits jump mode for a conversation (scroll-to-latest).
  clearJump: (conversationName: string) => Promise<void> | void;
  // Embedded deep-scroll (Activity detail pane pointing at the message an
  // activity references): scroll the list to this message once mounted.
  scrollToMessageId?: string;
  // Embedded deep-scroll for a DM: scroll to the first message whose room
  // version exceeds this read cursor (the user's last-read position).
  scrollToReadVersion?: bigint;
}

interface MessageScroller {
  scrollRef: RefObject<HTMLDivElement | null>;
  handleScroll: () => void;
  showScrollDown: boolean;
  scrollToBottom: () => Promise<void>;
  // Enter a focused jump window around messageId: release stick-to-bottom,
  // reset the jump latch, and quiet the sentinel-triggered history loads
  // until the window's scroll has been applied. The caller then runs its
  // jumpToMessage request and releases the suppression on failure (or the
  // scroll effect does on success).
  beginJumpWindow: (messageId: string) => void;
  releaseHistorySuppression: () => void;
  // Reset the per-conversation scroll state when switching channels.
  resetScrollState: () => void;
}

// Owns the chat message list's scroll state machine: stick-to-bottom for new
// messages, sentinel-triggered history paging with a manual scroll anchor
// (restore the pre-load viewport offset so prepending/appending pages never
// yank the rows the user is reading), focused jump windows, and the native
// CSS scroll-anchoring suppression windows. Mechanical move of
// chat-conversation's scroller; the page only wires the store pieces.
export function useMessageScroller({
  conversationName,
  messages,
  loadOlderMessages,
  loadNewerMessages,
  hasOlderMessages,
  hasNewerMessages,
  isJumpLoading,
  jumpTarget,
  jumpLoading,
  clearJump,
  scrollToMessageId = "",
  scrollToReadVersion = 0n,
}: UseMessageScrollerOptions): MessageScroller {
  const scrollRef = useRef<HTMLDivElement>(null);
  // The message rows can change height after their initial commit. Streamdown
  // may finish code highlighting or replace a lazy fallback on a later frame,
  // so native scroll anchoring alone is not reliable across browsers. The
  // stabilizer below owns those layout shifts and this flag keeps pagination
  // from re-enabling native anchoring while the stabilizer is active.
  const scrollHeightStabilizerActiveRef = useRef(false);
  const stabilizerAdjustingRef = useRef(false);
  const rowSnapshotsRef = useRef(
    new Map<string, { element: HTMLElement; top: number; height: number }>()
  );
  const listHeightRef = useRef<number | null>(null);
  const listScrollTopRef = useRef(0);
  const wasNearBottomRef = useRef(true);
  // Latest messages for the scroll handler without re-creating the handler
  // every time the watcher appends a message.
  const messagesRef = useRef(messages);

  // biome-ignore lint/correctness/useExhaustiveDependencies: rebind the observer when the rendered message set changes; all other inputs are held in refs.
  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    if (!scroller || typeof ResizeObserver === "undefined") return;

    const previousOverflowAnchor = scroller.style.overflowAnchor;
    const content = scroller.firstElementChild;
    if (!content) return;

    const readRows = () => {
      const scrollerRect = scroller.getBoundingClientRect();
      const rows = new Map<
        string,
        { element: HTMLElement; top: number; height: number }
      >();
      scroller.querySelectorAll<HTMLElement>("[data-msg-id]").forEach((row) => {
        const id = row.dataset.msgId;
        if (!id) return;
        const rect = row.getBoundingClientRect();
        rows.set(id, {
          element: row,
          top: rect.top - scrollerRect.top + scroller.scrollTop,
          height: row.getBoundingClientRect().height,
        });
      });
      return rows;
    };

    scrollHeightStabilizerActiveRef.current = true;
    scroller.style.overflowAnchor = "none";
    rowSnapshotsRef.current = readRows();
    listHeightRef.current = scroller.scrollHeight;
    listScrollTopRef.current = scroller.scrollTop;
    wasNearBottomRef.current =
      scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 100;

    const observer = new ResizeObserver(() => {
      const currentRows = readRows();
      const previousRows = rowSnapshotsRef.current;
      const previousHeight = listHeightRef.current;
      const previousScrollTop = listScrollTopRef.current;
      const currentHeight = scroller.scrollHeight;
      const currentScrollTop = scroller.scrollTop;
      const wasNearBottom = wasNearBottomRef.current;
      let delta = 0;

      // If the scroll position already moved with the content, the page's
      // bottom-follow effect has handled that growth. Only compensate the
      // remaining layout delta; otherwise this would double-adjust the tail.
      if (
        wasNearBottom &&
        previousHeight !== null &&
        Math.abs(currentScrollTop - previousScrollTop) < 1
      ) {
        delta = currentHeight - previousHeight;
      } else if (!wasNearBottom) {
        const viewportTop = scroller.scrollTop;
        const viewportBottom = viewportTop + scroller.clientHeight;
        // Anchor to the first row currently intersecting the viewport. If a
        // row above the viewport grows, this row moves and the displacement
        // must be compensated. If the anchored row itself grows, its top is
        // unchanged, so the user's reading position remains stable.
        let anchor: { element: HTMLElement; top: number } | null = null;
        for (const previous of previousRows.values()) {
          if (previous.top + previous.height > viewportTop + 1) {
            anchor = previous;
            break;
          }
          if (previous.top >= viewportBottom) break;
        }
        if (anchor) {
          const current = currentRows.get(anchor.element.dataset.msgId ?? "");
          if (current) delta = current.top - anchor.top;
        }
      }

      if (Math.abs(delta) > 0.5) {
        stabilizerAdjustingRef.current = true;
        scroller.scrollTop += delta;
        stabilizerAdjustingRef.current = false;
      }

      rowSnapshotsRef.current = readRows();
      listHeightRef.current = scroller.scrollHeight;
      listScrollTopRef.current = scroller.scrollTop;
      wasNearBottomRef.current =
        scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <
        100;
    });
    observer.observe(content);
    scroller
      .querySelectorAll<HTMLElement>("[data-msg-id]")
      .forEach((row) => observer.observe(row));

    return () => {
      observer.disconnect();
      rowSnapshotsRef.current.clear();
      listHeightRef.current = null;
      scrollHeightStabilizerActiveRef.current = false;
      scroller.style.overflowAnchor = previousOverflowAnchor;
    };
  }, [messages.length]);
  // Scroll anchor captured before an incremental history load. After the new
  // page is committed we restore the anchor's viewport offset so prepending
  // older messages (or appending newer ones) never yanks the rows the user is
  // currently reading out of view. This is deterministic and does not depend on
  // the browser choosing a valid scroll-anchor node (which can fail when every
  // visible row is still a lazy-markdown fallback).
  const pendingScrollAnchorRef = useRef<{
    anchorId: string;
    anchorTop: number;
  } | null>(null);
  // Tracks whether we have temporarily disabled the scroller's native CSS
  // scroll anchoring for an in-flight history transaction.
  const nativeScrollAnchorSuppressedRef = useRef(false);
  // Generation counters so a delayed re-enable/guard-clear from an older
  // transaction cannot clobber a newer transaction that has already started.
  const nativeScrollAnchorSuppressTokenRef = useRef(0);
  const restoringHistoryScrollTokenRef = useRef(0);
  // True while the manual anchor restore is applying its programmatic
  // scrollTop adjustment. The scroll event generated by that adjustment must
  // not be mistaken for a new user scroll gesture and trigger another page.
  const restoringHistoryScrollRef = useRef(false);
  // Previous scrollTop, used to require a real user scroll in the expected
  // direction before paging. This makes the trigger independent of rAF timing
  // across browsers.
  const lastScrollTopRef = useRef(0);
  // While a file jump is in flight (and until its scroll has been applied),
  // sentinel-triggered history loads must stay quiet. Otherwise the new window
  // can land with a sentinel visible and immediately load another page, yanking
  // the view away from the jump target.
  const suppressHistoryLoadRef = useRef(false);
  const stickToBottomRef = useRef(true);
  const [showScrollDown, setShowScrollDown] = useState(false);
  const [jumpMessageId, setJumpMessageId] = useState<string | null>(null);
  const jumpMessageIdRef = useRef<string | null>(null);

  // When embedded with a scrollToMessageId (the Activity detail pane pointing
  // at the message an activity references), scroll the main list to that
  // message once the channel's messages are mounted. Runs at most once per id
  // so it does not fight the user's own scrolling. Mirrors the thread deep-link
  // scroll.
  const scrollToMessageRef = useRef<string>("");
  useEffect(() => {
    if (!scrollToMessageId || messages.length === 0) return;
    if (scrollToMessageRef.current === scrollToMessageId) return;
    scrollToMessageRef.current = scrollToMessageId;
    requestAnimationFrame(() => {
      scrollRef.current
        ?.querySelector(`[data-msg-id="${scrollToMessageId}"]`)
        ?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
  }, [scrollToMessageId, messages.length]);

  // When embedded with a scrollToReadVersion (the Activity detail pane for a
  // DM, pointing at the user's last-read position), scroll the list to the
  // first message whose room_version exceeds the read cursor — the first
  // unread message, where the user resumes reading. If every loaded message
  // is already read (cursor caught up), stick to the bottom instead. Runs at
  // most once per version so it does not fight the user's own scrolling.
  const scrollToReadVersionRef = useRef<bigint>(0n);
  // biome-ignore lint/correctness/useExhaustiveDependencies: deep-link scroll runs at most once per version; keyed on length for mount timing.
  useEffect(() => {
    if (scrollToReadVersion <= 0n || messages.length === 0) return;
    if (scrollToReadVersionRef.current === scrollToReadVersion) return;
    scrollToReadVersionRef.current = scrollToReadVersion;
    // Find the first message strictly past the read cursor. Messages are
    // chronological (oldest first), so the first match is the resume point.
    const target = messages.find(
      (m) => (m.roomVersion ?? 0n) > scrollToReadVersion
    );
    // Defer so the layout has settled after the messages update.
    requestAnimationFrame(() => {
      if (target) {
        // Park at the resume point and release stick-to-bottom so the
        // background poller does not yank the user back down while they read
        // forward from their last-read position.
        stickToBottomRef.current = false;
        scrollRef.current
          ?.querySelector(`[data-msg-id="${target.id}"]`)
          ?.scrollIntoView({ block: "center", behavior: "smooth" });
      } else if (scrollRef.current) {
        // Caught up — land at the bottom.
        stickToBottomRef.current = true;
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
      }
    });
  }, [scrollToReadVersion, messages.length]);

  // When a file drawer jump is requested, scroll the focused window to the
  // target message once it has loaded. Runs at most once per target id so it
  // does not fight the user's own scrolling.
  useLayoutEffect(() => {
    // jumpMessageId is the intended target set by the caller (deep-link, file
    // jump, channel search). It can be reset to null when the
    // channel mounts (and again by React StrictMode's double-invoke), so fall
    // back to the committed jump anchor when the intent was cleared but the
    // jump window is already active.
    const targetId = jumpMessageId || jumpTarget?.messageId || null;
    if (!targetId) return;
    // setJumpMessageId renders before jumpToMessage finishes, while the old
    // window is still on screen. Scrolling then would center the target in the
    // old window; after the focused window replaces it, that scrollTop no
    // longer points at the target. Wait until the jump anchor for this exact
    // message is active, then scroll once.
    if (jumpTarget?.messageId !== targetId) return;
    if (jumpMessageIdRef.current === targetId) return;
    jumpMessageIdRef.current = targetId;
    stickToBottomRef.current = false;

    const scroller = scrollRef.current;
    if (!scroller || messages.length === 0) {
      suppressHistoryLoadRef.current = false;
      return;
    }

    let programmaticScrollTop = scroller.scrollTop;
    const centerTarget = () => {
      const target = scroller.querySelector(`[data-msg-id="${targetId}"]`);
      if (!target) return;
      // Center the target inside the message scroller only. scrollIntoView
      // would also scroll every scrollable ancestor, which can leave the
      // message list offset when the page itself is scrollable. Apply the
      // position before paint so no sentinel-triggered load can interleave
      // with a smooth-scroll animation and move the target again.
      const targetTop =
        target.getBoundingClientRect().top -
        scroller.getBoundingClientRect().top;
      const targetCenter =
        targetTop + target.getBoundingClientRect().height / 2;
      const scrollerCenter = scroller.clientHeight / 2;
      scroller.scrollTop = scroller.scrollTop + targetCenter - scrollerCenter;
      programmaticScrollTop = scroller.scrollTop;
    };

    restoringHistoryScrollTokenRef.current += 1;
    restoringHistoryScrollRef.current = true;
    centerTarget();
    const token = restoringHistoryScrollTokenRef.current;
    requestAnimationFrame(() => {
      if (restoringHistoryScrollTokenRef.current === token) {
        restoringHistoryScrollRef.current = false;
      }
    });

    // Async content (images, lazy markdown) can change row heights after the
    // jump and push the target off-center. Re-assert the centered position
    // whenever the message list resizes, until the user scrolls. Keep native
    // scroll anchoring off for the same window so the browser does not fight
    // the re-centering; the anchor-restore effect below would otherwise
    // re-enable it two frames after the jump.
    nativeScrollAnchorSuppressedRef.current = false;
    scroller.style.overflowAnchor = "none";

    const content = scroller.querySelector("[data-msg-id]")?.parentElement;
    const resizeObserver = content
      ? new ResizeObserver(() => centerTarget())
      : null;
    if (resizeObserver && content) resizeObserver.observe(content);

    let settled = false;
    const stopSettling = () => {
      if (settled) return;
      settled = true;
      resizeObserver?.disconnect();
      scroller.removeEventListener("wheel", stopSettling);
      scroller.removeEventListener("touchstart", stopSettling);
      scroller.removeEventListener("scroll", onUserScroll);
      scroller.style.overflowAnchor = "";
    };
    const onUserScroll = () => {
      // Ignore the scroll events emitted by our own re-centering; any other
      // scroll (wheel, keyboard, scrollbar) means the user took over.
      if (Math.abs(scroller.scrollTop - programmaticScrollTop) > 1) {
        stopSettling();
      }
    };
    scroller.addEventListener("wheel", stopSettling, { passive: true });
    scroller.addEventListener("touchstart", stopSettling, { passive: true });
    scroller.addEventListener("scroll", onUserScroll, { passive: true });

    suppressHistoryLoadRef.current = false;

    return () => {
      stopSettling();
    };
  }, [jumpMessageId, jumpTarget, messages.length]);

  // captureScrollAnchor records the viewport offset of a message before an
  // incremental page load. The layout effect below restores that offset after
  // React commits the new rows, which is what keeps the currently-visible
  // messages stationary while older/newer pages are inserted around them.
  const captureScrollAnchor = useCallback((anchorId: string) => {
    const scroller = scrollRef.current;
    const anchor = scroller?.querySelector(`[data-msg-id="${anchorId}"]`);
    if (!scroller || !anchor) return;
    pendingScrollAnchorRef.current = {
      anchorId,
      anchorTop:
        anchor.getBoundingClientRect().top -
        scroller.getBoundingClientRect().top,
    };
  }, []);

  // Native scroll anchoring is disabled only for the duration of a history
  // page transaction. The manual restore below owns the scroll position for
  // that commit; leaving native anchoring on would let the browser adjust the
  // same prepend/append a second time (or pick a fallback row as its anchor and
  // adjust differently). It is re-enabled two frames later, after the browser
  // has laid out the restored position.
  const suppressNativeScrollAnchor = useCallback(() => {
    const scroller = scrollRef.current;
    if (!scroller) return;
    nativeScrollAnchorSuppressTokenRef.current += 1;
    nativeScrollAnchorSuppressedRef.current = true;
    scroller.style.overflowAnchor = "none";
  }, []);

  const reenableNativeScrollAnchor = useCallback(() => {
    if (!scrollRef.current) return;
    const token = nativeScrollAnchorSuppressTokenRef.current;
    // The height stabilizer owns scroll anchoring while it is mounted. Do not
    // let a completed pagination transaction turn native anchoring back on
    // underneath it, which would apply both corrections to the same layout.
    if (scrollHeightStabilizerActiveRef.current) {
      nativeScrollAnchorSuppressedRef.current = false;
      return;
    }
    // The restore above runs in a layout effect, before the browser lays out
    // and paints the prepended rows. By the next animation frame that layout
    // has already happened, so native anchoring can be turned back on without
    // fighting the manual scrollTop adjustment.
    requestAnimationFrame(() => {
      if (nativeScrollAnchorSuppressTokenRef.current !== token) return;
      if (scrollRef.current) scrollRef.current.style.overflowAnchor = "";
      nativeScrollAnchorSuppressedRef.current = false;
    });
  }, []);

  // Restore the captured anchor after a history page is committed. This runs
  // before paint (useLayoutEffect) so the user never sees the jump that would
  // otherwise happen when older rows are prepended above the current viewport.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-runs on messages/jumpLoading identity as the history-commit trigger; the body reads refs only.
  useLayoutEffect(() => {
    // Only restore once the history request has actually committed. While
    // jumpLoading is still true, a watcher append or optimistic send can change
    // `messages`; restoring then would anchor against a list that has not yet
    // been prepended/appended and yank the viewport.
    if (jumpLoading) return;

    const pending = pendingScrollAnchorRef.current;
    pendingScrollAnchorRef.current = null;
    let didRestore = false;
    if (pending) {
      const scroller = scrollRef.current;
      const anchor = scroller?.querySelector(
        `[data-msg-id="${pending.anchorId}"]`
      );
      if (scroller && anchor) {
        restoringHistoryScrollTokenRef.current += 1;
        restoringHistoryScrollRef.current = true;
        const nextTop =
          anchor.getBoundingClientRect().top -
          scroller.getBoundingClientRect().top;
        scroller.scrollTop += nextTop - pending.anchorTop;
        didRestore = true;
      }
    }

    // Whether the load succeeded, failed, or produced no anchor, native
    // anchoring must be turned back on after this commit has been laid out.
    if (nativeScrollAnchorSuppressedRef.current) {
      reenableNativeScrollAnchor();
    }

    // The programmatic scrollTop change above emits a scroll event before the
    // next animation frame. Keep the guard up through that event so it cannot
    // immediately trigger another history page.
    if (didRestore) {
      const token = restoringHistoryScrollTokenRef.current;
      // The programmatic scroll event emitted by the scrollTop change above is
      // delivered before the next animation frame, so one frame is enough to
      // keep the guard up through that event.
      requestAnimationFrame(() => {
        if (restoringHistoryScrollTokenRef.current === token) {
          restoringHistoryScrollRef.current = false;
        }
      });
    }
  }, [messages, jumpLoading, reenableNativeScrollAnchor]);

  // Keep the scroll handler's view of messages current without making the
  // handler depend on the messages array identity.
  useEffect(() => {
    messagesRef.current = messages;
  }, [messages]);

  const scrollToBottom = useCallback(async () => {
    // When viewing a focused jump window, the scroll-to-latest button exits
    // jump mode and reloads the real latest messages first.
    if (jumpTarget) {
      await clearJump(conversationName);
      // Reset the jump-scroll latch so clicking the same file again later
      // scrolls to it instead of being treated as an already-handled jump.
      setJumpMessageId(null);
      jumpMessageIdRef.current = null;
      suppressHistoryLoadRef.current = false;
    }
    stickToBottomRef.current = true;
    if (scrollRef.current) {
      scrollRef.current.scrollTo({
        top: scrollRef.current.scrollHeight,
        behavior: "smooth",
      });
    }
  }, [jumpTarget, conversationName, clearJump]);

  // A focused jump window is a history view, not the live tail: never
  // auto-stick to the bottom while it is open, even if the user happens to be
  // near the bottom sentinel loading newer pages.
  useEffect(() => {
    if (jumpTarget) {
      stickToBottomRef.current = false;
    }
  }, [jumpTarget]);

  // Auto-stick before the browser paints. Using a passive effect here leaves
  // one frame at the previous scroll position after a new message or a
  // Markdown height change, which looks like a brief flash of different
  // content even though the final scroll position is correct.
  // biome-ignore lint/correctness/useExhaustiveDependencies: auto-stick is keyed on messages/jumpTarget changes; the body reads none of them.
  useLayoutEffect(() => {
    if (scrollRef.current && stickToBottomRef.current && !jumpTarget) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, jumpTarget]);

  const handleScroll = useCallback(() => {
    const scroller = scrollRef.current;
    if (!scroller) return;
    const { scrollTop, scrollHeight, clientHeight } = scroller;
    const prevScrollTop = lastScrollTopRef.current;
    lastScrollTopRef.current = scrollTop;
    const nearBottom = scrollHeight - scrollTop - clientHeight < 100;
    // In jump mode the bottom sentinel is a "load more history" affordance,
    // not the live tail, so reaching it must not re-enable auto-stick.
    if (!jumpTarget) {
      stickToBottomRef.current = nearBottom;
    }
    wasNearBottomRef.current = nearBottom;
    setShowScrollDown(!nearBottom);

    // Ignore the scroll event emitted by the height stabilizer's own
    // correction. It is not a user gesture and must not page history.
    if (stabilizerAdjustingRef.current) return;

    // History paging is driven by scroll position rather than IntersectionObserver
    // transitions. IO only fires when a sentinel crosses the root boundary; after
    // a small page is prepended the sentinel can remain inside the rootMargin,
    // so the next page never triggers until the user scrolls away and back. A
    // scroll-position check fires on every scroll gesture and is therefore
    // immune to that "stuck at Load earlier messages" state. Requiring the
    // scroll to move in the expected direction also ignores the programmatic
    // scroll event emitted by our own anchor restore.
    if (suppressHistoryLoadRef.current) return;
    if (restoringHistoryScrollRef.current) return;

    if (isJumpLoading()) return;

    const nearTop = scrollTop <= 80;
    if (nearTop && scrollTop < prevScrollTop && hasOlderMessages()) {
      const anchorId = messagesRef.current[0]?.id;
      if (!anchorId) return;
      captureScrollAnchor(anchorId);
      suppressNativeScrollAnchor();
      void loadOlderMessages(conversationName);
      return;
    }

    if (nearBottom && scrollTop > prevScrollTop && hasNewerMessages()) {
      const anchorId = messagesRef.current[messagesRef.current.length - 1]?.id;
      if (!anchorId) return;
      captureScrollAnchor(anchorId);
      suppressNativeScrollAnchor();
      void loadNewerMessages(conversationName);
    }
  }, [
    conversationName,
    jumpTarget,
    captureScrollAnchor,
    suppressNativeScrollAnchor,
    loadOlderMessages,
    loadNewerMessages,
    hasOlderMessages,
    hasNewerMessages,
    isJumpLoading,
  ]);

  const beginJumpWindow = useCallback(
    (messageId: string) => {
      // Entering a focused history view: release stick-to-bottom before the
      // window loads so the auto-stick effect never yanks the list to the
      // bottom of the jump window, and drop any stale scroll anchor/latch.
      stickToBottomRef.current = false;
      jumpMessageIdRef.current = null;
      suppressHistoryLoadRef.current = true;
      suppressNativeScrollAnchor();
      setJumpMessageId(messageId);
    },
    [suppressNativeScrollAnchor]
  );

  const releaseHistorySuppression = useCallback(() => {
    suppressHistoryLoadRef.current = false;
  }, []);

  const resetScrollState = useCallback(() => {
    setJumpMessageId(null);
    jumpMessageIdRef.current = null;
    suppressHistoryLoadRef.current = false;
    pendingScrollAnchorRef.current = null;
    nativeScrollAnchorSuppressedRef.current = false;
    restoringHistoryScrollRef.current = false;
    lastScrollTopRef.current = 0;
    if (scrollRef.current) {
      scrollRef.current.style.overflowAnchor =
        scrollHeightStabilizerActiveRef.current ? "none" : "";
    }
    stickToBottomRef.current = true;
  }, []);

  return {
    scrollRef,
    handleScroll,
    showScrollDown,
    scrollToBottom,
    beginJumpWindow,
    releaseHistorySuppression,
    resetScrollState,
  };
}
