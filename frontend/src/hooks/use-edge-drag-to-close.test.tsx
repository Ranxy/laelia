import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
  useIsDesktop: vi.fn(() => false),
}));

vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: mock.useIsDesktop,
}));

import { useEdgeDragToClose } from "./use-edge-drag-to-close";

function Harness({ onClose = vi.fn() }: { onClose?: () => void }) {
  const { setPopup, setOverlay } = useEdgeDragToClose({
    open: true,
    onClose,
  });
  return (
    <div>
      <div ref={setOverlay} data-testid="overlay" />
      <div ref={setPopup} data-testid="popup" />
    </div>
  );
}

interface Point {
  clientX: number;
  clientY: number;
}

function touch(type: string, points: Point[]) {
  const evt = new Event(type, { cancelable: true, bubbles: true }) as Event & {
    touches: Point[];
    changedTouches: Point[];
  };
  Object.defineProperty(evt, "touches", { value: points, configurable: true });
  Object.defineProperty(evt, "changedTouches", {
    value: points,
    configurable: true,
  });
  return evt;
}

function swipeFromEdge(target: HTMLElement, dx: number, dy = 0) {
  target.dispatchEvent(touch("touchstart", [{ clientX: 10, clientY: 100 }]));
  target.dispatchEvent(
    touch("touchmove", [{ clientX: 10 + dx, clientY: 100 + dy }])
  );
  target.dispatchEvent(
    touch("touchend", [{ clientX: 10 + dx, clientY: 100 + dy }])
  );
}

describe("useEdgeDragToClose", () => {
  beforeEach(() => {
    mock.useIsDesktop.mockReturnValue(false);
    Object.defineProperty(window, "innerWidth", {
      value: 375,
      configurable: true,
    });
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
  });

  it("is inert on desktop", () => {
    mock.useIsDesktop.mockReturnValue(true);
    render(<Harness />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => swipeFromEdge(overlay, 200));
    expect(popup.style.transform).toBe("");
    expect(overlay.style.opacity).toBe("");
  });

  it("ignores touches that start outside the left edge zone", () => {
    render(<Harness />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => {
      overlay.dispatchEvent(
        touch("touchstart", [{ clientX: 100, clientY: 100 }])
      );
      overlay.dispatchEvent(
        touch("touchmove", [{ clientX: 300, clientY: 110 }])
      );
      overlay.dispatchEvent(
        touch("touchend", [{ clientX: 300, clientY: 110 }])
      );
    });
    expect(popup.style.transform).toBe("");
    expect(overlay.style.opacity).toBe("");
  });

  it("ignores touches that start outside the sheet", () => {
    render(<Harness />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => {
      // On the page body, not on the popup or the overlay: the gesture only
      // owns touches that begin on the sheet surface itself.
      document.body.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      document.body.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
      document.body.dispatchEvent(
        touch("touchend", [{ clientX: 200, clientY: 110 }])
      );
    });
    expect(popup.style.transform).toBe("");
    expect(overlay.style.opacity).toBe("");
  });

  it("ignores vertical and leftward swipes", () => {
    render(<Harness />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => swipeFromEdge(overlay, 0, 200));
    expect(popup.style.transform).toBe("");
    act(() => swipeFromEdge(overlay, -60));
    expect(popup.style.transform).toBe("");
  });

  it("slides the sheet and fades the scrim while dragging", () => {
    render(<Harness />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => {
      overlay.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      overlay.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
    });
    // 190px of drag, capped at half the 375px viewport. The scrim is fully
    // transparent by the time the sheet reaches the drag limit.
    expect(popup.style.transform).toBe("translateX(187.5px)");
    expect(overlay.style.opacity).toBe("0");
  });

  it("closes past the threshold", () => {
    const onClose = vi.fn();
    render(<Harness onClose={onClose} />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => swipeFromEdge(overlay, 200));
    expect(popup.style.transform).toBe("translateX(100%)");
    expect(overlay.style.opacity).toBe("0");
    expect(onClose).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("springs back below the threshold", () => {
    const onClose = vi.fn();
    render(<Harness onClose={onClose} />);
    const popup = screen.getByTestId("popup");
    const overlay = screen.getByTestId("overlay");
    act(() => swipeFromEdge(overlay, 50));
    expect(popup.style.transform).toBe("translateX(0px)");
    expect(overlay.style.opacity).toBe("1");
    expect(onClose).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(popup.style.transform).toBe("");
    expect(overlay.style.opacity).toBe("");
    expect(onClose).not.toHaveBeenCalled();
  });
});
