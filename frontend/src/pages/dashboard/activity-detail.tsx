import { MessageSquareOff } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocation, useNavigate, useParams } from "react-router-dom";
import { EmptyState } from "@/components/chat/states";
import { ThreadPanel } from "@/components/chat/thread-panel";
import { Button } from "@/components/ui/button";
import { commandServiceClient } from "@/connect";
import { ChannelConversationView } from "@/pages/dashboard/chat-conversation";
import { useAppStore } from "@/stores";
import type { Activity, Conversation } from "@/types/proto-es/v1/command_pb";

// ActivityDetail is the right pane of the Activity page. It locates the
// selected activity's message and embeds the full view inline rather than
// navigating away:
//  - a thread-rooted item (task / reminder / thread reply / in-thread mention)
//    embeds ThreadPanel (fluid, with its reply composer and its own
//    "View in channel" header button).
//  - a top-level item (a mention or a task/reminder root posted at the channel
//    level) embeds ChannelConversationView read-only, scrolled to the message.
// Both embedded views expose a "View in channel" affordance that drops the user
// into the live channel for full interaction.
//
// The activity is read from router state (passed by the list when a row is
// clicked) so the pane renders even if the row has since dropped out of the
// filtered list; the store.activities list is a fallback for a direct load or
// a page refresh where the state is lost.
export function ActivityDetail() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { messageId } = useParams<{ messageId: string }>();
  const location = useLocation();

  const activities = useAppStore((s) => s.activities);
  const channels = useAppStore((s) => s.channels);
  const openThread = useAppStore((s) => s.openThread);
  const closeThread = useAppStore((s) => s.closeThread);
  const markConversationRead = useAppStore((s) => s.markConversationRead);
  const openFilePreview = useAppStore((s) => s.openFilePreview);
  const openImagePreview = useAppStore((s) => s.openImagePreview);

  const stateActivity = (location.state as { activity?: Activity } | null)
    ?.activity;
  const activity =
    stateActivity ??
    activities.find((a) => a.name.endsWith(`/${messageId ?? ""}`));

  // Conversation title: prefer the left-rail channel list; fall back to a
  // GetChannel fetch for conversations the user isn't a member of (e.g. an
  // admin viewing an agent-DM they were @mentioned in).
  const convId = activity ? (activity.conversation.split("/")[1] ?? "") : "";
  const convName = `conversations/${convId}`;
  const [fetchedChannel, setFetchedChannel] = useState<Conversation | null>(
    null
  );
  useEffect(() => {
    if (!convId) return;
    if (channels.some((c) => c.name === convName)) {
      setFetchedChannel(null);
      return;
    }
    let cancelled = false;
    commandServiceClient
      .getChannel({ name: convName })
      .then((res) => {
        if (!cancelled) setFetchedChannel(res);
      })
      .catch(() => {
        if (!cancelled) setFetchedChannel(null);
      });
    return () => {
      cancelled = true;
    };
  }, [convId, convName, channels]);
  const channel = channels.find((c) => c.name === convName) ?? fetchedChannel;
  const channelTitle = channel?.title ?? convId;

  const rootId = activity?.threadRoot
    ? (activity.threadRoot.split("/").pop() ?? "")
    : "";
  const msgId = activity?.message.split("/").pop() ?? "";

  // Open the activity's thread so ThreadPanel has messages to render (it reads
  // threadByRoot[rootId], populated by openThread which also starts polling).
  // Closed on unmount or when a different thread's activity is opened. Depends
  // only on the root id so a background activities re-fetch never reopens it.
  useEffect(() => {
    if (!convId || !rootId) return;
    openThread(`conversations/${convId}`, rootId);
    return () => closeThread();
  }, [convId, rootId, openThread, closeThread]);

  // Opening an activity marks its conversation read (UNREAD→READ), matching the
  // product contract: a viewed activity leaves the Unread filter but stays in
  // All until explicitly Marked Done. Read sync is per-conversation (advancing
  // user_channel_cursor), so reading a thread activity marks the channel's
  // activity at or below the cursor read — there is no per-thread read row.
  // Top-level items get this from ChannelConversationView's own init; thread
  // items (ThreadPanel has no read-mark of its own) need it here.
  useEffect(() => {
    if (!convId || !rootId) return;
    markConversationRead(convId);
  }, [convId, rootId, markConversationRead]);

  if (!activity) {
    return (
      <EmptyState
        icon={MessageSquareOff}
        message={t("activity.not-found")}
        className="h-full"
        action={
          <Button
            variant="outline"
            size="sm"
            onClick={() => navigate("/activity")}
          >
            {t("common.back")}
          </Button>
        }
      />
    );
  }

  const viewInChannel = () => {
    // Drop into the live channel. For a thread item, deep-link to the thread so
    // the channel page opens it; for a top-level item, land on the channel.
    if (rootId) navigate(`/${convId}?thread=${rootId}`);
    else navigate(`/${convId}`);
  };

  if (rootId) {
    return (
      <ThreadPanel
        fluid
        channelId={convId}
        channelTitle={channelTitle}
        rootMessageId={rootId}
        onClose={() => navigate("/activity")}
        onViewInChannel={viewInChannel}
        scrollToMessageId={msgId}
        onPreviewAttachment={(attachment, rootMessageId) =>
          openFilePreview(convName, rootMessageId, attachment)
        }
        onJumpToSection={(attachment, sectionId, rootMessageId) =>
          openFilePreview(
            convName,
            rootMessageId,
            attachment,
            sectionId,
            attachment.quotedText
          )
        }
        onPreviewImage={openImagePreview}
      />
    );
  }

  // Top-level message: embed the full channel view, writable (mirroring
  // task/reminder — the user replies inline without leaving Activity). The
  // scroll target depends on the conversation:
  //  - A DM (type 1 user↔agent or type 4 user↔user) folds plain top-level
  //    messages into one activity row per chat; scroll to the user's last-read
  //    position (channel.readVersion) so they resume reading where they left
  //    off, not at the latest message. (Task/reminder/thread items carry a
  //    thread_root and take the ThreadPanel branch above instead.)
  //  - A channel (type 2) top-level mention scrolls to the mentioning message
  //    (the precise pointer the user was @mentioned at).
  // channel already falls back to fetchedChannel (see its declaration above),
  // so a DM the user isn't a left-rail member of still resolves here.
  const isDM = channel?.type === 1 || channel?.type === 4;
  const readVersion = channel?.readVersion ?? 0n;
  return (
    <ChannelConversationView
      conversationId={convId}
      scrollToMessageId={isDM ? undefined : msgId}
      scrollToReadVersion={isDM ? readVersion : undefined}
      onViewInChannel={viewInChannel}
    />
  );
}
