import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  S3ConfigSetting,
  WorkspaceProfileSetting,
} from "@/types/proto-es/store/setting_pb";
import {
  LlmAgentConfigSettingSchema,
  S3ConfigSettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import {
  SettingSchema,
  SettingValueSchema,
  type UpdateSettingRequest,
} from "@/types/proto-es/v1/setting_pb";
import { useAppStore } from "./index";

// --- mock @/connect so the setting slice talks to controllable RPCs ---
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

const PROFILE = create(WorkspaceProfileSettingSchema, {
  externalUrl: "https://example.com",
  disallowSignup: true,
  domains: ["example.com"],
  requireEmailVerification: true,
});

beforeEach(() => {
  useAppStore.setState({
    workspaceProfile: undefined,
    smtpConfig: undefined,
    s3Config: undefined,
    llmAgentConfig: undefined,
    userMcpConfig: undefined,
    passwordRestriction: undefined,
  });
  mock.getSetting.mockReset();
  mock.updateSetting.mockReset();
});

function settingResponse(name: string, value: unknown) {
  return create(SettingSchema, {
    name,
    value: create(SettingValueSchema, value as never),
  });
}

describe("fetch", () => {
  it("caches the workspace profile from getSetting", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse("settings/workspace_profile", {
        value: { case: "workspaceProfile", value: PROFILE },
      })
    );

    const got = await useAppStore.getState().fetchWorkspaceProfile();

    expect(mock.getSetting).toHaveBeenCalledWith({
      name: "settings/workspace_profile",
    });
    expect(got?.disallowSignup).toBe(true);
    expect(useAppStore.getState().workspaceProfile?.externalUrl).toBe(
      "https://example.com"
    );
  });
});

describe("updateWorkspaceProfile", () => {
  it("sends only the masked paths and keeps unmasked fields out of the payload merge", async () => {
    useAppStore.setState({ workspaceProfile: PROFILE });
    mock.updateSetting.mockResolvedValue(
      settingResponse("settings/workspace_profile", {
        value: {
          case: "workspaceProfile",
          value: create(WorkspaceProfileSettingSchema, {
            ...PROFILE,
            disallowSignup: false,
          }),
        },
      })
    );

    const got = await useAppStore
      .getState()
      .updateWorkspaceProfile({ disallowSignup: false }, [
        "value.workspace_profile.disallow_signup",
      ]);

    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual([
      "value.workspace_profile.disallow_signup",
    ]);
    const sent = req.setting?.value?.value?.value as
      | WorkspaceProfileSetting
      | undefined;
    expect(sent?.disallowSignup).toBe(false);
    // The base profile is merged client-side, but the mask limits what the
    // server writes; unrelated fields must still round-trip in the payload.
    expect(sent?.externalUrl).toBe("https://example.com");

    expect(got?.disallowSignup).toBe(false);
    expect(useAppStore.getState().workspaceProfile?.disallowSignup).toBe(false);
  });

  it("clears an optional field when the patch value is undefined", async () => {
    useAppStore.setState({ workspaceProfile: PROFILE });
    mock.updateSetting.mockResolvedValue(
      settingResponse("settings/workspace_profile", {
        value: {
          case: "workspaceProfile",
          value: create(WorkspaceProfileSettingSchema, {
            ...PROFILE,
            requireEmailVerification: undefined,
          }),
        },
      })
    );

    await useAppStore
      .getState()
      .updateWorkspaceProfile({ requireEmailVerification: undefined }, [
        "value.workspace_profile.require_email_verification",
      ]);

    const sent = (mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest)
      .setting?.value?.value?.value as WorkspaceProfileSetting | undefined;
    expect(sent?.requireEmailVerification).toBeUndefined();
    expect(
      useAppStore.getState().workspaceProfile?.requireEmailVerification
    ).toBeUndefined();
  });
});

describe("other settings", () => {
  it("merges S3 patch over the cached config", async () => {
    useAppStore.setState({
      s3Config: create(S3ConfigSettingSchema, {
        endpoint: "https://s3.example.com",
        bucket: "b",
        secretKey: "real-secret",
      }),
    });
    mock.updateSetting.mockResolvedValue(
      settingResponse("settings/s3_config", {
        value: {
          case: "s3Config",
          value: create(S3ConfigSettingSchema, {
            endpoint: "https://s3.example.com",
            bucket: "other",
            secretKey: "real-secret",
          }),
        },
      })
    );

    const got = await useAppStore
      .getState()
      .updateS3Config({ bucket: "other" }, ["value.s3_config.bucket"]);

    const req = mock.updateSetting.mock.calls[0][0] as UpdateSettingRequest;
    expect(req.updateMask?.paths).toEqual(["value.s3_config.bucket"]);
    const sent = req.setting?.value?.value?.value as
      | S3ConfigSetting
      | undefined;
    expect(sent?.bucket).toBe("other");
    expect(sent?.secretKey).toBe("real-secret");
    expect(got?.bucket).toBe("other");
  });

  it("fetches and updates the llm agent config", async () => {
    mock.getSetting.mockResolvedValue(
      settingResponse("settings/llm_agent_config", {
        value: {
          case: "llmAgentConfig",
          value: create(LlmAgentConfigSettingSchema, {
            allowUserSelfProvidedKeys: true,
          }),
        },
      })
    );
    mock.updateSetting.mockResolvedValue(
      settingResponse("settings/llm_agent_config", {
        value: {
          case: "llmAgentConfig",
          value: create(LlmAgentConfigSettingSchema, {
            allowUserSelfProvidedKeys: false,
          }),
        },
      })
    );

    await useAppStore.getState().fetchLlmAgentConfig();
    const got = await useAppStore
      .getState()
      .updateLlmAgentConfig({ allowUserSelfProvidedKeys: false }, [
        "value.llm_agent_config.allow_user_self_provided_keys",
      ]);

    expect(got?.allowUserSelfProvidedKeys).toBe(false);
    expect(
      useAppStore.getState().llmAgentConfig?.allowUserSelfProvidedKeys
    ).toBe(false);
  });
});
