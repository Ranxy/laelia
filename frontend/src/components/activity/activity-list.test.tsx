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

vi.mock("@/lib/use-is-desktop", () => ({
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

  it("turns pages with the server's next token and returns to the cached first page", async () => {
    mock.listActivities.mockImplementation(async (req) =>
      page(
        [activity(`activities/p2-${req.pageToken || "first"}`)],
        req.pageToken ? "" : "tok-0"
      )
    );
    renderList();
    await screen.findByTestId("row-p2-first");

    fireEvent.click(screen.getByRole("button", { name: "activity.next" }));
    await screen.findByTestId("row-p2-tok-0");

    expect(mock.listActivities).toHaveBeenCalledTimes(2);
    expect(mock.listActivities.mock.calls[1][0].pageToken).toBe("tok-0");

    // Back to page 1: its cache entry is still mounted, so the rows render
    // without another fetch.
    fireEvent.click(screen.getByRole("button", { name: "activity.prev" }));
    await screen.findByTestId("row-p2-first");
    expect(mock.listActivities).toHaveBeenCalledTimes(2);
  });

  it("shows the previous rows while a newly entered page loads", async () => {
    let resolveSecond: (value: unknown) => void = () => {};
    mock.listActivities.mockImplementationOnce(async () =>
      page([activity("activities/p2-first")], "tok-0")
    );
    mock.listActivities.mockImplementationOnce(
      async () =>
        await new Promise((resolve) => {
          resolveSecond = resolve;
        })
    );
    renderList();
    await screen.findByTestId("row-p2-first");

    fireEvent.click(screen.getByRole("button", { name: "activity.next" }));
    // Page 2 is still loading — page 1's rows stay on screen.
    expect(screen.getByTestId("row-p2-first")).toBeInTheDocument();

    await act(async () => {
      resolveSecond(page([activity("activities/p2-tok-0")], ""));
    });
    await screen.findByTestId("row-p2-tok-0");
    expect(screen.queryByTestId("row-p2-first")).not.toBeInTheDocument();
  });

  it("polls the visible page every 5s", async () => {
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
