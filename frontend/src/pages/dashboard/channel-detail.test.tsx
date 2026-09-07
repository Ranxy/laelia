import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type {
  ChannelMember,
  Conversation,
} from "@/types/proto-es/v1/command_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { ChannelDetailPage } from "./channel-detail";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

// useAvatar would hit the avatar RPCs; stub it so rows render the pixel
// fallback without network noise.
vi.mock("@/lib/avatar-cache", () => ({
  useAvatar: () => null,
}));

const mock = vi.hoisted(() => ({
  getChannel: vi.fn(),
}));

vi.mock("@/connect", () => ({
  commandServiceClient: {
    getChannel: mock.getChannel,
  },
}));

const CONV: Conversation = {
  name: "conversations/c1",
  title: "Design",
  type: 2,
  memberCount: 3,
  ownerId: "1",
  ownerName: "Alice",
  closed: true,
  joinedAt: { seconds: 0n },
} as Conversation;

const MEMBERS: ChannelMember[] = [
  {
    memberType: 1,
    memberId: "1",
    displayName: "Alice",
    memberRole: 1,
    joinedAt: { seconds: 0n },
  },
  {
    memberType: 2,
    memberId: "agents/a",
    displayName: "Helper",
    memberRole: 3,
    joinedAt: { seconds: 0n },
  },
  {
    memberType: 1,
    memberId: "2",
    displayName: "Bob",
    memberRole: 2,
    joinedAt: { seconds: 0n },
  },
] as ChannelMember[];

function seedStore() {
  useAppStore.setState({
    currentUser: { name: "users/1" } as User,
    myChannels: [CONV],
    setConversationClosed: vi.fn(async () => undefined),
    listChannelMembers: vi.fn(async () => []),
    addChannelMember: vi.fn(async () => []),
    addChannelGroup: vi.fn(async () => []),
    removeChannelMember: vi.fn(async () => undefined),
    channelMembersByConv: { "conversations/c1": MEMBERS },
    channelMembersLoading: {},
  });
}

function renderPage() {
  // The page renders conversation rows / member panels that read the presence
  // map through usePresenceMap (Query cache) — provide a provider, seeded empty.
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const router = createMemoryRouter(
    [
      {
        path: "/members/channels/:channelId",
        element: <ChannelDetailPage />,
      },
      // Chat routes live at the root: "/" is the list, "/:conversationId" is
      // the conversation itself (see router/routes/dashboard.tsx).
      { path: "/:conversationId", element: <div>chat-route</div> },
    ],
    { initialEntries: ["/members/channels/c1"] }
  );
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

beforeEach(() => {
  seedStore();
  mock.getChannel.mockResolvedValue(CONV);
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("ChannelDetailPage", () => {
  it("shows channel metadata and the Message action", async () => {
    renderPage();

    expect(await screen.findByText("Design")).toBeTruthy();
    expect(screen.getByText("members.message-channel")).toBeTruthy();
    expect(screen.queryByText("channel.closed")).toBeNull();
    // The metadata card renders once the GetChannel read settles (the roster
    // entry already carries the same fields — wait for the loading gate).
    expect(await screen.findByText("channel.owner")).toBeTruthy();
    expect(screen.getAllByText("channel.joined-at").length).toBeGreaterThan(0);
  });

  it("lists members with role badges", async () => {
    renderPage();

    expect(await screen.findByText("Alice")).toBeTruthy();
    expect(screen.getByText("channel.role-owner")).toBeTruthy();
    expect(screen.getByText("channel.role-admin")).toBeTruthy();
    expect(screen.getByText("channel.role-member")).toBeTruthy();
    // Member rows are compact (no join dates); only the channel metadata
    // card still shows the channel's own joined-at.
    expect(screen.getAllByText("channel.joined-at")).toHaveLength(1);
  });

  it("shows the Message action for a live channel too", async () => {
    mock.getChannel.mockResolvedValue({ ...CONV, closed: false });
    renderPage();

    expect(await screen.findByText("Design")).toBeTruthy();
    expect(screen.getByText("members.message-channel")).toBeTruthy();
  });

  it("reopens a closed channel and navigates to chat on Message", async () => {
    renderPage();
    await screen.findByText("Design");

    fireEvent.click(screen.getByText("members.message-channel"));

    expect(useAppStore.getState().setConversationClosed).toHaveBeenCalledWith(
      "c1",
      false
    );
    expect(await screen.findByText("chat-route")).toBeTruthy();
  });

  it("navigates straight to chat for a live channel on Message", async () => {
    mock.getChannel.mockResolvedValue({ ...CONV, closed: false });
    renderPage();
    await screen.findByText("Design");
    // Wait for the GetChannel read: the roster entry is closed:true, and the
    // straight-navigation branch needs the fetched live channel to win.
    await waitFor(() =>
      expect(screen.queryByText("common.loading")).toBeNull()
    );

    fireEvent.click(screen.getByText("members.message-channel"));

    expect(useAppStore.getState().setConversationClosed).not.toHaveBeenCalled();
    expect(await screen.findByText("chat-route")).toBeTruthy();
  });
});
