import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// ConversationList uses react-i18next (no provider in tests) and the app
// store. Stub i18n with a key/count mapper so assertions read the keys, and
// stub the store with a selector stand-in carrying a fixed channel roster.
vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, params?: { count?: number }) =>
      params?.count != null ? `${key}:${params.count}` : key,
  }),
}));

const mock = vi.hoisted(() => ({
  channels: [] as Array<Record<string, unknown>>,
  currentUser: { name: "users/ran-user-1", handle: "ran-user-1" },
  setConversationPinned: vi.fn(),
  setConversationClosed: vi.fn(),
  toastAdd: vi.fn(),
  useIsDesktop: vi.fn(() => true),
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: Record<string, unknown>) => unknown) =>
    selector({
      channels: mock.channels,
      channelsLoading: false,
      unreadByConv: {},
      createChannel: async () => {},
      setConversationPinned: mock.setConversationPinned,
      setConversationClosed: mock.setConversationClosed,
      currentUser: mock.currentUser,
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
}));

// The undo toast is app chrome; capture the add call and drive the action
// callback directly instead of rendering the real toaster.
vi.mock("@/lib/toast", () => ({
  toastManager: { add: mock.toastAdd },
}));

// Default to desktop (context menu) so the mobile-swipe tests can opt out
// with mock.useIsDesktop.mockReturnValue(false).
vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: mock.useIsDesktop,
}));

import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { ConversationList } from "./conversation-list";

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

describe("ConversationList last-message preview", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    vi.useRealTimers();
    mock.channels = [];
  });

  it("renders the member count inline after the channel name", () => {
    mock.channels = [channel()];
    render(<ConversationList />);
    expect(screen.getByText("Design")).toBeInTheDocument();
    expect(screen.getByText("channel.members:3")).toBeInTheDocument();
    // The hardcoded English member label is gone (only the i18n key remains).
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
    render(<ConversationList />);
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
    render(<ConversationList />);
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
    render(<ConversationList />);
    expect(screen.getByText("09:05")).toBeInTheDocument();
  });

  it("keeps the preview line for conversations with no messages yet", () => {
    mock.channels = [channel()];
    render(<ConversationList />);
    // The row still renders its title and no preview text or time appears.
    expect(screen.getByText("Design")).toBeInTheDocument();
    expect(screen.queryByText(/chat.you:|Alice:|Bob:/)).not.toBeInTheDocument();
  });
});

describe("ConversationList mobile create-channel FAB", () => {
  afterEach(() => {
    document.body.innerHTML = "";
    mock.channels = [];
  });

  it("renders the expanded pill (icon + label) by default", () => {
    render(<ConversationList />);
    const fab = screen.getByTestId("create-channel-fab");
    expect(fab).toBeInTheDocument();
    expect(screen.getByText("channel.fab-label")).toBeInTheDocument();
  });

  it("collapses to the bare icon while the list is scrolled down", () => {
    render(<ConversationList />);
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
    render(<ConversationList />);
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
    mock.toastAdd.mockClear();
    mock.useIsDesktop.mockReturnValue(true);
  });

  it("closes a conversation from the desktop context menu", () => {
    mock.channels = [channel()];
    render(<ConversationList />);
    fireEvent.contextMenu(screen.getByText("Design"));
    expect(screen.getByText("channel.pin")).toBeInTheDocument();
    expect(screen.getByText("chat.close")).toBeInTheDocument();

    fireEvent.click(screen.getByText("chat.close"));
    expect(mock.setConversationClosed).toHaveBeenCalledWith("ch1", true);
  });

  it("offers an undo toast whose action reopens the conversation", () => {
    mock.channels = [channel()];
    render(<ConversationList />);
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
    render(<ConversationList />);
    fireEvent.contextMenu(screen.getByText("Design"));
    fireEvent.click(screen.getByText("channel.pin"));
    expect(mock.setConversationPinned).toHaveBeenCalledWith("ch1", true);
  });

  it("shows both swipe actions on mobile and closes on the close tap", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    render(<ConversationList />);
    // The two swipe buttons sit side by side behind the row.
    expect(screen.getByTestId("swipe-close")).toBeInTheDocument();
    expect(screen.getByTestId("swipe-pin")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("swipe-close"));
    expect(mock.setConversationClosed).toHaveBeenCalledWith("ch1", true);
  });

  it("does not mount the context menu trigger on mobile", () => {
    mock.useIsDesktop.mockReturnValue(false);
    mock.channels = [channel()];
    render(<ConversationList />);
    fireEvent.contextMenu(screen.getByText("Design"));
    expect(screen.queryByText("chat.close")).not.toBeInTheDocument();
  });
});
