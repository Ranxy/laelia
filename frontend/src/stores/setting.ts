import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { settingServiceClient } from "@/connect";
import type {
  LlmAgentConfigSetting,
  PasswordRestrictionSetting,
  ProvisioningSetting,
  S3ConfigSetting,
  SMTPSetting,
  UserMcpConfigSetting,
  WorkspaceProfileSetting,
} from "@/types/proto-es/store/setting_pb";
import {
  LlmAgentConfigSettingSchema,
  PasswordRestrictionSettingSchema,
  ProvisioningSettingSchema,
  S3ConfigSettingSchema,
  SMTPSettingSchema,
  UserMcpConfigSettingSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import {
  SettingSchema,
  SettingValueSchema,
  UpdateSettingRequestSchema,
} from "@/types/proto-es/v1/setting_pb";
import type { AppSliceCreator } from "./types";

// SettingSlice owns the workspace-level settings (workspace profile, SMTP, S3,
// LLM agent, user MCP, password restriction). Configs are cached as full proto
// messages after fetch; update methods merge only the mask-listed paths into
// the stored value server-side (bytebase-style field-level update) and refresh
// the cache from the authoritative response.
export interface SettingSlice {
  workspaceProfile?: WorkspaceProfileSetting;
  smtpConfig?: SMTPSetting;
  s3Config?: S3ConfigSetting;
  llmAgentConfig?: LlmAgentConfigSetting;
  userMcpConfig?: UserMcpConfigSetting;
  passwordRestriction?: PasswordRestrictionSetting;
  provisioningConfig?: ProvisioningSetting;

  fetchWorkspaceProfile: () => Promise<WorkspaceProfileSetting | undefined>;
  fetchSmtpConfig: () => Promise<SMTPSetting | undefined>;
  fetchS3Config: () => Promise<S3ConfigSetting | undefined>;
  fetchLlmAgentConfig: () => Promise<LlmAgentConfigSetting | undefined>;
  fetchUserMcpConfig: () => Promise<UserMcpConfigSetting | undefined>;
  fetchPasswordRestriction: () => Promise<
    PasswordRestrictionSetting | undefined
  >;
  fetchProvisioningConfig: () => Promise<ProvisioningSetting | undefined>;

  updateWorkspaceProfile: (
    patch: Partial<WorkspaceProfileSetting>,
    paths: string[]
  ) => Promise<WorkspaceProfileSetting | undefined>;
  updateSmtpConfig: (
    patch: Partial<SMTPSetting>,
    paths: string[]
  ) => Promise<SMTPSetting | undefined>;
  updateS3Config: (
    patch: Partial<S3ConfigSetting>,
    paths: string[]
  ) => Promise<S3ConfigSetting | undefined>;
  updateLlmAgentConfig: (
    patch: Partial<LlmAgentConfigSetting>,
    paths: string[]
  ) => Promise<LlmAgentConfigSetting | undefined>;
  updateUserMcpConfig: (
    patch: Partial<UserMcpConfigSetting>,
    paths: string[]
  ) => Promise<UserMcpConfigSetting | undefined>;
  updatePasswordRestriction: (
    patch: Partial<PasswordRestrictionSetting>,
    paths: string[]
  ) => Promise<PasswordRestrictionSetting | undefined>;
  updateProvisioningConfig: (
    patch: Partial<ProvisioningSetting>,
    paths: string[]
  ) => Promise<ProvisioningSetting | undefined>;
}

// Update-mask path lists (bytebase-style full paths, e.g.
// "value.workspace_profile.disallow_signup"). The backend only writes the
// masked fields of the request payload into the stored value, so callers pass
// the subset they are saving and never round-trip unrelated fields.
export const workspaceProfilePaths = [
  "value.workspace_profile.external_url",
  "value.workspace_profile.disallow_signup",
  "value.workspace_profile.require_2fa",
  "value.workspace_profile.token_duration",
  "value.workspace_profile.maximum_role_expiration",
  "value.workspace_profile.domains",
  "value.workspace_profile.enforce_identity_domain",
  "value.workspace_profile.disallow_password_signin",
  "value.workspace_profile.enable_metric_collection",
  "value.workspace_profile.require_email_verification",
  "value.workspace_profile.disallow_user_create_machine",
] as const;

export const smtpConfigPaths = [
  "value.smtp_config.host",
  "value.smtp_config.port",
  "value.smtp_config.username",
  "value.smtp_config.password",
  "value.smtp_config.from",
  "value.smtp_config.use_tls",
] as const;

export const s3ConfigPaths = [
  "value.s3_config.endpoint",
  "value.s3_config.region",
  "value.s3_config.bucket",
  "value.s3_config.access_key",
  "value.s3_config.secret_key",
  "value.s3_config.force_path_style",
  "value.s3_config.use_ssl",
] as const;

export const llmAgentConfigPaths = [
  "value.llm_agent_config.allow_user_self_provided_keys",
] as const;

export const userMcpConfigPaths = [
  "value.user_mcp_config.allow_user_mcp_servers",
  "value.user_mcp_config.mcp_ip_policy",
] as const;

export const passwordRestrictionPaths = [
  "value.password_restriction.min_length",
  "value.password_restriction.require_number",
  "value.password_restriction.require_letter",
  "value.password_restriction.require_uppercase_letter",
  "value.password_restriction.require_special_character",
  "value.password_restriction.require_reset_password_for_first_login",
  "value.password_restriction.password_rotation",
] as const;

export const provisioningPaths = [
  "value.provisioning.runtime_image",
  "value.provisioning.binary_target",
  "value.provisioning.allow_custom_images",
  "value.provisioning.custom_image_allowlist_enabled",
  "value.provisioning.custom_image_allowlist",
] as const;

// settingResourceName maps a setting to its resource name, matching the
// backend's "settings/{name}" convention.
function settingResourceName(name: string): string {
  return `settings/${name}`;
}

export const createSettingSlice: AppSliceCreator<SettingSlice> = (
  set,
  get
) => ({
  workspaceProfile: undefined,
  smtpConfig: undefined,
  s3Config: undefined,
  llmAgentConfig: undefined,
  userMcpConfig: undefined,
  passwordRestriction: undefined,
  provisioningConfig: undefined,

  async fetchWorkspaceProfile() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("workspace_profile"),
    });
    const v = res.value?.value;
    const profile = v?.case === "workspaceProfile" ? v.value : undefined;
    set({ workspaceProfile: profile });
    return profile;
  },

  async fetchSmtpConfig() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("smtp_config"),
    });
    const v = res.value?.value;
    const cfg = v?.case === "smtpConfig" ? v.value : undefined;
    set({ smtpConfig: cfg });
    return cfg;
  },

  async fetchS3Config() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("s3_config"),
    });
    const v = res.value?.value;
    const cfg = v?.case === "s3Config" ? v.value : undefined;
    set({ s3Config: cfg });
    return cfg;
  },

  async fetchLlmAgentConfig() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("llm_agent_config"),
    });
    const v = res.value?.value;
    const cfg = v?.case === "llmAgentConfig" ? v.value : undefined;
    set({ llmAgentConfig: cfg });
    return cfg;
  },

  async fetchUserMcpConfig() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("user_mcp_config"),
    });
    const v = res.value?.value;
    const cfg = v?.case === "userMcpConfig" ? v.value : undefined;
    set({ userMcpConfig: cfg });
    return cfg;
  },

  async fetchPasswordRestriction() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("password_restriction"),
    });
    const v = res.value?.value;
    const cfg = v?.case === "passwordRestriction" ? v.value : undefined;
    set({ passwordRestriction: cfg });
    return cfg;
  },

  async fetchProvisioningConfig() {
    const res = await settingServiceClient.getSetting({
      name: settingResourceName("provisioning"),
    });
    const v = res.value?.value;
    const cfg = v?.case === "provisioning" ? v.value : undefined;
    set({ provisioningConfig: cfg });
    return cfg;
  },

  async updateWorkspaceProfile(patch, paths) {
    const base =
      get().workspaceProfile ?? create(WorkspaceProfileSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("workspace_profile"),
          value: create(SettingValueSchema, {
            value: {
              case: "workspaceProfile" as const,
              value: { ...base, ...patch },
            },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const profile = v?.case === "workspaceProfile" ? v.value : undefined;
    set({ workspaceProfile: profile });
    return profile;
  },

  async updateSmtpConfig(patch, paths) {
    const base = get().smtpConfig ?? create(SMTPSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("smtp_config"),
          value: create(SettingValueSchema, {
            value: {
              case: "smtpConfig" as const,
              value: { ...base, ...patch },
            },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const cfg = v?.case === "smtpConfig" ? v.value : undefined;
    set({ smtpConfig: cfg });
    return cfg;
  },

  async updateS3Config(patch, paths) {
    const base = get().s3Config ?? create(S3ConfigSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("s3_config"),
          value: create(SettingValueSchema, {
            value: { case: "s3Config" as const, value: { ...base, ...patch } },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const cfg = v?.case === "s3Config" ? v.value : undefined;
    set({ s3Config: cfg });
    return cfg;
  },

  async updateLlmAgentConfig(patch, paths) {
    const base =
      get().llmAgentConfig ?? create(LlmAgentConfigSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("llm_agent_config"),
          value: create(SettingValueSchema, {
            value: {
              case: "llmAgentConfig" as const,
              value: { ...base, ...patch },
            },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const cfg = v?.case === "llmAgentConfig" ? v.value : undefined;
    set({ llmAgentConfig: cfg });
    return cfg;
  },

  async updateUserMcpConfig(patch, paths) {
    const base = get().userMcpConfig ?? create(UserMcpConfigSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("user_mcp_config"),
          value: create(SettingValueSchema, {
            value: {
              case: "userMcpConfig" as const,
              value: { ...base, ...patch },
            },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const cfg = v?.case === "userMcpConfig" ? v.value : undefined;
    set({ userMcpConfig: cfg });
    return cfg;
  },

  async updatePasswordRestriction(patch, paths) {
    const base =
      get().passwordRestriction ?? create(PasswordRestrictionSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("password_restriction"),
          value: create(SettingValueSchema, {
            value: {
              case: "passwordRestriction" as const,
              value: { ...base, ...patch },
            },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const cfg = v?.case === "passwordRestriction" ? v.value : undefined;
    set({ passwordRestriction: cfg });
    return cfg;
  },

  async updateProvisioningConfig(patch, paths) {
    const base =
      get().provisioningConfig ?? create(ProvisioningSettingSchema, {});
    const res = await settingServiceClient.updateSetting(
      create(UpdateSettingRequestSchema, {
        setting: create(SettingSchema, {
          name: settingResourceName("provisioning"),
          value: create(SettingValueSchema, {
            value: {
              case: "provisioning" as const,
              value: { ...base, ...patch },
            },
          }),
        }),
        updateMask: create(FieldMaskSchema, { paths: [...paths] }),
      })
    );
    const v = res.value?.value;
    const cfg = v?.case === "provisioning" ? v.value : undefined;
    set({ provisioningConfig: cfg });
    return cfg;
  },
});
