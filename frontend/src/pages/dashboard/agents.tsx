import { Trash } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Outlet, useNavigate, useParams } from "react-router-dom";
import { ConnectionBadge } from "@/components/connection-badge";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/components/ui/sheet";
import { buildAgentRunCommand } from "@/lib/agent-token";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import {
  type Agent,
  AgentStatus_ConnectionState,
} from "@/types/proto-es/v1/agent_pb";

type Lifecycle =
  | "waiting-connection"
  | "pending-config"
  | "ready"
  | "configured-offline";

// agentLifecycle classifies an agent's operational state for both the left-rail
// polling loop (we keep refreshing while any agent is non-ready) and the profile
// tab's lifecycle label. Exported so agent-profile can render the same label.
export function agentLifecycle(agent: Agent): Lifecycle {
  const online = agent.status?.state === AgentStatus_ConnectionState.ONLINE;
  const cfg = agent.info?.acpConfig;
  // An agent is "configured" when it has either a selected provider or a
  // custom executable. A built-in provider derives its command from the
  // registry, so executable is empty for it.
  const configured = !!cfg?.provider || !!cfg?.executable;
  if (online && configured) return "ready";
  if (online && !configured) return "pending-config";
  if (!online && configured) return "configured-offline";
  return "waiting-connection";
}

export function lifecycleLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  state: Lifecycle
): string {
  return t(`agent.lifecycle.${state}`);
}

export function AgentsPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId: selectedAgentId } = useParams<{ agentId: string }>();
  const fetchAgents = useAppStore((s) => s.fetchAgents);
  const agents = useAppStore((s) => s.agents);
  const loading = useAppStore((s) => s.agentsLoading);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [token, setToken] = useState<string | null>(null);
  const [tokenOpen, setTokenOpen] = useState(false);
  const [tokenFromRotation, setTokenFromRotation] = useState(false);
  const [creating, setCreating] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{
    name: string;
    title: string;
  } | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(() => {
    fetchAgents({ pageSize: 100 });
  }, [fetchAgents]);

  useEffect(() => {
    load();
  }, [load]);

  // Refresh while any agent is not yet ready (waiting for connection or
  // pending configuration), so the list flips to "pending config" / "ready"
  // promptly once the agent connects or gets configured. Silent refreshes skip
  // the loading flag and skip the state update when nothing changed, so polls
  // cause no re-render/flicker unless the data actually changed.
  const anyNonReady = agents.some((a) => agentLifecycle(a) !== "ready");
  useEffect(() => {
    if (!anyNonReady) return;
    const id = setInterval(
      () => fetchAgents({ pageSize: 100 }, { silent: true }),
      3000
    );
    return () => clearInterval(id);
  }, [anyNonReady, fetchAgents]);

  async function handleCreate() {
    if (!name.trim()) return;
    setCreating(true);
    try {
      const createAgent = useAppStore.getState().createAgent;
      const agent = await createAgent(name.trim());
      if (agent.bootstrapToken) {
        setToken(agent.bootstrapToken);
        setTokenFromRotation(false);
        setTokenOpen(true);
      }
      setName("");
      setCreateOpen(false);
      load();
    } finally {
      setCreating(false);
    }
  }

  async function handleConfirmDelete() {
    if (!deleteTarget) return;
    const deleteAgent = useAppStore.getState().deleteAgent;
    setDeleting(true);
    try {
      await deleteAgent(deleteTarget.name);
      setDeleteOpen(false);
      setDeleteTarget(null);
      load();
    } finally {
      setDeleting(false);
    }
  }

  return (
    <div className="flex h-full w-full">
      <aside className="flex w-56 shrink-0 flex-col border-r border-control-border overflow-hidden">
        <div className="flex items-center justify-between gap-2 border-b border-control-border px-3 py-3 shrink-0">
          <h1 className="text-sm font-semibold text-main truncate">
            {t("agent.title")}
          </h1>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            {t("agent.create")}
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto py-1">
          {loading ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("common.loading")}
            </p>
          ) : agents.length === 0 ? (
            <p className="px-3 py-2 text-sm text-control-light">
              {t("common.no-data")}
            </p>
          ) : (
            <ul className="flex flex-col">
              {agents.map((agent) => {
                const resourceId = agent.name.replace(/^agents\//, "");
                const selected = resourceId === selectedAgentId;
                return (
                  <li key={agent.name}>
                    <div
                      role="button"
                      tabIndex={0}
                      aria-label={t("agent.row-open-detail", {
                        title: agent.title,
                      })}
                      className={cn(
                        "group flex cursor-pointer items-center gap-2 px-3 py-2 transition-colors",
                        "border-l-2",
                        selected
                          ? "border-accent bg-control-bg"
                          : "border-transparent hover:bg-control-bg/60"
                      )}
                      onClick={() => navigate(`/agents/${resourceId}`)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          navigate(`/agents/${resourceId}`);
                        }
                      }}
                    >
                      <div className="min-w-0 flex-1 flex flex-col gap-1">
                        <span className="truncate text-sm font-medium text-main">
                          {agent.title}
                        </span>
                        <ConnectionBadge state={agent.status?.state} />
                      </div>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="size-6 shrink-0 p-0 opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
                        aria-label={t("common.delete")}
                        onClick={(e) => {
                          e.stopPropagation();
                          setDeleteTarget({
                            name: agent.name,
                            title: agent.title,
                          });
                          setDeleteOpen(true);
                        }}
                      >
                        <Trash className="size-3.5" />
                      </Button>
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </aside>

      <div className="flex-1 overflow-hidden">
        <Outlet />
      </div>

      <Sheet
        open={createOpen}
        onOpenChange={(next) => !next && setCreateOpen(false)}
      >
        <SheetContent width="narrow">
          <SheetTitle>{t("agent.create-title")}</SheetTitle>
          <SheetDescription>{t("agent.create-description")}</SheetDescription>
          <div className="flex flex-col gap-4 pt-4">
            <Input
              placeholder={t("agent.create-name-placeholder")}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <Button disabled={creating || !name.trim()} onClick={handleCreate}>
              {creating ? t("common.creating") : t("common.create")}
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      <Dialog
        open={tokenOpen}
        onOpenChange={(next) => !next && setTokenOpen(false)}
      >
        <DialogContent className="max-w-lg">
          <DialogTitle>
            {tokenFromRotation
              ? t("agent.rotate-token-success-title")
              : t("agent.created-title")}
          </DialogTitle>
          <DialogDescription>
            {tokenFromRotation
              ? t("agent.rotate-token-success-description")
              : t("agent.created-description")}
          </DialogDescription>
          <div className="mt-4 space-y-3">
            <p className="text-sm text-control-light">
              {t("agent.created-run-hint")}
            </p>
            <div className="rounded bg-white border border-control-border p-3 font-mono text-xs break-all text-black dark:bg-zinc-900 dark:text-white">
              {token && buildAgentRunCommand(token, true)}
            </div>
            <Button
              variant="outline"
              className="w-full"
              onClick={() => {
                if (token) {
                  navigator.clipboard
                    .writeText(buildAgentRunCommand(token, false))
                    .catch(() => {});
                }
              }}
            >
              {t("common.copy")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={deleteOpen}
        onOpenChange={(next) => {
          setDeleteOpen(next);
          if (!next) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>{t("agent.delete-confirm-title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("agent.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={deleting}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={deleting}
              onClick={handleConfirmDelete}
            >
              {deleting ? t("common.saving") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
