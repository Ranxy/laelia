import { create } from "@bufbuild/protobuf";
import { agentServiceClient, machineServiceClient } from "@/connect";
import type {
  WorkspaceEntry,
  WorkspaceReadResponse,
} from "@/types/proto-es/v1/agent_pb";
import {
  ListAgentWorkspaceRequestSchema,
  ReadAgentWorkspaceFileRequestSchema,
  WorkspaceReadResponseSchema,
} from "@/types/proto-es/v1/agent_pb";
import type { MachineWorkspaceSummary } from "@/types/proto-es/v1/machine_pb";
import {
  DeleteMachineWorkspaceRequestSchema,
  ListMachineWorkspacesRequestSchema,
} from "@/types/proto-es/v1/machine_pb";
import type { AppSliceCreator, WorkspaceSlice } from "./types";

// createWorkspaceSlice exposes the workspace browser RPCs. Authorization is
// handler-gated server-side (agent owner/admin, machine creator/admin); the
// page components additionally gate tab visibility on canEdit/canManage.
export const createWorkspaceSlice: AppSliceCreator<WorkspaceSlice> = () => ({
  async listAgentWorkspaceDir(
    name: string,
    dirPath: string,
    includeHidden: boolean
  ): Promise<WorkspaceEntry[]> {
    const res = await agentServiceClient.listAgentWorkspace(
      create(ListAgentWorkspaceRequestSchema, { name, dirPath, includeHidden })
    );
    return res.entries;
  },

  async readAgentWorkspaceFile(
    name: string,
    path: string
  ): Promise<WorkspaceReadResponse> {
    const res = await agentServiceClient.readAgentWorkspaceFile(
      create(ReadAgentWorkspaceFileRequestSchema, { name, path })
    );
    return res.file ?? create(WorkspaceReadResponseSchema, {});
  },

  async listMachineWorkspaces(
    name: string
  ): Promise<MachineWorkspaceSummary[]> {
    const res = await machineServiceClient.listMachineWorkspaces(
      create(ListMachineWorkspacesRequestSchema, { name })
    );
    return res.workspaces;
  },

  async deleteMachineWorkspace(name: string, directoryName: string) {
    await machineServiceClient.deleteMachineWorkspace(
      create(DeleteMachineWorkspaceRequestSchema, { name, directoryName })
    );
  },
});
