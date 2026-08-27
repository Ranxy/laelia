import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ChannelMember } from "@/types/proto-es/v1/command_pb";
import { ChannelMembersPanel } from "./channel-members-panel";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

// useAvatar would hit the avatar RPCs; stub it so rows render the pixel
// fallback without network noise.
vi.mock("@/lib/avatar-cache", () => ({
  avatarNameForAgentId: (id: string) => `agents/${id}/avatar`,
  avatarNameForUserId: (id: string) => `users/${id}/avatar`,
  useAvatar: () => null,
}));

// Group/user pickers hit @/connect; they stay inert here (add-member UI is
// gated behind canManage), stub the clients so module import succeeds. The
// member detail sheet also fetches the clicked user/agent through these.
vi.mock("@/connect", () => ({
  groupServiceClient: {},
  userServiceClient: {
    getUser: vi.fn(async () => ({
      name: "users/alice",
      title: "Alice",
      handle: "alice",
      email: "alice@laelia.test",
    })),
    batchGetUsers: vi.fn(async () => ({ users: [] })),
    listUsers: vi.fn(async () => ({ users: [] })),
  },
  agentServiceClient: {
    getAgent: vi.fn(async () => ({
      name: "agents/online-agent",
      title: "Online Agent",
      handle: "online-agent",
    })),
  },
}));

import { useAppStore } from "@/stores";

const roster: ChannelMember[] = [
  {
    memberType: 1,
    memberId: "alice",
    displayName: "Alice",
    memberRole: 1,
  },
  {
    memberType: 1,
    memberId: "bob",
    displayName: "Bob",
    memberRole: 2,
  },
  {
    memberType: 2,
    memberId: "online-agent",
    displayName: "Online Agent",
    memberRole: 2,
  },
  {
    memberType: 2,
    memberId: "offline-agent",
    displayName: "Offline Agent",
    memberRole: 2,
  },
] as unknown as ChannelMember[];

function seedStore(overrides: Record<string, unknown> = {}) {
  useAppStore.setState({
    channelMembersByConv: { "conversations/c1": roster },
    channelMembersLoading: {},
    // The panel refetches on mount; stub the action so the test renders
    // against the seeded roster.
    listChannelMembers: vi.fn(async () => roster),
    agents: [
      { name: "agents/online-agent", status: { state: 1 } }, // ONLINE
      { name: "agents/offline-agent", status: { state: 2 } }, // OFFLINE
    ] as never,
    onlineUsers: { "users/alice": true, "users/bob": false },
    ...overrides,
  });
}

beforeEach(() => {
  seedStore();
});

afterEach(() => {
  document.body.innerHTML = "";
});

// The panel renders the member detail Sheet, which uses useNavigate.
function renderPanel(
  props?: Partial<Parameters<typeof ChannelMembersPanel>[0]>
) {
  return render(
    <MemoryRouter>
      <ChannelMembersPanel
        conversationId="c1"
        canManage={false}
        membershipFixed={true}
        {...props}
      />
    </MemoryRouter>
  );
}

describe("ChannelMembersPanel presence badge", () => {
  it("badges only the online members' avatars, like the chat list", () => {
    renderPanel();

    // Alice (online human) + the online agent carry a dot; Bob (offline
    // human) and the offline agent stay plain.
    expect(screen.getAllByTestId("presence-badge")).toHaveLength(2);
  });

  it("shows the directory connection badge on agent rows only", () => {
    renderPanel();

    // The online and offline agents render the same text badge as the
    // members directory; human rows carry no connection badge.
    expect(screen.getAllByText("agent.status-online")).toHaveLength(1);
    expect(screen.getAllByText("agent.status-offline")).toHaveLength(1);
  });
});

describe("ChannelMembersPanel remove column", () => {
  it("renders remove buttons for non-owner rows and reserves the owner slot", () => {
    renderPanel({ canManage: true, membershipFixed: false });

    // Bob + the two agents are removable; the owner's own row renders an
    // empty placeholder so its badges stay aligned with the others.
    expect(screen.getAllByLabelText("common.delete")).toHaveLength(3);
  });
});

describe("ChannelMembersPanel member detail", () => {
  it("opens the user detail sheet when a human row is clicked", async () => {
    renderPanel();

    // Row buttons carry the member name plus the role badge as their
    // accessible name; match on the name part.
    fireEvent.click(screen.getByRole("button", { name: /Alice/ }));

    // The detail sheet is the same popup as clicking a sender avatar in chat.
    expect(await screen.findByText("User Details")).toBeTruthy();
  });

  it("opens the agent detail sheet when an agent row is clicked", async () => {
    renderPanel();

    fireEvent.click(screen.getByRole("button", { name: /Online Agent/ }));

    expect(await screen.findByText("Agent Details")).toBeTruthy();
  });
});
