import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useMessageScroller } from "./use-message-scroller";

// The page passes its own store-backed actions; these unit tests exercise the
// scroll-position state machine itself: direction/threshold guards (a fast or
// programmatic scroll must not trigger a page load), the jump-window
// suppression, and scroll-to-latest exiting a focused jump window.
interface HarnessProps {
  hasOlderMessages?: () => boolean;
  hasNewerMessages?: () => boolean;
  isJumpLoading?: () => boolean;
  jumpTarget?: { messageId: string } | null;
  loadOlderMessages?: () => Promise<void> | void;
  loadNewerMessages?: () => Promise<void> | void;
  clearJump?: () => Promise<void> | void;
}

function Harness(props: HarnessProps) {
  const {
    beginJumpWindow,
    releaseHistorySuppression,
    scrollToBottom,
    showScrollDown,
    scrollRef,
    handleScroll,
  } = useMessageScroller({
    conversationName: "conversations/c1",
    messages: [{ id: "m1" }, { id: "m2" }, { id: "m3" }],
    loadOlderMessages: props.loadOlderMessages ?? (() => Promise.resolve()),
    loadNewerMessages: props.loadNewerMessages ?? (() => Promise.resolve()),
    hasOlderMessages: props.hasOlderMessages ?? (() => true),
    hasNewerMessages: props.hasNewerMessages ?? (() => true),
    isJumpLoading: props.isJumpLoading ?? (() => false),
    jumpTarget: props.jumpTarget ?? null,
    jumpLoading: false,
    clearJump: props.clearJump ?? (() => Promise.resolve()),
  });
  return (
    <div>
      <div ref={scrollRef} onScroll={handleScroll} data-testid="scroller" />
      <button
        type="button"
        data-testid="begin-jump"
        onClick={() => beginJumpWindow("m2")}
      />
      <button
        type="button"
        data-testid="release"
        onClick={releaseHistorySuppression}
      />
      <button
        type="button"
        data-testid="scroll-bottom"
        onClick={() => void scrollToBottom()}
      />
      <span data-testid="show-scroll-down">{String(showScrollDown)}</span>
    </div>
  );
}

// jsdom has no layout, so scrollTop is written directly onto the element
// before dispatching the scroll event the handler reacts to.
function scrollTo(top: number) {
  const scroller = screen.getByTestId("scroller");
  Object.defineProperty(scroller, "scrollTop", {
    value: top,
    configurable: true,
  });
  act(() => {
    scroller.dispatchEvent(new Event("scroll"));
  });
}

describe("useMessageScroller", () => {
  it("pages older messages on a genuine upward scroll near the top", async () => {
    const loadOlder = vi.fn();
    // Seed move goes down and hasNewer is false so the seed cannot page.
    render(
      <Harness hasNewerMessages={() => false} loadOlderMessages={loadOlder} />
    );
    scrollTo(100);
    scrollTo(40);
    await vi.waitFor(() =>
      expect(loadOlder).toHaveBeenCalledWith("conversations/c1")
    );
  });

  it("does not page older when scrolling down inside the top zone", () => {
    const loadOlder = vi.fn();
    const loadNewer = vi.fn();
    render(
      <Harness loadOlderMessages={loadOlder} loadNewerMessages={loadNewer} />
    );
    // All moves are downward (or the upward threshold is never crossed at the
    // top): the direction guard must keep the older-side paging quiet.
    scrollTo(10);
    scrollTo(50);
    scrollTo(70);
    expect(loadOlder).not.toHaveBeenCalled();
  });

  it("stays quiet while a history page load is in flight", () => {
    const loadOlder = vi.fn();
    render(
      <Harness
        isJumpLoading={() => true}
        hasNewerMessages={() => false}
        loadOlderMessages={loadOlder}
      />
    );
    scrollTo(100);
    scrollTo(40);
    expect(loadOlder).not.toHaveBeenCalled();
  });

  it("suppresses sentinel paging while a jump window is open and re-allows after release", async () => {
    const loadOlder = vi.fn();
    render(
      <Harness hasNewerMessages={() => false} loadOlderMessages={loadOlder} />
    );
    scrollTo(100);
    act(() => {
      screen.getByTestId("begin-jump").click();
    });
    scrollTo(40);
    expect(loadOlder).not.toHaveBeenCalled();

    act(() => {
      screen.getByTestId("release").click();
    });
    scrollTo(20);
    await vi.waitFor(() => expect(loadOlder).toHaveBeenCalled());
  });

  it("exits the focused jump window on scroll-to-latest", async () => {
    const clearJump = vi.fn();
    render(<Harness jumpTarget={{ messageId: "m2" }} clearJump={clearJump} />);
    // jsdom does not implement Element.scrollTo.
    const scrollerEl = screen.getByTestId("scroller");
    (scrollerEl as HTMLElement & { scrollTo: unknown }).scrollTo = vi.fn();
    act(() => {
      screen.getByTestId("scroll-bottom").click();
    });
    await vi.waitFor(() =>
      expect(clearJump).toHaveBeenCalledWith("conversations/c1")
    );
    expect(screen.getByTestId("show-scroll-down").textContent).toBe("false");
  });
});
