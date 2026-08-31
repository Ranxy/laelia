import { create, equals } from "@bufbuild/protobuf";
import { machineServiceClient } from "@/connect";
import { queryClient } from "@/lib/query-client";
import type {
  AgentModelOption,
  AgentProviderInfo,
  AgentSummary,
} from "@/types/proto-es/v1/agent_pb";
import type { Machine, MachineSummary } from "@/types/proto-es/v1/machine_pb";
import {
  DeleteMachineRequestSchema,
  ForceDisconnectMachineRequestSchema,
  MachineSummarySchema,
  RefreshMachineModelsRequestSchema,
  RefreshMachineProvidersRequestSchema,
  RevokeMachineTokenRequestSchema,
  TransferMachineOwnershipRequestSchema,
  UpdateMachineRequestSchema,
  UpgradeMachineRequestSchema,
} from "@/types/proto-es/v1/machine_pb";
import type { AppSliceCreator } from "./types";
import type { AgentACPConfigInput } from "./ui-models";

// MachineSlice owns the machine roster and the machine-management mutations.
// A machine authenticates via the device-code flow (no bootstrap token) and
// hosts every agent bound to it; rename/transfer, revoke and provider
// discovery are machine-scoped.
export interface MachineSlice {
  machines: MachineSummary[];
  machinesLoading: boolean;

  fetchMachines: (
    params?: {
      pageSize?: number;
      pageToken?: string;
      showDeleted?: boolean;
    },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
  getMachine: (name: string) => Promise<Machine | undefined>;
  updateMachine: (name: string, title: string) => Promise<Machine>;
  transferMachineOwnership: (
    name: string,
    newOwner: string,
    reason?: string
  ) => Promise<void>;
  deleteMachine: (name: string) => Promise<void>;
  revokeMachineToken: (name: string, reason?: string) => Promise<void>;
  forceDisconnectMachine: (name: string, reason?: string) => Promise<void>;
  refreshMachineProviders: (name: string) => Promise<AgentProviderInfo[]>;
  // refreshMachineModels probes one provider's models on the machine with the
  // given (possibly unsaved) custom_env, for the add-agent form. Session-only.
  refreshMachineModels: (
    name: string,
    acpConfig: AgentACPConfigInput
  ) => Promise<AgentModelOption[]>;
  upgradeMachine: (name: string, reason?: string) => Promise<void>;
  listMachineAgents: (name: string) => Promise<AgentSummary[]>;
}

// Query cache key family for this slice (ADR-1: query keys live with the slice
// that fetches them). showDeleted selects a different server view (active vs
// recycled machines), so it participates in the key; pageSize/pageToken stay
// OUT of the key on purpose — the pageToken callers page strictly
// sequentially, and one stable key per view lets fetchQuery's in-flight merge
// dedupe overlapping refreshes (e.g. the machine list poll racing a
// detail-page refresh). The tradeoff: two truly concurrent same-key calls with
// different paging params resolve with whichever queryFn registered first —
// today every caller pins pageSize: 100, so the merged result matches what the
// merged-away call would have fetched. staleTime: 0 keeps "action call == one
// explicit fetch" semantics so page-level pollers keep their cadence;
// retry: false keeps the explicit failure path deterministic.

// Logout clears the slice's Query cache (whole ["machines", ...] family).
// Wired up at the batch-3 unified release point; until then a logout-relogin
// can serve gcTime-stale data briefly, bounded by the 5-minute gcTime in
// query-client defaults.
export function invalidateMachinesCache(): void {
  void queryClient.removeQueries({ queryKey: ["machines"] });
}

export const createMachineSlice: AppSliceCreator<MachineSlice> = (
  set,
  get
) => ({
  machines: [],
  machinesLoading: false,

  async fetchMachines(params, opts) {
    const showDeleted = params?.showDeleted ?? false;
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the table swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ machinesLoading: true });
    try {
      const res = await queryClient.fetchQuery({
        queryKey: ["machines", showDeleted],
        // Action semantics: exactly one RPC attempt per call — retries are
        // batch-3 page-level useQuery territory.
        retry: false,
        staleTime: 0,
        queryFn: () =>
          machineServiceClient.listMachines({
            pageSize: params?.pageSize ?? 100,
            pageToken: params?.pageToken ?? "",
            showDeleted,
          }),
      });
      // Skip the state update entirely when nothing changed, so unchanged
      // polls cause no re-render at all (the store field is a mirrored view
      // of the Query cache during the migration; components still subscribe
      // to the store).
      if (silent && machinesEqual(get().machines, res.machines)) {
        return { nextPageToken: res.nextPageToken };
      }
      set({ machines: res.machines, machinesLoading: false });
      return { nextPageToken: res.nextPageToken };
    } catch {
      if (!silent) set({ machines: [], machinesLoading: false });
      return undefined;
    }
  },

  // getMachine fetches the full Machine on every call. It is intentionally NOT
  // cached: Machine.canEdit and status are per-caller / mutable, so a persistent
  // cache would survive a user switch and surface stale state. The profile page
  // holds the result in local state and re-fetches after mutations.
  async getMachine(name) {
    try {
      return await machineServiceClient.getMachine({ name });
    } catch {
      return undefined;
    }
  },

  async updateMachine(name: string, title: string) {
    const res = await machineServiceClient.updateMachine(
      create(UpdateMachineRequestSchema, { name, title })
    );
    // Keep the local roster in sync so the machines list and the detail
    // header show the new title immediately instead of after a refetch.
    set((state) => ({
      machines: state.machines.map((m) =>
        m.name === name ? create(MachineSummarySchema, { ...m, title }) : m
      ),
    }));
    return res;
  },

  async transferMachineOwnership(
    name: string,
    newOwner: string,
    reason?: string
  ) {
    await machineServiceClient.transferMachineOwnership(
      create(TransferMachineOwnershipRequestSchema, {
        name,
        newOwner,
        reason: reason ?? "",
      })
    );
  },

  async deleteMachine(name: string) {
    await machineServiceClient.deleteMachine(
      create(DeleteMachineRequestSchema, { name })
    );
    set((state) => ({
      machines: state.machines.filter((m) => m.name !== name),
    }));
  },

  async revokeMachineToken(name: string, reason?: string) {
    await machineServiceClient.revokeMachineToken(
      create(RevokeMachineTokenRequestSchema, { name, reason: reason ?? "" })
    );
  },

  async forceDisconnectMachine(name: string, reason?: string) {
    await machineServiceClient.forceDisconnectMachine(
      create(ForceDisconnectMachineRequestSchema, {
        name,
        reason: reason ?? "",
      })
    );
  },

  async refreshMachineProviders(name: string): Promise<AgentProviderInfo[]> {
    const res = await machineServiceClient.refreshMachineProviders(
      create(RefreshMachineProvidersRequestSchema, { name })
    );
    return res.providers;
  },

  // refreshMachineModels probes one provider's models on the machine with the
  // given (possibly unsaved) custom_env, so the add-agent form's model picker
  // reflects a custom env (e.g. CODEX_HOME) before the agent exists.
  // Session-only. Returns the fresh model list, or throws on a probe failure
  // surfaced in the response error.
  async refreshMachineModels(
    name: string,
    acpConfig: AgentACPConfigInput
  ): Promise<AgentModelOption[]> {
    const res = await machineServiceClient.refreshMachineModels(
      create(RefreshMachineModelsRequestSchema, { name, acpConfig })
    );
    if (res.error) {
      throw new Error(res.error);
    }
    return res.models;
  },

  // upgradeMachine asks the machine to self-upgrade to the manager's embedded
  // binary. Fire-and-forget: progress is followed via getMachine polling
  // (Machine.upgradeStatus).
  async upgradeMachine(name: string, reason?: string) {
    await machineServiceClient.upgradeMachine(
      create(UpgradeMachineRequestSchema, { name, reason: reason ?? "" })
    );
  },

  // listMachineAgents returns *every* agent bound to the machine, draining the
  // full page stream rather than only the first page so a machine with >100
  // agents is not silently truncated. The cap guards against a runaway cursor.
  async listMachineAgents(name: string): Promise<AgentSummary[]> {
    const all: AgentSummary[] = [];
    let pageToken = "";
    for (let page = 0; page < 50; page++) {
      const res = await machineServiceClient.listMachineAgents({
        name,
        pageSize: 100,
        pageToken,
      });
      all.push(...res.agents);
      pageToken = res.nextPageToken;
      if (!pageToken) break;
    }
    return all;
  },
});

// machinesEqual reports whether two machine summary lists are structurally
// identical, used to skip redundant state updates during background polling.
function machinesEqual(
  prev: MachineSummary[],
  next: MachineSummary[]
): boolean {
  if (prev.length !== next.length) return false;
  for (let i = 0; i < prev.length; i++) {
    if (prev[i].name !== next[i].name) return false;
    if (!equals(MachineSummarySchema, prev[i], next[i])) return false;
  }
  return true;
}
