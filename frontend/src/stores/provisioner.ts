import { create } from "@bufbuild/protobuf";
import { provisionerServiceClient } from "@/connect";
import { queryClient } from "@/lib/query-client";
import type { Machine } from "@/types/proto-es/v1/machine_pb";
import type { Provisioner } from "@/types/proto-es/v1/provisioner_pb";
import {
  CreateProvisionerRequestSchema,
  DeleteProvisionerRequestSchema,
  ProvisionMachineRequestSchema,
  RotateProvisionerTokenRequestSchema,
} from "@/types/proto-es/v1/provisioner_pb";
import { registerCleanup } from "./cleanup-registry";
import type { AppSliceCreator } from "./types";

// ProvisionerSlice owns the provisioner roster plus the provisioning
// mutations. Provisioners are the enterprise workers that create machine
// workloads in their backend (kubernetes StatefulSets today); the roster feeds
// both the settings page and the /machines/new provisioned-machine picker.
// CreateProvisioner/RotateProvisionerToken return the one-time token, which
// the backend never stores in plaintext and never returns again.
export interface ProvisionerSlice {
  provisioners: Provisioner[];
  provisionersLoading: boolean;

  fetchProvisioners: (
    params?: { pageSize?: number; pageToken?: string },
    opts?: { silent?: boolean }
  ) => Promise<{ nextPageToken: string } | undefined>;
  // getProvisioner fetches the full Provisioner on every call, uncached like
  // getMachine: status (connected/retain data) is per-moment and callers
  // (machine profile card, delete confirmation) must not serve stale state.
  getProvisioner: (name: string) => Promise<Provisioner | undefined>;
  createProvisioner: (input: {
    title: string;
    backend: string;
    description: string;
  }) => Promise<{ provisioner: Provisioner | undefined; token: string }>;
  // rotateProvisionerToken bumps the token version (killing the old token at
  // its next use) and returns the new one-time token.
  rotateProvisionerToken: (name: string, reason?: string) => Promise<string>;
  deleteProvisioner: (name: string) => Promise<void>;
  // provisionMachine enqueues a provisioning job for a machine owned by the
  // caller. runtimeImage optionally overrides the workspace default image for
  // this machine alone (must match the admin-configured allowlist). The
  // created Machine carries its provisioning status; the caller navigates to
  // the machine profile which polls until the machine is ONLINE.
  provisionMachine: (
    provisioner: string,
    title: string,
    runtimeImage?: string
  ) => Promise<Machine>;
}

// Query cache key for this slice (ADR-1: query keys live with the slice that
// fetches them); same action semantics as the other roster slices.
const QUERY_KEY = ["provisioners"];

// Logout clears the slice's Query cache via the unified cleanup registry.
export function invalidateProvisionersCache(): void {
  void queryClient.removeQueries({ queryKey: QUERY_KEY });
}

export const createProvisionerSlice: AppSliceCreator<ProvisionerSlice> = (
  set,
  get
) => ({
  provisioners: [],
  provisionersLoading: false,

  async fetchProvisioners(params, opts) {
    const silent = opts?.silent;
    // Silent (background) refreshes must not flip the loading flag — otherwise
    // the table swaps to "Loading…" and back on every poll, causing flicker.
    if (!silent) set({ provisionersLoading: true });
    try {
      const res = await queryClient.fetchQuery({
        queryKey: QUERY_KEY,
        retry: false,
        staleTime: 0,
        queryFn: () =>
          provisionerServiceClient.listProvisioners({
            pageSize: params?.pageSize ?? 100,
            pageToken: params?.pageToken ?? "",
          }),
      });
      set({
        provisioners: res.provisioners ?? [],
        provisionersLoading: false,
      });
      return { nextPageToken: res.nextPageToken };
    } catch {
      // On a silent refresh, keep the existing list instead of wiping it on a
      // transient error; only an explicit load reports failure + clears.
      if (!silent) set({ provisioners: [], provisionersLoading: false });
      return undefined;
    }
  },

  async getProvisioner(name) {
    try {
      return await provisionerServiceClient.getProvisioner({ name });
    } catch {
      return undefined;
    }
  },

  async createProvisioner(input) {
    const res = await provisionerServiceClient.createProvisioner(
      create(CreateProvisionerRequestSchema, {
        provisioner: {
          title: input.title,
          backend: input.backend,
          description: input.description,
        },
      })
    );
    // Refresh the roster so the new row shows without a manual reload.
    void get().fetchProvisioners({ pageSize: 100 }, { silent: true });
    return { provisioner: res.provisioner, token: res.token };
  },

  async rotateProvisionerToken(name, reason) {
    const res = await provisionerServiceClient.rotateProvisionerToken(
      create(RotateProvisionerTokenRequestSchema, {
        name,
        reason: reason ?? "",
      })
    );
    void get().fetchProvisioners({ pageSize: 100 }, { silent: true });
    return res.token;
  },

  async deleteProvisioner(name) {
    await provisionerServiceClient.deleteProvisioner(
      create(DeleteProvisionerRequestSchema, { name })
    );
    set((state) => ({
      provisioners: state.provisioners.filter((p) => p.name !== name),
    }));
  },

  async provisionMachine(provisioner, title, runtimeImage) {
    return provisionerServiceClient.provisionMachine(
      create(ProvisionMachineRequestSchema, {
        provisioner,
        title,
        runtimeImage: runtimeImage ?? "",
      })
    );
  },
});

registerCleanup(invalidateProvisionersCache);
