import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Activity } from "@/types/proto-es/v1/command_pb";
import {
  ActivityCategory,
  ActivityState,
} from "@/types/proto-es/v1/command_pb";
import { ActivityList } from "./activity-list";

const mock = vi.hoisted(() => ({
  isDesktop: true,
  listActivities: vi.fn(),
  markActivityDone: vi.fn(),
}));

vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: () => mock.isDesktop,
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listActivities: mock.listActivities,
    markActivityDone: mock.markActivityDone,
  },
}));

vi.mock("@/components/activity/activity-row", () => ({
  ActivityRow: (props: Record<string, unknown>) => {
    const activity = props.activity as Activity;
    return (
      <div data-testid={`row-${activity.name.split("/").pop()}`}>
        <button onClick={props.onSelect as () => void}>select</button>
        <button onClick={props.onMarkDone as () => void}>done</button>
      </div>
    );
  },
}));

const tFn = (key: string, opts?: Record<string, unknown>) =>
  opts?.n !== undefined ? `${key}:${opts.n}` : key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

// jsdom has no IntersectionObserver; the list's infinite-scroll effect only
// needs a constructor that hands its callback back so tests can fire
// intersections against the latest registered observer.
let fireIntersection: IntersectionObserverCallback = () => {};
class IntersectionObserverStub {
  constructor(callback: IntersectionObserverCallback) {
    fireIntersection = callback;
  }
  observe() {}
  disconnect() {}
  unobserve() {}
}

// intersect pretends the bottom sentinel just entered the viewport.
function intersect() {
  act(() => {
    fireIntersection(
      [{ isIntersecting: true } as unknown as IntersectionObserverEntry],
      {} as IntersectionObserver
    );
  });
}

// listActivities receives a proto-es ListActivitiesRequest; assert on plain
// fields (proto-es repeated/enum fields read back as numbers).
function activity(
  name: string,
  state: ActivityState = ActivityState.UNREAD
): Activity {
  return {
    name,
    conversation: "conversations/c1",
    message: "messages/x",
    threadRoot: "",
    state,
    categories: Number(ActivityCategory.MENTION),
  } as unknown as Activity;
}

function page(rows: Activity[], nextPageToken = "") {
  return { activities: rows, nextPageToken };
}

function renderList() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={["/activity"]}>
        <Routes>
          <Route path="/activity" element={<ActivityList />} />
          <Route path="/activity/:messageId" element={<ActivityList />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  mock.isDesktop = true;
  mock.listActivities.mockReset().mockResolvedValue(page([]));
  mock.markActivityDone.mockReset().mockResolvedValue({});
  fireIntersection = () => {};
  Object.defineProperty(window, "IntersectionObserver", {
    value: IntersectionObserverStub,
    configurable: true,
  });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("activity-list", () => {
  it("renders the first page and the unread count", async () => {
    mock.listActivities.mockResolvedValue(
      page([activity("activities/a1"), activity("activities/a2")])
    );
    renderList();

    const rows = await screen.findAllByTestId(/^row-/);
    expect(rows).toHaveLength(2);
    expect(mock.listActivities.mock.calls[0][0]).toMatchObject({
      pageSize: 50,
      pageToken: "",
    });
    expect(Number(mock.listActivities.mock.calls[0][0].readStateFilter)).toBe(
      Number(ActivityState.UNREAD)
    );
    expect(screen.getByText("activity.active-count:2")).toBeInTheDocument();
  });

  it("refetches with the new filter when a tab is selected", async () => {
    mock.listActivities.mockResolvedValue(page([activity("activities/a1")]));
    renderList();
    await screen.findAllByTestId(/^row-/);

    // Filter "all" = UNSPECIFIED with no category filter.
    fireEvent.click(screen.getByRole("tab", { name: "activity.filter-all" }));
    await screen.findAllByTestId(/^row-/);

    expect(mock.listActivities).toHaveBeenCalledTimes(2);
    const second = mock.listActivities.mock.calls[1][0];
    expect(Number(second.readStateFilter)).toBe(
      Number(ActivityState.UNSPECIFIED)
    );
    expect(second.filter).toEqual([]);
  });

  it("appends the next page when the scroll sentinel intersects", async () => {
    mock.listActivities.mockImplementation(async (req) =>
      page(
        [activity(`activities/m-${req.pageToken || "first"}`)],
        req.pageToken ? "" : "tok-0"
      )
    );
    renderList();
    await screen.findByTestId("row-m-first");

    intersect();

    await screen.findByTestId("row-m-tok-0");
    expect(mock.listActivities).toHaveBeenCalledTimes(2);
    expect(mock.listActivities.mock.calls[1][0].pageToken).toBe("tok-0");
    // Loaded pages stay mounted: page 1's rows remain above the appended ones.
    expect(screen.getByTestId("row-m-first")).toBeInTheDocument();
    // The desktop Prev/Next footer is gone — infinite scroll is the only
    // paging semantic now.
    expect(
      screen.queryByRole("button", { name: "activity.next" })
    ).not.toBeInTheDocument();
  });

  it("keeps loaded rows while an appended page is still loading", async () => {
    let resolveSecond: (value: unknown) => void = () => {};
    mock.listActivities.mockResolvedValueOnce(
      page([activity("activities/a1")], "tok-0")
    );
    mock.listActivities.mockImplementationOnce(
      async () =>
        await new Promise((resolve) => {
          resolveSecond = resolve;
        })
    );
    renderList();
    await screen.findByTestId("row-a1");

    intersect();
    // Page 2 is still loading — page 1's rows stay on screen and the sentinel
    // shows the appender spinner.
    expect(screen.getByTestId("row-a1")).toBeInTheDocument();
    expect(mock.listActivities).toHaveBeenCalledTimes(2);
    expect(document.querySelector(".animate-spin")).not.toBeNull();

    await act(async () => {
      resolveSecond(page([activity("activities/a2")], ""));
    });
    await screen.findByTestId("row-a2");
    expect(screen.getByTestId("row-a1")).toBeInTheDocument();
    expect(document.querySelector(".animate-spin")).toBeNull();
  });

  it("does not fetch more pages once the feed is exhausted", async () => {
    mock.listActivities.mockResolvedValue(page([activity("activities/a1")]));
    renderList();
    await screen.findByTestId("row-a1");

    intersect();
    intersect();

    expect(mock.listActivities).toHaveBeenCalledTimes(1);
  });

  it("polls page 0 every 5s on every viewport", async () => {
    vi.useFakeTimers();
    mock.listActivities.mockResolvedValue(page([activity("activities/a1")]));
    renderList();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(mock.listActivities).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(mock.listActivities).toHaveBeenCalledTimes(2);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(mock.listActivities).toHaveBeenCalledTimes(3);
  });

  it("keeps the rows when a poll fails instead of emptying the list", async () => {
    vi.useFakeTimers();
    mock.listActivities.mockResolvedValue(
      page([activity("activities/a1")], "tok-0")
    );
    renderList();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByTestId("row-a1")).toBeInTheDocument();

    mock.listActivities.mockRejectedValueOnce(new Error("boom"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByTestId("row-a1")).toBeInTheDocument();
  });

  it("marks a row done and removes it from the list", async () => {
    // Every view mounted here is a non-Done view — the server excludes Done
    // rows — so once the row is done it never comes back on a refetch.
    let done = false;
    mock.listActivities.mockImplementation(async () =>
      page(
        done
          ? [activity("activities/a2", ActivityState.READ)]
          : [
              activity("activities/a1"),
              activity("activities/a2", ActivityState.READ),
            ]
      )
    );
    mock.markActivityDone.mockImplementation(async () => {
      done = true;
      return {};
    });
    renderList();
    await screen.findByTestId("row-a1");

    fireEvent.click(
      within(screen.getByTestId("row-a1")).getByRole("button", {
        name: "done",
      })
    );

    await waitFor(() => expect(mock.markActivityDone).toHaveBeenCalledTimes(1));
    await screen.findByTestId("row-a2");
    await waitFor(() =>
      expect(screen.queryByTestId("row-a1")).not.toBeInTheDocument()
    );
  });
});
