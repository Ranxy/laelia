import { ArrowLeft, Hash, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ChannelMembersPanel } from "@/components/chat/channel-members-panel";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { commandServiceClient } from "@/connect";
import { useAppStore } from "@/stores";
import type { Conversation } from "@/types/proto-es/v1/command_pb";

// ChannelDetailPage is the right-pane detail view for a channel opened from
// the Members directory's Channels roster. It shows the channel's metadata
// (member count, owner, the viewer's join time), flags closed channels with a
// badge, and offers an "Open" action that reopens a closed channel and jumps
// into the chat. Membership management (add/remove) reuses the same
// ChannelMembersPanel as the chat page's members sheet.
export function ChannelDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { channelId } = useParams<{ channelId: string }>();
  const currentUser = useAppStore((s) => s.currentUser);
  const setConversationClosed = useAppStore((s) => s.setConversationClosed);
  const myChannels = useAppStore((s) => s.myChannels);
  const conversationName = `conversations/${channelId ?? ""}`;

  // GetChannel enriches the roster entry with the viewer's joined_at (and is
  // the source of truth on a deep link when the roster hasn't loaded yet).
  const [channel, setChannel] = useState<Conversation | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!channelId) return;
    let cancelled = false;
    setLoading(true);
    commandServiceClient
      .getChannel({ name: conversationName })
      .then((res) => {
        if (cancelled) return;
        setChannel(res);
        setLoading(false);
      })
      .catch(() => {
        if (cancelled) return;
        setChannel(null);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [channelId, conversationName]);

  const rosterChannel =
    myChannels.find((c) => c.name === conversationName) ?? null;
  const conv = channel ?? rosterChannel;
  const isOwner =
    !!conv &&
    !!currentUser?.name &&
    conv.ownerId === currentUser.name.split("/").pop();

  const handleOpen = async () => {
    if (!channelId) return;
    // Reopen restores the row to the left rail (setConversationClosed clears
    // the closed flag and refetches the list), then jump straight into chat.
    try {
      await setConversationClosed(channelId, false);
    } finally {
      navigate(`/chat/${channelId}`);
    }
  };

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-4 py-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/members")}
          aria-label={t("common.back")}
          className="size-7 p-0"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="min-w-0 flex-1 truncate text-sm font-semibold text-main">
          {conv?.title ?? channelId ?? ""}
        </h1>
        {conv?.closed && (
          <Badge variant="secondary" className="text-xs shrink-0">
            {t("channel.closed")}
          </Badge>
        )}
        {conv?.closed && (
          <Button variant="default" size="sm" onClick={() => void handleOpen()}>
            {t("channel.open")}
          </Button>
        )}
      </div>

      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <LoadingState />
        ) : !conv ? (
          <EmptyState icon={Hash} message={t("members.channel-not-found")} />
        ) : (
          <div className="flex flex-col gap-4 p-4">
            {/* Channel metadata */}
            <div className="flex flex-col gap-2 rounded-xs border border-control-border p-4">
              <div className="flex items-center gap-2 text-sm font-semibold text-main">
                <span className="flex size-7 items-center justify-center rounded-md bg-control-bg text-control">
                  <Hash className="size-3.5" />
                </span>
                {conv.title}
              </div>
              <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-control-light">
                <span className="flex items-center gap-1">
                  <Users className="size-3.5" />
                  {t("channel.members", { count: conv.memberCount ?? 0 })}
                </span>
                {conv.ownerName && (
                  <span>{t("channel.owner", { name: conv.ownerName })}</span>
                )}
                {conv.joinedAt && (
                  <span>
                    {t("channel.joined-at", {
                      date: new Date(
                        Number(conv.joinedAt.seconds) * 1000
                      ).toLocaleDateString(),
                    })}
                  </span>
                )}
              </div>
            </div>

            {/* Membership — shared panel: roster + role badges + join times,
                and add/remove controls for the channel owner. */}
            <div className="flex flex-col gap-2">
              <h2 className="text-xs font-bold uppercase tracking-widest text-control">
                {t("channel.members-heading", { count: conv.memberCount ?? 0 })}
              </h2>
              <ChannelMembersPanel
                conversationId={channelId ?? ""}
                canManage={isOwner}
                membershipFixed={false}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
