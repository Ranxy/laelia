import { fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type { MemberSummary } from "@/stores/types";
import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { MembersPage } from "./members";

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

vi.mock("@/components/connection-badge", () => ({
  ConnectionBadge: () => <span data-testid="conn" />,
}));

function seedStore() {
  useAppStore.setState({
    members: [
      {
        kind: "agent",
        name: "agents/alpha",
        title: "Alpha Agent",
        subtitle: "machines/m1",
        connectionState: 2,
      },
      {
        kind: "agent",
        name: "agents/beta",
        title: "Beta Agent",
        subtitle: "machines/m1",
        connectionState: 1,
      },
      { kind: "user", name: "users/1", title: "Alice", subtitle: "" },
      { kind: "user", name: "users/2", title: "Bob", subtitle: "" },
    ] as MemberSummary[],
    membersLoading: false,
    membersError: false,
    fetchMembers: vi.fn(async () => undefined),
    machines: [],
    machinesLoading: false,
    fetchMachines: vi.fn(async () => undefined),
    myChannels: [
      { name: "conversations/c1", title: "Design", type: 2, closed: false },
      { name: "conversations/c2", title: "Retired", type: 2, closed: true },
    ] as Conversation[],
    myChannelsLoading: false,
    fetchMyChannels: vi.fn(async () => undefined),
  });
}

function renderPage() {
  const router = createMemoryRouter(
    [
      {
        path: "/members",
        element: <MembersPage />,
        children: [
          { path: "agents/:agentId/*", element: <div /> },
          { path: "users/:userId/*", element: <div /> },
          {
            path: "channels/:channelId",
            element: <div>channel-detail-route</div>,
          },
        ],
      },
    ],
    { initialEntries: ["/members"] }
  );
  return render(<RouterProvider router={router} />);
}

function searchInput() {
  return screen.getByPlaceholderText("members.search-placeholder");
}

beforeEach(() => {
  seedStore();
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("MembersPage search", () => {
  it("shows the full roster without a query", () => {
    renderPage();

    expect(screen.getByText("Alpha Agent")).toBeTruthy();
    expect(screen.getByText("Beta Agent")).toBeTruthy();
    expect(screen.getByText("Alice")).toBeTruthy();
    expect(screen.getByText("Bob")).toBeTruthy();
  });

  it("filters agents and humans by title, case-insensitively", () => {
    renderPage();

    fireEvent.change(searchInput(), { target: { value: "alice" } });

    expect(screen.getByText("Alice")).toBeTruthy();
    expect(screen.queryByText("Alpha Agent")).toBeNull();
    expect(screen.queryByText("Beta Agent")).toBeNull();
    expect(screen.queryByText("Bob")).toBeNull();
  });

  it("matches the member id as well as the title", () => {
    renderPage();

    fireEvent.change(searchInput(), { target: { value: "beta" } });

    expect(screen.getByText("Beta Agent")).toBeTruthy();
    expect(screen.queryByText("Alpha Agent")).toBeNull();
    expect(screen.queryByText("Alice")).toBeNull();
  });

  it("hides sections with no matches while searching", () => {
    renderPage();

    fireEvent.change(searchInput(), { target: { value: "alice" } });

    expect(screen.getByText("Alice")).toBeTruthy();
    expect(screen.queryByText("members.section-agents")).toBeNull();
    expect(screen.getByText("members.section-humans")).toBeTruthy();
  });

  it("shows a no-results message when nothing matches", () => {
    renderPage();

    fireEvent.change(searchInput(), { target: { value: "zzz" } });

    expect(screen.getByText("members.no-search-results")).toBeTruthy();
    expect(screen.queryByText("Alpha Agent")).toBeNull();
    expect(screen.queryByText("members.section-agents")).toBeNull();
    expect(screen.queryByText("members.section-humans")).toBeNull();
  });

  it("restores the full roster when the query is cleared", () => {
    renderPage();

    fireEvent.change(searchInput(), { target: { value: "alice" } });
    fireEvent.change(searchInput(), { target: { value: "" } });

    expect(screen.getByText("Alpha Agent")).toBeTruthy();
    expect(screen.getByText("Beta Agent")).toBeTruthy();
    expect(screen.getByText("Alice")).toBeTruthy();
    expect(screen.getByText("Bob")).toBeTruthy();
  });
});

describe("MembersPage channels roster", () => {
  it("starts collapsed: rows are hidden until the section is toggled", () => {
    renderPage();

    expect(screen.getByText("members.section-channels")).toBeTruthy();
    expect(screen.queryByText("Design")).toBeNull();
    expect(screen.queryByText("Retired")).toBeNull();

    fireEvent.click(screen.getByText("members.section-channels"));

    expect(screen.getByText("Design")).toBeTruthy();
    expect(screen.getByText("Retired")).toBeTruthy();
  });

  it("badges closed channels in the roster", () => {
    renderPage();
    fireEvent.click(screen.getByText("members.section-channels"));

    expect(screen.getByText("channel.closed")).toBeTruthy();
    // only one of the two channels is closed
    expect(screen.getAllByText("channel.closed")).toHaveLength(1);
  });

  it("navigates to the channel detail on row click", () => {
    renderPage();
    fireEvent.click(screen.getByText("members.section-channels"));
    fireEvent.click(screen.getByText("Design"));

    expect(screen.getByText("channel-detail-route")).toBeTruthy();
  });
});
