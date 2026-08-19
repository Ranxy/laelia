import { ExternalLink, Loader2, MessageSquare } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { agentServiceClient, userServiceClient } from "@/connect";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import { agentResourceName } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { useSwipeToCloseSheet } from "@/lib/use-swipe-to-close-sheet";
import { useAppStore } from "@/stores";
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
    <div className="grid grid-cols-[7rem_minmax(0,1fr)] items-start gap-x-4 py-2">
      <dt className="pt-0.5 text-xs font-semibold uppercase tracking-wide text-control">
        {label}
      </dt>
      <dd className="min-w-0 break-words text-sm text-main">{children}</dd>
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
  const { t } = useTranslation();
  const navigate = useNavigate();
  const getOrCreateConversation = useAppStore((s) => s.getOrCreateConversation);
  const fetchChannels = useAppStore((s) => s.fetchChannels);
  const [agent, setAgent] = useState<Agent | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(false);
  const [startingChat, setStartingChat] = useState(false);
  const [popup, setPopup] = useState<HTMLDivElement | null>(null);
  const [overlay, setOverlay] = useState<HTMLDivElement | null>(null);

  // Mobile-only: mirror the thread panel's swipe-back gesture so the sheet can
  // be dragged from the left edge to close, revealing the page underneath.
  useSwipeToCloseSheet({ open, onClose, popup, overlay });

  useEffect(() => {
    if (!open) return;

    setLoading(true);
    if (type === "agent") {
      agentServiceClient
        .getAgent({ name: agentResourceName(id) })
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
  // The mention's `name` is the handle; once the entity loads, prefer its
  // display title (and its canonical handle) so the sheet never shows the
  // handle where a display name belongs.
  const displayName =
    type === "agent" ? (agent?.title ?? name) : (user?.title ?? name);
  const handle =
    type === "agent" ? (agent?.handle ?? id) : (user?.handle ?? id);

  // Avatar: use the entity's uploaded avatar resource name when available,
  // otherwise synthesize the canonical avatar resource name from its id so
  // useAvatar probes the cache and falls back to the pixel identicon.
  const avatarName =
    type === "agent"
      ? agent?.avatar ||
        (agent?.name ? `${agent.name}/avatar` : avatarNameForAgentId(id))
      : user?.avatar ||
        (user?.name ? `${user.name}/avatar` : avatarNameForUserId(id));
  const avatarSrc = useAvatar(avatarName);

  // Opens (or reuses) the user↔agent DM and jumps to the chat surface, the
  // same flow as the agent detail page's "Chat" action.
  async function handleSendMessage() {
    if (startingChat) return;
    setStartingChat(true);
    try {
      const conversation = await getOrCreateConversation(`agents/${id}`);
      await fetchChannels();
      onClose();
      navigate(`/${conversation.split("/").pop()}`);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("chat.open-conversation-failed"),
        description:
          err instanceof Error
            ? err.message
            : t("chat.open-conversation-failed"),
      });
    } finally {
      setStartingChat(false);
    }
  }

  function handleViewDetails() {
    onClose();
    navigate(`/members/agents/${id}`);
  }

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent width="medium" ref={setPopup} overlayRef={setOverlay}>
        <SheetHeader>
          <SheetTitle>
            {type === "agent" ? "Agent Details" : "User Details"}
          </SheetTitle>
          <SheetDescription className="sr-only">{name}</SheetDescription>
        </SheetHeader>
        <SheetBody>
          <div className="flex h-full flex-col gap-5">
            {/* Header card */}
            <div className="flex items-center gap-3 rounded-xs border border-control-border bg-control-bg/50 p-3">
              <Avatar seed={id || handle} src={avatarSrc} />
              <div className="flex flex-col gap-0.5 min-w-0">
                <span className="text-sm font-medium text-main truncate">
                  {displayName}
                </span>
                <span className="text-xs text-control-light">@{handle}</span>
              </div>
            </div>

            {loading ? (
              <div className="flex items-center justify-center gap-2 py-10 text-control-light text-sm">
                <Loader2 className="size-4 animate-spin" />
                Loading...
              </div>
            ) : (
              <div className="rounded-xs border border-control-border bg-background p-3">
                <dl className="divide-y divide-control-border/50">
                  <DetailRow label="Name">{displayName}</DetailRow>
                  <DetailRow label="Handle">@{handle}</DetailRow>
                  <DetailRow label="Type">{entityLabel}</DetailRow>

                  {type === "agent" && agent && (
                    <>
                      <DetailRow label="Status">
                        <ConnectionBadge
                          state={agent.status?.state}
                          enabled={agent.enabled}
                        />
                      </DetailRow>
                      <DetailRow label="Owner">
                        {agent.ownerName || agent.owner || "-"}
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
                      {agent.description && (
                        <DetailRow label="Description">
                          <span className="whitespace-pre-wrap">
                            {agent.description}
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
                </dl>
              </div>
            )}

            {type === "agent" && (
              <div className="mt-auto flex gap-2">
                <Button
                  className="flex-1"
                  onClick={handleSendMessage}
                  disabled={startingChat}
                >
                  {startingChat ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <MessageSquare className="size-4" />
                  )}
                  {t("chat.send-message")}
                </Button>
                <Button
                  variant="outline"
                  className="flex-1"
                  onClick={handleViewDetails}
                >
                  <ExternalLink className="size-4" />
                  {t("chat.view-details")}
                </Button>
              </div>
            )}
          </div>
        </SheetBody>
      </SheetContent>
    </Sheet>
  );
}
