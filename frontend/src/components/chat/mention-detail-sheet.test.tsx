import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mock = vi.hoisted(() => ({
  navigate: vi.fn(),
  getOrCreateConversation: vi.fn(),
  fetchChannels: vi.fn(),
  toastAdd: vi.fn(),
  agent: {
    name: "agents/alpha",
    title: "Alpha Agent",
    handle: "alpha",
    status: { state: 1 },
  },
  user: {
    name: "users/1",
    title: "Alice Lee",
    handle: "alice-user-1",
    email: "alice@example.com",
  },
}));

vi.mock("react-router-dom", () => ({
  useNavigate: () => mock.navigate,
}));

vi.mock("@/connect", () => ({
  agentServiceClient: {
    async getAgent() {
      return mock.agent;
    },
  },
  userServiceClient: {
    async getUser() {
      return mock.user;
    },
  },
}));

vi.mock("@/stores", () => ({
  useAppStore: (selector: (s: unknown) => unknown) =>
    selector({
      getOrCreateConversation: mock.getOrCreateConversation,
      fetchChannels: mock.fetchChannels,
    }),
}));

vi.mock("@/lib/toast", () => ({
  toastManager: { add: mock.toastAdd },
}));

vi.mock("@/components/connection-badge", () => ({
  ConnectionBadge: () => <span data-testid="connection-badge" />,
}));

// The real Sheet portals into a layer root; render a plain div so the test
// focuses on the sheet's content and actions.
vi.mock("@/components/ui/sheet", () => ({
  Sheet: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SheetBody: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SheetContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SheetDescription: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SheetHeader: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SheetTitle: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

import { MentionDetailSheet } from "./mention-detail-sheet";

describe("MentionDetailSheet actions", () => {
  afterEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
  });

  it("shows send-message and view-details actions for agent mentions", async () => {
    render(
      <MentionDetailSheet
        open
        type="agent"
        id="alpha"
        name="Alpha"
        onClose={vi.fn()}
      />
    );

    expect(await screen.findByText("chat.send-message")).toBeTruthy();
    expect(screen.getByText("chat.view-details")).toBeTruthy();
  });

  it("hides actions for user mentions", async () => {
    render(
      <MentionDetailSheet
        open
        type="user"
        id="1"
        name="Alice"
        onClose={vi.fn()}
      />
    );

    // The sheet shows the display title (header + Name row), never the handle.
    expect((await screen.findAllByText("Alice Lee")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("@alice-user-1").length).toBeGreaterThan(0);
    expect(screen.queryByText("chat.send-message")).toBeNull();
    expect(screen.queryByText("chat.view-details")).toBeNull();
  });

  it("opens the agent DM and navigates on send message", async () => {
    mock.getOrCreateConversation.mockResolvedValue("conversations/c42");
    mock.fetchChannels.mockResolvedValue(undefined);
    const onClose = vi.fn();
    render(
      <MentionDetailSheet
        open
        type="agent"
        id="alpha"
        name="Alpha"
        onClose={onClose}
      />
    );

    fireEvent.click(await screen.findByText("chat.send-message"));

    await waitFor(() => {
      expect(mock.getOrCreateConversation).toHaveBeenCalledWith("agents/alpha");
      expect(mock.fetchChannels).toHaveBeenCalled();
      expect(onClose).toHaveBeenCalled();
      expect(mock.navigate).toHaveBeenCalledWith("/c42");
    });
  });

  it("navigates to the agent detail page on view details", async () => {
    const onClose = vi.fn();
    render(
      <MentionDetailSheet
        open
        type="agent"
        id="alpha"
        name="Alpha"
        onClose={onClose}
      />
    );

    fireEvent.click(await screen.findByText("chat.view-details"));

    expect(onClose).toHaveBeenCalled();
    expect(mock.navigate).toHaveBeenCalledWith("/members/agents/alpha");
  });

  it("shows a toast when opening the conversation fails", async () => {
    mock.getOrCreateConversation.mockRejectedValue(new Error("boom"));
    render(
      <MentionDetailSheet
        open
        type="agent"
        id="alpha"
        name="Alpha"
        onClose={vi.fn()}
      />
    );

    fireEvent.click(await screen.findByText("chat.send-message"));

    await waitFor(() => {
      expect(mock.toastAdd).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "chat.open-conversation-failed",
        })
      );
    });
  });
});
