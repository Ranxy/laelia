import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mock = vi.hoisted(() => ({
  platformOwnsEdgeSwipe: vi.fn(() => false),
  store: {
    tasksByConv: {},
    tasksLoading: {},
    tasksNextPageToken: {},
    taskCountsByConv: {},
    loadTasks: vi.fn(),
    loadMoreTasks: vi.fn(),
    loadTaskCounts: vi.fn(),
  },
}));

vi.mock("@/lib/platform-edge-swipe", () => ({
  platformOwnsEdgeSwipe: mock.platformOwnsEdgeSwipe,
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: unknown) => unknown) =>
    selector({
      tasksByConv: {},
      tasksLoading: {},
      tasksNextPageToken: {},
      taskCountsByConv: {},
      loadTasks: mock.store.loadTasks,
      loadMoreTasks: mock.store.loadMoreTasks,
      loadTaskCounts: mock.store.loadTaskCounts,
    }),
}));

import { TasksPanel } from "@/components/chat/tasks-panel";

// jsdom has no IntersectionObserver; the panel's infinite-scroll effect only
// needs a no-op stub.
class IntersectionObserverStub {
  observe() {}
  disconnect() {}
  unobserve() {}
}

describe("TasksPanel", () => {
  beforeEach(() => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(false);
    mock.store.loadTasks.mockReset();
    mock.store.loadMoreTasks.mockReset();
    mock.store.loadTaskCounts.mockReset();
    Object.defineProperty(window, "innerWidth", {
      value: 375,
      configurable: true,
    });
    Object.defineProperty(window, "IntersectionObserver", {
      value: IntersectionObserverStub,
      configurable: true,
    });
    window.history.replaceState(null, "");
  });

  afterEach(() => {
    window.history.replaceState(null, "");
    document.body.innerHTML = "";
  });

  it("renders as a full-screen mobile overlay driven by the swipe CSS vars", () => {
    const { container } = render(
      <TasksPanel
        channelId="c1"
        channelTitle="chan"
        onClose={vi.fn()}
        onOpenTask={vi.fn()}
      />
    );
    const aside = container.querySelector("aside") as HTMLElement;
    expect(aside.className).toContain("fixed inset-0 z-panel");
    expect(aside.style.transform).toBe("translateX(var(--swipe-offset, 0px))");
  });

  it("pushes a history sentinel on real-iOS-like platforms and closes on popstate", () => {
    mock.platformOwnsEdgeSwipe.mockReturnValue(true);
    const onClose = vi.fn();
    render(
      <TasksPanel
        channelId="c1"
        channelTitle="chan"
        onClose={onClose}
        onOpenTask={vi.fn()}
      />
    );
    expect(
      typeof (window.history.state as Record<string, unknown> | null)?.[
        "laelia.historySentinel"
      ]
    ).toBe("string");

    window.history.replaceState(null, "");
    act(() => {
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("leaves history untouched when the platform owns no edge swipe", () => {
    render(
      <TasksPanel
        channelId="c1"
        channelTitle="chan"
        onClose={vi.fn()}
        onOpenTask={vi.fn()}
      />
    );
    expect(
      (window.history.state as Record<string, unknown> | null)?.[
        "laelia.historySentinel"
      ]
    ).toBeUndefined();
  });
});
