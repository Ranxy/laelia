import { Send } from "lucide-react";
import { memo, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MessageRow } from "@/components/chat/message-row";
import { EmptyState } from "@/components/chat/states";
import { senderKeyForMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/types";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// ThreadReplies renders the beginning-of-replies divider + the reply list. It
// is memoized so typing a reply (which re-renders the panel's header/composer
// state) does not rebuild every reply row; its props are stable store refs,
// callbacks, and the memoized replies array, so it bails out unless a reply
// actually changed.
export const ThreadReplies = memo(function ThreadReplies({
  replies,
  loading,
  agentTitleFor,
  onViewDetails,
  onPreviewAttachment,
  onJumpToSection,
  onPreviewImage,
  debugMode,
  currentPrincipalId,
  mentionLabel,
  onSenderClick,
  onCopyMarkdown,
  onConvertToTask,
  scrollRoot,
}: {
  replies: ChatMessageUI[];
  loading: boolean;
  agentTitleFor: (msg: ChatMessageUI) => string;
  onViewDetails: (commandId: string, agentId: string) => void;
  onPreviewAttachment?: (attachment: Attachment, rootMessageId: string) => void;
  onJumpToSection?: (
    attachment: Attachment,
    sectionId: string,
    rootMessageId: string
  ) => void;
  onPreviewImage?: (attachment: Attachment) => void;
  debugMode: boolean;
  currentPrincipalId?: string;
  mentionLabel?: (handle: string) => string | undefined;
  onSenderClick?: (type: "user" | "agent", id: string, name: string) => void;
  onCopyMarkdown?: (content: string) => void;
  onConvertToTask?: (msg: ChatMessageUI) => void;
  scrollRoot?: RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation();
  return (
    <>
      {/* Beginning-of-replies divider. */}
      <div className="flex items-center gap-2 py-1">
        <div className="h-px flex-1 bg-control-border" />
        <span className="text-[11px] text-control-light">
          {t("chat.thread-beginning")}
        </span>
        <div className="h-px flex-1 bg-control-border" />
      </div>

      {/* Replies. */}
      {replies.length === 0 && !loading && (
        <EmptyState icon={Send} message={t("chat.thread-empty")} />
      )}
      {replies.map((msg, idx) => {
        const prev = idx > 0 ? replies[idx - 1] : null;
        const showAvatar =
          !prev || senderKeyForMessage(prev) !== senderKeyForMessage(msg);
        return (
          <div key={msg.id} data-msg-id={msg.id}>
            <MessageRow
              msg={msg}
              showAvatar={showAvatar}
              agentTitle={agentTitleFor(msg)}
              onViewDetails={onViewDetails}
              onSenderClick={onSenderClick}
              mentionLabel={mentionLabel}
              MentionBadge={MentionBadge}
              markdownCustomId="thread-chat"
              onPreviewAttachment={onPreviewAttachment}
              onJumpToSection={onJumpToSection}
              onPreviewImage={onPreviewImage}
              debugMode={debugMode}
              currentPrincipalId={currentPrincipalId}
              scrollRoot={scrollRoot}
              onCopyMarkdown={onCopyMarkdown}
              onConvertToTask={onConvertToTask}
              // In a thread, opening a thread from a reply is meaningless, so
              // hide the context-menu "Open thread" entry on every row.
              contextMenuRootOnly
              // Small threads render markdown synchronously to avoid the
              // per-row fallback→swap flash on open; large threads keep the
              // lazy gate so off-screen replies stay cheap.
              eager={replies.length <= 40}
            />
          </div>
        );
      })}
    </>
  );
});
