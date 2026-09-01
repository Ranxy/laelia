import { create } from "@bufbuild/protobuf";
import { MessageSquare, Send, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { Button } from "@/components/ui/button";
import { SidePanel } from "@/components/ui/side-panel";
import { Textarea } from "@/components/ui/textarea";
import { useAvatar } from "@/lib/avatar-cache";
import type { CommentAnchor } from "@/lib/markdown-file";
import { avatarNameForAgentId, avatarNameForUserId } from "@/lib/resource";
import { useAppStore } from "@/stores";
import { isOwnUserMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/ui-models";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";
import { AttachmentCommentCard } from "./attachment-comment-card";

// usePreviewComments loads the previewed file's thread and returns the
// section-anchored comments on this file (replies whose attachment carries a
// sectionAnchor referencing the same file id). The html overlay reuses the
// same snapshot to draw anchor pins over the iframe.
export function usePreviewComments(
  conversation: string,
  rootMessageId: string,
  attachmentId: string,
  enabled: boolean
): ChatMessageUI[] {
  const thread = useAppStore((s) => s.threadByRoot[rootMessageId]);
  const loadThreadMessages = useAppStore((s) => s.loadThreadMessages);

  useEffect(() => {
    if (enabled) loadThreadMessages(conversation, rootMessageId);
  }, [conversation, rootMessageId, loadThreadMessages, enabled]);

  return useMemo(
    () =>
      (thread?.messages ?? []).filter((m) =>
        m.attachments?.some(
          (a) => a.sectionAnchor !== "" && a.id === attachmentId
        )
      ),
    [thread?.messages, attachmentId]
  );
}

// CommentsPanel is the right-side comment panel shared by the markdown and
// html preview overlays (formerly two line-for-line twins). It renders only:
// the comment list, the composer, and one pending-anchor chip. Everything
// viewer-specific is injected:
//   - `pendingAnchor` is controlled: the markdown aside derives it from DOM
//     text selection in the rendered document, the html overlay from the
//     bridge-reported iframe selection (the sandboxed document cannot be
//     inspected from the parent).
//   - `onJump(sectionId, quote)` is the locator seam: markdown locates by
//     DOM scrollIntoView, html locates through the bridge.
//   - `jumpRequiresQuote` keeps each viewer's jump affordance byte-identical:
//     html needs the quoted text for bridge locate, so the comment card's
//     anchor chip stays disabled without one; markdown jumps on the section
//     id alone.
export function CommentsPanel({
  conversationId,
  rootMessageId,
  attachment,
  comments,
  pendingAnchor,
  onSetPendingAnchor,
  focusKey = 0,
  onJump,
  jumpRequiresQuote = true,
}: {
  conversationId: string; // bare id
  rootMessageId: string;
  attachment: Attachment;
  comments: ChatMessageUI[];
  pendingAnchor: CommentAnchor | null;
  onSetPendingAnchor: (anchor: CommentAnchor | null) => void;
  focusKey?: number;
  onJump: (sectionId: string, quote: string) => void;
  jumpRequiresQuote?: boolean;
}) {
  const { t } = useTranslation();
  const sendThreadMessage = useAppStore((s) => s.sendThreadMessage);
  // The {user} segment of the current user's "users/{user}" name is the
  // principal id used to tell their own comments from other users' comments.
  const currentPrincipalId = useAppStore((s) => s.currentUser?.handle);
  // Per-user chat keybinding (see chat-conversation.tsx for rationale).
  const enterToSend = useAppStore(
    (s) => s.currentUser?.chatPreferences?.enterToSend ?? true
  );

  const [body, setBody] = useState("");
  const [sending, setSending] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Bumping focusKey (e.g. from the floating "add comment" button over the
  // iframe) moves focus into the composer.
  useEffect(() => {
    if (focusKey > 0) textareaRef.current?.focus();
  }, [focusKey]);

  async function handleSend() {
    const text = body.trim();
    if (!text || !pendingAnchor || sending) return;
    setSending(true);
    try {
      await sendThreadMessage(
        conversationId,
        rootMessageId,
        text,
        [],
        [
          create(AttachmentSchema, {
            id: attachment.id,
            name: attachment.name,
            mimeType: attachment.mimeType,
            sizeBytes: attachment.sizeBytes,
            sectionAnchor: pendingAnchor.sectionAnchor,
            sectionId: pendingAnchor.sectionId,
            quotedText: pendingAnchor.quotedText,
          }),
        ]
      );
      setBody("");
      onSetPendingAnchor(null);
    } catch (err) {
      console.error("comment send failed", err);
    } finally {
      setSending(false);
    }
  }

  const canSend = body.trim().length > 0 && pendingAnchor !== null && !sending;

  const footerContent = (
    <>
      {pendingAnchor && (
        <div className="mb-2 flex items-start gap-1.5 rounded-md border border-control-border bg-control-bg/40 p-1.5 text-[11px]">
          <div className="min-w-0 flex-1">
            <div className="truncate font-medium text-main">
              {pendingAnchor.sectionAnchor}
            </div>
            <div className="line-clamp-2 italic text-control-light">
              {pendingAnchor.quotedText}
            </div>
          </div>
          <button
            type="button"
            onClick={() => onSetPendingAnchor(null)}
            aria-label={t("common.close")}
            className="shrink-0 text-control-placeholder hover:text-main"
          >
            <X className="size-3" />
          </button>
        </div>
      )}
      <div className="rounded-lg border border-control-border bg-control-bg/40 focus-within:border-accent">
        <Textarea
          ref={textareaRef}
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder={t("preview.comments-placeholder", {
            name: attachment.name,
          })}
          rows={2}
          className="max-h-32 min-h-10 resize-none border-0 bg-transparent text-sm focus-visible:ring-0"
          onKeyDown={(e) => {
            if (e.nativeEvent.isComposing) return;
            if (e.key !== "Enter") return;
            const wantSend = enterToSend ? !e.shiftKey : e.shiftKey;
            if (wantSend) {
              e.preventDefault();
              handleSend();
            }
          }}
        />
        <div className="flex items-center justify-between gap-2 px-1.5 pb-1.5">
          <span className="text-[10px] text-control-placeholder">
            {pendingAnchor
              ? t(
                  enterToSend
                    ? "preview.comments-ready"
                    : "preview.comments-ready-inverted"
                )
              : t("preview.comments-select-hint")}
          </span>
          <Button
            type="button"
            size="sm"
            onClick={handleSend}
            disabled={!canSend}
            className="flex size-7 items-center justify-center p-0"
            aria-label={t("preview.comments-send")}
          >
            <Send className="size-3.5" />
          </Button>
        </div>
      </div>
    </>
  );

  return (
    <SidePanel
      label={t("preview.comments")}
      icon={<MessageSquare className="size-3.5 shrink-0 text-control-light" />}
      title={
        <>
          {t("preview.comments")} · {attachment.name}
        </>
      }
      className="w-80 shrink-0"
      footer={<div className="p-2">{footerContent}</div>}
    >
      <div className="px-3 py-2">
        {comments.length === 0 && (
          <p className="text-xs text-control-placeholder">
            {t("preview.comments-empty")}
          </p>
        )}
        <ul className="flex flex-col gap-3">
          {comments.map((m) => (
            <CommentRow
              key={m.id}
              msg={m}
              attachmentId={attachment.id}
              currentPrincipalId={currentPrincipalId}
              onJump={onJump}
              jumpRequiresQuote={jumpRequiresQuote}
            />
          ))}
        </ul>
      </div>
    </SidePanel>
  );
}

function CommentRow({
  msg,
  attachmentId,
  currentPrincipalId,
  onJump,
  jumpRequiresQuote,
}: {
  msg: ChatMessageUI;
  attachmentId: string;
  currentPrincipalId?: string;
  onJump: (sectionId: string, quote: string) => void;
  jumpRequiresQuote: boolean;
}) {
  const { t, i18n } = useTranslation();
  const att = msg.attachments?.find(
    (a) => a.sectionAnchor !== "" && a.id === attachmentId
  );
  const isUser = msg.role === "user";
  const isOwnUser = isOwnUserMessage(msg, currentPrincipalId);
  const avatarSeed = isUser
    ? msg.principalId || currentPrincipalId || ""
    : msg.agentId || msg.senderName || "agent";
  const avatarName = isUser
    ? msg.principalId || currentPrincipalId
      ? avatarNameForUserId(msg.principalId || currentPrincipalId || "")
      : undefined
    : msg.agentId
      ? avatarNameForAgentId(msg.agentId)
      : undefined;
  // Hooks must run before the `!att` early return below so the hook order is
  // stable across renders regardless of the attachment's presence.
  const avatarSrc = useAvatar(avatarName);
  if (!att) return null;
  const quote = att.quotedText;
  const jump =
    quote || !jumpRequiresQuote
      ? (sectionId: string) => onJump(sectionId, quote)
      : undefined;
  return (
    <li className="flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <Avatar
          seed={avatarSeed}
          src={avatarSrc}
          accent={isUser ? isOwnUser : false}
        />
        <span className="min-w-0 flex-1 truncate text-[11px] font-medium text-main">
          {isUser
            ? isOwnUser
              ? t("chat.you")
              : (msg.senderName ?? t("chat.you"))
            : (msg.senderName ?? t("chat.agent"))}
        </span>
        <span className="shrink-0 text-[10px] text-control-placeholder">
          {formatTime(msg.timestamp, i18n.language)}
        </span>
      </div>
      <AttachmentCommentCard
        attachment={att}
        variant="compact"
        body={msg.content}
        onJumpToSection={jump}
      />
    </li>
  );
}
