import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mock = vi.hoisted(() => ({
  // Simulates a real iOS/iPadOS browser whose system edge-swipe recognizer
  // owns edge touches (see platform-edge-swipe.ts).
  platformOwnsEdgeSwipe: vi.fn(() => false),
}));

vi.mock("@/lib/platform-edge-swipe", () => ({
  platformOwnsEdgeSwipe: mock.platformOwnsEdgeSwipe,
}));

import { ChatDrawerSheet } from "@/components/chat/chat-drawer-sheet";
import { SheetHeader, SheetTitle } from "@/components/ui/sheet";

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

function swipeFromLeftEdge(target: HTMLElement, dx: number, dy = 0) {
  target.dispatchEvent(touch("touchstart", [{ clientX: 10, clientY: 100 }]));
  target.dispatchEvent(
    touch("touchmove", [{ clientX: 10 + dx, clientY: 100 + dy }])
  );
  target.dispatchEvent(
    touch("touchend", [{ clientX: 10 + dx, clientY: 100 + dy }])
  );
}

async function renderDrawer(onClose: () => void) {
  render(
    <ChatDrawerSheet open onClose={onClose}>
      <SheetHeader>
        <SheetTitle>Drawer Under Test</SheetTitle>
      </SheetHeader>
    </ChatDrawerSheet>
  );
  return (await screen.findByText("Drawer Under Test")).closest(
    "[role='dialog']"
  ) as HTMLElement;
}

describe("ChatDrawerSheet mobile swipe-to-close", () => {
  beforeEach(() => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(false);
    Object.defineProperty(window, "innerWidth", {
      value: 375,
      configurable: true,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
  });

  it("slides the drawer out and closes past the threshold", async () => {
    const onClose = vi.fn();
    const popup = await renderDrawer(onClose);

    vi.useFakeTimers();
    act(() => swipeFromLeftEdge(popup, 200));

    expect(popup.style.transform).toBe("translateX(100%)");
    expect(onClose).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("fades the scrim while dragging so the page underneath is visible", async () => {
    const popup = await renderDrawer(vi.fn());
    const portal = popup.parentElement as HTMLElement;
    const overlay = Array.from(portal.children).find((child) =>
      (child as HTMLElement).className.includes("bg-overlay/50")
    ) as HTMLElement;
    expect(overlay).toBeTruthy();

    act(() => {
      popup.dispatchEvent(touch("touchstart", [{ clientX: 10, clientY: 100 }]));
      popup.dispatchEvent(touch("touchmove", [{ clientX: 200, clientY: 100 }]));
    });

    expect(overlay.style.opacity).toBe("0");
  });

  it("springs back below the threshold without closing", async () => {
    const onClose = vi.fn();
    const popup = await renderDrawer(onClose);

    vi.useFakeTimers();
    act(() => swipeFromLeftEdge(popup, 50));
    expect(popup.style.transform).toBe("translateX(0px)");
    expect(onClose).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(popup.style.transform).toBe("");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("leaves vertical scroll gestures untouched", async () => {
    const onClose = vi.fn();
    const popup = await renderDrawer(onClose);

    act(() => swipeFromLeftEdge(popup, 0, 200));

    expect(popup.style.transform).toBe("");
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("ChatDrawerSheet on browsers whose system gesture owns the edge", () => {
  beforeEach(() => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    window.history.replaceState(null, "");
  });

  afterEach(() => {
    window.history.replaceState(null, "");
    document.body.innerHTML = "";
  });

  it("keeps the synthetic swipe gesture inert (the system owns the touch)", async () => {
    const onClose = vi.fn();
    const popup = await renderDrawer(onClose);

    act(() => swipeFromLeftEdge(popup, 200));

    expect(popup.style.transform).toBe("");
    expect(onClose).not.toHaveBeenCalled();
  });

  it("pushes a history sentinel while open and closes on popstate", async () => {
    const onClose = vi.fn();
    await renderDrawer(onClose);

    // Opening pushed a duplicate history entry marked as the sentinel.
    expect(
      (window.history.state as Record<string, unknown> | null)?.[
        "laelia.historySentinel"
      ]
    ).toEqual(expect.any(String));

    // The system back gesture / back button pops it, landing the history on
    // the drawer-free entry: the drawer is dismissed instead of the user
    // leaving the page.
    window.history.replaceState(null, "");
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("consumes the sentinel when the drawer closes through its own UI", async () => {
    const back = vi.spyOn(window.history, "back");
    const onClose = vi.fn();
    const { rerender } = render(
      <ChatDrawerSheet open onClose={onClose}>
        <SheetHeader>
          <SheetTitle>Drawer Under Test</SheetTitle>
        </SheetHeader>
      </ChatDrawerSheet>
    );
    await screen.findByText("Drawer Under Test");
    expect(
      (window.history.state as Record<string, unknown> | null)?.[
        "laelia.historySentinel"
      ]
    ).toEqual(expect.any(String));

    vi.useFakeTimers();
    act(() => {
      rerender(
        <ChatDrawerSheet open={false} onClose={onClose}>
          <SheetHeader>
            <SheetTitle>Drawer Under Test</SheetTitle>
          </SheetHeader>
        </ChatDrawerSheet>
      );
    });

    // The cleanup pops the sentinel so the stack stays balanced.
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(back).toHaveBeenCalled();
    back.mockRestore();
    vi.useRealTimers();
  });
});
