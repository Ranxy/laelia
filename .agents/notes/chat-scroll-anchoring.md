# Chat Scroll Anchoring & History Pagination

## Context

The channel chat list (`frontend/src/pages/dashboard/chat-conversation.tsx`) combines
three features that all affect scroll position:

- lazy markdown rendering (`frontend/src/components/chat/lazy-markdown.tsx`)
- incremental history paging (`loadOlderMessages` / `loadNewerMessages`)
- native CSS scroll anchoring (`overflow-anchor`)

When these features are changed independently, the chat list can jump, get stuck,
or flicker while the user scrolls through history. This note records the root
causes and the rules to follow when touching any of them.

## Rule 1: Never disable native scroll anchoring globally

`overflow-anchor: none` on the message scroller disables the browser's ability to
absorb ordinary layout shifts, including lazy-markdown fallback -> markdown swaps.

- Keep the scroller's native `overflow-anchor` enabled by default.
- Only disable it for the exact duration of a history-page transaction, and
  re-enable it immediately after the manual restore has been laid out.
- Do not key the suppression off `hasOlder || hasNewer`; that is effectively
  permanent for any paginated conversation.

## Rule 2: Pagination must use a deterministic manual anchor

Do not rely on the browser to choose a valid scroll-anchor node during a prepend
or append. It can fail when every visible row is still a lazy-markdown fallback
(those rows intentionally opt out of anchoring) or when the sentinel is excluded.

The correct pattern is:

1. Before calling `loadOlderMessages` / `loadNewerMessages`, capture the viewport
   offset of a stable message:
   - older page: first message in the current list
   - newer page: last message in the current list
2. Temporarily set `scroller.style.overflowAnchor = "none"`.
3. After the new page commits, restore the captured message's offset in a
   `useLayoutEffect` (before paint).
4. Re-enable native anchoring on the next animation frame.

This keeps the rows the user is reading stationary regardless of whether the
browser has a usable anchor node.

## Rule 3: Drive paging from scroll position, not IntersectionObserver transitions

`IntersectionObserver` only fires when a sentinel crosses the root boundary.
After a small page is prepended, the sentinel can remain inside the rootMargin,
so the next page never triggers until the user scrolls away and back. This is the
"stuck at Load earlier messages" bug.

Use the scroll handler instead:

- older page: `scrollTop <= threshold` and `hasOlder`
- newer page: `scrollHeight - scrollTop - clientHeight < threshold` and `hasNewer`

Also require the scroll to move in the expected direction:

- older page: `scrollTop < previousScrollTop`
- newer page: `scrollTop > previousScrollTop`

The direction check naturally ignores the programmatic scroll event emitted by
the manual anchor restore.

## Rule 4: Guard against self-triggered scroll events

The manual restore changes `scrollTop`, which emits a scroll event. Without a
guard, that event can immediately trigger another page load.

Use a short-lived `restoringHistoryScrollRef` guard:

- set it before applying the manual `scrollTop` adjustment
- clear it on the next animation frame (after the programmatic scroll event has
  been delivered)

Use generation tokens for delayed re-enable / guard-clear callbacks so an older
transaction cannot clobber a newer one that has already started.

## Rule 5: Keep loading indicators height-stable

The top/bottom sentinels switch between text and a spinner while `jumpLoading`
is true. If the two states have different heights, that height change happens
while native anchoring is suppressed and causes a brief visible flicker.

Give the sentinel a fixed height and center its content:

```tsx
className="flex h-6 items-center justify-center overflow-hidden whitespace-nowrap text-control-placeholder"
```

Any element that changes content during a scroll transaction must keep the same
box height in all states.

## Rule 6: Lazy markdown rows must opt out of anchoring while in fallback state

A lazy-markdown row showing raw text has a different height than the final
rendered markdown. If the browser picks that row as its scroll anchor, the
fallback -> markdown swap keeps the row's top edge fixed while pushing the
content below it down.

`LazyMarkdown` therefore sets `overflow-anchor: none` on its `[data-msg-id]`
wrapper while the fallback is visible, and restores it after the markdown has
rendered and the browser has compensated for the height change.

Do not remove this exclusion without replacing it with an equivalent guarantee.

## Checklist for future changes

- Does the scroller keep native `overflow-anchor` enabled outside history-page
  transactions?
- Does every prepend/append capture and restore a deterministic message anchor?
- Is paging triggered by scroll position and scroll direction, not by sentinel
  intersection transitions?
- Are programmatic `scrollTop` changes guarded so they cannot trigger another
  page?
- Do sentinels/loading indicators keep a constant height across loading states?
- Do lazy/fallback rows remain excluded from native anchor selection until their
  final height is known?
