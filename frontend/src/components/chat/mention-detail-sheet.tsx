import { Bot, Loader2, User as UserIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ConnectionBadge } from "@/components/connection-badge";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
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

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-baseline gap-3 py-1.5">
      <span className="text-xs font-semibold uppercase tracking-wide text-control w-24 shrink-0">
        {label}
      </span>
      <span className="text-sm text-main">{children}</span>
    </div>
  );
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

  const entityLabel = type === "agent" ? "Agent" : "User";

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width="medium">
        <SheetHeader>
          <SheetTitle>
            {type === "agent" ? "Agent Details" : "User Details"}
          </SheetTitle>
          <SheetDescription className="sr-only">{name}</SheetDescription>
        </SheetHeader>
        <SheetBody>
          <div className="flex flex-col gap-5">
            {/* Header card */}
            <div className="flex items-center gap-3 rounded-xs border border-control-border bg-control-bg/50 p-3">
              <div className="flex size-9 items-center justify-center rounded-full bg-accent/10 text-accent">
                {type === "agent" ? (
                  <Bot className="size-4.5" />
                ) : (
                  <UserIcon className="size-4.5" />
                )}
              </div>
              <div className="flex flex-col gap-0.5 min-w-0">
                <span className="text-sm font-medium text-main truncate">
                  {name}
                </span>
                <span className="text-xs text-control-light">
                  {entityLabel}
                </span>
              </div>
            </div>

            {loading ? (
              <div className="flex items-center justify-center gap-2 py-10 text-control-light text-sm">
                <Loader2 className="size-4 animate-spin" />
                Loading...
              </div>
            ) : (
              <div className="rounded-xs border border-control-border bg-background p-3">
                <div className="flex flex-col divide-y divide-control-border/50">
                  <DetailRow label="Name">{name}</DetailRow>
                  <DetailRow label="Type">{entityLabel}</DetailRow>

                  {type === "agent" && agent && (
                    <>
                      <DetailRow label="Status">
                        <ConnectionBadge state={agent.status?.state} />
                      </DetailRow>
                      {agent.info?.hostname && (
                        <DetailRow label="Hostname">
                          {agent.info.hostname}
                        </DetailRow>
                      )}
                      {agent.info?.os && (
                        <DetailRow label="OS">
                          {agent.info.os}
                          {agent.info.arch ? ` / ${agent.info.arch}` : ""}
                        </DetailRow>
                      )}
                      {agent.info?.ip && (
                        <DetailRow label="IP">{agent.info.ip}</DetailRow>
                      )}
                      {agent.info?.version && (
                        <DetailRow label="Version">
                          {agent.info.version}
                        </DetailRow>
                      )}
                      {agent.info?.acpConfig?.personaPrompt && (
                        <DetailRow label="Persona">
                          <span className="whitespace-pre-wrap">
                            {agent.info.acpConfig.personaPrompt}
                          </span>
                        </DetailRow>
                      )}
                    </>
                  )}

                  {type === "user" && user && (
                    <>
                      <DetailRow label="Email">{user.email || "-"}</DetailRow>
                      <DetailRow label="Title">{user.title || "-"}</DetailRow>
                      {user.description && (
                        <DetailRow label="Description">
                          <span className="whitespace-pre-wrap">
                            {user.description}
                          </span>
                        </DetailRow>
                      )}
                    </>
                  )}

                  {!loading && !agent && !user && (
                    <div className="py-6 text-center text-sm text-control-light">
                      Failed to load details
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
