import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
  useIsDesktop: vi.fn(() => false),
  navigate: vi.fn(),
  routeName: null as string | null,
  activeThreadRoot: null as string | null,
  closeThread: vi.fn(),
}));

vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: mock.useIsDesktop,
}));
vi.mock("react-router-dom", () => ({
  useNavigate: () => mock.navigate,
}));
vi.mock("@/router/use-current-route", () => ({
  useCurrentRoute: () => ({ name: mock.routeName }),
}));
vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: unknown) => unknown) =>
    selector({
      closeThread: mock.closeThread,
      activeThreadRoot: mock.activeThreadRoot,
    }),
}));

import { useSwipeBack } from "./use-swipe-back";

function Harness() {
  const ref = useSwipeBack();
  return (
    <div ref={ref} data-testid="shell">
      <div data-bb-layer-family="overlay">
        <div data-testid="overlay-target" />
      </div>
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

function swipeFromEdge(dx: number, dy = 0) {
  window.dispatchEvent(touch("touchstart", [{ clientX: 10, clientY: 100 }]));
  window.dispatchEvent(
    touch("touchmove", [{ clientX: 10 + dx, clientY: 100 + dy }])
  );
  window.dispatchEvent(
    touch("touchend", [{ clientX: 10 + dx, clientY: 100 + dy }])
  );
}

describe("useSwipeBack", () => {
  beforeEach(() => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.navigate.mockReset();
    mock.closeThread.mockReset();
    mock.routeName = "chat.detail";
    mock.activeThreadRoot = null;
    Object.defineProperty(window, "innerWidth", {
      value: 375,
      configurable: true,
    });
  });

  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("is inert on desktop", () => {
    mock.useIsDesktop.mockReturnValue(true);
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(200));
    expect(shell.style.transform).toBe("");
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("ignores touches that start outside the left edge zone", () => {
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => {
      window.dispatchEvent(
        touch("touchstart", [{ clientX: 100, clientY: 100 }])
      );
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 300, clientY: 110 }])
      );
      window.dispatchEvent(touch("touchend", [{ clientX: 300, clientY: 110 }]));
    });
    expect(shell.style.transform).toBe("");
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("ignores vertical scrolls", () => {
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(0, 200));
    expect(shell.style.transform).toBe("");
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("ignores leftward swipes (row actions)", () => {
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(-60));
    expect(shell.style.transform).toBe("");
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("slides the shell with the finger and navigates back past the threshold", () => {
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => {
      window.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
    });
    // 190px of drag, capped at half the 375px viewport.
    expect(shell.style.transform).toBe("translateX(187.5px)");
    act(() => {
      window.dispatchEvent(touch("touchend", [{ clientX: 200, clientY: 110 }]));
    });
    expect(mock.navigate).toHaveBeenCalledWith("/");
    expect(shell.style.transform).toBe("translateX(0px)");
  });

  it("springs back without navigating below the threshold", () => {
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(50));
    expect(mock.navigate).not.toHaveBeenCalled();
    expect(shell.style.transform).toBe("translateX(0px)");
  });

  it("closes the thread panel first (one level at a time)", () => {
    mock.activeThreadRoot = "conversations/c1/messages/m1";
    render(<Harness />);
    act(() => swipeFromEdge(200));
    expect(mock.closeThread).toHaveBeenCalledTimes(1);
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("does nothing on top-level tab routes without a back target", () => {
    mock.routeName = "chat";
    render(<Harness />);
    act(() => swipeFromEdge(200));
    expect(mock.navigate).not.toHaveBeenCalled();
    expect(mock.closeThread).not.toHaveBeenCalled();
  });

  it("ignores touches over layer overlays (sheets/dialogs/previews)", () => {
    render(<Harness />);
    const overlayTarget = screen.getByTestId("overlay-target");
    act(() => {
      overlayTarget.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      overlayTarget.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
      overlayTarget.dispatchEvent(
        touch("touchend", [{ clientX: 200, clientY: 110 }])
      );
    });
    expect(mock.navigate).not.toHaveBeenCalled();
  });
});
