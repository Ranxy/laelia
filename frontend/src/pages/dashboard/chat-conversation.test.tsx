import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// ChatConversationPage and ChannelConversationView read the presence map out
// of the Query cache through useOnlineUsers — tests render under a fresh
// provider (the badge data itself is covered by use-presence.test.tsx).
function renderWithQuery(ui: React.ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>
  );
}

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

// The route is driven by useParams/useSearchParams; an updater-style
// setSearchParams mock lets the deep-link cleanup pass be observed.
const mockRouter = vi.hoisted(() => ({
  navigate: vi.fn(),
  params: {} as Record<string, string | undefined>,
  searchParams: new URLSearchParams(),
  setSearchParams: vi.fn(),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mockRouter.navigate,
  useParams: () => mockRouter.params,
  useSearchParams: () => [mockRouter.searchParams, mockRouter.setSearchParams],
}));

vi.mock("markstream-react", () => ({
  MarkdownRender: ({ content }: { content: string }) => <>{content}</>,
  setCustomComponents: () => {},
  default: ({ content }: { content: string }) => <>{content}</>,
}));

const mockClient = vi.hoisted(() => ({
  getChannel: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: mockClient,
}));

vi.mock("@/components/chat/message-row", () => ({
  MessageRow: ({ msg }: { msg: { id: string; content: string } }) => (
    <div data-msg-id={msg.id}>{msg.content}</div>
  ),
  rowStreamingProps: () => ({ streamingContent: "", streamingEvents: [] }),
  EMPTY_EVENTS: [],
}));

vi.mock("@/components/chat/states", () => ({
  EmptyState: ({ message }: { message: string }) => <div>{message}</div>,
  LoadingState: () => <div data-testid="loading" />,
}));

vi.mock("@/components/chat/mention-badge", () => ({
  MentionBadge: () => <div />,
}));

vi.mock("@/components/chat/mention-popup", () => ({
  MentionPopup: () => <div />,
}));

vi.mock("@/components/chat/mention-detail-sheet", () => ({
  MentionDetailSheet: () => <div />,
}));

vi.mock("@/components/chat/thread-panel", () => ({
  ThreadPanel: ({ rootMessageId }: { rootMessageId: string }) => (
    <div data-testid="thread-panel">{rootMessageId}</div>
  ),
}));

vi.mock("@/components/chat/chat-composer", () => ({
  ChatComposer: () => <div data-testid="composer" />,
}));

vi.mock("@/composables/useMentionTargets", () => ({
  useMentionTargets: () => [],
  useMentionLabelResolver: () => () => undefined,
  targetToMention: (t: unknown) => t,
}));

import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/ui-models";
import type { Conversation } from "@/types/proto-es/v1/command_pb";
import {
  ChannelConversationView,
  ChatConversationPage,
  ChatEmptyState,
} from "./chat-conversation";

// Store action doubles: the page's init/watcher/read lifecycle is the
// behavior under test, so the actions themselves are the observation points.
const mockedActions = vi.hoisted(() => ({
  loadMessages: vi.fn(),
  listChannelMembers: vi.fn(),
  startWatchingChannel: vi.fn(),
  stopWatchingChannel: vi.fn(),
  markConversationRead: vi.fn(),
  fetchAgents: vi.fn(),
  jumpToMessage: vi.fn(),
  loadOlderMessages: vi.fn(),
  loadNewerMessages: vi.fn(),
  clearJump: vi.fn(),
  openThread: vi.fn(),
  closeThread: vi.fn(),
  toggleTasksPanel: vi.fn(),
  closeTasksPanel: vi.fn(),
  toggleReaction: vi.fn(),
  convertMessageToTask: vi.fn(),
  openFilePreview: vi.fn(),
  openImagePreview: vi.fn(),
}));

const CONV = "conversations/c1";

function conversation(
  overrides?: Partial<Conversation> & { type?: number }
): Conversation {
  return {
    name: "conversations/c1",
    type: 2,
    title: "General",
    ownerId: "users/1",
    archived: false,
    ...overrides,
  } as unknown as Conversation;
}

function message(id: string, content: string): ChatMessageUI {
  return {
    id: `conversations/c1/messages/${id}`,
    role: "user",
    content,
    timestamp: new Date(0),
  };
}

function seedStore(channel: Conversation | null) {
  useAppStore.getState().reset();
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      handle: "users/1",
      title: "Ran",
      chatPreferences: { enterToSend: true },
      permissions: [],
    } as never,
    channels: channel ? [channel] : [],
    chatMessages: {
      [CONV]: [message("m1", "hello world"), message("m2", "second message")],
    },
    loadMessages: mockedActions.loadMessages,
    listChannelMembers: mockedActions.listChannelMembers,
    startWatchingChannel: mockedActions.startWatchingChannel,
    stopWatchingChannel: mockedActions.stopWatchingChannel,
    markConversationRead: mockedActions.markConversationRead,
    fetchAgents: mockedActions.fetchAgents,
    jumpToMessage: mockedActions.jumpToMessage,
    loadOlderMessages: mockedActions.loadOlderMessages,
    loadNewerMessages: mockedActions.loadNewerMessages,
    clearJump: mockedActions.clearJump,
    openThread: mockedActions.openThread,
    closeThread: mockedActions.closeThread,
    toggleTasksPanel: mockedActions.toggleTasksPanel,
    closeTasksPanel: mockedActions.closeTasksPanel,
    toggleReaction: mockedActions.toggleReaction,
    convertMessageToTask: mockedActions.convertMessageToTask,
    openFilePreview: mockedActions.openFilePreview,
    openImagePreview: mockedActions.openImagePreview,
  });
}

describe("ChatConversationPage init lifecycle", () => {
  beforeEach(() => {
    seedStore(conversation());
    Object.assign(mockRouter, {
      params: { conversationId: "c1" },
      searchParams: new URLSearchParams(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("loads messages, members, read-mark and starts the watcher on mount; stops and closes on unmount", async () => {
    const { unmount } = renderWithQuery(<ChatConversationPage />);

    await waitFor(() => {
      expect(mockedActions.loadMessages).toHaveBeenCalledWith(CONV);
    });
    expect(mockedActions.listChannelMembers).toHaveBeenCalledWith("c1");
    expect(mockedActions.markConversationRead).toHaveBeenCalledWith("c1");
    expect(mockedActions.startWatchingChannel).toHaveBeenCalledWith(CONV);

    unmount();
    expect(mockedActions.stopWatchingChannel).toHaveBeenCalledWith(CONV);
    expect(mockedActions.closeThread).toHaveBeenCalled();
  });

  it("renders the message list from the store slice", async () => {
    renderWithQuery(<ChatConversationPage />);
    expect(await screen.findByText("hello world")).toBeInTheDocument();
    expect(screen.getByText("second message")).toBeInTheDocument();
  });
});

describe("ChatConversationPage thread deep link", () => {
  beforeEach(() => {
    seedStore(conversation());
    Object.assign(mockRouter, {
      params: { conversationId: "c1" },
      searchParams: new URLSearchParams("thread=conversations/c1/messages/m1"),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("opens the thread panel for ?thread= and cleans the URL params", async () => {
    renderWithQuery(<ChatConversationPage />);

    await waitFor(() => {
      expect(mockedActions.openThread).toHaveBeenCalledWith(
        CONV,
        "conversations/c1/messages/m1"
      );
    });
    // The deep link is one-shot: parameters are removed afterwards.
    await waitFor(() => {
      expect(mockRouter.setSearchParams).toHaveBeenCalled();
    });
    const updater = mockRouter.setSearchParams.mock.calls[0][0];
    const cleaned = updater(new URLSearchParams("thread=x&message=y&v=1"));
    expect(cleaned.get("thread")).toBeNull();
    expect(cleaned.get("message")).toBeNull();
  });
});

describe("ChatConversationPage agent-DM view-only", () => {
  beforeEach(() => {
    // Agent DMs are excluded from the left rail, so the page must resolve the
    // conversation via GetChannel before it can know the type.
    seedStore(null);
    mockClient.getChannel.mockResolvedValue(conversation({ type: 3 }));
    Object.assign(mockRouter, {
      params: { conversationId: "c1" },
      searchParams: new URLSearchParams(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("fetches the conversation metadata and replaces the composer with the view-only notice", async () => {
    renderWithQuery(<ChatConversationPage />);

    await waitFor(() => {
      expect(mockClient.getChannel).toHaveBeenCalledWith({ name: CONV });
    });
    expect(
      await screen.findByText("chat.agent-dm-view-only")
    ).toBeInTheDocument();
    expect(screen.queryByTestId("composer")).toBeNull();
  });
});

describe("ChannelConversationView embedded mode", () => {
  beforeEach(() => {
    seedStore(conversation());
    Object.assign(mockRouter, {
      params: {},
      searchParams: new URLSearchParams(),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("uses the explicit conversationId prop over the route param", async () => {
    renderWithQuery(<ChannelConversationView conversationId="c1" />);

    await waitFor(() => {
      expect(mockedActions.loadMessages).toHaveBeenCalledWith(CONV);
    });
    expect(mockedActions.startWatchingChannel).toHaveBeenCalledWith(CONV);
  });
});

describe("ChatEmptyState", () => {
  it("renders the standalone chat empty state", () => {
    render(<ChatEmptyState />);
    expect(screen.getByText("chat.select-conversation")).toBeInTheDocument();
  });
});
