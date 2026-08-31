import { create, equals } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import { agentServiceClient } from "@/connect";
import { queryClient } from "@/lib/query-client";
import type {
  Agent,
  AgentModelOption,
  AgentSummary,
  CreateAgentResponse,
  PiModel,
  RotateAgentTokenResponse,
  TransferAgentOwnershipResponse,
} from "@/types/proto-es/v1/agent_pb";
import {
  AgentACPConfigSchema,
  AgentInfoSchema,
  AgentSchema,
  AgentSummarySchema,
  CreateAgentRequestSchema,
  DeleteAgentRequestSchema,
  ListPiModelsRequestSchema,
  RefreshAgentModelsRequestSchema,
  RestartAgentRequestSchema,
  RevokeAgentTokenRequestSchema,
  RotateAgentTokenRequestSchema,
  StartAgentRequestSchema,
  StopAgentRequestSchema,
  TransferAgentOwnershipRequestSchema,
  UpdateAgentACPConfigRequestSchema,
  UpdateAgentMcpConfigRequestSchema,
  UpdateAgentRequestSchema,
} from "@/types/proto-es/v1/agent_pb";
import type { AppSliceCreator } from "./types";
import type { AgentACPConfigInput } from "./ui-models";

export interface AgentSlice {
  agents: AgentSummary[];
  agentsLoading: boolean;

  fetchAgents: (
    params?: {
      pageSize?: number;
      pageToken?: string;
    },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
  getAgent: (name: string) => Promise<Agent | undefined>;
  // createAgent binds a new agent to a machine. The machine app picks the agent
  // up automatically over its MachineChannel — no bootstrap token is returned
  // (CreateAgentResponse.bootstrapToken is empty under the machine-hosts-many
  // model). acpConfig optionally sets the agent's provider/model/persona/env at
  // creation time so the agent is fully configured without a second visit to the
  // agent profile; when omitted the agent is created with the server default.
  // allowAddToChannel controls whether other users may add this agent to a
  // channel; when false (default) only the agent's owner or a workspace admin
  // may add it. description is the public agent intro shown to other
  // users/agents (not injected into the agent's own prompt).
  createAgent: (
    title: string,
    machine: string,
    acpConfig?: AgentACPConfigInput,
    labels?: Record<string, string>,
    allowAddToChannel?: boolean,
    description?: string
  ) => Promise<CreateAgentResponse>;
  // updateAgent patches the agent's mutable fields (allow_add_to_channel,
  // follow_owner_permissions, can_manage_channel_members, description); only the
  // keys present in `fields` are sent. Authorized server-side for the agent's
  // owner or a workspace admin.
  updateAgent: (
    name: string,
    fields: {
      allowAddToChannel?: boolean;
      followOwnerPermissions?: boolean;
      canManageChannelMembers?: boolean;
      description?: string;
    }
  ) => Promise<Agent>;
  deleteAgent: (name: string) => Promise<void>;
  // stopAgent stops an agent: its machine runner is torn down and it processes
  // no session messages until startAgent. The agent row is preserved.
  stopAgent: (name: string) => Promise<void>;
  // startAgent resumes a stopped agent so it processes messages again.
  startAgent: (name: string) => Promise<void>;
  // restartAgent force-cold-restarts an agent: it ends the agent's current
  // LLM session and clears its persisted session state so the next turn
  // starts from a fresh cold start.
  restartAgent: (name: string) => Promise<void>;
  rotateAgentToken: (
    name: string,
    reason?: string
  ) => Promise<RotateAgentTokenResponse>;
  revokeAgentToken: (name: string, reason?: string) => Promise<void>;
  updateAgentACPConfig: (
    name: string,
    acpConfig: AgentACPConfigInput
  ) => Promise<void>;
  // updateAgentMcpConfig replaces the MCP servers enabled on an agent. Only
  // servers the caller may use are accepted server-side.
  updateAgentMcpConfig: (name: string, mcpServers: string[]) => Promise<void>;
  // transferAgentOwnership reassigns the agent's owner to another user.
  // Unilateral and immediately effective (the target user does not accept);
  // authorized server-side for the current owner or a workspace admin.
  transferAgentOwnership: (
    name: string,
    newOwner: string,
    reason?: string
  ) => Promise<TransferAgentOwnershipResponse>;
  // listPiModels proxies an LLM API provider's model-listing API through the
  // manager (CORS + key hygiene). Fetched dynamically so the model list is never
  // hardcoded. apiKey is required for deepseek and custom; ignored for
  // openrouter. apiBaseUrl is required for custom.
  listPiModels: (
    apiProvider: string,
    apiKey: string,
    apiBaseUrl?: string
  ) => Promise<PiModel[]>;
  // refreshAgentModels probes one provider's models on the agent's machine with
  // the given (possibly unsaved) custom_env, so the model picker reflects an
  // agent's custom env (e.g. CODEX_HOME) before saving. Session-only.
  refreshAgentModels: (
    name: string,
    acpConfig: AgentACPConfigInput
  ) => Promise<AgentModelOption[]>;
}

// Query cache key for this slice (ADR-1: query keys live with the slice that
// fetches them). The list request has no filter beyond paging, so a single
// fixed key covers the roster; pageSize/pageToken stay OUT of the key on
// purpose: the only pageToken caller (members.ts drainRoster) pages strictly
// sequentially, and the key must stay identical across the presence
// heartbeat's silent refresh and any explicit load so overlapping calls merge
// into one in-flight promise — the dedupe protection the old hand-rolled path
// lacked. The tradeoff: two truly concurrent same-key calls with different
// paging params resolve with whichever queryFn registered first — today every
// caller pins pageSize: 100, so the merged result matches what the
// merged-away call would have fetched. staleTime: 0 keeps "action call == one
// explicit fetch" semantics (one heartbeat tick is still exactly one RPC, so
// the 30s presence cadence is untouched); retry: false keeps the explicit
// failure path deterministic.
const QUERY_KEY = ["agents"];

// Logout clears the slice's Query cache. Wired up at the batch-3 unified
// release point; until then a logout-relogin can serve gcTime-stale data
// briefly, bounded by the 5-minute gcTime in query-client defaults.
export function invalidateAgentsCache(): void {
  void queryClient.removeQueries({ queryKey: QUERY_KEY });
}

export const createAgentSlice: AppSliceCreator<AgentSlice> = (set, get) => ({
  agents: [],
  agentsLoading: false,

  async fetchAgents(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the table swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ agentsLoading: true });
    try {
      const res = await queryClient.fetchQuery({
        queryKey: QUERY_KEY,
        // Action semantics: exactly one RPC attempt per call — retries are
        // batch-3 page-level useQuery territory.
        retry: false,
        staleTime: 0,
        queryFn: () =>
          agentServiceClient.listAgents({
            pageSize: params?.pageSize ?? 100,
            pageToken: params?.pageToken ?? "",
          }),
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all (the store field is a mirrored view
      // of the Query cache during the migration; components still subscribe
      // to the store).
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
    allowAddToChannel?: boolean,
    description?: string
  ) {
    const res = await agentServiceClient.createAgent(
      create(CreateAgentRequestSchema, {
        agent: create(AgentSchema, {
          title,
          description,
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

  // updateAgent patches the agent's mutable fields. Only the keys present
  // in `fields` are sent (the update_mask is built from them), so the caller
  // never overwrites a field it did not touch.
  async updateAgent(
    name: string,
    fields: {
      allowAddToChannel?: boolean;
      followOwnerPermissions?: boolean;
      canManageChannelMembers?: boolean;
      description?: string;
    }
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
    if (fields.canManageChannelMembers !== undefined) {
      agent.canManageChannelMembers = fields.canManageChannelMembers;
      paths.push("can_manage_channel_members");
    }
    if (fields.description !== undefined) {
      agent.description = fields.description;
      paths.push("description");
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

  async stopAgent(name: string) {
    await agentServiceClient.stopAgent(
      create(StopAgentRequestSchema, { name })
    );
    // Reflect the disabled state locally so the UI updates without a refetch.
    set((state) => ({
      agents: state.agents.map((a) =>
        a.name === name ? { ...a, enabled: false } : a
      ),
    }));
  },

  async startAgent(name: string) {
    await agentServiceClient.startAgent(
      create(StartAgentRequestSchema, { name })
    );
    set((state) => ({
      agents: state.agents.map((a) =>
        a.name === name ? { ...a, enabled: true } : a
      ),
    }));
  },

  async restartAgent(name: string) {
    await agentServiceClient.restartAgent(
      create(RestartAgentRequestSchema, { name })
    );
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

  async updateAgentMcpConfig(name: string, mcpServers: string[]) {
    await agentServiceClient.updateAgentMcpConfig(
      create(UpdateAgentMcpConfigRequestSchema, {
        name,
        mcpServers,
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

  async listPiModels(
    apiProvider: string,
    apiKey: string,
    apiBaseUrl?: string
  ): Promise<PiModel[]> {
    const res = await agentServiceClient.listPiModels(
      create(ListPiModelsRequestSchema, {
        apiProvider,
        apiKey,
        apiBaseUrl: apiBaseUrl ?? "",
      })
    );
    return res.models;
  },

  // refreshAgentModels probes one provider's models on the agent's machine using
  // the given (possibly unsaved) ACP config's custom_env, so the model picker
  // reflects an agent's custom env (e.g. CODEX_HOME) before saving. The result
  // is session-only (not persisted). Returns the fresh model list, or throws on
  // a probe failure surfaced in the response error.
  async refreshAgentModels(
    name: string,
    acpConfig: AgentACPConfigInput
  ): Promise<AgentModelOption[]> {
    const res = await agentServiceClient.refreshAgentModels(
      create(RefreshAgentModelsRequestSchema, { name, acpConfig })
    );
    if (res.error) {
      throw new Error(res.error);
    }
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
