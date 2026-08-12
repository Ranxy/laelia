import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Conversation, Reminder } from "@/types/proto-es/v1/command_pb";
import { ReminderStatus } from "@/types/proto-es/v1/command_pb";
import { ReminderDetailPage } from "./reminder-detail";

const mock = vi.hoisted(() => ({
  getReminder: vi.fn(),
  updateReminder: vi.fn(),
  cancelReminder: vi.fn(),
  openThread: vi.fn(),
  closeThread: vi.fn(),
  channels: [] as Conversation[],
}));

vi.mock("@/stores", () => {
  const state = {
    getReminder: mock.getReminder,
    updateReminder: mock.updateReminder,
    cancelReminder: mock.cancelReminder,
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
  return render(
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
  );
}

beforeEach(() => {
  mock.getReminder.mockReset();
  mock.updateReminder.mockReset();
  mock.cancelReminder.mockReset();
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

  it("shows the not-found state with a back action", async () => {
    mock.getReminder.mockResolvedValue(undefined);

    renderPage();

    expect(await screen.findByText("reminders.not-found")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /reminders\.back/ }));
    expect(screen.getByTestId("list")).toBeInTheDocument();
  });

  it("renders the reminder details and opens its thread", async () => {
    mock.getReminder.mockResolvedValue(reminder());
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

  it("saves edits through the update action", async () => {
    mock.getReminder.mockResolvedValue(reminder());
    mock.updateReminder.mockResolvedValue(
      reminder({ taskContent: "Ship it now" })
    );

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "reminders.edit" })
    );

    const task = screen.getByText("reminders.field-task")
      .nextElementSibling as HTMLTextAreaElement;
    fireEvent.change(task, { target: { value: "Ship it now" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => expect(mock.updateReminder).toHaveBeenCalledTimes(1));
    const [name, patch] = mock.updateReminder.mock.calls[0] as [
      string,
      { taskContent: string },
    ];
    expect(name).toBe("reminders/r1");
    expect(patch.taskContent).toBe("Ship it now");
  });

  it("rejects saving an edit without a schedule", async () => {
    mock.getReminder.mockResolvedValue(reminder());

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

  it("cancels the reminder after confirmation", async () => {
    mock.getReminder.mockResolvedValue(reminder());
    mock.cancelReminder.mockResolvedValue(
      reminder({ status: ReminderStatus.CANCELLED })
    );

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "reminders.cancel" })
    );
    expect(
      screen.getByText("reminders.cancel-confirm-title")
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "reminders.cancel" }));

    await waitFor(() =>
      expect(mock.cancelReminder).toHaveBeenCalledWith("reminders/r1")
    );
  });

  it("hides edit and cancel for terminal reminders", async () => {
    mock.getReminder.mockResolvedValue(
      reminder({ status: ReminderStatus.COMPLETED })
    );

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
