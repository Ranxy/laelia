import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithQueryClient } from "@/test/query";
import type { Reminder } from "@/types/proto-es/v1/command_pb";
import { ReminderStatus } from "@/types/proto-es/v1/command_pb";
import { ReminderListPage } from "./reminder-list";

// The page reads through TanStack Query (ADR-1) and never touches the store
// slice, so only the RPC layer is mocked here. The old vi.mock("@/stores") is
// gone on purpose.
const mock = vi.hoisted(() => ({
  listReminders: vi.fn(),
  getReminder: vi.fn(),
  updateReminder: vi.fn(),
  cancelReminder: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    listReminders: mock.listReminders,
    getReminder: mock.getReminder,
    updateReminder: mock.updateReminder,
    cancelReminder: mock.cancelReminder,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

// The composable calls the RPC with a single proto-es request message.
interface ListCall {
  agent: string;
  pageSize: number;
  pageToken: string;
  statusFilter: ReminderStatus[];
}

function lastListCall(): ListCall {
  const calls = mock.listReminders.mock.calls;
  return calls[calls.length - 1][0] as ListCall;
}

function reminder(name: string, taskContent: string, status: ReminderStatus) {
  return {
    name,
    taskContent,
    status,
    cronExpr: "",
    tz: "UTC",
    fireAt: { seconds: 0n, nanos: 0 },
    assigneeName: "Alice",
  } as unknown as Reminder;
}

function renderPage() {
  return renderWithQueryClient(
    <MemoryRouter initialEntries={["/members/agents/a1/reminders"]}>
      <Routes>
        <Route
          path="/members/agents/:agentId/reminders"
          element={<ReminderListPage />}
        />
        <Route
          path="/members/agents/:agentId/reminders/:reminderId"
          element={<div data-testid="detail" />}
        />
      </Routes>
    </MemoryRouter>
  );
}

function SwitchAgentButton() {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      data-testid="switch-agent"
      onClick={() => navigate("/members/agents/a2/reminders")}
    >
      switch agent
    </button>
  );
}

function renderPageWithAgentSwitch() {
  return renderWithQueryClient(
    <MemoryRouter initialEntries={["/members/agents/a1/reminders"]}>
      <SwitchAgentButton />
      <Routes>
        <Route
          path="/members/agents/:agentId/reminders"
          element={<ReminderListPage />}
        />
        <Route
          path="/members/agents/:agentId/reminders/:reminderId"
          element={<div data-testid="detail" />}
        />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.listReminders.mockReset();
  mock.getReminder.mockReset();
  mock.updateReminder.mockReset();
  mock.cancelReminder.mockReset();
  // No-op implementations so incidental client imports stay harmless.
  mock.getReminder.mockResolvedValue(undefined);
  mock.updateReminder.mockResolvedValue(undefined);
  mock.cancelReminder.mockResolvedValue(undefined);
});

describe("reminder-list", () => {
  it("renders reminder rows with status, task, schedule and assignee", async () => {
    mock.listReminders.mockResolvedValue({
      reminders: [
        reminder(
          "agents/a1/reminders/r1",
          "Ship the release",
          ReminderStatus.PENDING
        ),
        reminder(
          "agents/a1/reminders/r2",
          "Follow up",
          ReminderStatus.COMPLETED
        ),
      ],
      nextPageToken: "",
    });

    renderPage();

    expect(await screen.findByText("Ship the release")).toBeInTheDocument();
    expect(screen.getByText("Follow up")).toBeInTheDocument();
    expect(screen.getAllByText("once").length).toBe(2);
    expect(screen.getAllByText("Alice").length).toBe(2);
    expect(mock.listReminders).toHaveBeenCalledTimes(1);
    expect(lastListCall()).toMatchObject({
      agent: "agents/a1",
      pageSize: 50,
      pageToken: "",
      statusFilter: [],
    });
  });

  it("shows the empty hint when there are no reminders", async () => {
    mock.listReminders.mockResolvedValue({ reminders: [], nextPageToken: "" });

    renderPage();

    expect(await screen.findByText("reminders.empty")).toBeInTheDocument();
  });

  it("renders the empty table when the initial load fails", async () => {
    mock.listReminders.mockRejectedValue(new Error("rpc down"));

    renderPage();

    // Parity with the old page: a failed initial load (data undefined)
    // renders the empty table, with no skeleton stuck on screen.
    expect(await screen.findByText("reminders.empty")).toBeInTheDocument();
    expect(screen.queryByText("common.loading")).not.toBeInTheDocument();
  });

  it("refetches page 1 with the selected status filter when a tab is clicked", async () => {
    mock.listReminders
      .mockResolvedValueOnce({
        reminders: [
          reminder("agents/a1/reminders/r1", "Ship it", ReminderStatus.PENDING),
        ],
        nextPageToken: "tok2",
      })
      .mockResolvedValueOnce({
        reminders: [
          reminder(
            "agents/a1/reminders/r2",
            "Page two",
            ReminderStatus.PENDING
          ),
        ],
        nextPageToken: "",
      });

    renderPage();
    expect(await screen.findByText("Ship it")).toBeInTheDocument();

    // Turn to page 1 first so the reset below is provable.
    fireEvent.click(screen.getByRole("button", { name: /reminders.next/ }));
    await waitFor(() => expect(mock.listReminders).toHaveBeenCalledTimes(2));

    // Filter change: the body remounts (key change) — token stack resets to
    // page 0 and the RPC carries the new statusFilter. The fresh key starts
    // as a real skeleton; the previous view's rows must not leak in as
    // placeholder data (this fetch is held open to keep it observable).
    mock.listReminders.mockImplementationOnce(
      () => new Promise<never>(() => {})
    );
    fireEvent.click(
      screen.getByRole("button", { name: "reminders.filter-due" })
    );

    await waitFor(() => expect(mock.listReminders).toHaveBeenCalledTimes(3));
    const call = lastListCall();
    expect(call.statusFilter).toEqual([ReminderStatus.DUE]);
    expect(call.pageToken).toBe("");
    expect(screen.getByText("common.loading")).toBeInTheDocument();
    expect(screen.queryByText("Page two")).not.toBeInTheDocument();
  });

  it("navigates to the detail page when a row is clicked", async () => {
    mock.listReminders.mockResolvedValue({
      reminders: [
        reminder("agents/a1/reminders/r1", "Ship it", ReminderStatus.PENDING),
      ],
      nextPageToken: "",
    });

    renderPage();
    fireEvent.click(await screen.findByText("Ship it"));

    expect(screen.getByTestId("detail")).toBeInTheDocument();
  });

  it("paginates with next and prev buttons", async () => {
    let releasePageTwo:
      | ((v: { reminders: Reminder[]; nextPageToken: string }) => void)
      | undefined;
    // Default reply for any extra call beyond the queued Once handlers
    // (e.g. the cache-hit refetch on Prev).
    mock.listReminders.mockResolvedValue({
      reminders: [
        reminder("agents/a1/reminders/r1", "Page one", ReminderStatus.PENDING),
      ],
      nextPageToken: "tok2",
    });
    mock.listReminders
      .mockResolvedValueOnce({
        reminders: [
          reminder(
            "agents/a1/reminders/r1",
            "Page one",
            ReminderStatus.PENDING
          ),
        ],
        nextPageToken: "tok2",
      })
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            releasePageTwo = (v) => resolve(v);
          })
      );

    renderPage();
    expect(await screen.findByText("Page one")).toBeInTheDocument();

    // Next: page 2 is requested with the token page 1 handed out. While the
    // turn is in flight the old rows stay on screen (keepPreviousData) and
    // refreshing — not the skeleton — covers the fetch.
    fireEvent.click(screen.getByRole("button", { name: /reminders.next/ }));
    await waitFor(() => expect(mock.listReminders).toHaveBeenCalledTimes(2));
    expect(lastListCall().pageToken).toBe("tok2");
    expect(screen.getByText("Page one")).toBeInTheDocument();
    expect(screen.queryByText("common.loading")).not.toBeInTheDocument();

    act(() => {
      releasePageTwo?.({
        reminders: [
          reminder(
            "agents/a1/reminders/r2",
            "Page two",
            ReminderStatus.PENDING
          ),
        ],
        nextPageToken: "",
      });
    });
    expect(await screen.findByText("Page two")).toBeInTheDocument();
    expect(screen.queryByText("Page one")).not.toBeInTheDocument();

    // Prev goes back to page 0. Assertion choice (as the ticket allows):
    // "rows on screen at once" — the page paints synchronously from the
    // query cache, without waiting for any RPC. staleTime 0 still schedules
    // a background refetch of the cached key afterwards; the assertion does
    // not depend on it.
    fireEvent.click(screen.getByRole("button", { name: /reminders.prev/ }));
    expect(screen.getByText("Page one")).toBeInTheDocument();
    expect(screen.queryByText("Page two")).not.toBeInTheDocument();
  });

  it("resets pagination and refetches when the agent route param changes", async () => {
    mock.listReminders.mockImplementation((req: ListCall) =>
      Promise.resolve({
        reminders: [
          reminder(
            `${req.agent}/reminders/r1`,
            req.agent === "agents/a1" ? "Agent one row" : "Agent two row",
            ReminderStatus.PENDING
          ),
        ],
        nextPageToken: "",
      })
    );

    renderPageWithAgentSwitch();
    expect(await screen.findByText("Agent one row")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("switch-agent"));

    expect(await screen.findByText("Agent two row")).toBeInTheDocument();
    expect(mock.listReminders).toHaveBeenCalledTimes(2);
    const call = lastListCall();
    expect(call.agent).toBe("agents/a2");
    expect(call.pageToken).toBe("");
    expect(call.statusFilter).toEqual([]);
  });

  it("polls the current page every 5s", async () => {
    vi.useFakeTimers();
    try {
      mock.listReminders.mockResolvedValue({
        reminders: [
          reminder("agents/a1/reminders/r1", "Ship it", ReminderStatus.PENDING),
        ],
        nextPageToken: "",
      });

      renderPage();
      // The current page loads on mount.
      expect(mock.listReminders).toHaveBeenCalledTimes(1);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(screen.getByText("Ship it")).toBeInTheDocument();

      // One poll beat: the current page refetches in place, no skeleton.
      mock.listReminders.mockResolvedValueOnce({
        reminders: [
          reminder(
            "agents/a1/reminders/r1",
            "Polled row",
            ReminderStatus.PENDING
          ),
        ],
        nextPageToken: "",
      });
      // One poll beat: the current page refetches in place, no skeleton.
      mock.listReminders.mockResolvedValueOnce({
        reminders: [
          reminder(
            "agents/a1/reminders/r1",
            "Polled row",
            ReminderStatus.PENDING
          ),
        ],
        nextPageToken: "",
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });

      // The beat re-fires the RPC for the SAME view (root + agent + filter +
      // token unchanged): the poll refreshes the current page. The DOM-side
      // swap is not asserted on fake timers (React's flush races the fake
      // clock); the keep-rows-on-fetch behavior is covered deterministically
      // by the pagination test via placeholderData.
      expect(mock.listReminders).toHaveBeenCalledTimes(2);
      // Silent-poll parity with the old hook: rows stay, no skeleton.
      expect(screen.getByText("Ship it")).toBeInTheDocument();
      expect(screen.queryByText("common.loading")).not.toBeInTheDocument();
      expect(lastListCall()).toMatchObject({
        agent: "agents/a1",
        pageSize: 50,
        pageToken: "",
        statusFilter: [],
      });
    } finally {
      vi.useRealTimers();
    }
  });
});
