import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { type McpServer, McpServerScope } from "@/types/proto-es/v1/mcp_pb";
import { SettingsMcpServersPage } from "./settings-mcp-servers";

const mock = vi.hoisted(() => ({
  getSetting: vi.fn(),
  listMyMcpServers: vi.fn(),
  listMcpServers: vi.fn(),
  listUserMcpServers: vi.fn(),
  createMcpServer: vi.fn(),
  updateMcpServer: vi.fn(),
  deleteMcpServer: vi.fn(),
  listUsers: vi.fn(),
  listGroups: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: { getSetting: mock.getSetting },
  mcpServerServiceClient: {
    listMyMcpServers: mock.listMyMcpServers,
    listMcpServers: mock.listMcpServers,
    listUserMcpServers: mock.listUserMcpServers,
    createMcpServer: mock.createMcpServer,
    updateMcpServer: mock.updateMcpServer,
    deleteMcpServer: mock.deleteMcpServer,
  },
  userServiceClient: { listUsers: mock.listUsers },
  groupServiceClient: { listGroups: mock.listGroups },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/lib/connect-errors", () => ({
  describeError: (err: unknown) =>
    err instanceof Error ? err.message : String(err),
}));

function server(overrides?: Partial<McpServer>): McpServer {
  return {
    name: "mcpServers/1",
    title: "Files",
    description: "File access",
    transport: {
      case: "http",
      value: { url: "https://mcp.example.com", headers: [] },
    },
    members: [],
    scope: McpServerScope.WORKSPACE,
    createdBy: "users/1",
    ...overrides,
  } as unknown as McpServer;
}

function renderPage() {
  return render(<SettingsMcpServersPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.mcpServers.list",
        "laelia.mcpServers.create",
        "laelia.mcpServers.update",
      ],
    } as never,
  });
  mock.getSetting.mockReset();
  mock.listMyMcpServers.mockReset();
  mock.listMcpServers.mockReset();
  mock.listUserMcpServers.mockReset();
  mock.createMcpServer.mockReset();
  mock.updateMcpServer.mockReset();
  mock.deleteMcpServer.mockReset();
  mock.listUsers.mockReset();
  mock.listGroups.mockReset();
  mock.getSetting.mockResolvedValue({ value: { value: { case: undefined } } });
  mock.listMyMcpServers.mockResolvedValue({ mcpServers: [] });
  mock.listMcpServers.mockResolvedValue({ mcpServers: [] });
  mock.listUserMcpServers.mockResolvedValue({ mcpServers: [] });
  mock.listUsers.mockResolvedValue({ users: [] });
  mock.listGroups.mockResolvedValue({ groups: [] });
  mock.createMcpServer.mockResolvedValue({});
  mock.updateMcpServer.mockResolvedValue({});
  mock.deleteMcpServer.mockResolvedValue({});
  toastMock.add.mockReset();
});

describe("settings-mcp-servers", () => {
  it("renders the workspace tab with servers for admins", async () => {
    mock.listMcpServers.mockResolvedValue({ mcpServers: [server()] });

    renderPage();

    expect(await screen.findByText("Files")).toBeInTheDocument();
    expect(screen.getByText("HTTP")).toBeInTheDocument();
    expect(screen.getByText("https://mcp.example.com")).toBeInTheDocument();
  });

  it("shows only the my tab for non-admins", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });
    mock.listMyMcpServers.mockResolvedValue({
      mcpServers: [server({ scope: McpServerScope.USER })],
    });

    renderPage();

    expect(await screen.findByText("Files")).toBeInTheDocument();
    expect(
      screen.queryByText("settings.mcp-servers.tab-workspace")
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("settings.mcp-servers.tab-users")
    ).not.toBeInTheDocument();
  });

  it("creates a workspace server", async () => {
    renderPage();

    fireEvent.click(await screen.findByText("settings.mcp-servers.create"));

    const title = await screen.findByPlaceholderText(
      "settings.mcp-servers.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "Files" } });
    const url = screen.getByPlaceholderText(
      "settings.mcp-servers.field-url-placeholder"
    );
    fireEvent.change(url, { target: { value: "https://mcp.example.com" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.createMcpServer).toHaveBeenCalledWith(
        expect.objectContaining({
          mcpServer: expect.objectContaining({
            title: "Files",
            scope: McpServerScope.WORKSPACE,
            transport: expect.objectContaining({ case: "http" }),
          }),
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("requires a title and url when creating", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("settings.mcp-servers.create"));

    fireEvent.click(await screen.findByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "settings.mcp-servers.title-required",
        })
      );
    });
    expect(mock.createMcpServer).not.toHaveBeenCalled();
  });

  it("edits a server", async () => {
    mock.listMcpServers.mockResolvedValue({ mcpServers: [server()] });
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.edit"));

    const title = await screen.findByDisplayValue("Files");
    fireEvent.change(title, { target: { value: "Files Pro" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateMcpServer).toHaveBeenCalledWith(
        expect.objectContaining({
          mcpServer: expect.objectContaining({
            name: "mcpServers/1",
            title: "Files Pro",
          }),
          updateMask: { paths: ["title", "description", "http", "members"] },
        })
      );
    });
  });

  it("deletes a server after confirmation", async () => {
    mock.listMcpServers.mockResolvedValue({ mcpServers: [server()] });
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.delete"));

    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => {
      expect(mock.deleteMcpServer).toHaveBeenCalledWith({
        name: "mcpServers/1",
      });
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("switches to the users tab and filters by creator", async () => {
    mock.listUserMcpServers.mockResolvedValue({
      mcpServers: [
        server({
          name: "mcpServers/2",
          title: "Personal",
          scope: McpServerScope.USER,
          createdBy: "users/1",
        }),
      ],
    });
    mock.listUsers.mockResolvedValue({
      users: [
        {
          name: "users/1",
          title: "Alice",
          email: "alice@example.com",
        },
      ],
    });
    renderPage();

    fireEvent.click(await screen.findByText("settings.mcp-servers.tab-users"));

    expect(await screen.findByText("Personal")).toBeInTheDocument();
    expect(screen.getByText("alice@example.com")).toBeInTheDocument();

    const search = screen.getByPlaceholderText(
      "settings.mcp-servers.search-creator-placeholder"
    );
    fireEvent.change(search, { target: { value: "nobody" } });

    await waitFor(() => {
      expect(screen.queryByText("Personal")).not.toBeInTheDocument();
    });
  });
});
