import { render, screen } from "@testing-library/react";
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
// gated behind canManage), stub the clients so module import succeeds.
vi.mock("@/connect", () => ({
  groupServiceClient: {},
  userServiceClient: {},
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

describe("ChannelMembersPanel presence badge", () => {
  it("badges only the online members' avatars, like the chat list", () => {
    render(
      <ChannelMembersPanel
        conversationId="c1"
        canManage={false}
        membershipFixed={true}
      />
    );

    // Alice (online human) + the online agent carry a dot; Bob (offline
    // human) and the offline agent stay plain.
    expect(screen.getAllByTestId("presence-badge")).toHaveLength(2);
  });
});
