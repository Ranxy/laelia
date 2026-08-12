import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Reminder } from "@/types/proto-es/v1/command_pb";
import { ReminderStatus } from "@/types/proto-es/v1/command_pb";
import { ReminderListPage } from "./reminder-list";

const mock = vi.hoisted(() => ({
  listReminders: vi.fn(),
  reminders: [] as Reminder[],
  remindersLoading: false,
}));

vi.mock("@/stores", () => {
  const state = {
    get reminders() {
      return mock.reminders;
    },
    get remindersLoading() {
      return mock.remindersLoading;
    },
    listReminders: mock.listReminders,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

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

// The real store action writes the fetched rows into the store; the mock must
// mirror that so the page re-renders with the rows.
function mockList(rows: Reminder[], nextPageToken = "") {
  mock.reminders = rows;
  mock.listReminders.mockResolvedValue({ reminders: rows, nextPageToken });
}

function renderPage() {
  return render(
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

beforeEach(() => {
  mock.listReminders.mockReset();
  mock.reminders = [];
  mock.remindersLoading = false;
});

describe("reminder-list", () => {
  it("renders reminder rows with status, task, schedule and assignee", async () => {
    mockList([
      reminder(
        "agents/a1/reminders/r1",
        "Ship the release",
        ReminderStatus.PENDING
      ),
      reminder("agents/a1/reminders/r2", "Follow up", ReminderStatus.COMPLETED),
    ]);

    renderPage();

    expect(await screen.findByText("Ship the release")).toBeInTheDocument();
    expect(screen.getByText("Follow up")).toBeInTheDocument();
    expect(screen.getAllByText("once").length).toBe(2);
    expect(screen.getAllByText("Alice").length).toBe(2);
  });

  it("shows the empty hint when there are no reminders", async () => {
    mockList([]);

    renderPage();

    expect(await screen.findByText("reminders.empty")).toBeInTheDocument();
  });

  it("refetches with the selected status filter when a tab is clicked", async () => {
    mockList([]);

    renderPage();
    await screen.findByText("reminders.empty");
    fireEvent.click(
      screen.getByRole("button", { name: "reminders.filter-due" })
    );

    await waitFor(() => expect(mock.listReminders).toHaveBeenCalledTimes(2));
    const [, req] = mock.listReminders.mock.calls[1] as [
      string,
      { statusFilter: ReminderStatus[] },
    ];
    expect(req.statusFilter).toEqual([ReminderStatus.DUE]);
  });

  it("navigates to the detail page when a row is clicked", async () => {
    mockList([
      reminder("agents/a1/reminders/r1", "Ship it", ReminderStatus.PENDING),
    ]);

    renderPage();
    fireEvent.click(await screen.findByText("Ship it"));

    expect(screen.getByTestId("detail")).toBeInTheDocument();
  });

  it("paginates with next and prev buttons", async () => {
    mockList(
      [reminder("agents/a1/reminders/r1", "Page one", ReminderStatus.PENDING)],
      "tok2"
    );

    renderPage();
    await screen.findByText("Page one");
    const next = screen.getByRole("button", { name: /reminders.next/ });
    fireEvent.click(next);

    await waitFor(() => expect(mock.listReminders).toHaveBeenCalledTimes(2));
    const [, req] = mock.listReminders.mock.calls[1] as [
      string,
      { pageToken: string },
    ];
    expect(req.pageToken).toBe("tok2");
  });
});
