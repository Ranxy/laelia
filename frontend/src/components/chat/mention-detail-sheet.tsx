import { useEffect, useState } from "react";
import { ConnectionBadge } from "@/components/connection-badge";
import { Badge } from "@/components/ui/badge";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from "@/components/ui/sheet";
import { agentServiceClient, userServiceClient } from "@/connect";
import type { Agent } from "@/types/proto-es/v1/agent_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

interface MentionDetailSheetProps {
  open: boolean;
  type: "user" | "agent";
  id: string;
  name: string;
  onClose: () => void;
}

export function MentionDetailSheet({
  open,
  type,
  id,
  name,
  onClose,
}: MentionDetailSheetProps) {
  const [agent, setAgent] = useState<Agent | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open) return;

    setLoading(true);
    if (type === "agent") {
      agentServiceClient
        .getAgent({ name: id })
        .then(setAgent)
        .catch(() => setAgent(null))
        .finally(() => setLoading(false));
    } else {
      userServiceClient
        .getUser({ name: `users/${id}` })
        .then(setUser)
        .catch(() => setUser(null))
        .finally(() => setLoading(false));
    }
  }, [open, type, id]);

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width="narrow">
        <SheetTitle>
          {type === "agent" ? "Agent Details" : "User Details"}
        </SheetTitle>
        <SheetDescription className="sr-only">{name}</SheetDescription>

        <div className="flex flex-col gap-2 text-sm mt-2">
          <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
            <span className="text-control-light whitespace-nowrap">Name</span>
            <span>{name}</span>

            <span className="text-control-light whitespace-nowrap">Type</span>
            <span>
              <Badge variant="secondary">
                {type === "agent" ? "Agent" : "User"}
              </Badge>
            </span>

            {type === "agent" && agent && (
              <>
                <span className="text-control-light whitespace-nowrap">
                  Status
                </span>
                <span>
                  <ConnectionBadge state={agent.status?.state} />
                </span>
                {agent.info?.hostname && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      Hostname
                    </span>
                    <span>{agent.info.hostname}</span>
                  </>
                )}
                {agent.info?.os && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      OS
                    </span>
                    <span>
                      {agent.info.os}
                      {agent.info.arch ? ` / ${agent.info.arch}` : ""}
                    </span>
                  </>
                )}
                {agent.info?.ip && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      IP
                    </span>
                    <span>{agent.info.ip}</span>
                  </>
                )}
                {agent.info?.version && (
                  <>
                    <span className="text-control-light whitespace-nowrap">
                      Version
                    </span>
                    <span>{agent.info.version}</span>
                  </>
                )}
              </>
            )}

            {type === "user" && user && (
              <>
                <span className="text-control-light whitespace-nowrap">
                  Email
                </span>
                <span>{user.email || "-"}</span>
                <span className="text-control-light whitespace-nowrap">
                  Title
                </span>
                <span>{user.title || "-"}</span>
              </>
            )}

            {loading && (
              <div className="col-span-2 text-xs text-control-placeholder">
                Loading...
              </div>
            )}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
