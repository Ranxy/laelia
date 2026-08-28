import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
  useIsDesktop: vi.fn(() => false),
  navigate: vi.fn(),
  routeName: null as string | null,
  activeThreadRoot: null as string | null,
  closeThread: vi.fn(),
  tasksPanelOpen: {} as Record<string, boolean>,
  closeTasksPanel: vi.fn(),
  location: { pathname: "/test" },
  // Simulates a real iOS/iPadOS browser whose system edge-swipe recognizer
  // owns left-edge touches (see platform-edge-swipe.ts).
  platformOwnsEdgeSwipe: vi.fn(() => false),
}));

vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: mock.useIsDesktop,
}));
vi.mock("@/lib/platform-edge-swipe", () => ({
  platformOwnsEdgeSwipe: mock.platformOwnsEdgeSwipe,
}));
vi.mock("react-router-dom", () => ({
  useNavigate: () => mock.navigate,
  useLocation: () => mock.location,
}));
vi.mock("@/router/use-current-route", () => ({
  useCurrentRoute: () => ({ name: mock.routeName }),
}));
vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: unknown) => unknown) =>
    selector({
      closeThread: mock.closeThread,
      activeThreadRoot: mock.activeThreadRoot,
      tasksPanelOpen: mock.tasksPanelOpen,
      closeTasksPanel: mock.closeTasksPanel,
    }),
  setSuppressLoadingFlags: vi.fn(),
}));

import { useSwipeBack } from "./use-swipe-back";

function Harness() {
  const { rootRef, currentPageRef, previewPath } = useSwipeBack();
  return (
    <div ref={rootRef} data-testid="shell">
      <div ref={currentPageRef} data-testid="page">
        page
      </div>
      {previewPath && <div data-testid="preview">{previewPath}</div>}
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
    mock.platformOwnsEdgeSwipe.mockReturnValue(false);
    mock.routeName = "chat.detail";
    mock.activeThreadRoot = null;
    mock.tasksPanelOpen = {};
    mock.closeTasksPanel.mockReset();
    Object.defineProperty(window, "innerWidth", {
      value: 375,
      configurable: true,
    });
    // Simulate a session with an in-app previous entry (React Router's data
    // router records the position in history.state.idx) — the condition under
    // which a real browser's system edge-swipe has something to navigate to.
    window.history.replaceState({ idx: 1 }, "");
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    window.history.replaceState(null, "");
    document.body.innerHTML = "";
  });

  it("is inert on desktop", () => {
    mock.useIsDesktop.mockReturnValue(true);
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => swipeFromEdge(200));
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("ignores touches that start outside the left edge zone", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => {
      window.dispatchEvent(
        touch("touchstart", [{ clientX: 100, clientY: 100 }])
      );
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 300, clientY: 110 }])
      );
      window.dispatchEvent(touch("touchend", [{ clientX: 300, clientY: 110 }]));
    });
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("ignores vertical scrolls", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => swipeFromEdge(0, 200));
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("ignores leftward swipes (row actions)", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => swipeFromEdge(-60));
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("previews the back target under the page while dragging", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => {
      window.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
    });
    // The destination route is rendered underneath while the page follows the
    // finger (190px of drag, capped at half the 375px viewport).
    expect(screen.getByTestId("preview").textContent).toBe("/");
    expect(page.style.transform).toBe("translateX(187.5px)");
  });

  it("slides the page out and navigates back past the threshold", () => {
    const { rerender } = render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => swipeFromEdge(200));
    // Commit animation: the page slides out to the full viewport width.
    expect(page.style.transform).toBe("translateX(375px)");
    expect(mock.navigate).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(mock.navigate).toHaveBeenCalledWith("/", { replace: true });
    // The preview stays mounted until the data router finishes the
    // navigation (location changes). Simulate the location change:
    mock.location = { pathname: "/" };
    act(() => {
      rerender(<Harness />);
    });
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(page.style.transform).toBe("");
  });

  it("springs back without navigating below the threshold", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => swipeFromEdge(50));
    expect(page.style.transform).toBe("translateX(0px)");
    expect(mock.navigate).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(page.style.transform).toBe("");
  });

  it("closes the thread panel first (one level at a time)", () => {
    mock.activeThreadRoot = "conversations/c1/messages/m1";
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(200));
    // The thread panel (full-screen overlay) follows the finger via CSS
    // variables on the shell; no route preview is rendered.
    expect(shell.style.getPropertyValue("--swipe-offset")).toBe("375px");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.closeThread).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(mock.closeThread).toHaveBeenCalledTimes(1);
    expect(mock.navigate).not.toHaveBeenCalled();
    expect(shell.style.getPropertyValue("--swipe-offset")).toBe("");
  });

  it("does nothing on top-level tab routes without a back target", () => {
    mock.routeName = "chat";
    render(<Harness />);
    act(() => swipeFromEdge(200));
    expect(mock.navigate).not.toHaveBeenCalled();
    expect(mock.closeThread).not.toHaveBeenCalled();
    expect(screen.queryByTestId("preview")).toBeNull();
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
    expect(screen.queryByTestId("preview")).toBeNull();
  });

  it("yields bezel-originated touches to the OS edge swipe (route mode)", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => {
      // A bezel-originated touch reports its first position at the viewport
      // edge; the browser's system edge-swipe owns it on real devices.
      window.dispatchEvent(touch("touchstart", [{ clientX: 1, clientY: 100 }]));
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
      window.dispatchEvent(touch("touchend", [{ clientX: 200, clientY: 110 }]));
    });
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("keeps the full edge zone when the browser has no previous entry", () => {
    // Deep link: no history to swipe back to, so the platform gesture has
    // nothing to do and the synthetic gesture owns the bezel too.
    window.history.replaceState({ idx: 0 }, "");
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => {
      window.dispatchEvent(touch("touchstart", [{ clientX: 1, clientY: 100 }]));
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
    });
    expect(screen.getByTestId("preview").textContent).toBe("/");
    expect(page.style.transform).toBe("translateX(187.5px)");
  });

  it("resets instantly when the touch is cancelled mid-drag", () => {
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => {
      window.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
      window.dispatchEvent(
        touch("touchcancel", [{ clientX: 200, clientY: 110 }])
      );
    });
    // No spring-back animation: a touchcancel usually means a system gesture
    // claimed the touch, and our layers must not animate under its transition.
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
  });

  it("yields route-level swipes entirely on browsers whose system gesture owns the edge", () => {
    // Real iOS/iPadOS browsers: the system edge-swipe recognizer's zone covers
    // the whole edge area, so ANY synthetic route gesture there stacks the
    // browser's own transition snapshot underneath our layers (the three-layer
    // artifact). Route back is delegated to the platform.
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => swipeFromEdge(200));
    expect(page.style.transform).toBe("");
    expect(screen.queryByTestId("preview")).toBeNull();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("yields thread swipes on browsers whose system gesture owns the edge", () => {
    // On real iOS/iPadOS browsers the system edge-swipe owns the touch; the
    // thread panel dismisses through its history sentinel
    // (useHistorySentinel in ThreadPanel) instead of this synthetic gesture,
    // which would stack the browser's back-transition snapshot underneath it.
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    mock.activeThreadRoot = "conversations/c1/messages/m1";
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(200));
    expect(shell.style.getPropertyValue("--swipe-offset")).toBe("");
    expect(mock.closeThread).not.toHaveBeenCalled();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("drives the tasks board panel like the thread panel", () => {
    // The tasks board is the other full-screen panel overlay sharing the
    // --swipe-offset mechanism; its commit closes the board, not the thread.
    mock.tasksPanelOpen = { "conversations/c1": true };
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(200));
    expect(shell.style.getPropertyValue("--swipe-offset")).toBe("375px");
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(mock.closeTasksPanel).toHaveBeenCalledWith("conversations/c1");
    expect(mock.closeThread).not.toHaveBeenCalled();
    expect(mock.navigate).not.toHaveBeenCalled();
  });

  it("yields tasks-board swipes on browsers whose system gesture owns the edge", () => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    mock.tasksPanelOpen = { "conversations/c1": true };
    render(<Harness />);
    const shell = screen.getByTestId("shell");
    act(() => swipeFromEdge(200));
    expect(shell.style.getPropertyValue("--swipe-offset")).toBe("");
    expect(mock.closeTasksPanel).not.toHaveBeenCalled();
    expect(mock.closeThread).not.toHaveBeenCalled();
  });

  it("keeps route-level swipes on platform-owned-edge browsers without history", () => {
    // Deep link on a real device: the system edge swipe has no previous entry
    // to navigate to, so it cannot engage and the synthetic gesture runs.
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    window.history.replaceState({ idx: 0 }, "");
    render(<Harness />);
    const page = screen.getByTestId("page");
    act(() => {
      window.dispatchEvent(
        touch("touchstart", [{ clientX: 10, clientY: 100 }])
      );
      window.dispatchEvent(
        touch("touchmove", [{ clientX: 200, clientY: 110 }])
      );
    });
    expect(screen.getByTestId("preview").textContent).toBe("/");
    expect(page.style.transform).toBe("translateX(187.5px)");
  });
});
