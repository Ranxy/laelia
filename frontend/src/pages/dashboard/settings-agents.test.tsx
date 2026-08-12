import { create } from "@bufbuild/protobuf";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  LlmAgentConfigSettingSchema,
  McpIpPolicy_Scope,
  McpIpPolicySchema,
  UserMcpConfigSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import {
  SettingSchema,
  SettingValueSchema,
  UpdateSettingRequest,
} from "@/types/proto-es/v1/setting_pb";
import { SettingsAgentsPage } from "./settings-agents";

const mock = vi.hoisted(() => ({
  getSetting: vi.fn(),
  updateSetting: vi.fn(),
}));

vi.mock("@/connect", () => ({
  settingServiceClient: {
    getSetting: mock.getSetting,
    updateSetting: mock.updateSetting,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

function llmResponse(allow: boolean) {
  return create(SettingSchema, {
    name: "settings/llm_agent_config",
    value: create(SettingValueSchema, {
      value: {
        case: "llmAgentConfig",
        value: create(LlmAgentConfigSettingSchema, {
          allowUserSelfProvidedKeys: allow,
        }),
      },
    }),
  });
}

function mcpResponse(allow: boolean, enabled: boolean) {
  return create(SettingSchema, {
    name: "settings/user_mcp_config",
    value: create(SettingValueSchema, {
      value: {
        case: "userMcpConfig",
        value: create(UserMcpConfigSettingSchema, {
          allowUserMcpServers: allow,
          mcpIpPolicy: create(McpIpPolicySchema, {
            enabled,
            scope: McpIpPolicy_Scope.USER_CREATED,
            allowCidrs: ["10.0.0.0/8"],
            denyCidrs: ["192.168.0.0/16"],
          }),
        }),
      },
    }),
  });
}

function renderPage() {
  return render(<SettingsAgentsPage />);
}

function rowSwitch(labelKey: string) {
  const heading = screen.getByText(labelKey);
  const row = heading.closest(
    ".flex.items-center.justify-between"
  ) as HTMLElement;
  return within(row).getByRole("switch");
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      permissions: ["laelia.settings.update"],
    } as never,
    llmAgentConfig: undefined,
    userMcpConfig: undefined,
  });
  mock.getSetting.mockReset();
  mock.updateSetting.mockReset();
  toastMock.add.mockReset();
});

describe("settings-agents", () => {
  it("loads and renders the stored toggles and policy", async () => {
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(true, true)
      )
    );

    renderPage();

    const llm = await waitFor(() =>
      rowSwitch("settings.agents.self-provided-keys")
    );
    expect(llm).toBeChecked();
    expect(rowSwitch("settings.agents.allow-user-mcp")).toBeChecked();
    expect(
      screen.getByText("settings.agents.ip-policy-title")
    ).toBeInTheDocument();
    expect(screen.getByText("10.0.0.0/8")).toBeInTheDocument();
    expect(screen.getByText("192.168.0.0/16")).toBeInTheDocument();
  });

  it("saves the LLM toggle with the llm update mask", async () => {
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(true, false)
      )
    );
    mock.updateSetting.mockResolvedValue(llmResponse(false));

    renderPage();
    const llm = await waitFor(() =>
      rowSwitch("settings.agents.self-provided-keys")
    );
    fireEvent.click(llm);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.llm_agent_config.allow_user_self_provided_keys",
    ]);
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("requires confirmation before enabling personal MCP servers", async () => {
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(false, false)
      )
    );

    renderPage();
    const mcp = await waitFor(() =>
      rowSwitch("settings.agents.allow-user-mcp")
    );
    fireEvent.click(mcp);

    expect(
      screen.getByText("settings.agents.allow-user-mcp-confirm-title")
    ).toBeInTheDocument();
    expect(mock.updateSetting).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", {
        name: "settings.agents.allow-user-mcp-confirm-action",
      })
    );

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.setting?.value?.value).toMatchObject({
      case: "userMcpConfig",
      value: { allowUserMcpServers: true },
    });
  });

  it("disables the MCP toggle immediately when turning it off", async () => {
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(true, false)
      )
    );
    mock.updateSetting.mockResolvedValue(mcpResponse(false, false));

    renderPage();
    const mcp = await waitFor(() =>
      rowSwitch("settings.agents.allow-user-mcp")
    );
    fireEvent.click(mcp);

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.setting?.value?.value).toMatchObject({
      case: "userMcpConfig",
      value: { allowUserMcpServers: false },
    });
  });

  it("saves the policy with the mcp update mask", async () => {
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(true, true)
      )
    );
    mock.updateSetting.mockResolvedValue(mcpResponse(true, true));

    renderPage();
    await waitFor(() => rowSwitch("settings.agents.self-provided-keys"));
    fireEvent.click(
      screen.getByRole("button", { name: "settings.agents.ip-policy-save" })
    );

    await waitFor(() => expect(mock.updateSetting).toHaveBeenCalledTimes(1));
    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.user_mcp_config.allow_user_mcp_servers",
      "value.user_mcp_config.mcp_ip_policy",
    ]);
  });

  it("appends the preset deny list without duplicating entries", async () => {
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(true, true)
      )
    );

    renderPage();
    await waitFor(() => rowSwitch("settings.agents.self-provided-keys"));
    fireEvent.click(
      screen.getByRole("button", { name: "settings.agents.ip-policy-preset" })
    );

    const deny = screen.getByPlaceholderText(
      "settings.agents.ip-policy-deny-placeholder"
    ) as HTMLTextAreaElement;
    expect(deny.value).toContain("192.168.0.0/16");
    expect(deny.value).toContain("10.0.0.0/8");
    expect(deny.value).toContain("0.0.0.0/8");
  });

  it("hides the policy editor from callers without update permission", async () => {
    useAppStore.setState({
      currentUser: {
        name: "users/2",
        permissions: [],
      } as never,
    });
    mock.getSetting.mockImplementation((req: { name: string }) =>
      Promise.resolve(
        req.name === "settings/llm_agent_config"
          ? llmResponse(true)
          : mcpResponse(true, false)
      )
    );

    renderPage();

    await waitFor(() => rowSwitch("settings.agents.self-provided-keys"));
    expect(
      screen.queryByText("settings.agents.ip-policy-title")
    ).not.toBeInTheDocument();
    expect(rowSwitch("settings.agents.self-provided-keys")).toHaveAttribute(
      "aria-disabled",
      "true"
    );
  });
});
