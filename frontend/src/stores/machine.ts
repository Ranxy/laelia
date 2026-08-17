import { create, equals } from "@bufbuild/protobuf";
import { machineServiceClient } from "@/connect";
import type {
  AgentProviderInfo,
  AgentSummary,
} from "@/types/proto-es/v1/agent_pb";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";
import {
  DeleteMachineRequestSchema,
  ForceDisconnectMachineRequestSchema,
  MachineSummarySchema,
  RefreshMachineProvidersRequestSchema,
  RevokeMachineTokenRequestSchema,
  TransferMachineOwnershipRequestSchema,
  UpdateMachineRequestSchema,
  UpgradeMachineRequestSchema,
} from "@/types/proto-es/v1/machine_pb";
import type { AppSliceCreator, MachineSlice } from "./types";

export const createMachineSlice: AppSliceCreator<MachineSlice> = (
  set,
  get
) => ({
  machines: [],
  machinesLoading: false,

  async fetchMachines(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the table swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ machinesLoading: true });
    try {
      const res = await machineServiceClient.listMachines({
        pageSize: params?.pageSize ?? 100,
        pageToken: params?.pageToken ?? "",
        showDeleted: params?.showDeleted ?? false,
      });
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
