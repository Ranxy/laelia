import { Timestamp } from "@bufbuild/protobuf/wkt";
import { useCallback, useEffect, useState } from "react";
import { Badge } from "@/react/components/ui/badge";
import { Button } from "@/react/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/react/components/ui/dialog";
import { Input } from "@/react/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/react/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/react/components/ui/table";
import { useAppStore } from "@/react/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";

export function AgentsPage() {
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const agents = useAppStore((s) => s.agents);
  const loading = useAppStore((s) => s.agentsLoading);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [token, setToken] = useState<string | null>(null);
  const [tokenOpen, setTokenOpen] = useState(false);
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);
  const [detailOpen, setDetailOpen] = useState(false);
  const [creating, setCreating] = useState(false);

  const load = useCallback(() => {
    fetchAgents({ pageSize: 100 });
  }, [fetchAgents]);

  useEffect(() => {
    load();
  }, [load]);

  async function handleCreate() {
    if (!name.trim()) return;
    setCreating(true);
    try {
      const createAgent = useAppStore.getState().createAgent;
      const agent = await createAgent(name.trim());
      if (agent.token) {
        setToken(agent.token);
        setTokenOpen(true);
      }
      setName("");
      setCreateOpen(false);
      load();
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(name: string) {
    const deleteAgent = useAppStore.getState().deleteAgent;
    try {
      await deleteAgent(name);
      load();
    } catch {
      // handled by api client
    }
  }

  function handleRowClick(agent: Agent) {
    setSelectedAgent(agent);
    setDetailOpen(true);
  }

  return (
    <div className="p-6 flex flex-col gap-4 w-full">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-main">Agents</h1>
        <Button onClick={() => setCreateOpen(true)}>Create Agent</Button>
      </div>

      <Sheet
        open={createOpen}
        onOpenChange={(next) => !next && setCreateOpen(false)}
      >
        <SheetContent width="narrow">
          <SheetTitle>Create Agent</SheetTitle>
          <SheetDescription>Enter a name for the new agent.</SheetDescription>
          <div className="flex flex-col gap-4 pt-4">
            <Input
              placeholder="Agent name"
              value={name}
              onChange={(e) => setName((e.target as HTMLInputElement).value)}
            />
            <Button disabled={creating || !name.trim()} onClick={handleCreate}>
              {creating ? "Creating..." : "Create"}
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      <Dialog
        open={tokenOpen}
        onOpenChange={(next) => !next && setTokenOpen(false)}
      >
        <DialogContent className="max-w-md">
          <DialogTitle>Agent Token</DialogTitle>
          <DialogDescription>
            Copy this token now. It won't be shown again.
          </DialogDescription>
          <div className="mt-4 rounded bg-overlay p-3 font-mono text-xs break-all">
            {token}
          </div>
          <Button
            variant="outline"
            className="mt-2"
            onClick={() => {
              if (token) navigator.clipboard.writeText(token);
            }}
          >
            Copy
          </Button>
        </DialogContent>
      </Dialog>

      <Dialog
        open={detailOpen}
        onOpenChange={(next) => !next && setDetailOpen(false)}
      >
        <DialogContent className="max-w-md">
          <DialogTitle>Agent Details</DialogTitle>
          {selectedAgent && (
            <div className="flex flex-col gap-2 text-sm mt-2">
              <p>
                <span className="text-control-light">Name:</span>{" "}
                {selectedAgent.title}
              </p>
              <p>
                <span className="text-control-light">Status:</span>{" "}
                <ConnectionBadge state={selectedAgent.status?.state} />
              </p>
              {selectedAgent.info && (
                <>
                  {selectedAgent.info.hostname && (
                    <p>
                      <span className="text-control-light">Hostname:</span>{" "}
                      {selectedAgent.info.hostname}
                    </p>
                  )}
                  {selectedAgent.info.os && (
                    <p>
                      <span className="text-control-light">OS:</span>{" "}
                      {selectedAgent.info.os}/{selectedAgent.info.arch}
                    </p>
                  )}
                  {selectedAgent.info.ip && (
                    <p>
                      <span className="text-control-light">IP:</span>{" "}
                      {selectedAgent.info.ip}
                    </p>
                  )}
                  {selectedAgent.info.version && (
                    <p>
                      <span className="text-control-light">Version:</span>{" "}
                      {selectedAgent.info.version}
                    </p>
                  )}
                </>
              )}
              {selectedAgent.status?.connectedTime && (
                <p>
                  <span className="text-control-light">Connected:</span>{" "}
                  {formatTimestamp(selectedAgent.status.connectedTime)}
                </p>
              )}
              {selectedAgent.status?.lastHeartbeatTime && (
                <p>
                  <span className="text-control-light">Last Heartbeat:</span>{" "}
                  {formatTimestamp(selectedAgent.status.lastHeartbeatTime)}
                </p>
              )}
              {selectedAgent.createdAt && (
                <p>
                  <span className="text-control-light">Created:</span>{" "}
                  {formatTimestamp(selectedAgent.createdAt)}
                </p>
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>

      {loading ? (
        <p className="text-control-light">Loading...</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Hostname</TableHead>
              <TableHead>OS</TableHead>
              <TableHead>IP</TableHead>
              <TableHead>Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className="text-center text-control-light"
                >
                  No agents yet.
                </TableCell>
              </TableRow>
            ) : (
              agents.map((agent) => (
                <TableRow
                  key={agent.name}
                  className="cursor-pointer"
                  onClick={() => handleRowClick(agent)}
                >
                  <TableCell>{agent.title}</TableCell>
                  <TableCell>
                    <ConnectionBadge state={agent.status?.state} />
                  </TableCell>
                  <TableCell>{agent.info?.hostname ?? "-"}</TableCell>
                  <TableCell>
                    {agent.info?.os
                      ? `${agent.info.os}/${agent.info.arch ?? ""}`
                      : "-"}
                  </TableCell>
                  <TableCell>{agent.info?.ip ?? "-"}</TableCell>
                  <TableCell>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        if (window.confirm(`Delete agent ${agent.title}?`)) {
                          handleDelete(agent.name);
                        }
                      }}
                    >
                      Delete
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function ConnectionBadge({ state }: { state?: AgentStatus_ConnectionState }) {
  switch (state) {
    case AgentStatus_ConnectionState.ONLINE:
      return <Badge variant="success">Online</Badge>;
    case AgentStatus_ConnectionState.ERROR:
      return <Badge variant="destructive">Error</Badge>;
    default:
      return <Badge variant="secondary">Offline</Badge>;
  }
}

function formatTimestamp(ts: Timestamp): string {
  return new Date(Number(ts)).toLocaleString();
}
