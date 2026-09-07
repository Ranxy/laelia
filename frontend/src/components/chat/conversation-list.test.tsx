import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PRESENCE_QUERY_KEY } from "@/hooks/use-presence";

// ConversationList uses react-i18next (no provider in tests) and the app
// store. Stub i18n with a key/count mapper so assertions read the keys, and
// stub the store with a selector stand-in carrying a fixed channel roster.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, params?: { count?: number; filter?: string }) =>
      params?.count != null
        ? `${key}:${params.count}`
        : params?.filter != null
          ? `${key}:${params.filter}`
          : key,
  }),
}));

const mock = vi.hoisted(() => ({
  channels: [] as Array<Record<string, unknown>>,
  currentUser: { name: "users/ran-user-1", handle: "ran-user-1" },
  unreadByConv: {} as Record<string, number>,
  // Presence inputs: the agent roster (with connection state) and the human
  // presence map, both read by the DM rows' green badge.
  agents: [] as Array<Record<string, unknown>>,
  presences: {} as Record<string, { online: boolean }>,
  setConversationPinned: vi.fn(),
  setConversationClosed: vi.fn(),
  setConversationMuted: vi.fn(),
  toastAdd: vi.fn(),
  useIsDesktop: vi.fn(() => true),
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: Record<string, unknown>) => unknown) =>
    selector({
      channels: mock.channels,
      channelsLoading: false,
      unreadByConv: mock.unreadByConv,
      createChannel: async () => {},
      setConversationPinned: mock.setConversationPinned,
      setConversationClosed: mock.setConversationClosed,
      setConversationMuted: mock.setConversationMuted,
      currentUser: mock.currentUser,
      agents: mock.agents,
    }),
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => vi.fn(),
  useParams: () => ({ conversationId: "ch1" }),
}));

// Avatar fetching hits @/connect; stub it so DM rows never touch the network.
vi.mock("@/connect", () => ({
  agentServiceClient: {},
  userServiceClient: {},
  presenceServiceClient: {},
}));

// The undo toast is app chrome; capture the add call and drive the action
// callback directly instead of rendering the real toaster.
vi.mock("@/lib/toast", () => ({
  toastManager: { add: mock.toastAdd },
}));

// Default to desktop (context menu) so the mobile-swipe tests can opt out
// with mock.useIsDesktop.mockReturnValue(false).
vi.mock("@/hooks/use-is-desktop", () => ({
  useIsDesktop: mock.useIsDesktop,
}));

import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { ConversationList } from "./conversation-list";

beforeEach(() => {
  localStorage.clear();
  mock.unreadByConv = {};
  mock.channels = [];
  mock.agents = [];
  mock.presences = {};
  mock.useIsDesktop.mockReturnValue(true);
});

function channel(overrides: Record<string, unknown> = {}): Conversation {
  return {
    name: "conversations/ch1",
    title: "Design",
    type: 2,
    memberCount: 3,
    pinned: false,
    lastMessage: "",
    lastMessageSender: "",
    lastMessagePrincipalId: "",
    ...overrides,
  } as unknown as Conversation;
}

// Human presence badges read the presence Query cache (the read loop is its
// only writer), so tests seed it with the mock map at render time.
function renderList() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(PRESENCE_QUERY_KEY, { ...mock.presences });
  return render(
    <QueryClientProvider client={client}>
      <ConversationList />
    </QueryClientProvider>
  );
}

describe("ConversationList last-message preview", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
    mock.channels = [];
  });

  it("does not render a member count after the channel name", () => {
    mock.channels = [channel()];
    renderList();
    expect(screen.getByText("Design")).toBeInTheDocument();
    // Member counts were removed from the chat list so long channel names can
    // use the full row width.
    expect(screen.queryByText("channel.members:3")).not.toBeInTheDocument();
    expect(screen.queryByText("3 members")).not.toBeInTheDocument();
    expect(screen.queryByText("3 member")).not.toBeInTheDocument();
  });

  it("prefixes the viewer's own message with the You label", () => {
    mock.channels = [
      channel({
        lastMessage: "on my way",
        lastMessageSender: "Alice",
        lastMessagePrincipalId: "ran-user-1",
      }),
    ];
    renderList();
    expect(screen.getByText("chat.you: on my way")).toBeInTheDocument();
  });

  it("prefixes another user's message with their sender name", () => {
    mock.channels = [
      channel({
        lastMessage: "lgtm",
        lastMessageSender: "Bob",
        lastMessagePrincipalId: "bob-user-1",
      }),
    ];
    renderList();
    expect(screen.getByText("Bob: lgtm")).toBeInTheDocument();
  });

  it("shows HH:MM for a message sent today", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 7, 11, 15, 30));
    const today = new Date(2026, 7, 11, 9, 5);
    mock.channels = [
      channel({
        lastMessage: "hi",
        lastMessageSender: "Alice",
        lastMessagePrincipalId: "7",
        lastMessageAt: {
          seconds: BigInt(Math.floor(today.getTime() / 1000)),
          nanos: 0,
        },
      }),
    ];
    renderList();
    expect(screen.getByText("09:05")).toBeInTheDocument();
  });

  it("keeps the preview line for conversations with no messages yet", () => {
    mock.channels = [channel()];
    renderList();
    // The row still renders its title and no preview text or time appears.
    expect(screen.getByText("Design")).toBeInTheDocument();
    expect(screen.queryByText(/chat.you:|Alice:|Bob:/)).not.toBeInTheDocument();
  });
});

describe("ConversationList filters", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    mock.channels = [];
    mock.unreadByConv = {};
    mock.useIsDesktop.mockReturnValue(true);
  });

  it("renders the five desktop filter tags", () => {
    mock.channels = [channel()];
    renderList();
    expect(
      screen.getByRole("group", { name: "chat.filter-label" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-all" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-unread" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-humans" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-agents" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-groups" })
    ).toBeInTheDocument();
  });

  it("shows only unread conversations when the unread tag is selected", () => {
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Channel", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "Agent DM",
        type: 1,
        peer: "agents/agent-1",
      }),
    ];
    mock.unreadByConv = { "conversations/ch2": 2 };
    renderList();
    expect(screen.getByText("Channel")).toBeInTheDocument();
    expect(screen.getByText("Agent DM")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-unread" }));
    expect(screen.queryByText("Channel")).not.toBeInTheDocument();
    expect(screen.getByText("Agent DM")).toBeInTheDocument();
  });

  it("filters humans, agents and groups by conversation type", () => {
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Group", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "Agent",
        type: 1,
        peer: "agents/agent-1",
      }),
      channel({
        name: "conversations/ch3",
        title: "Human",
        type: 4,
        peer: "users/alice",
      }),
    ];
    renderList();
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("Human")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-humans" }));
    expect(screen.queryByText("Group")).not.toBeInTheDocument();
    expect(screen.queryByText("Agent")).not.toBeInTheDocument();
    expect(screen.getByText("Human")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-agents" }));
    expect(screen.queryByText("Group")).not.toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.queryByText("Human")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-groups" }));
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.queryByText("Agent")).not.toBeInTheDocument();
    expect(screen.queryByText("Human")).not.toBeInTheDocument();
  });

  it("clears the filter when clicking the active tag again", () => {
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Group", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "Agent",
        type: 1,
        peer: "agents/agent-1",
      }),
    ];
    renderList();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-agents" }));
    expect(screen.queryByText("Group")).not.toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-agents" }));
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
  });

  it("shows all conversations from the all tag and keeps them on re-click", () => {
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Group", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "Agent",
        type: 1,
        peer: "agents/agent-1",
      }),
    ];
    renderList();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-agents" }));
    expect(screen.queryByText("Group")).not.toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-all" }));
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();

    // Clicking All again keeps showing everything (no toggle back to a filter).
    fireEvent.click(screen.getByRole("button", { name: "chat.filter-all" }));
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
  });

  it("persists the selection to localStorage for the current account", () => {
    mock.channels = [channel()];
    renderList();
    fireEvent.click(screen.getByRole("button", { name: "chat.filter-groups" }));
    expect(localStorage.getItem("laelia-chat-filter:ran-user-1")).toBe(
      "groups"
    );
  });

  it("restores a persisted selection on mount", () => {
    localStorage.setItem("laelia-chat-filter:ran-user-1", "humans");
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Group", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "Human",
        type: 4,
        peer: "users/user",
      }),
    ];
    renderList();
    expect(screen.queryByText("Group")).not.toBeInTheDocument();
    expect(screen.getByText("Human")).toBeInTheDocument();
  });

  it("renders the filter chips on mobile and collapses via the funnel", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    renderList();
    expect(
      screen.getByRole("group", { name: "chat.filter-label" })
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-collapse" })
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "chat.filter-collapse" })
    );
    expect(
      screen.queryByRole("group", { name: "chat.filter-label" })
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "chat.filter-expand" })
    ).toBeInTheDocument();
  });

  it("shows an active-filter notice with a clear action", () => {
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Group", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "Human",
        type: 4,
        peer: "users/alice",
      }),
    ];
    renderList();
    fireEvent.click(screen.getByRole("button", { name: "chat.filter-humans" }));
    expect(
      screen.getByText("chat.filter-notice:chat.filter-humans")
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "chat.filter-clear" }));
    expect(
      screen.queryByText("chat.filter-notice:chat.filter-humans")
    ).not.toBeInTheDocument();
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Human")).toBeInTheDocument();
  });
});

describe("ConversationList mobile create-channel FAB", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    mock.channels = [];
  });

  it("renders the expanded pill (icon + label) by default", () => {
    renderList();
    const fab = screen.getByTestId("create-channel-fab");
    expect(fab).toBeInTheDocument();
    expect(screen.getByText("channel.fab-label")).toBeInTheDocument();
  });

  it("collapses to the bare icon while the list is scrolled down", () => {
    renderList();
    const list = screen.getByTestId("conversation-list-scroll");
    expect(screen.getByText("channel.fab-label")).toBeInTheDocument();

    list.scrollTop = 50;
    fireEvent.scroll(list);
    expect(screen.queryByText("channel.fab-label")).not.toBeInTheDocument();

    // Scrolling back to the top restores the label.
    list.scrollTop = 0;
    fireEvent.scroll(list);
    expect(screen.getByText("channel.fab-label")).toBeInTheDocument();
  });

  it("opens the create dialog from the FAB", () => {
    renderList();
    fireEvent.click(screen.getByTestId("create-channel-fab"));
    expect(screen.getByText("channel.create-title")).toBeInTheDocument();
  });
});

describe("ConversationList close and context menu", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
    mock.channels = [];
    mock.setConversationClosed.mockClear();
    mock.setConversationPinned.mockClear();
    mock.setConversationMuted.mockClear();
    mock.toastAdd.mockClear();
    mock.useIsDesktop.mockReturnValue(true);
  });

  it("closes a conversation from the desktop context menu", () => {
    mock.channels = [channel()];
    renderList();
    fireEvent.contextMenu(screen.getByText("Design"));
    expect(screen.getByText("channel.pin")).toBeInTheDocument();
    expect(screen.getByText("chat.close")).toBeInTheDocument();

    fireEvent.click(screen.getByText("chat.close"));
    expect(mock.setConversationClosed).toHaveBeenCalledWith("ch1", true);
  });

  it("offers an undo toast whose action reopens the conversation", () => {
    mock.channels = [channel()];
    renderList();
    fireEvent.contextMenu(screen.getByText("Design"));
    fireEvent.click(screen.getByText("chat.close"));

    expect(mock.toastAdd).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "info",
        title: "chat.closed-title",
        timeout: 5000,
        actionProps: expect.objectContaining({ children: "chat.undo" }),
      })
    );
    const { actionProps } = mock.toastAdd.mock.calls[0][0];
    act(() => actionProps.onClick());
    expect(mock.setConversationClosed).toHaveBeenCalledWith("ch1", false);
  });

  it("pins and unpins from the desktop context menu", () => {
    mock.channels = [channel()];
    renderList();
    fireEvent.contextMenu(screen.getByText("Design"));
    fireEvent.click(screen.getByText("channel.pin"));
    expect(mock.setConversationPinned).toHaveBeenCalledWith("ch1", true);
  });

  it("mutes and unmutes from the desktop context menu", () => {
    mock.channels = [channel()];
    renderList();
    fireEvent.contextMenu(screen.getByText("Design"));
    fireEvent.click(screen.getByText("channel.mute"));
    expect(mock.setConversationMuted).toHaveBeenCalledWith("ch1", true);
  });

  it("shows both swipe actions on mobile and closes on the close tap", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    renderList();
    // The two swipe buttons sit side by side behind the row.
    expect(screen.getByTestId("swipe-close")).toBeInTheDocument();
    expect(screen.getByTestId("swipe-pin")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("swipe-close"));
    expect(mock.setConversationClosed).toHaveBeenCalledWith("ch1", true);
  });

  it("does not drag the row when the touch travels vertically", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    renderList();
    const row = screen.getByText("Design").closest("button") as HTMLElement;

    // A scroll gesture: mostly vertical travel with the 1-2px horizontal
    // jitter every real finger produces. The direction lock (10px) must
    // classify it as a scroll and leave the row at rest.
    fireEvent.touchStart(row, { touches: [{ clientX: 100, clientY: 100 }] });
    fireEvent.touchMove(row, { touches: [{ clientX: 98, clientY: 140 }] });
    fireEvent.touchEnd(row);
    expect(row.style.transform).toBe("translateX(0px)");
  });

  it("snaps the row open when the horizontal drag passes half the action width", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    renderList();
    const row = screen.getByText("Design").closest("button") as HTMLElement;

    fireEvent.touchStart(row, { touches: [{ clientX: 200, clientY: 100 }] });
    // 80px horizontal travel (past half of SWIPE_ACTION_WIDTH=144) with only
    // vertical jitter below the direction lock.
    fireEvent.touchMove(row, { touches: [{ clientX: 120, clientY: 104 }] });
    expect(row.style.transform).toBe("translateX(-80px)");
    fireEvent.touchEnd(row);
    expect(row.style.transform).toBe("translateX(-144px)");
  });

  it("keeps the row at rest for sub-lock horizontal jitter", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    renderList();
    const row = screen.getByText("Design").closest("button") as HTMLElement;

    fireEvent.touchStart(row, { touches: [{ clientX: 100, clientY: 100 }] });
    fireEvent.touchMove(row, { touches: [{ clientX: 95, clientY: 100 }] });
    fireEvent.touchEnd(row);
    expect(row.style.transform).toBe("translateX(0px)");
  });

  it("does not mount the context menu trigger on mobile", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    renderList();
    fireEvent.contextMenu(screen.getByText("Design"));
    expect(screen.queryByText("chat.close")).not.toBeInTheDocument();
  });
});

describe("ConversationList agent badge", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    mock.channels = [];
  });

  it("marks agent DM rows with the agent badge and leaves other rows unmarked", () => {
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Design", type: 2 }),
      channel({
        name: "conversations/ch2",
        title: "My Agent",
        type: 1,
        peer: "agents/agent-1",
      }),
      channel({
        name: "conversations/ch3",
        title: "Alice",
        type: 4,
        peer: "users/alice",
      }),
    ];
    renderList();

    // The mocked t returns keys, so the badge text is the chat.agent key.
    expect(screen.getAllByText("chat.agent")).toHaveLength(1);
  });
});

describe("ConversationList presence badge", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    mock.channels = [];
    mock.agents = [];
    mock.presences = {};
  });

  it("badges an online agent DM peer and not an offline one", () => {
    mock.agents = [
      { name: "agents/online-agent", status: { state: 1 } }, // ONLINE
      { name: "agents/offline-agent", status: { state: 2 } }, // OFFLINE
      { name: "agents/stopped-agent", status: { state: 5 } }, // STOPPED
    ];
    mock.channels = [
      channel({
        name: "conversations/ch1",
        title: "Online Agent",
        type: 1,
        peer: "agents/online-agent",
      }),
      channel({
        name: "conversations/ch2",
        title: "Offline Agent",
        type: 1,
        peer: "agents/offline-agent",
      }),
      channel({
        name: "conversations/ch3",
        title: "Stopped Agent",
        type: 1,
        peer: "agents/stopped-agent",
      }),
    ];
    renderList();

    // Exactly one green dot: the ONLINE agent's row.
    expect(screen.getAllByTestId("presence-badge")).toHaveLength(1);
  });

  it("badges an online human DM peer from the presence map", () => {
    mock.presences = {
      "users/alice": { online: true },
      "users/bob": { online: false },
    };
    mock.channels = [
      channel({
        name: "conversations/ch1",
        title: "Alice",
        type: 4,
        peer: "users/alice",
      }),
      channel({
        name: "conversations/ch2",
        title: "Bob",
        type: 4,
        peer: "users/bob",
      }),
    ];
    renderList();

    expect(screen.getAllByTestId("presence-badge")).toHaveLength(1);
  });

  it("never badges channel rows", () => {
    mock.presences = { "users/alice": { online: true } };
    mock.channels = [
      channel({ name: "conversations/ch1", title: "Design", type: 2 }),
    ];
    renderList();

    expect(screen.queryByTestId("presence-badge")).not.toBeInTheDocument();
  });
});
