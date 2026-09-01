# Chat Scroll Anchoring & History Pagination

## Context

The channel chat list (`frontend/src/pages/dashboard/chat-conversation.tsx`) combines
three features that all affect scroll position:

- lazy markdown rendering (`frontend/src/components/chat/lazy-markdown.tsx`)
- incremental history paging (`loadOlderMessages` / `loadNewerMessages`)
- native CSS scroll anchoring (`overflow-anchor`)

When these features are changed independently, the chat list can jump, get stuck,
or flicker while the user scrolls through history. This note records the root
causes and the rules to follow when touching any of them. The important distinction
is between a scroll-position jump and a one-frame flash: a jump is usually an
incorrect `scrollTop`, while a flash is usually a real intermediate DOM/layout
state being painted before the next correction runs.

## Rule 1: Let one owner control layout-shift anchoring

The message scroller has a dedicated `ResizeObserver`-based stabilizer in
`use-message-scroller.ts`. It disables native `overflow-anchor` while mounted,
measures message rows in scroller-content coordinates, and compensates height
changes itself. This is intentional: native anchoring and manual compensation
must not run at the same time, or both can adjust the same layout shift.

- The stabilizer is the owner of ordinary Markdown/image/layout-shift
  compensation. Do not re-enable native anchoring underneath it.
- History pagination still temporarily suppresses native anchoring through its
  transaction guard, but the stabilizer's suppression takes precedence and
  keeps it disabled until the stabilizer is torn down.
- Keep the stabilizer's snapshots and `scrollTop` baseline synchronized after
  every compensation and after user scroll events. Otherwise the next
  `ResizeObserver` callback can interpret a user scroll as an automatic shift.
- Do not add a second `scrollTop` correction in a `requestAnimationFrame` after
  the observer callback. That paints an intermediate frame and is the source of
  the short flash this note is intended to prevent.

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
4. Request native anchoring to be re-enabled on the next animation frame only
   when the stabilizer is not active. In the normal chat list the stabilizer
   remains the owner, so this request must not overwrite `overflow-anchor: none`.

This keeps the rows the user is reading stationary regardless of whether the
browser has a usable anchor node. The manual pagination restore and the
ResizeObserver stabilizer must still update the same snapshots/baselines so the
next layout change is measured from the restored position.

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

## Rule 7: Complete automatic positioning before paint

The live-tail auto-stick path must use `useLayoutEffect`, not `useEffect`. A
passive effect runs after the browser paints, so a new message or a Markdown
height change can expose one frame at the old `scrollTop` before the list moves
to the bottom. The same rule applies to any correction that is required to make
the committed DOM and its scroll position appear atomically:

- Use `useLayoutEffect` for automatic bottom-following and pagination anchor
  restoration.
- Apply `ResizeObserver` height compensation synchronously in its callback;
  do not defer the correction to `requestAnimationFrame`.
- Keep `requestAnimationFrame` for non-visual guards, such as clearing the
  programmatic-scroll flag after the browser has delivered its scroll event.

## Rule 8: Windowing must not create an intermediate render state

Light-windowing replaces distant message rows with fixed-height placeholders.
That optimization must not briefly replace the whole list with real rows during
pagination or swap a visible row through a fallback while scrolling:

- Preserve measured row heights and the current window when `rowIds` changes;
  do not reset to full rendering and then window again.
- Recompute a changed window in a `useLayoutEffect` so the range is settled
  before paint.
- Rows inside the active light-window should render Markdown eagerly. Rows
  outside it can remain height-only placeholders. If a mounted row becomes part
  of the active window, promote its lazy Markdown state in a layout effect so
  the user does not see a raw-text fallback for one frame.
- Keep window margins and overscan large enough that ordinary scrolling does
  not repeatedly mount/unmount rows at the viewport edge.

The goal is not only stable geometry; it is a single visually coherent commit.
A layout that is corrected on the next frame may still feel like a flash even
when the final `scrollTop` is correct.


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
