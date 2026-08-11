import { render, screen } from "@testing-library/react";
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
  currentUser: { name: "users/42" },
  setConversationPinned: vi.fn(),
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: Record<string, unknown>) => unknown) =>
    selector({
      channels: mock.channels,
      channelsLoading: false,
      unreadByConv: {},
      createChannel: async () => {},
      setConversationPinned: mock.setConversationPinned,
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
        lastMessagePrincipalId: "42",
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
        lastMessagePrincipalId: "7",
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
