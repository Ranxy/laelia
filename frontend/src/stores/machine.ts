import { create, equals } from "@bufbuild/protobuf";
import { machineServiceClient } from "@/connect";
import type {
  AgentProviderInfo,
  AgentSummary,
} from "@/types/proto-es/v1/agent_pb";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";
import {
  CreateMachineRequestSchema,
  DeleteMachineRequestSchema,
  ForceDisconnectMachineRequestSchema,
  MachineSchema,
  MachineSummarySchema,
  RefreshMachineProvidersRequestSchema,
  RevokeMachineTokenRequestSchema,
  RotateMachineTokenRequestSchema,
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

  async createMachine(title: string, labels?: Record<string, string>) {
    const res = await machineServiceClient.createMachine(
      create(CreateMachineRequestSchema, {
        machine: create(MachineSchema, { title, labels }),
      })
    );
    return res;
  },

  async deleteMachine(name: string) {
    await machineServiceClient.deleteMachine(
      create(DeleteMachineRequestSchema, { name })
    );
    set((state) => ({
      machines: state.machines.filter((m) => m.name !== name),
    }));
  },

  async rotateMachineToken(name: string, reason?: string) {
    return machineServiceClient.rotateMachineToken(
      create(RotateMachineTokenRequestSchema, { name, reason: reason ?? "" })
    );
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

  async listMachineAgents(name: string): Promise<AgentSummary[]> {
    const res = await machineServiceClient.listMachineAgents({
      name,
      pageSize: 100,
      pageToken: "",
    });
    return res.agents;
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
