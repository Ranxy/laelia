import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useParams } from "react-router-dom";
import { Card } from "@/components/profile-common";
import { Button } from "@/components/ui/button";
import { agentResourceName } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import { type McpServer, McpServerScope } from "@/types/proto-es/v1/mcp_pb";

function McpServerCheckboxRow({
  server,
  enabled,
  canEdit,
  onToggle,
}: {
  server: McpServer;
  enabled: boolean;
  canEdit: boolean;
  onToggle: () => void;
}) {
  return (
    <label className="flex items-center justify-between gap-2 py-1.5 px-2 rounded-xs hover:bg-control-bg cursor-pointer">
      <span className="flex flex-col gap-0.5 min-w-0">
        <span className="text-sm truncate">{server.title}</span>
        <span className="text-xs text-control-placeholder truncate">
          {server.transport.value?.url ?? ""}
        </span>
      </span>
      <input
        type="checkbox"
        checked={enabled}
        disabled={!canEdit}
        onChange={onToggle}
        className="accent-accent"
      />
    </label>
  );
}

// AgentMcpPage is the MCP features tab of the agent detail pane: it lets the
// caller pick which MCP servers the agent loads at startup. Read-only for
// non-editors (the agent's owner or a workspace admin can edit).
export function AgentMcpPage() {
  const { t } = useTranslation();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const mcpServers = useAppStore((s) => s.mcpServers);
  const fetchMcpServers = useAppStore((s) => s.fetchMcpServers);
  const [agent, setAgent] = useState<Agent | undefined>(undefined);
  const [loadError, setLoadError] = useState(false);
  const [selectedMcpServers, setSelectedMcpServers] = useState<string[]>([]);
  const [mcpSaving, setMcpSaving] = useState(false);
  const workspaceMcpServers = mcpServers.filter(
    (s) => s.scope !== McpServerScope.USER
  );
  const myMcpServers = mcpServers.filter(
    (s) => s.scope === McpServerScope.USER
  );

  const agentName = agentResourceName(agentId);

  async function loadAgent() {
    if (!agentId) return;
    const a = await getAgent(agentName);
    setAgent(a);
    setLoadError(!a);
  }

  useEffect(() => {
    if (!agentId) return;
    void loadAgent();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agentId, agentName, getAgent]);

  // Seed the selection once per agent so the refetch after a save does not
  // clobber in-progress toggles.
  useEffect(() => {
    setSelectedMcpServers(agent?.mcpServers ? [...agent.mcpServers] : []);
  }, [agent?.name]);

  // Load the MCP server roster the caller may use (once) for the picker.
  useEffect(() => {
    if (mcpServers.length === 0) {
      void fetchMcpServers({ pageSize: 100 }, { silent: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function saveMcpServers() {
    if (!canEdit) return;
    setMcpSaving(true);
    try {
      const updateAgentMcpConfig = useAppStore.getState().updateAgentMcpConfig;
      await updateAgentMcpConfig(agentName, selectedMcpServers);
      setAgent(await getAgent(agentName));
      toastManager.add({
        type: "success",
        title: t("agent.mcp-saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("agent.mcp-save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setMcpSaving(false);
    }
  }

  if (!agent) {
    return (
      <div className="h-full overflow-y-auto p-6">
        {loadError ? (
          <p className="text-sm text-control-light">
            {t("agent.profile.load-failed")}
          </p>
        ) : (
          <p className="text-sm text-control-light">{t("common.loading")}</p>
        )}
      </div>
    );
  }

  const canEdit = agent.canEdit;

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">
        <Card
          title={t("agent.mcp-section-title")}
          actions={
            mcpSaving ? (
              <span className="flex items-center gap-1 text-xs text-control-light">
                <Loader2 className="size-3 animate-spin" />
                {t("agent.mcp-saving")}
              </span>
            ) : null
          }
        >
          <div className="flex flex-col gap-3">
            <p className="text-xs text-control-light">
              {t("agent.mcp-section-hint")}
            </p>
            {mcpServers.length === 0 ? (
              <p className="text-xs text-control-placeholder">
                {t("agent.mcp-empty")}
              </p>
            ) : (
              <div className="flex flex-col gap-3">
                {workspaceMcpServers.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-medium text-main">
                      {t("agent.mcp-workspace-section")}
                    </div>
                    {workspaceMcpServers.map((server) => {
                      const enabled = selectedMcpServers.includes(server.name);
                      return (
                        <McpServerCheckboxRow
                          key={server.name}
                          server={server}
                          enabled={enabled}
                          canEdit={canEdit}
                          onToggle={() =>
                            setSelectedMcpServers((prev) =>
                              enabled
                                ? prev.filter((n) => n !== server.name)
                                : [...prev, server.name]
                            )
                          }
                        />
                      );
                    })}
                  </div>
                )}
                {myMcpServers.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    <div className="text-xs font-medium text-main">
                      {t("agent.mcp-my-section")}
                    </div>
                    {myMcpServers.map((server) => {
                      const enabled = selectedMcpServers.includes(server.name);
                      return (
                        <McpServerCheckboxRow
                          key={server.name}
                          server={server}
                          enabled={enabled}
                          canEdit={canEdit}
                          onToggle={() =>
                            setSelectedMcpServers((prev) =>
                              enabled
                                ? prev.filter((n) => n !== server.name)
                                : [...prev, server.name]
                            )
                          }
                        />
                      );
                    })}
                  </div>
                )}
              </div>
            )}
            {canEdit && mcpServers.length > 0 && (
              <div className="flex justify-end">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={mcpSaving}
                  onClick={() => void saveMcpServers()}
                >
                  {t("agent.mcp-save")}
                </Button>
              </div>
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}
