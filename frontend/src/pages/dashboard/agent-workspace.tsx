import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { WorkspaceFilePanel } from "@/components/workspace/workspace-file-panel";
import { WorkspaceTree } from "@/components/workspace/workspace-tree";
import { AGENT_ROUTE_PROFILE } from "@/router/handles";
import { resolvePath } from "@/router/route-index";
import { useAppStore } from "@/stores";
import type { Agent, WorkspaceEntry } from "@/types/proto-es/v1/agent_pb";

// AgentWorkspacePage is the workspace browser tab of the agent detail pane.
// Like the tab in the layout, it re-checks canEdit (owner/admin only) and
// redirects to the profile when the caller lacks access, so a deep link to
// /members/agents/:id/workspace cannot bypass the tab gate. The page is a
// two-pane layout: the file tree on the left, the selected file's content on
// the right.
export function AgentWorkspacePage() {
  const navigate = useNavigate();
  const { agentId } = useParams<{ agentId: string }>();
  const getAgent = useAppStore((s) => s.getAgent);
  const [agent, setAgent] = useState<Agent | undefined>(undefined);
  const [checking, setChecking] = useState(true);
  const [selectedEntry, setSelectedEntry] = useState<WorkspaceEntry | null>(
    null
  );

  useEffect(() => {
    let cancelled = false;
    if (!agentId) return;
    getAgent(`agents/${agentId}`).then((a) => {
      if (cancelled) return;
      setAgent(a);
      setChecking(false);
      if (!a?.canEdit) {
        navigate(resolvePath(AGENT_ROUTE_PROFILE, { agentId }), {
          replace: true,
        });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [agentId, getAgent, navigate]);

  if (checking || !agent?.canEdit) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="size-5 animate-spin text-control-light" />
      </div>
    );
  }

  return (
    <div className="flex h-full overflow-hidden">
      <div className="w-72 shrink-0 overflow-hidden border-r border-control-border">
        <WorkspaceTree
          agentName={`agents/${agentId ?? ""}`}
          onPreview={setSelectedEntry}
        />
      </div>
      <div className="min-w-0 flex-1 overflow-hidden">
        <WorkspaceFilePanel
          agentName={`agents/${agentId ?? ""}`}
          entry={selectedEntry}
          onClose={() => setSelectedEntry(null)}
        />
      </div>
    </div>
  );
}
