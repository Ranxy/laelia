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
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Conversation, Reminder } from "@/types/proto-es/v1/command_pb";
import { ReminderStatus } from "@/types/proto-es/v1/command_pb";
import { ReminderDetailPage } from "./reminder-detail";

const mock = vi.hoisted(() => ({
  getReminder: vi.fn(),
  updateReminder: vi.fn(),
  cancelReminder: vi.fn(),
  // Backstop: nothing on this page should call it, but the mocked client
  // keeps any stray read off the real transport.
  listAgents: vi.fn(),
  openThread: vi.fn(),
  closeThread: vi.fn(),
  channels: [] as Conversation[],
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    getReminder: mock.getReminder,
    updateReminder: mock.updateReminder,
    cancelReminder: mock.cancelReminder,
    listAgents: mock.listAgents,
  },
}));

vi.mock("@/stores", () => {
  const state = {
    openThread: mock.openThread,
    closeThread: mock.closeThread,
    get channels() {
      return mock.channels;
    },
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

vi.mock("@/components/chat/thread-panel", () => ({
  ThreadPanel: (props: Record<string, unknown>) => (
    <div data-testid="thread-panel" data-props={JSON.stringify(props)} />
  ),
}));

const tFn = (key: string, params?: Record<string, string | number>) => {
  if (!params) return key;
  const values = Object.values(params);
  return values.length > 0 ? `${key}:${values.join(":")}` : key;
};
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function reminder(overrides?: Partial<Reminder>): Reminder {
  return {
    name: "reminders/r1",
    conversation: "conversations/c1",
    message: "conversations/c1/messages/m1",
    assigneeAgent: "agents/a1",
    assigneeName: "Alice",
    taskContent: "Ship the release",
    fireAt: { seconds: 1700000000n, nanos: 0 },
    cronExpr: "",
    tz: "UTC",
    status: ReminderStatus.PENDING,
    retryCount: 2,
    result: "",
    ...overrides,
  } as unknown as Reminder;
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/members/agents/a1/reminders/r1"]}>
        <Routes>
          <Route
            path="/members/agents/:agentId/reminders/:reminderId"
            element={<ReminderDetailPage />}
          />
          <Route
            path="/members/agents/:agentId/reminders"
            element={<div data-testid="list" />}
          />
          <Route
            path="/:conversationId"
            element={<div data-testid="channel" />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
  return { client, ...view };
}

beforeEach(() => {
  mock.getReminder.mockReset();
  mock.updateReminder.mockReset();
  mock.cancelReminder.mockReset();
  mock.listAgents.mockReset();
  mock.openThread.mockReset();
  mock.closeThread.mockReset();
  mock.channels = [];
});

describe("reminder-detail", () => {
  it("shows the loading hint while the reminder is being fetched", () => {
    mock.getReminder.mockReturnValue(new Promise(() => {}));

    renderPage();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
  });

  it("shows the not-found state when the RPC fails", async () => {
    // Old getReminder caught RPC errors and returned undefined; the mutation
    // to TanStack Query surfaces the same failure as the not-found screen.
    mock.getReminder.mockRejectedValue(new Error("rpc unavailable"));

    renderPage();

    expect(await screen.findByText("reminders.not-found")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /reminders\.back/ }));
    expect(screen.getByTestId("list")).toBeInTheDocument();
  });

  it("renders the reminder details and opens its thread", async () => {
    mock.getReminder.mockResolvedValue({ reminder: reminder() });
    mock.channels = [
      { name: "conversations/c1", title: "General" } as Conversation,
    ];

    renderPage();

    expect(await screen.findByText("Ship the release")).toBeInTheDocument();
    expect(screen.getByText("reminders.once")).toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("reminders.retry-count:2")).toBeInTheDocument();
    expect(mock.openThread).toHaveBeenCalledWith("conversations/c1", "m1");
    const props = JSON.parse(
      screen.getByTestId("thread-panel").getAttribute("data-props") ?? "{}"
    );
    expect(props.channelTitle).toBe("General");
    expect(props.rootMessageId).toBe("m1");
  });

  it("stops the 2s poll loop once a terminal status is observed", async () => {
    vi.useFakeTimers();
    try {
      mock.getReminder.mockResolvedValue({
        reminder: reminder({ status: ReminderStatus.COMPLETED }),
      });

      renderPage();
      expect(mock.getReminder).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10);
      });
      expect(screen.getByText("Ship the release")).toBeInTheDocument();

      // Terminal reminders are immutable: the poll interval must not refetch.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(2000);
      });
      expect(mock.getReminder).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps polling every 2s while the reminder is not terminal", async () => {
    vi.useFakeTimers();
    try {
      mock.getReminder.mockResolvedValue({ reminder: reminder() });

      renderPage();
      expect(mock.getReminder).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(10);
      });
      expect(screen.getByText("Ship the release")).toBeInTheDocument();

      // The interval phase starts at mount, so nothing refetches until the
      // clock reaches t=2000 (10ms were already advanced above).
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1989);
      });
      expect(mock.getReminder).toHaveBeenCalledTimes(1);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1);
      });
      expect(mock.getReminder).toHaveBeenCalledTimes(2);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(2000);
      });
      expect(mock.getReminder).toHaveBeenCalledTimes(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it("saves edits through the mocked client and backfills the UI", async () => {
    mock.getReminder.mockResolvedValue({ reminder: reminder() });
    mock.updateReminder.mockResolvedValue({
      reminder: reminder({ taskContent: "Ship it now" }),
    });

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "reminders.edit" })
    );

    const task = screen.getByText("reminders.field-task")
      .nextElementSibling as HTMLTextAreaElement;
    fireEvent.change(task, { target: { value: "Ship it now" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => expect(mock.updateReminder).toHaveBeenCalledTimes(1));
    const [request] = mock.updateReminder.mock.calls[0] as [
      { name: string; taskContent: string },
    ];
    expect(request.name).toBe("reminders/r1");
    expect(request.taskContent).toBe("Ship it now");

    // The mutation's onSuccess wrote the returned reminder into the detail
    // cache, so the body shows the new content immediately and the sheet
    // closes (old setReminder + setEditOpen behavior).
    expect(await screen.findByText("Ship it now")).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "common.save" })
      ).not.toBeInTheDocument()
    );
  });

  it("rejects saving an edit without a schedule", async () => {
    mock.getReminder.mockResolvedValue({ reminder: reminder() });

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "reminders.edit" })
    );

    const task = screen.getByText("reminders.field-task")
      .nextElementSibling as HTMLTextAreaElement;
    fireEvent.change(task, { target: { value: "No schedule" } });
    // The sheet renders in a portal, so query the document for the input.
    const fireAt = document.querySelector(
      'input[type="datetime-local"]'
    ) as HTMLInputElement;
    fireEvent.change(fireAt, { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    expect(
      await screen.findByText("reminders.edit-fire-required")
    ).toBeInTheDocument();
    expect(mock.updateReminder).not.toHaveBeenCalled();
  });

  it("shows the failure hint when the update RPC fails", async () => {
    mock.getReminder.mockResolvedValue({ reminder: reminder() });
    mock.updateReminder.mockRejectedValue(new Error("rpc unavailable"));

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "reminders.edit" })
    );

    const task = screen.getByText("reminders.field-task")
      .nextElementSibling as HTMLTextAreaElement;
    fireEvent.change(task, { target: { value: "Ship it now" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    // The old implementation escaped as an unhandled rejection; the mutation
    // onError now lands it in the sheet's actionError slot.
    expect(
      await screen.findByText("reminders.edit-failed")
    ).toBeInTheDocument();
  });

  it("cancels the reminder after confirmation", async () => {
    mock.getReminder.mockResolvedValue({ reminder: reminder() });
    mock.cancelReminder.mockResolvedValue({
      reminder: reminder({ status: ReminderStatus.CANCELLED }),
    });

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "reminders.cancel" })
    );
    expect(
      screen.getByText("reminders.cancel-confirm-title")
    ).toBeInTheDocument();
    const dialog = screen.getByRole("alertdialog");
    fireEvent.click(
      within(dialog).getByRole("button", { name: "reminders.cancel" })
    );

    await waitFor(() => {
      expect(mock.cancelReminder).toHaveBeenCalledTimes(1);
      const [request] = mock.cancelReminder.mock.calls[0] as [{ name: string }];
      expect(request.name).toBe("reminders/r1");
    });

    // The cached reminder flips to CANCELLED (terminal), so the edit and
    // cancel actions disappear and the dialog closes.
    await waitFor(() => {
      expect(
        screen.queryByRole("button", { name: "reminders.edit" })
      ).not.toBeInTheDocument();
      expect(
        screen.queryByText("reminders.cancel-confirm-title")
      ).not.toBeInTheDocument();
    });
  });

  it("hides edit and cancel for terminal reminders", async () => {
    mock.getReminder.mockResolvedValue({
      reminder: reminder({ status: ReminderStatus.COMPLETED }),
    });

    renderPage();

    await screen.findByText("Ship the release");
    expect(
      screen.queryByRole("button", { name: "reminders.edit" })
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "reminders.cancel" })
    ).not.toBeInTheDocument();
  });
});
