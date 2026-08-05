import { create, equals } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { agentServiceClient } from "@/connect";
import type {
  AgentProviderInfo,
  AgentSummary,
  PiModel,
} from "@/types/proto-es/v1/agent_pb";
import {
  AgentACPConfigSchema,
  AgentInfoSchema,
  AgentSchema,
  AgentSummarySchema,
  CreateAgentRequestSchema,
  DeleteAgentRequestSchema,
  ListPiModelsRequestSchema,
  RefreshAgentProvidersRequestSchema,
  RevokeAgentTokenRequestSchema,
  RotateAgentTokenRequestSchema,
  TransferAgentOwnershipRequestSchema,
  UpdateAgentACPConfigRequestSchema,
  UpdateAgentRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import type { AgentACPConfigInput, AgentSlice, AppSliceCreator } from "./types";

export const createAgentSlice: AppSliceCreator<AgentSlice> = (set, get) => ({
  agents: [],
  agentsLoading: false,

  async fetchAgents(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the table swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ agentsLoading: true });
    try {
      const res = await agentServiceClient.listAgents({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all.
      if (silent && agentsEqual(get().agents, res.agents)) {
        return { nextPageToken: res.nextPageToken };
      }
      set({
        agents: res.agents,
        agentsLoading: false,
      });
      return { nextPageToken: res.nextPageToken };
    } catch {
      // On a silent refresh, keep the existing list instead of wiping it on a
      // transient error; only an explicit load reports failure + clears.
      if (!silent) set({ agents: [], agentsLoading: false });
      return undefined;
    }
  },

  // getAgent fetches the full Agent on every call. It is intentionally NOT
  // cached: Agent.canEdit and acp_config are per-caller / mutable, so a
  // persistent cache would survive a user switch (admin → normal user) and
  // surface a stale canEdit to the profile page. Callers that need the agent
  // (the profile page) hold it in local component state and re-fetch after
  // mutations. The agent-detail layout does not call this at all — it reads
  // the AgentSummary list.
  async getAgent(name) {
    try {
      return await agentServiceClient.getAgent({ name });
    } catch {
      return undefined;
    }
  },

  async createAgent(
    title: string,
    machine: string,
    acpConfig?: AgentACPConfigInput,
    labels?: Record<string, string>,
    allowAddToChannel?: boolean
  ) {
    const res = await agentServiceClient.createAgent(
      create(CreateAgentRequestSchema, {
        agent: create(AgentSchema, {
          title,
          machine,
          labels,
          allowAddToChannel,
          info: acpConfig
            ? create(AgentInfoSchema, {
                acpConfig: create(AgentACPConfigSchema, acpConfig),
              })
            : undefined,
        }),
      })
    );
    return res;
  },

  // updateAgent patches the agent's mutable flag fields. Only the keys present
  // in `fields` are sent (the update_mask is built from them), so the caller
  // never overwrites a flag it did not touch.
  async updateAgent(
    name: string,
    fields: { allowAddToChannel?: boolean; followOwnerPermissions?: boolean }
  ) {
    const agent = create(AgentSchema, { name });
    const paths: string[] = [];
    if (fields.allowAddToChannel !== undefined) {
      agent.allowAddToChannel = fields.allowAddToChannel;
      paths.push("allow_add_to_channel");
    }
    if (fields.followOwnerPermissions !== undefined) {
      agent.followOwnerPermissions = fields.followOwnerPermissions;
      paths.push("follow_owner_permissions");
    }
    return agentServiceClient.updateAgent(
      create(UpdateAgentRequestSchema, {
        agent,
        updateMask: create(FieldMaskSchema, { paths }),
      })
    );
  },

  async deleteAgent(name: string) {
    await agentServiceClient.deleteAgent(
      create(DeleteAgentRequestSchema, { name })
    );
    set((state) => ({
      agents: state.agents.filter((a) => a.name !== name),
    }));
  },

  async rotateAgentToken(name: string, reason?: string) {
    return agentServiceClient.rotateAgentToken(
      create(RotateAgentTokenRequestSchema, { name, reason: reason ?? "" })
    );
  },

  async revokeAgentToken(name: string, reason?: string) {
    await agentServiceClient.revokeAgentToken(
      create(RevokeAgentTokenRequestSchema, { name, reason: reason ?? "" })
    );
  },

  async updateAgentACPConfig(name: string, acpConfig: AgentACPConfigInput) {
    await agentServiceClient.updateAgentACPConfig(
      create(UpdateAgentACPConfigRequestSchema, {
        name,
        acpConfig: create(AgentACPConfigSchema, acpConfig),
      })
    );
  },

  // transferAgentOwnership reassigns the agent's owner to another user. It is
  // unilateral and effective immediately — the target user does not accept, and
  // the previous owner loses owner authority at once. Authorized server-side for
  // the current owner or a workspace admin.
  async transferAgentOwnership(
    name: string,
    newOwner: string,
    reason?: string
  ) {
    return agentServiceClient.transferAgentOwnership(
      create(TransferAgentOwnershipRequestSchema, {
        name,
        newOwner,
        reason: reason ?? "",
      })
    );
  },

  async refreshAgentProviders(name: string): Promise<AgentProviderInfo[]> {
    const res = await agentServiceClient.refreshAgentProviders(
      create(RefreshAgentProvidersRequestSchema, { name })
    );
    // The caller (agent-profile) re-fetches GetAgent so the new
    // available_providers surface in the editor; this slice no longer holds a
    // cached agent to refresh.
    return res.providers;
  },

  async listPiModels(apiProvider: string, apiKey: string): Promise<PiModel[]> {
    const res = await agentServiceClient.listPiModels(
      create(ListPiModelsRequestSchema, { apiProvider, apiKey })
    );
    return res.models;
  },
});

// agentsEqual reports whether two agent summary lists are structurally
// identical, used to skip redundant state updates during background polling.
function agentsEqual(prev: AgentSummary[], next: AgentSummary[]): boolean {
  if (prev.length !== next.length) return false;
  for (let i = 0; i < prev.length; i++) {
    if (prev[i].name !== next[i].name) return false;
    if (!equals(AgentSummarySchema, prev[i], next[i])) return false;
  }
  return true;
}
