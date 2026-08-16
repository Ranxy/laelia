import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mock = vi.hoisted(() => ({
  navigate: vi.fn(),
  getOrCreateConversation: vi.fn(),
  fetchChannels: vi.fn(),
  agent: { name: "agents/alpha", title: "Alpha Agent", handle: "alpha" },
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mock.navigate,
}));

vi.mock("@/connect", () => ({
  agentServiceClient: {
    async getAgent() {
      return mock.agent;
    },
  },
  userServiceClient: {
    async getUser() {
      return { name: "users/1", title: "Alice", handle: "alice" };
    },
  },
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: unknown) => unknown) =>
    selector({
      getOrCreateConversation: mock.getOrCreateConversation,
      fetchChannels: mock.fetchChannels,
    }),
}));

vi.mock("@/lib/toast", () => ({
  toastManager: { add: vi.fn() },
}));

vi.mock("@/components/connection-badge", () => ({
  ConnectionBadge: () => <span data-testid="connection-badge" />,
}));

import { MentionDetailSheet } from "./mention-detail-sheet";

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

function swipeFromLeftEdge(target: HTMLElement, dx: number) {
  target.dispatchEvent(touch("touchstart", [{ clientX: 10, clientY: 100 }]));
  target.dispatchEvent(
    touch("touchmove", [{ clientX: 10 + dx, clientY: 100 }])
  );
  target.dispatchEvent(touch("touchend", [{ clientX: 10 + dx, clientY: 100 }]));
}

describe("MentionDetailSheet mobile swipe-to-close", () => {
  beforeEach(() => {
    Object.defineProperty(window, "innerWidth", {
      value: 375,
      configurable: true,
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    document.body.innerHTML = "";
  });

  it("slides the real sheet out and closes past the threshold", async () => {
    const onClose = vi.fn();
    render(
      <MentionDetailSheet
        open
        type="agent"
        id="alpha"
        name="Alpha"
        onClose={onClose}
      />
    );

    const popup = (await screen.findByText("Agent Details")).closest(
      "[role='dialog']"
    ) as HTMLElement;
    expect(popup).not.toBeNull();

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
    render(
      <MentionDetailSheet
        open
        type="agent"
        id="alpha"
        name="Alpha"
        onClose={vi.fn()}
      />
    );

    const popup = (await screen.findByText("Agent Details")).closest(
      "[role='dialog']"
    ) as HTMLElement;
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
});
