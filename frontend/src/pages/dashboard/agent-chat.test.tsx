import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Conversation } from "@/types/proto-es/v1/command_pb";
import { AgentChatPage } from "./agent-chat";

const mock = vi.hoisted(() => ({
  fetchChannelsForAgent: vi.fn(),
  channels: [] as Conversation[],
  loading: false,
}));

vi.mock("@/stores", () => {
  const state = {
    fetchChannelsForAgent: mock.fetchChannelsForAgent,
    get agentChannelsByAgent() {
      return { "agents/a1": mock.channels };
    },
    get agentChannelsLoading() {
      return mock.loading;
    },
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const tFn = (key: string, params?: { count?: number }) =>
  params?.count != null ? `${key}:${params.count}` : key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function conv(name: string, title: string, type: number): Conversation {
  return { name, title, type, memberCount: 3 } as unknown as Conversation;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/members/agents/a1/chat"]}>
      <Routes>
        <Route
          path="/members/agents/:agentId/chat"
          element={<AgentChatPage />}
        />
        <Route path="/:conversationId" element={<div data-testid="conv" />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.fetchChannelsForAgent.mockReset();
  mock.channels = [];
  mock.loading = false;
});

describe("agent-chat", () => {
  it("fetches the agent's channels on mount", async () => {
    renderPage();

    await waitFor(() =>
      expect(mock.fetchChannelsForAgent).toHaveBeenCalledWith("agents/a1")
    );
  });

  it("shows the loading hint while the first fetch is in flight", () => {
    mock.loading = true;

    renderPage();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
  });

  it("shows the empty hint when the agent has no conversations", async () => {
    renderPage();

    expect(await screen.findByText("agent.chat-empty")).toBeInTheDocument();
  });

  it("renders DM, channel and agent-DM rows with their icons and metadata", async () => {
    mock.channels = [
      conv("conversations/1", "DM with Alice", 1),
      conv("conversations/2", "General", 2),
      conv("conversations/3", "Agent DM", 3),
    ];

    renderPage();

    expect(await screen.findByText("DM with Alice")).toBeInTheDocument();
    expect(screen.getByText("General")).toBeInTheDocument();
    expect(screen.getByText("Agent DM")).toBeInTheDocument();
    // Channel rows show the member count; agent-DM rows show the label.
    expect(screen.getByText("channel.members:3")).toBeInTheDocument();
    expect(screen.getByText("agent.chat-agent-dm-row")).toBeInTheDocument();
  });

  it("navigates to the conversation when a row is clicked", async () => {
    mock.channels = [conv("conversations/7", "General", 2)];

    renderPage();
    fireEvent.click(await screen.findByText("General"));

    expect(screen.getByTestId("conv")).toBeInTheDocument();
  });
});
