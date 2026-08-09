import { create } from "@bufbuild/protobuf";
import { MessageSquare, Send, X } from "lucide-react";
import { RefObject, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, formatTime } from "@/components/chat/avatar";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  avatarNameForAgentId,
  avatarNameForUserId,
  useAvatar,
} from "@/lib/avatar-cache";
import {
  anchorForSelection,
  type CommentAnchor,
  type OutlineItem,
} from "@/lib/markdown-file";
import { useAppStore } from "@/stores";
import { isOwnUserMessage } from "@/stores/chat-helpers";
import type { ChatMessageUI } from "@/stores/types";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";
import { AttachmentCommentCard } from "./attachment-comment-card";

// CommentsAside is the right-side panel inside the markdown preview overlay.
// It lists the section-anchored comments already posted on the previewed
// file's thread, and lets the user select text in the document, write a
// comment, and send it as a thread reply carrying an anchored attachment.
export function CommentsAside({
  conversation,
  conversationId,
  rootMessageId,
  attachment,
  contentRef,
  outline,
  onJumpToSection,
}: {
  conversation: string; // "conversations/{id}"
  conversationId: string; // bare id
  rootMessageId: string;
  attachment: Attachment;
  contentRef: RefObject<HTMLDivElement | null>;
  outline: OutlineItem[];
  onJumpToSection: (sectionId: string) => void;
}) {
  const { t } = useTranslation();
  const thread = useAppStore((s) => s.threadByRoot[rootMessageId]);
  const loadThreadMessages = useAppStore((s) => s.loadThreadMessages);
  const sendThreadMessage = useAppStore((s) => s.sendThreadMessage);
  // The {user} segment of the current user's "users/{user}" name is the
  // principal id used to tell their own comments from other users' comments.
  const currentPrincipalId = useAppStore((s) =>
    s.currentUser?.name.split("/").pop()
  );
  // Per-user chat keybinding (see chat-conversation.tsx for rationale).
  const enterToSend = useAppStore(
    (s) => s.currentUser?.chatPreferences?.enterToSend ?? true
  );

  const [pendingAnchor, setPendingAnchor] = useState<CommentAnchor | null>(
    null
  );
  const [body, setBody] = useState("");
  const [sending, setSending] = useState(false);

  // Load the thread snapshot once (and whenever the root changes) so existing
  // comments are visible. No watcher is started — the sender's own reply is
  // optimistically appended by sendThreadMessage, and reopening reloads.
  useEffect(() => {
    loadThreadMessages(conversation, rootMessageId);
  }, [conversation, rootMessageId, loadThreadMessages]);

  // Capture text selections in the markdown body as a pending comment anchor.
  // Active only while this aside is mounted (i.e. comment mode is on).
  useEffect(() => {
    const onSelection = () => {
      const container = contentRef.current;
      if (!container) return;
      const sel = window.getSelection();
      if (!sel) return;
      const anchor = anchorForSelection(container, sel, outline);
      if (anchor) setPendingAnchor(anchor);
    };
    document.addEventListener("mouseup", onSelection);
    return () => document.removeEventListener("mouseup", onSelection);
  }, [contentRef, outline]);

  const comments = useMemo(() => {
    const msgs = thread?.messages ?? [];
    return msgs.filter((m) =>
      m.attachments?.some(
        (a) => a.sectionAnchor !== "" && a.id === attachment.id
      )
    );
  }, [thread?.messages, attachment.id]);

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
      setPendingAnchor(null);
    } catch (err) {
      console.error("comment send failed", err);
    } finally {
      setSending(false);
    }
  }

  const canSend = body.trim().length > 0 && pendingAnchor !== null && !sending;

  return (
    <aside className="flex w-80 shrink-0 flex-col border-l border-control-border bg-background">
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2">
        <MessageSquare className="size-3.5 shrink-0 text-control-light" />
        <span className="truncate text-xs font-semibold text-main">
          {t("preview.comments")} · {attachment.name}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
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
              onJumpToSection={onJumpToSection}
            />
          ))}
        </ul>
      </div>

      <div className="shrink-0 border-t border-control-border p-2">
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
              onClick={() => setPendingAnchor(null)}
              aria-label={t("common.close")}
              className="shrink-0 text-control-placeholder hover:text-main"
            >
              <X className="size-3" />
            </button>
          </div>
        )}
        <div className="rounded-lg border border-control-border bg-control-bg/40 focus-within:border-accent">
          <Textarea
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
      </div>
    </aside>
  );
}

function CommentRow({
  msg,
  attachmentId,
  currentPrincipalId,
  onJumpToSection,
}: {
  msg: ChatMessageUI;
  attachmentId: string;
  currentPrincipalId?: string;
  onJumpToSection: (sectionId: string) => void;
}) {
  const { t, i18n } = useTranslation();
  const att = msg.attachments?.find(
    (a) => a.sectionAnchor !== "" && a.id === attachmentId
  );
  if (!att) return null;
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
  const avatarSrc = useAvatar(avatarName);
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
        onJumpToSection={onJumpToSection}
      />
    </li>
  );
}
