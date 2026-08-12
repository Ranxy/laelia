import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { type Agent, AgentSchema } from "@/types/proto-es/v1/agent_pb";
import {
  type McpServer,
  McpServerSchema,
  McpServerScope,
} from "@/types/proto-es/v1/mcp_pb";
import { AgentMcpPage } from "./agent-mcp";

const mock = vi.hoisted(() => ({
  getAgent: vi.fn(),
  fetchMcpServers: vi.fn(),
  updateAgentMcpConfig: vi.fn(),
  mcpServers: [] as McpServer[],
}));

vi.mock("@/stores", () => {
  const state = {
    getAgent: mock.getAgent,
    get mcpServers() {
      return mock.mcpServers;
    },
    fetchMcpServers: mock.fetchMcpServers,
    updateAgentMcpConfig: mock.updateAgentMcpConfig,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

type AgentOverrides = Omit<Partial<Agent>, "$typeName" | "$unknown">;

function agent(overrides?: AgentOverrides): Agent {
  return create(AgentSchema, {
    name: "agents/a1",
    title: "Agent A",
    canEdit: true,
    mcpServers: ["mcp/workspace-1"],
    ...overrides,
  });
}

function mcpServer(
  name: string,
  title: string,
  scope: McpServerScope
): McpServer {
  return create(McpServerSchema, {
    name,
    title,
    scope,
    transport: { case: "http", value: { url: `http://${name}` } },
  });
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/members/agents/a1/mcp"]}>
      <Routes>
        <Route path="/members/agents/:agentId/mcp" element={<AgentMcpPage />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.getAgent.mockReset();
  mock.fetchMcpServers.mockReset();
  mock.updateAgentMcpConfig.mockReset();
  toastMock.add.mockReset();
  mock.mcpServers = [
    mcpServer("mcp/workspace-1", "Workspace Server", McpServerScope.WORKSPACE),
    mcpServer("mcp/workspace-2", "Second Server", McpServerScope.WORKSPACE),
    mcpServer("mcp/my-1", "My Server", McpServerScope.USER),
  ];
});

describe("agent-mcp", () => {
  it("shows loading while the agent is being fetched", () => {
    mock.getAgent.mockReturnValue(new Promise(() => {}));

    renderPage();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
  });

  it("shows the load-failed message when the agent cannot be fetched", async () => {
    mock.getAgent.mockResolvedValue(undefined);

    renderPage();

    expect(
      await screen.findByText("agent.profile.load-failed")
    ).toBeInTheDocument();
  });

  it("renders workspace and personal MCP servers with the agent's selection", async () => {
    mock.getAgent.mockResolvedValue(agent());

    renderPage();

    expect(
      await screen.findByText("agent.mcp-workspace-section")
    ).toBeInTheDocument();
    expect(screen.getByText("agent.mcp-my-section")).toBeInTheDocument();
    // The selection-seeding effect runs after the agent render; wait for it.
    await waitFor(() =>
      expect(
        screen.getByRole("checkbox", { name: /Workspace Server/ })
      ).toBeChecked()
    );
    expect(
      screen.getByRole("checkbox", { name: /Second Server/ })
    ).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: /My Server/ })
    ).not.toBeChecked();
  });

  it("toggles a server on and saves the selection", async () => {
    mock.getAgent.mockResolvedValue(agent());
    mock.updateAgentMcpConfig.mockResolvedValue(agent());

    renderPage();
    const ws2 = await screen.findByRole("checkbox", { name: /Second Server/ });
    fireEvent.click(ws2);
    expect(ws2).toBeChecked();

    fireEvent.click(screen.getByRole("button", { name: "agent.mcp-save" }));

    await waitFor(() =>
      expect(mock.updateAgentMcpConfig).toHaveBeenCalledWith("agents/a1", [
        "mcp/workspace-1",
        "mcp/workspace-2",
      ])
    );
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success", title: "agent.mcp-saved" })
    );
  });

  it("toasts an error when saving fails", async () => {
    mock.getAgent.mockResolvedValue(agent());
    mock.updateAgentMcpConfig.mockRejectedValue(new Error("mcp down"));

    renderPage();
    await screen.findByRole("checkbox", { name: /Workspace Server/ });
    fireEvent.click(screen.getByRole("button", { name: "agent.mcp-save" }));

    await waitFor(() =>
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "agent.mcp-save-failed",
          description: "mcp down",
        })
      )
    );
  });

  it("disables checkboxes and hides the save button for non-editors", async () => {
    mock.getAgent.mockResolvedValue(agent({ canEdit: false }));

    renderPage();

    const ws1 = await screen.findByRole("checkbox", {
      name: /Workspace Server/,
    });
    expect(ws1).toBeDisabled();
    expect(
      screen.queryByRole("button", { name: "agent.mcp-save" })
    ).not.toBeInTheDocument();
  });
});
