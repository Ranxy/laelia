import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  type Agent,
  type AgentACPConfig,
  type AgentInfo,
  type AgentStatus,
  AgentStatus_ConnectionState,
  type PiModel,
} from "@/types/proto-es/v1/agent_pb";
import type { Machine } from "@/types/proto-es/v1/machine_pb";
import { AgentProfilePage } from "./agent-profile";

const mock = vi.hoisted(() => ({
  getSetting: vi.fn(),
  getAgent: vi.fn(),
  getMachine: vi.fn(),
  fetchAgents: vi.fn(),
  fetchUsers: vi.fn(),
  updateAgent: vi.fn(),
  updateAgentACPConfig: vi.fn(),
  listPiModels: vi.fn(),
  transferAgentOwnership: vi.fn(),
  uploadAgentAvatar: vi.fn(),
  deleteAgentAvatar: vi.fn(),
  avatarChange: vi.fn(),
  avatarRemove: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: { getSetting: mock.getSetting },
}));

vi.mock("@/lib/avatar-cache", () => ({
  useAvatar: () => "avatar-url",
  uploadAgentAvatar: mock.uploadAgentAvatar,
  deleteAgentAvatar: mock.deleteAgentAvatar,
}));

vi.mock("@/composables/useAvatarEditor", () => ({
  useAvatarEditor: () => ({
    busy: false,
    onChange: mock.avatarChange,
    onRemove: mock.avatarRemove,
  }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/components/chat/avatar", () => ({
  Avatar: () => <div data-testid="avatar" />,
}));

vi.mock("@/components/connection-badge", () => ({
  ConnectionBadge: () => <span data-testid="conn" />,
}));

const tFn = (key: string, params?: Record<string, string | number>) => {
  if (!params) return key;
  const values = Object.values(params);
  return values.length > 0 ? `${key}:${values.join(":")}` : key;
};
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function acpConfig(overrides?: Record<string, unknown>): AgentACPConfig {
  return {
    executable: "",
    args: [],
    allowEnv: [],
    provider: "custom",
    model: "",
    customEnv: {},
    personaPrompt: "You are helpful",
    protocol: "acp-v2",
    apiProvider: "",
    apiKey: "",
    globalProvider: "",
    globalProviderEntry: "",
    ...overrides,
  } as unknown as AgentACPConfig;
}

function agent(overrides?: Partial<Agent>): Agent {
  return {
    name: "agents/a1",
    handle: "a1",
    state: 0,
    title: "Alpha",
    info: {
      agentType: "acp",
      hostname: "h",
      os: "linux",
      arch: "x64",
      ip: "1.2.3.4",
      version: "1.0",
      acpConfig: acpConfig(),
    },
    status: {
      state: AgentStatus_ConnectionState.ONLINE,
      lastHeartbeatTime: undefined,
      connectedTime: undefined,
      errorMessage: "",
    },
    createdAt: undefined,
    labels: {},
    lastTokenRotatedAt: undefined,
    tokenVersion: 0,
    createdBy: "users/1",
    canEdit: true,
    avatar: "",
    machine: "machines/m1",
    allowAddToChannel: false,
    owner: "users/1",
    ownerName: "Alice",
    followOwnerPermissions: true,
    mcpServers: [],
    canManageChannelMembers: true,
    machineTitle: "Mac Mini",
    ...overrides,
  } as unknown as Agent;
}

function machine(overrides?: Partial<Machine>): Machine {
  return {
    name: "machines/m1",
    title: "Mac Mini",
    info: {
      availableProviders: [
        {
          providerId: "opencode",
          displayName: "OpenCode",
          version: "1.0",
          executablePath: "",
          supportsModelConfigOption: true,
          models: [{ value: "gpt-4o", name: "GPT-4o" }],
        },
      ],
    },
    ...overrides,
  } as unknown as Machine;
}

function seedStore(overrides?: {
  users?: { name: string; title: string }[];
  apiProviders?: {
    name: string;
    title: string;
    entries: { name: string; model: string; label?: string }[];
  }[];
  currentUser?: { name: string; title: string; permissions: string[] };
}) {
  useAppStore.setState({
    users: overrides?.users ?? [{ name: "users/1", title: "Alice" }],
    apiProviders: overrides?.apiProviders ?? [],
    currentUser: overrides?.currentUser ?? {
      name: "users/1",
      title: "Alice",
      permissions: ["laelia.agents.edit"],
    },
    getAgent: mock.getAgent,
    getMachine: mock.getMachine,
    fetchAgents: mock.fetchAgents,
    fetchUsers: mock.fetchUsers,
    updateAgent: mock.updateAgent,
    updateAgentACPConfig: mock.updateAgentACPConfig,
    listPiModels: mock.listPiModels,
    transferAgentOwnership: mock.transferAgentOwnership,
  } as never);
}

function renderPage(agentId = "a1") {
  const router = createMemoryRouter(
    [
      {
        path: "/agents",
        element: <Outlet />,
        children: [{ path: ":agentId", element: <AgentProfilePage /> }],
      },
      { path: "/machines/:machineId", element: <div>machine-route</div> },
    ],
    { initialEntries: [`/agents/${agentId}`] }
  );
  return render(<RouterProvider router={router} />);
}

// Select helper: open a Base UI select trigger and pick the item with the
// given text (pointer sequence required by Base UI).
async function pickSelect(trigger: HTMLElement, itemText: string) {
  fireEvent.click(trigger);
  const item = await screen.findByText(itemText);
  fireEvent.pointerDown(item);
  fireEvent.pointerUp(item);
  fireEvent.click(item);
}

beforeEach(() => {
  seedStore();
  mock.getSetting.mockReset();
  mock.getAgent.mockReset();
  mock.getMachine.mockReset();
  mock.fetchAgents.mockReset();
  mock.fetchUsers.mockReset();
  mock.updateAgent.mockReset();
  mock.updateAgentACPConfig.mockReset();
  mock.listPiModels.mockReset();
  mock.transferAgentOwnership.mockReset();
  mock.uploadAgentAvatar.mockReset();
  mock.deleteAgentAvatar.mockReset();
  mock.avatarChange.mockReset();
  mock.avatarRemove.mockReset();
  mock.getSetting.mockResolvedValue({
    value: {
      value: { allowUserSelfProvidedKeys: true },
      case: "llmAgentConfig",
    },
  });
  mock.getAgent.mockResolvedValue(agent());
  mock.getMachine.mockResolvedValue(machine());
  mock.fetchAgents.mockResolvedValue(undefined);
  mock.fetchUsers.mockResolvedValue(undefined);
  mock.updateAgent.mockResolvedValue(undefined);
  mock.updateAgentACPConfig.mockResolvedValue(undefined);
  mock.listPiModels.mockResolvedValue([
    { id: "deepseek-chat", name: "DeepSeek Chat" },
  ] as PiModel[]);
  mock.transferAgentOwnership.mockResolvedValue(undefined);
  toastMock.add.mockReset();
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("AgentProfilePage", () => {
  it("shows the loading state then the load-failed alert with retry", async () => {
    mock.getAgent.mockResolvedValueOnce(undefined);
    renderPage();

    expect(
      await screen.findByText("agent.profile.load-failed")
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "common.retry" }));
    await waitFor(() => {
      expect(mock.getAgent).toHaveBeenCalledTimes(2);
    });
    expect(await screen.findByText("Alpha")).toBeInTheDocument();
  });

  it("renders the identity grid with machine link, owner and creator", async () => {
    renderPage();

    expect(await screen.findByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("agent.detail-name")).toBeInTheDocument();
    // The handle rides along with the name (like the human profile), never as
    // a separate labeled field.
    expect(screen.getByText("@a1")).toBeInTheDocument();
    expect(screen.queryByText("agent.detail-handle")).toBeNull();
    expect(screen.getByText("agent.detail-status")).toBeInTheDocument();
    expect(screen.getByText("agent.lifecycle.ready")).toBeInTheDocument();
    // Machine link navigates to the machine profile.
    fireEvent.click(screen.getByText("Mac Mini"));
    expect(await screen.findByText("machine-route")).toBeInTheDocument();
  });

  it("shows the edit-not-allowed alert when the caller cannot edit", async () => {
    mock.getAgent.mockResolvedValue(agent({ canEdit: false }));
    renderPage();

    expect(
      await screen.findByText("agent.profile.edit-not-allowed")
    ).toBeInTheDocument();
  });

  it("shows the waiting-connection hint for an unconfigured offline agent", async () => {
    mock.getAgent.mockResolvedValue(
      agent({
        status: {
          state: AgentStatus_ConnectionState.OFFLINE,
          lastHeartbeatTime: undefined,
          connectedTime: undefined,
          errorMessage: "",
        } as unknown as AgentStatus,
        info: {
          ...agent().info,
          acpConfig: acpConfig({ provider: "" }),
        } as unknown as AgentInfo,
      })
    );
    renderPage();

    expect(
      await screen.findByText("agent.waiting-connection-hint")
    ).toBeInTheDocument();
  });

  it("edits and saves the persona prompt", async () => {
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.edit"));
    const textarea = screen.getByPlaceholderText(
      "agent.profile.persona-prompt-placeholder"
    );
    fireEvent.change(textarea, { target: { value: "Be concise" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateAgentACPConfig).toHaveBeenCalledWith(
        "agents/a1",
        expect.objectContaining({
          personaPrompt: "Be concise",
          provider: "custom",
          protocol: "acp-v2",
        })
      );
    });
    expect(await screen.findByText("Be concise")).toBeInTheDocument();
  });

  it("cancels the persona edit and restores the persisted prompt", async () => {
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.edit"));
    fireEvent.change(
      screen.getByPlaceholderText("agent.profile.persona-prompt-placeholder"),
      { target: { value: "Draft" } }
    );
    fireEvent.click(screen.getByRole("button", { name: "common.cancel" }));

    expect(mock.updateAgentACPConfig).not.toHaveBeenCalled();
    expect(screen.getByText("You are helpful")).toBeInTheDocument();
  });

  it("shows the persona-empty placeholder when no prompt is set", async () => {
    mock.getAgent.mockResolvedValue(
      agent({
        info: {
          ...agent().info,
          acpConfig: acpConfig({ personaPrompt: "" }),
        } as unknown as AgentInfo,
      })
    );
    renderPage();

    expect(
      await screen.findByText("agent.profile.persona-empty")
    ).toBeInTheDocument();
  });

  it("uploads and removes the avatar", async () => {
    mock.getAgent.mockResolvedValue(agent({ avatar: "agents/a1/avatar" }));
    renderPage();

    const uploadButton = await screen.findByRole("button", {
      name: "agent.profile.avatar-upload",
    });
    fireEvent.click(uploadButton);
    const input = document.querySelector(
      'input[type="file"]'
    ) as HTMLInputElement;
    const file = new File(["x"], "a.png", { type: "image/png" });
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() => {
      expect(mock.avatarChange).toHaveBeenCalledWith(file);
    });

    fireEvent.click(
      screen.getByRole("button", { name: "agent.profile.avatar-remove" })
    );
    await waitFor(() => {
      expect(mock.avatarRemove).toHaveBeenCalled();
    });
  });

  it("toggles allow-add-to-channel and refetches the agent", async () => {
    renderPage();

    const switches = await screen.findAllByRole("switch");
    fireEvent.click(switches[0]);

    await waitFor(() => {
      expect(mock.updateAgent).toHaveBeenCalledWith("agents/a1", {
        allowAddToChannel: true,
      });
    });
    expect(mock.getAgent).toHaveBeenCalled();
    expect(mock.fetchAgents).toHaveBeenCalled();
  });

  it("toggles follow-owner-permissions", async () => {
    renderPage();

    const switches = await screen.findAllByRole("switch");
    fireEvent.click(switches[1]);

    await waitFor(() => {
      expect(mock.updateAgent).toHaveBeenCalledWith("agents/a1", {
        followOwnerPermissions: false,
      });
    });
  });

  it("toggles can-manage-channel-members", async () => {
    renderPage();

    const switches = await screen.findAllByRole("switch");
    fireEvent.click(switches[2]);

    await waitFor(() => {
      expect(mock.updateAgent).toHaveBeenCalledWith("agents/a1", {
        canManageChannelMembers: false,
      });
    });
  });

  it("shows an error toast when a toggle save fails", async () => {
    mock.updateAgent.mockRejectedValue(new Error("boom"));
    renderPage();

    const switches = await screen.findAllByRole("switch");
    fireEvent.click(switches[0]);

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          description: "boom",
        })
      );
    });
  });

  it("switches to the custom provider and saves executable + protocol on blur", async () => {
    renderPage();

    const providerTrigger = (await screen.findAllByRole("combobox"))[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-custom");

    const executable = await screen.findByPlaceholderText(
      "agent.acp-config-executable-placeholder"
    );
    fireEvent.change(executable, { target: { value: "npx" } });
    fireEvent.blur(executable);

    await waitFor(() => {
      expect(mock.updateAgentACPConfig).toHaveBeenCalledWith(
        "agents/a1",
        expect.objectContaining({
          provider: "custom",
          executable: "npx",
          protocol: "acp-v2",
        })
      );
    });
  });

  it("saves args and allow-env via the string list editors", async () => {
    mock.getAgent.mockResolvedValue(
      agent({
        info: {
          ...agent().info,
          acpConfig: acpConfig({ executable: "npx" }),
        } as unknown as AgentInfo,
      })
    );
    renderPage();

    // Wait for the custom-provider block (rendered after the editor is seeded).
    await screen.findByPlaceholderText(
      "agent.acp-config-executable-placeholder"
    );
    const addButtons = await screen.findAllByRole("button", {
      name: "common.add",
    });
    // First add button belongs to the args editor.
    fireEvent.click(addButtons[0]);
    const argInput = await screen.findByPlaceholderText(
      "agent.acp-config-args-placeholder"
    );
    fireEvent.change(argInput, { target: { value: "--verbose" } });
    fireEvent.blur(argInput);

    await waitFor(() => {
      expect(mock.updateAgentACPConfig).toHaveBeenCalledWith(
        "agents/a1",
        expect.objectContaining({ args: ["--verbose"] })
      );
    });
  });

  it("saves custom env entries via the key-value editor", async () => {
    mock.getAgent.mockResolvedValue(
      agent({
        info: {
          ...agent().info,
          acpConfig: acpConfig({ executable: "npx" }),
        } as unknown as AgentInfo,
      })
    );
    renderPage();

    await screen.findByPlaceholderText(
      "agent.acp-config-executable-placeholder"
    );
    const addButtons = await screen.findAllByRole("button", {
      name: "common.add",
    });
    // Second add button belongs to the custom-env editor.
    fireEvent.click(addButtons[1]);
    const keyInput = await screen.findByPlaceholderText(
      "agent.acp-config-custom-env-key-placeholder"
    );
    fireEvent.change(keyInput, { target: { value: "FOO" } });
    fireEvent.blur(keyInput);

    await waitFor(() => {
      expect(mock.updateAgentACPConfig).toHaveBeenCalledWith(
        "agents/a1",
        expect.objectContaining({ customEnv: { FOO: "" } })
      );
    });
  });

  it("switches to builtin-pi and loads models from the provider", async () => {
    renderPage();

    const providerTrigger = (await screen.findAllByRole("combobox"))[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-builtin-pi");

    // Pick the API provider.
    const comboboxes = screen.getAllByRole("combobox");
    const apiProviderSelect = comboboxes[comboboxes.length - 1];
    await pickSelect(apiProviderSelect, "deepseek");

    // Type the API key and blur: blur persists the key and fetches models.
    const keyInput = await screen.findByPlaceholderText(
      "agent.acp-config-pi-api-key-placeholder"
    );
    fireEvent.change(keyInput, { target: { value: "sk-123" } });
    fireEvent.blur(keyInput);
    await waitFor(() => {
      expect(mock.listPiModels).toHaveBeenCalledWith("deepseek", "sk-123");
    });

    // Type into the model combobox, then blur the api key to trigger a save
    // (the model is persisted by the next saveConfig trigger).
    const modelInput = screen.getByPlaceholderText(
      "agent.acp-config-pi-model-placeholder"
    );
    fireEvent.change(modelInput, { target: { value: "deepseek-chat" } });
    fireEvent.blur(keyInput);

    await waitFor(() => {
      expect(mock.updateAgentACPConfig).toHaveBeenCalledWith(
        "agents/a1",
        expect.objectContaining({
          provider: "builtin-pi",
          apiProvider: "deepseek",
          model: "deepseek-chat",
        })
      );
    });
  });

  it("shows the save-error status when a config save fails", async () => {
    mock.updateAgentACPConfig.mockRejectedValue(new Error("boom"));
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.edit"));
    fireEvent.change(
      screen.getByPlaceholderText("agent.profile.persona-prompt-placeholder"),
      { target: { value: "New persona" } }
    );
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    expect(
      await screen.findByText("agent.acp-config-save-error")
    ).toBeInTheDocument();
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "error" })
    );
  });

  it("selects a machine provider model", async () => {
    renderPage();

    const providerTrigger = (await screen.findAllByRole("combobox"))[0];
    await pickSelect(providerTrigger, "OpenCode (1.0)");

    const comboboxes = screen.getAllByRole("combobox");
    const modelSelect = comboboxes[comboboxes.length - 1];
    await pickSelect(modelSelect, "GPT-4o");

    await waitFor(() => {
      expect(mock.updateAgentACPConfig).toHaveBeenCalledWith(
        "agents/a1",
        expect.objectContaining({ provider: "opencode", model: "gpt-4o" })
      );
    });
  });

  it("transfers ownership through the two-step dialog", async () => {
    seedStore({
      users: [
        { name: "users/1", title: "Alice" },
        { name: "users/2", title: "Bob" },
      ],
    });
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "agent.transfer-owner" })
    );

    // Pick the target in the first dialog.
    const comboboxes = screen.getAllByRole("combobox");
    const targetCombobox = comboboxes[comboboxes.length - 1];
    await pickSelect(targetCombobox, "Bob");

    fireEvent.click(screen.getByRole("button", { name: "common.next" }));

    // Confirm in the second dialog.
    fireEvent.click(
      await screen.findByRole("button", {
        name: "agent.transfer-owner-confirm",
      })
    );

    await waitFor(() => {
      expect(mock.transferAgentOwnership).toHaveBeenCalledWith(
        "agents/a1",
        "users/2",
        ""
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("shows a transfer error when ownership transfer fails", async () => {
    mock.transferAgentOwnership.mockRejectedValue(new Error("nope"));
    seedStore({
      users: [
        { name: "users/1", title: "Alice" },
        { name: "users/2", title: "Bob" },
      ],
    });
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "agent.transfer-owner" })
    );
    const comboboxes = screen.getAllByRole("combobox");
    await pickSelect(comboboxes[comboboxes.length - 1], "Bob");
    fireEvent.click(screen.getByRole("button", { name: "common.next" }));
    fireEvent.click(
      await screen.findByRole("button", {
        name: "agent.transfer-owner-confirm",
      })
    );

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({ type: "error", description: "nope" })
      );
    });
  });
});
