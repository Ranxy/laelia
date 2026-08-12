import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Activity, Conversation } from "@/types/proto-es/v1/command_pb";
import { ActivityDetail } from "./activity-detail";

const mock = vi.hoisted(() => ({
  getChannel: vi.fn(),
  openThread: vi.fn(),
  closeThread: vi.fn(),
  markConversationRead: vi.fn(),
  openFilePreview: vi.fn(),
  openImagePreview: vi.fn(),
  activities: [] as Activity[],
  channels: [] as Conversation[],
}));

vi.mock("@/connect", () => ({
  commandServiceClient: { getChannel: mock.getChannel },
}));

vi.mock("@/stores", () => {
  const state = {
    get activities() {
      return mock.activities;
    },
    get channels() {
      return mock.channels;
    },
    openThread: mock.openThread,
    closeThread: mock.closeThread,
    markConversationRead: mock.markConversationRead,
    openFilePreview: mock.openFilePreview,
    openImagePreview: mock.openImagePreview,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const stringifyProps = (props: Record<string, unknown>) =>
  JSON.stringify(props, (_key, value) =>
    typeof value === "bigint" ? value.toString() : value
  );

vi.mock("@/components/chat/thread-panel", () => ({
  ThreadPanel: (props: Record<string, unknown>) => (
    <div data-testid="thread-panel" data-props={stringifyProps(props)} />
  ),
}));

vi.mock("@/pages/dashboard/chat-conversation", () => ({
  ChannelConversationView: (props: Record<string, unknown>) => (
    <div data-testid="channel-view" data-props={stringifyProps(props)} />
  ),
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function activity(overrides: Partial<Activity>): Activity {
  return {
    name: "activities/msg1",
    conversation: "conversations/c1",
    message: "messages/msg1",
    threadRoot: "",
    ...overrides,
  } as unknown as Activity;
}

function renderPage(messageId = "msg1") {
  return render(
    <MemoryRouter initialEntries={[`/activity/${messageId}`]}>
      <Routes>
        <Route path="/activity/:messageId" element={<ActivityDetail />} />
        <Route path="/activity" element={<div data-testid="activity" />} />
        <Route
          path="/:conversationId"
          element={<div data-testid="channel" />}
        />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.getChannel.mockReset();
  mock.openThread.mockReset();
  mock.closeThread.mockReset();
  mock.markConversationRead.mockReset();
  mock.openFilePreview.mockReset();
  mock.openImagePreview.mockReset();
  mock.activities = [];
  mock.channels = [];
});

describe("activity-detail", () => {
  it("shows the not-found state with a back action when the activity is missing", () => {
    renderPage();

    expect(screen.getByText("activity.not-found")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "common.back" }));
    expect(screen.getByTestId("activity")).toBeInTheDocument();
  });

  it("embeds the thread panel for a thread-rooted activity", async () => {
    mock.activities = [
      activity({
        name: "activities/msg1",
        conversation: "conversations/c1",
        threadRoot: "messages/root1",
      }),
    ];
    mock.channels = [
      { name: "conversations/c1", title: "General", type: 2 } as Conversation,
    ];

    renderPage();

    expect(await screen.findByTestId("thread-panel")).toBeInTheDocument();
    expect(mock.openThread).toHaveBeenCalledWith("conversations/c1", "root1");
    expect(mock.markConversationRead).toHaveBeenCalledWith("c1");
    const props = JSON.parse(
      screen.getByTestId("thread-panel").getAttribute("data-props") ?? "{}"
    );
    expect(props.channelId).toBe("c1");
    expect(props.channelTitle).toBe("General");
    expect(props.rootMessageId).toBe("root1");
    expect(props.scrollToMessageId).toBe("msg1");
  });

  it("embeds the channel view for a top-level activity", async () => {
    mock.activities = [activity({ name: "activities/msg1" })];
    mock.channels = [
      { name: "conversations/c1", title: "General", type: 2 } as Conversation,
    ];

    renderPage();

    const view = await screen.findByTestId("channel-view");
    const props = JSON.parse(view.getAttribute("data-props") ?? "{}");
    expect(props.conversationId).toBe("c1");
    expect(props.scrollToMessageId).toBe("msg1");
    expect(props.scrollToReadVersion).toBeUndefined();
  });

  it("scrolls a DM activity to the read version instead of a message", async () => {
    mock.activities = [activity({ name: "activities/msg1" })];
    mock.channels = [
      {
        name: "conversations/c1",
        title: "DM",
        type: 1,
        readVersion: 7n,
      } as unknown as Conversation,
    ];

    renderPage();

    const view = await screen.findByTestId("channel-view");
    const props = JSON.parse(view.getAttribute("data-props") ?? "{}");
    expect(props.scrollToMessageId).toBeUndefined();
    expect(props.scrollToReadVersion).toBe("7");
  });

  it("fetches the conversation when it is not in the left-rail list", async () => {
    mock.activities = [activity({ name: "activities/msg1" })];
    mock.getChannel.mockResolvedValue({
      name: "conversations/c1",
      title: "Fetched",
      type: 2,
    });

    renderPage();

    await waitFor(() =>
      expect(mock.getChannel).toHaveBeenCalledWith({ name: "conversations/c1" })
    );
    const view = await screen.findByTestId("channel-view");
    const props = JSON.parse(view.getAttribute("data-props") ?? "{}");
    expect(props.conversationId).toBe("c1");
  });
});
