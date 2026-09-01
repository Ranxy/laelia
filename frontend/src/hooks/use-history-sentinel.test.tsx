import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
  platformOwnsEdgeSwipe: vi.fn(() => true),
}));

vi.mock("@/lib/platform-edge-swipe", () => ({
  platformOwnsEdgeSwipe: mock.platformOwnsEdgeSwipe,
}));

import { useHistorySentinel } from "./use-history-sentinel";

const SENTINEL_KEY = "laelia.historySentinel";

function currentToken(): string | undefined {
  const state = window.history.state as Record<string, unknown> | null;
  const token = state?.[SENTINEL_KEY];
  return typeof token === "string" ? token : undefined;
}

function Harness({
  active,
  onClose,
}: {
  active: boolean;
  onClose: () => void;
}) {
  useHistorySentinel(active, onClose);
  return <div />;
}

describe("useHistorySentinel", () => {
  beforeEach(() => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    window.history.replaceState(null, "");
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    window.history.replaceState(null, "");
    document.body.innerHTML = "";
  });

  it("is inert when the platform does not own edge swipes", () => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(false);
    render(<Harness active onClose={vi.fn()} />);
    expect(currentToken()).toBeUndefined();
  });

  it("pushes a tokenized sentinel while active and closes on popstate", () => {
    const onClose = vi.fn();
    render(<Harness active onClose={onClose} />);
    const token = currentToken();
    expect(token).toEqual(expect.any(String));

    // The system back gesture / back button consumed the sentinel: the
    // overlay is dismissed instead of leaving the page.
    window.history.replaceState(null, "");
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("does not close when a stacked overlay's sentinel was popped", () => {
    const onCloseA = vi.fn();
    const onCloseB = vi.fn();
    render(<Harness active onClose={onCloseA} />);
    const tokenA = currentToken();
    render(<Harness active onClose={onCloseB} />);
    const tokenB = currentToken();
    expect(tokenB).not.toBe(tokenA);

    // The top sentinel (B) was popped; A's sentinel is still on top, so only
    // B's overlay closes.
    window.history.replaceState({ [SENTINEL_KEY]: tokenA }, "");
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(onCloseB).toHaveBeenCalledTimes(1);
    expect(onCloseA).not.toHaveBeenCalled();
  });

  it("consumes the sentinel when the overlay closes through its own UI", () => {
    const back = vi.spyOn(window.history, "back");
    const onClose = vi.fn();
    const { rerender } = render(<Harness active onClose={onClose} />);
    expect(currentToken()).toEqual(expect.any(String));

    act(() => {
      rerender(<Harness active={false} onClose={onClose} />);
    });
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(back).toHaveBeenCalled();
    back.mockRestore();
  });

  it("cancels its own cleanup pop and reuses the sentinel on quick reopen", () => {
    const back = vi.spyOn(window.history, "back");
    const onClose = vi.fn();
    const { rerender } = render(<Harness active onClose={onClose} />);
    const firstToken = currentToken();

    // Deactivate and immediately reactivate (StrictMode remount / rapid
    // toggle): the cleanup's pop is cancelled before it fires and the
    // existing sentinel is reused — no back(), no stack growth.
    act(() => {
      rerender(<Harness active={false} onClose={onClose} />);
      rerender(<Harness active onClose={onClose} />);
    });
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(back).not.toHaveBeenCalled();
    expect(currentToken()).toBe(firstToken);

    // The re-armed sentinel still dismisses the overlay.
    window.history.replaceState(null, "");
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
    back.mockRestore();
  });
});
