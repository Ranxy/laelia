import { Code, ConnectError } from "@connectrpc/connect";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  AgentStatus_ConnectionState,
  type AgentSummary,
  type PiModel,
} from "@/types/proto-es/v1/agent_pb";
import {
  type Machine,
  type MachineInfo,
  type MachineStatus,
  MachineStatus_ConnectionState,
} from "@/types/proto-es/v1/machine_pb";
import { MachineProfilePage } from "./machine-profile";

const mock = vi.hoisted(() => ({
  getSetting: vi.fn(),
  getMachine: vi.fn(),
  fetchMachines: vi.fn(),
  listMachineAgents: vi.fn(),
  fetchApiProviders: vi.fn(),
  fetchUsers: vi.fn(),
  listPiModels: vi.fn(),
  refreshMachineProviders: vi.fn(),
  revokeMachineToken: vi.fn(),
  forceDisconnectMachine: vi.fn(),
  createAgent: vi.fn(),
  getMachineIamPolicy: vi.fn(),
  setMachineIamPolicy: vi.fn(),
  listGroups: vi.fn(),
  transferMachineOwnership: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: { getSetting: mock.getSetting },
  iamServiceClient: {
    getMachineIamPolicy: mock.getMachineIamPolicy,
    setMachineIamPolicy: mock.setMachineIamPolicy,
  },
  groupServiceClient: { listGroups: mock.listGroups },
}));

vi.mock("@/lib/use-is-desktop", () => ({
  useIsDesktop: () => true,
}));

vi.mock("@/lib/machine-token", () => ({
  buildMachineInstallCommand: (os: string) => `INSTALL-${os}`,
  buildMachineSetupCommand: () => "SETUP-CMD",
  machineInstallOSFromInfo: (os: string | undefined) =>
    os === "windows" ? "windows" : os === "darwin" ? "macos" : "linux",
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

function machine(overrides?: Partial<Machine>): Machine {
  return {
    name: "machines/m1",
    state: 0,
    title: "Mac Mini",
    info: {
      hostname: "mac.local",
      os: "darwin",
      arch: "arm64",
      ip: "10.0.0.5",
      version: "2.1.0",
      labels: {},
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
    status: {
      state: MachineStatus_ConnectionState.ONLINE,
      lastHeartbeatTime: undefined,
      connectedTime: undefined,
      errorMessage: "",
      activeSessionId: "",
    },
    createdAt: undefined,
    labels: {},
    createdBy: "users/1",
    canEdit: true,
    canCreateAgent: true,
    canManage: true,
    ...overrides,
  } as unknown as Machine;
}

function agentSummary(overrides?: Partial<AgentSummary>): AgentSummary {
  return {
    name: "agents/a1",
    state: 0,
    title: "Beta",
    status: {
      state: AgentStatus_ConnectionState.ONLINE,
      lastHeartbeatTime: undefined,
      connectedTime: undefined,
      errorMessage: "",
    },
    provider: "custom",
    executable: "npx",
    machine: "machines/m1",
    createdBy: "users/1",
    allowAddToChannel: false,
    owner: "users/1",
    followOwnerPermissions: true,
    canManageChannelMembers: true,
    ...overrides,
  } as unknown as AgentSummary;
}

function seedStore(overrides?: {
  users?: { name: string; title: string; email?: string }[];
  groups?: {
    name: string;
    title: string;
    email?: string;
    members?: string[];
  }[];
  apiProviders?: {
    name: string;
    title: string;
    entries: { name: string; model: string; label?: string }[];
  }[];
}) {
  useAppStore.setState({
    users: overrides?.users ?? [{ name: "users/1", title: "Alice" }],
    apiProviders: overrides?.apiProviders ?? [],
    getMachine: mock.getMachine,
    fetchMachines: mock.fetchMachines,
    listMachineAgents: mock.listMachineAgents,
    fetchApiProviders: mock.fetchApiProviders,
    fetchUsers: mock.fetchUsers,
    listPiModels: mock.listPiModels,
    refreshMachineProviders: mock.refreshMachineProviders,
    revokeMachineToken: mock.revokeMachineToken,
    forceDisconnectMachine: mock.forceDisconnectMachine,
    transferMachineOwnership: mock.transferMachineOwnership,
    createAgent: mock.createAgent,
  } as never);
}

function renderPage(machineId = "m1") {
  const router = createMemoryRouter(
    [
      {
        path: "/machines",
        element: <Outlet />,
        children: [{ path: ":machineId", element: <MachineProfilePage /> }],
      },
      { path: "/members/agents/:agentId", element: <div>agent-route</div> },
      { path: "/members/users/:userId", element: <div>user-route</div> },
    ],
    { initialEntries: [`/machines/${machineId}`] }
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

async function openAddSheet() {
  fireEvent.click(
    (await screen.findAllByRole("button", { name: "machine.add-agent" }))[0]
  );
  await screen.findByText("machine.add-agent-title");
}

beforeEach(() => {
  seedStore();
  mock.getSetting.mockReset();
  mock.getMachine.mockReset();
  mock.fetchMachines.mockReset();
  mock.listMachineAgents.mockReset();
  mock.fetchApiProviders.mockReset();
  mock.fetchUsers.mockReset();
  mock.listPiModels.mockReset();
  mock.refreshMachineProviders.mockReset();
  mock.revokeMachineToken.mockReset();
  mock.forceDisconnectMachine.mockReset();
  mock.createAgent.mockReset();
  mock.getMachineIamPolicy.mockReset();
  mock.setMachineIamPolicy.mockReset();
  mock.listGroups.mockReset();
  mock.transferMachineOwnership.mockReset();
  mock.getSetting.mockResolvedValue({
    value: {
      value: { allowUserSelfProvidedKeys: true },
      case: "llmAgentConfig",
    },
  });
  mock.getMachine.mockResolvedValue(machine());
  mock.fetchMachines.mockResolvedValue(undefined);
  mock.listMachineAgents.mockResolvedValue([]);
  mock.fetchApiProviders.mockResolvedValue(undefined);
  mock.fetchUsers.mockResolvedValue(undefined);
  mock.listPiModels.mockResolvedValue([
    { id: "deepseek-chat", name: "DeepSeek Chat" },
  ] as PiModel[]);
  mock.refreshMachineProviders.mockResolvedValue([]);
  mock.revokeMachineToken.mockResolvedValue(undefined);
  mock.forceDisconnectMachine.mockResolvedValue(undefined);
  mock.createAgent.mockResolvedValue(undefined);
  mock.getMachineIamPolicy.mockResolvedValue({
    policy: { bindings: [] },
    etag: "etag-1",
  });
  mock.setMachineIamPolicy.mockResolvedValue({
    policy: { bindings: [] },
    etag: "etag-2",
  });
  mock.listGroups.mockResolvedValue({ groups: [] });
  mock.transferMachineOwnership.mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  });
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("MachineProfilePage", () => {
  it("shows the load-failed alert with retry when the machine cannot be fetched", async () => {
    mock.getMachine.mockResolvedValueOnce(undefined);
    renderPage();

    expect(
      await screen.findByText("machine.profile.load-failed")
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "common.retry" }));
    await waitFor(() => {
      expect(mock.getMachine).toHaveBeenCalledTimes(2);
    });
    expect(await screen.findByText("machine.detail-name")).toBeInTheDocument();
  });

  it("renders the identity grid with host info and connection state", async () => {
    renderPage();

    expect(await screen.findByText("Mac Mini")).toBeInTheDocument();
    expect(screen.getByText("machine.detail-name")).toBeInTheDocument();
    expect(screen.getByText("machine.detail-status")).toBeInTheDocument();
    expect(screen.getByText("machine.status-online")).toBeInTheDocument();
    expect(screen.getByText("mac.local")).toBeInTheDocument();
    expect(screen.getByText("darwin/arm64")).toBeInTheDocument();
    expect(screen.getByText("10.0.0.5")).toBeInTheDocument();
    expect(screen.getByText("2.1.0")).toBeInTheDocument();
  });

  it("shows the machine owner and navigates to the user detail page", async () => {
    renderPage();

    expect(await screen.findByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("machine.detail-owner")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Alice" }));

    expect(await screen.findByText("user-route")).toBeInTheDocument();
  });

  it("shows offline reconnection commands in the token card", async () => {
    mock.getMachine.mockResolvedValue(
      machine({
        status: {
          state: MachineStatus_ConnectionState.OFFLINE,
          lastHeartbeatTime: undefined,
          connectedTime: undefined,
          errorMessage: "",
          activeSessionId: "",
        } as unknown as MachineStatus,
      })
    );
    renderPage();

    expect(
      await screen.findByText("machine.profile.offline-install-note")
    ).toBeInTheDocument();
    expect(
      screen.getByText("machine.profile.offline-install-hint")
    ).toBeInTheDocument();
    expect(screen.getByText("INSTALL-macos")).toBeInTheDocument();
    expect(
      screen.getByText("machine.profile.offline-command-hint")
    ).toBeInTheDocument();
    expect(screen.getByText("SETUP-CMD")).toBeInTheDocument();
  });

  it("uses the Windows install command for an offline Windows machine", async () => {
    mock.getMachine.mockResolvedValue(
      machine({
        info: {
          ...machine().info,
          os: "windows",
        } as unknown as MachineInfo,
        status: {
          state: MachineStatus_ConnectionState.OFFLINE,
          lastHeartbeatTime: undefined,
          connectedTime: undefined,
          errorMessage: "",
          activeSessionId: "",
        } as unknown as MachineStatus,
      })
    );
    renderPage();

    expect(
      await screen.findByText("machine.profile.offline-install-note")
    ).toBeInTheDocument();
    expect(screen.getByText("INSTALL-windows")).toBeInTheDocument();
  });

  it("does not show offline reconnection commands while the machine is online", async () => {
    renderPage();

    expect(await screen.findByText("Mac Mini")).toBeInTheDocument();
    expect(
      screen.queryByText("machine.profile.offline-install-note")
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("machine.profile.offline-command-hint")
    ).not.toBeInTheDocument();
  });

  it("shows the edit-not-allowed alert when the caller has no capability", async () => {
    mock.getMachine.mockResolvedValue(
      machine({ canEdit: false, canCreateAgent: false, canManage: false })
    );
    renderPage();

    expect(
      (await screen.findAllByText("machine.profile.edit-not-allowed")).length
    ).toBeGreaterThan(0);
    // Token controls are hidden for non-managers.
    expect(
      screen.queryByRole("button", { name: "machine.revoke-token" })
    ).not.toBeInTheDocument();
  });

  it("revokes the machine token after confirmation", async () => {
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "machine.revoke-token" })
    );
    fireEvent.click(
      screen.getByRole("button", { name: "machine.revoke-token" })
    );

    await waitFor(() => {
      expect(mock.revokeMachineToken).toHaveBeenCalledWith("machines/m1");
    });
  });

  it("transfers ownership after the two-step confirm", async () => {
    seedStore({
      users: [
        { name: "users/1", title: "Alice" },
        { name: "users/2", title: "Bob" },
      ],
    });
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "machine.transfer-owner" })
    );
    await screen.findByText("machine.transfer-owner-title");

    // Pick the target user (users/2) via the select.
    const trigger = screen.getByRole("combobox");
    fireEvent.click(trigger);
    const item = await screen.findByText("Bob");
    fireEvent.pointerDown(item);
    fireEvent.pointerUp(item);
    fireEvent.click(item);

    fireEvent.click(screen.getByRole("button", { name: "common.next" }));
    fireEvent.click(
      screen.getByRole("button", { name: "machine.transfer-owner-confirm" })
    );

    await waitFor(() => {
      expect(mock.transferMachineOwnership).toHaveBeenCalledWith(
        "machines/m1",
        "users/2",
        ""
      );
    });
  });

  it("force-disconnects the machine after confirmation", async () => {
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "machine.force-disconnect" })
    );
    fireEvent.click(
      screen.getByRole("button", { name: "machine.force-disconnect" })
    );

    await waitFor(() => {
      expect(mock.forceDisconnectMachine).toHaveBeenCalledWith("machines/m1");
    });
  });

  it("lists available providers and refreshes them", async () => {
    renderPage();

    expect(await screen.findByText("OpenCode (1.0)")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "machine.refresh-providers" })
    );
    await waitFor(() => {
      expect(mock.refreshMachineProviders).toHaveBeenCalledWith("machines/m1");
    });
  });

  it("shows the no-providers hint and the refresh error", async () => {
    mock.getMachine.mockResolvedValue(
      machine({
        info: {
          ...machine().info,
          availableProviders: [],
        } as unknown as MachineInfo,
      })
    );
    mock.refreshMachineProviders.mockRejectedValue(new Error("boom"));
    renderPage();

    expect(await screen.findByText("machine.no-providers")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "machine.refresh-providers" })
    );
    expect(await screen.findByText("boom")).toBeInTheDocument();
  });

  it("renders the agent roster and navigates to the agent profile", async () => {
    mock.listMachineAgents.mockResolvedValue([agentSummary()]);
    renderPage();

    expect(await screen.findByText("Beta")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Beta"));
    expect(await screen.findByText("agent-route")).toBeInTheDocument();
  });

  it("shows the empty roster hint", async () => {
    renderPage();

    expect(await screen.findByText("machine.no-agents")).toBeInTheDocument();
  });

  it("shows the access card members and opens the manage sheet", async () => {
    mock.getMachineIamPolicy.mockResolvedValue({
      policy: {
        bindings: [{ role: "roles/machineAgentCreator", members: ["users/1"] }],
      },
      etag: "etag-1",
    });
    renderPage();

    expect((await screen.findAllByText("Alice")).length).toBeGreaterThan(0);

    fireEvent.click(
      screen.getByRole("button", { name: "machine.access-manage" })
    );
    expect(
      await screen.findByText("machine.access-manage-title")
    ).toBeInTheDocument();
    // The current member is listed in the sheet with a remove button.
    expect(
      await screen.findByRole("button", {
        name: "machine.access-remove-member:Alice",
      })
    ).toBeInTheDocument();
  });

  it("shows the no-members hint when the policy has no agent creators", async () => {
    renderPage();

    expect(
      await screen.findByText("machine.access-no-members")
    ).toBeInTheDocument();
  });

  it("adds a member and saves the access policy", async () => {
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "machine.access-manage" })
    );
    await screen.findByText("machine.access-manage-title");

    // Pick Alice from the member picker.
    fireEvent.click(screen.getAllByText("Alice").at(-1)!);
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.setMachineIamPolicy).toHaveBeenCalledWith(
        expect.objectContaining({
          name: "machines/m1",
          etag: "etag-1",
          policy: expect.objectContaining({
            bindings: expect.arrayContaining([
              expect.objectContaining({
                role: "roles/machineAgentCreator",
                members: ["users/1"],
              }),
            ]),
          }),
        })
      );
    });
  });

  it("reports an etag mismatch and reloads the policy", async () => {
    mock.setMachineIamPolicy.mockRejectedValue(
      new ConnectError("stale", Code.Aborted)
    );
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "machine.access-manage" })
    );
    await screen.findByText("machine.access-manage-title");
    fireEvent.click(screen.getAllByText("Alice").at(-1)!);
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.getMachineIamPolicy).toHaveBeenCalledTimes(2);
    });
  });

  it("creates a custom-provider agent from the add sheet", async () => {
    renderPage();
    await openAddSheet();

    fireEvent.change(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder"),
      {
        target: { value: "My Agent" },
      }
    );

    const providerTrigger = screen.getAllByRole("combobox")[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-custom");

    const executable = await screen.findByPlaceholderText(
      "agent.acp-config-executable-placeholder"
    );
    fireEvent.change(executable, { target: { value: "npx" } });

    // Args, custom env and allow-env editors each have a "common.add" button.
    const addButtons = screen.getAllByRole("button", { name: "common.add" });
    fireEvent.click(addButtons[0]);
    const argInput = await screen.findByPlaceholderText(
      "agent.acp-config-args-placeholder"
    );
    fireEvent.change(argInput, { target: { value: "--verbose" } });

    fireEvent.click(addButtons[1]);
    const envKey = await screen.findByPlaceholderText(
      "agent.acp-config-custom-env-key-placeholder"
    );
    fireEvent.change(envKey, { target: { value: "FOO" } });
    const envValue = screen.getByPlaceholderText(
      "agent.acp-config-custom-env-value-placeholder"
    );
    fireEvent.change(envValue, { target: { value: "bar" } });

    fireEvent.click(addButtons[2]);
    const allowEnv = await screen.findByPlaceholderText(
      "agent.acp-config-allow-env-placeholder"
    );
    fireEvent.change(allowEnv, { target: { value: "PATH" } });

    fireEvent.change(
      screen.getByPlaceholderText(
        "agent.acp-config-persona-prompt-placeholder"
      ),
      { target: { value: "Be concise" } }
    );

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    await waitFor(() => {
      expect(mock.createAgent).toHaveBeenCalledWith(
        "My Agent",
        "machines/m1",
        expect.objectContaining({
          provider: "custom",
          executable: "npx",
          args: ["--verbose"],
          customEnv: { FOO: "bar" },
          allowEnv: ["PATH"],
          personaPrompt: "Be concise",
        }),
        undefined,
        false,
        ""
      );
    });
    expect(
      await screen.findByText("machine.agent-created-title")
    ).toBeInTheDocument();
  });

  it("creates a builtin-pi agent with a managed global provider entry", async () => {
    seedStore({
      apiProviders: [
        {
          name: "providers/p1",
          title: "DeepSeek",
          entries: [{ name: "entries/e1", model: "deepseek-chat" }],
        },
      ],
    });
    renderPage();
    await openAddSheet();

    fireEvent.change(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder"),
      {
        target: { value: "Pi Agent" },
      }
    );

    const providerTrigger = screen.getAllByRole("combobox")[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-builtin-pi");

    // Global provider + entry selects appear (default pi mode is "global").
    const comboboxes = screen.getAllByRole("combobox");
    const globalProviderSelect = comboboxes[comboboxes.length - 1];
    await pickSelect(globalProviderSelect, "DeepSeek");

    const entrySelect = screen.getAllByRole("combobox").at(-1)!;
    await pickSelect(entrySelect, "deepseek-chat");

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    await waitFor(() => {
      expect(mock.createAgent).toHaveBeenCalledWith(
        "Pi Agent",
        "machines/m1",
        expect.objectContaining({
          provider: "builtin-pi",
          globalProvider: "providers/p1",
          globalProviderEntry: "entries/e1",
        }),
        undefined,
        false,
        ""
      );
    });
  });

  it("creates a builtin-pi agent with self-provided keys", async () => {
    renderPage();
    await openAddSheet();

    fireEvent.change(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder"),
      {
        target: { value: "Pi Self" },
      }
    );

    const providerTrigger = screen.getAllByRole("combobox")[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-builtin-pi");

    // Switch to self-provided mode (comboboxes: provider, pi-mode, global provider).
    const piModeSelect = screen.getAllByRole("combobox")[1];
    await pickSelect(piModeSelect, "agent.acp-config-pi-mode-self");

    const apiProviderSelect = screen.getAllByRole("combobox").at(-1)!;
    await pickSelect(apiProviderSelect, "deepseek");

    const keyInput = await screen.findByPlaceholderText(
      "agent.acp-config-pi-api-key-placeholder"
    );
    fireEvent.change(keyInput, { target: { value: "sk-123" } });

    // Refresh fetches the model list for the provider.
    fireEvent.click(
      screen.getByRole("button", { name: "agent.acp-config-pi-models-refresh" })
    );
    await waitFor(() => {
      expect(mock.listPiModels).toHaveBeenCalledWith("deepseek", "sk-123", "");
    });

    const modelInput = screen.getByPlaceholderText(
      "agent.acp-config-pi-model-placeholder"
    );
    fireEvent.change(modelInput, { target: { value: "deepseek-chat" } });

    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    await waitFor(() => {
      expect(mock.createAgent).toHaveBeenCalledWith(
        "Pi Self",
        "machines/m1",
        expect.objectContaining({
          provider: "builtin-pi",
          apiProvider: "deepseek",
          apiKey: "sk-123",
          model: "deepseek-chat",
        }),
        undefined,
        false,
        ""
      );
    });
  });

  it("keeps the create button disabled until the required fields are filled", async () => {
    renderPage();
    await openAddSheet();

    const createButton = screen.getByRole("button", { name: "common.create" });
    expect(createButton).toBeDisabled();

    fireEvent.change(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder"),
      {
        target: { value: "My Agent" },
      }
    );
    const providerTrigger = screen.getAllByRole("combobox")[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-custom");
    fireEvent.change(
      await screen.findByPlaceholderText(
        "agent.acp-config-executable-placeholder"
      ),
      { target: { value: "npx" } }
    );
    expect(createButton).toBeEnabled();
  });

  it("shows the create error inside the add sheet", async () => {
    mock.createAgent.mockRejectedValue(new Error("boom"));
    renderPage();
    await openAddSheet();

    fireEvent.change(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder"),
      {
        target: { value: "My Agent" },
      }
    );
    const providerTrigger = screen.getAllByRole("combobox")[0];
    await pickSelect(providerTrigger, "agent.acp-config-provider-custom");
    fireEvent.change(
      await screen.findByPlaceholderText(
        "agent.acp-config-executable-placeholder"
      ),
      { target: { value: "npx" } }
    );
    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    expect(await screen.findByText("boom")).toBeInTheDocument();
  });

  it("cancels the add sheet and reopens it blank", async () => {
    renderPage();
    await openAddSheet();

    fireEvent.change(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder"),
      {
        target: { value: "My Agent" },
      }
    );
    fireEvent.click(screen.getByRole("button", { name: "common.cancel" }));
    await waitFor(() => {
      expect(
        screen.queryByText("machine.add-agent-title")
      ).not.toBeInTheDocument();
    });

    await openAddSheet();
    expect(
      screen.getByPlaceholderText("machine.add-agent-name-placeholder")
    ).toHaveValue("");
  });

  it("renders the mobile add-agent FAB for creators", async () => {
    renderPage();

    const fab = await screen.findByTestId("add-agent-fab");
    fireEvent.click(fab);
    expect(
      await screen.findByText("machine.add-agent-title")
    ).toBeInTheDocument();
  });
});
