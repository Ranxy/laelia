import { create } from "@bufbuild/protobuf";
import {
  ArrowLeft,
  ExternalLink,
  Loader2,
  Paperclip,
  Send,
  X,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MentionBadge } from "@/components/chat/mention-badge";
import { MentionPopup } from "@/components/chat/mention-popup";
import {
  EMPTY_EVENTS,
  MessageRow,
  rowStreamingProps,
} from "@/components/chat/message-row";
import { RemoteImage } from "@/components/chat/remote-image";
import { EmptyState, LoadingState } from "@/components/chat/states";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { detectMention } from "@/composables/useMentionDetect";
import {
  type MentionTarget,
  targetToMention,
  useMentionTargets,
} from "@/composables/useMentionTargets";
import { commandServiceClient } from "@/connect";
import { getCaretCoordinates } from "@/lib/caret-position";
import { isImageAttachment } from "@/lib/image-file";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { ChatMessageUI } from "@/stores/types";
import type { Attachment } from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

const MENTION_POPUP_ID = "thread-mention-popup";

export interface ThreadPanelProps {
  channelId: string;
  channelTitle: string;
  rootMessageId: string;
  onClose: () => void;
  onViewInChannel: () => void;
  onPreviewAttachment?: (attachment: Attachment, rootMessageId: string) => void;
  onJumpToSection?: (
    attachment: Attachment,
    sectionId: string,
    rootMessageId: string
  ) => void;
  onPreviewImage?: (attachment: Attachment) => void;
  // fluid makes the panel fill its container's width/height instead of the
  // fixed 420px right-dock aside used in the channel page. Used when the
  // panel is embedded standalone (e.g. the reminder detail page).
  fluid?: boolean;
}

export function ThreadPanel({
  channelId,
  channelTitle,
  rootMessageId,
  onClose,
  onViewInChannel,
  onPreviewAttachment,
  onJumpToSection,
  onPreviewImage,
  fluid,
}: ThreadPanelProps) {
  const { t } = useTranslation();
  const conversationName = `conversations/${channelId}`;
  const asideClass = fluid
    ? "flex h-full w-full flex-col"
    : "flex w-[420px] shrink-0 flex-col border-l border-control-border";

  const thread = useAppStore((s) => s.threadByRoot[rootMessageId]);
  const sendThreadMessage = useAppStore((s) => s.sendThreadMessage);
  const agents = useAppStore((s) => s.agents);
  const openImagePreview = useAppStore((s) => s.openImagePreview);

  const messages = thread?.messages ?? EMPTY_THREAD;
  const loading = thread?.loading ?? false;
  // The first message is always the root (context); the rest are replies.
  const rootMsg = messages.length > 0 ? messages[0] : null;
  const replies = rootMsg ? messages.slice(1) : messages;

  const mentionTargets = useMentionTargets(channelId);

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [pendingAttachments, setPendingAttachments] = useState<Attachment[]>(
    []
  );
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const stickToBottomRef = useRef(true);

  const [mentionState, setMentionState] = useState<{
    active: boolean;
    query: string;
    startIndex: number;
    matched: MentionTarget[];
  } | null>(null);
  const [mentionSelectedIndex, setMentionSelectedIndex] = useState(0);
  const [mentionMap, setMentionMap] = useState<MentionTarget[]>([]);
  const [cursorPos, setCursorPos] = useState(0);

  const agentTitleFor = useCallback(
    (msg: ChatMessageUI) => {
      if (msg.role === "user") return "";
      const agent = agents.find(
        (a) =>
          a.name === `agents/${msg.senderName}` || a.title === msg.senderName
      );
      return agent?.title ?? msg.senderName ?? "";
    },
    [agents]
  );

  // Auto-stick to bottom as replies arrive.
  useEffect(() => {
    if (scrollRef.current && stickToBottomRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [replies.length]);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    stickToBottomRef.current = scrollHeight - scrollTop - clientHeight < 100;
  }, []);

  const autoResize = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, []);

  useEffect(() => {
    autoResize();
  }, [input, autoResize]);

  const uploadFile = useCallback(
    async (file: File): Promise<Attachment | null> => {
      try {
        const res = await commandServiceClient.uploadFile({
          conversation: conversationName,
          originalName: file.name,
          mimeType: file.type || "",
          data: new Uint8Array(await file.arrayBuffer()),
        });
        return create(AttachmentSchema, {
          id: res.id,
          name: res.originalName,
          mimeType: res.mimeType,
          sizeBytes: res.sizeBytes,
        });
      } catch (err) {
        console.error("thread file upload failed", err);
        return null;
      }
    },
    [conversationName]
  );

  const handleFiles = useCallback(
    async (files: FileList | File[]) => {
      const list = Array.from(files);
      if (list.length === 0) return;
      setUploading(true);
      try {
        for (const file of list) {
          const att = await uploadFile(file);
          if (att) setPendingAttachments((prev) => [...prev, att]);
        }
      } finally {
        setUploading(false);
      }
    },
    [uploadFile]
  );

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if ((!text && pendingAttachments.length === 0) || sending) return;
    const attachments = pendingAttachments;
    setInput("");
    setMentionState(null);
    setPendingAttachments([]);
    setSending(true);
    stickToBottomRef.current = true;
    const mentions = mentionMap.map(targetToMention);
    try {
      await sendThreadMessage(
        channelId,
        rootMessageId,
        text,
        mentions,
        attachments
      );
    } catch {
      setPendingAttachments(attachments);
    } finally {
      setSending(false);
    }
  }, [
    input,
    sending,
    channelId,
    rootMessageId,
    mentionMap,
    sendThreadMessage,
    pendingAttachments,
  ]);

  const handleMentionSelect = useCallback(
    (target: MentionTarget) => {
      if (!mentionState) return;
      const before = input.slice(0, mentionState.startIndex);
      const after = input.slice(cursorPos);
      const newInput = `${before}@${target.name} ${after}`;
      setInput(newInput);
      setMentionMap((prev) =>
        prev.some((m) => m.id === target.id && m.type === target.type)
          ? prev
          : [...prev, target]
      );
      setMentionState(null);
      setMentionSelectedIndex(0);
      setTimeout(() => {
        const el = textareaRef.current;
        if (el) {
          const newPos = mentionState.startIndex + target.name.length + 2;
          el.focus();
          el.setSelectionRange(newPos, newPos);
        }
      }, 0);
    },
    [input, cursorPos, mentionState]
  );

  const noopViewDetails = useCallback((_: string) => {}, []);

  if (loading && !rootMsg) {
    return (
      <aside className={asideClass}>
        <ThreadHeader
          title={t("chat.thread-title")}
          channelName={channelTitle}
          channelId={channelId}
          rootMsg={null}
          onClose={onClose}
          onViewInChannel={onViewInChannel}
        />
        <LoadingState />
      </aside>
    );
  }

  return (
    <aside className={asideClass}>
      <ThreadHeader
        title={t("chat.thread-title")}
        channelName={channelTitle}
        channelId={channelId}
        rootMsg={rootMsg}
        onClose={onClose}
        onViewInChannel={onViewInChannel}
      />

      {/* Scroll area: root context + replies + composer. */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto"
      >
        <div className="flex flex-col gap-3 px-4 pt-4 pb-4">
          {/* Root message context. Rendered through the shared MessageRow so it
              gets the same markdown/mention/attachment treatment as channel
              chat and thread replies (it previously rendered raw text). */}
          {rootMsg && (
            <MessageRow
              msg={rootMsg}
              showAvatar
              agentTitle={agentTitleFor(rootMsg)}
              streamingContent=""
              streamingEvents={rootMsg.events ?? EMPTY_EVENTS}
              onViewDetails={noopViewDetails}
              MentionBadge={MentionBadge}
              markdownCustomId="thread-chat"
              onPreviewAttachment={onPreviewAttachment}
              onJumpToSection={onJumpToSection}
              onPreviewImage={onPreviewImage}
            />
          )}

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
              !prev ||
              prev.role !== msg.role ||
              prev.senderName !== msg.senderName;
            const rowProps = rowStreamingProps(msg, false, "", EMPTY_EVENTS);
            return (
              <MessageRow
                key={msg.id}
                msg={msg}
                showAvatar={showAvatar}
                agentTitle={agentTitleFor(msg)}
                streamingContent={rowProps.streamingContent}
                streamingEvents={rowProps.streamingEvents}
                onViewDetails={noopViewDetails}
                MentionBadge={MentionBadge}
                markdownCustomId="thread-chat"
                onPreviewAttachment={onPreviewAttachment}
                onJumpToSection={onJumpToSection}
                onPreviewImage={onPreviewImage}
              />
            );
          })}
        </div>
      </div>

      {/* Composer. */}
      <div className="shrink-0 border-t border-control-border bg-background px-3 pb-3 pt-2">
        <div
          className="rounded-2xl border border-control-border bg-control-bg/40 focus-within:border-accent focus-within:bg-background transition-colors"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            if (e.dataTransfer.files.length > 0)
              handleFiles(e.dataTransfer.files);
          }}
        >
          {pendingAttachments.length > 0 && (
            <div className="flex flex-wrap gap-1.5 px-3 pt-2">
              {pendingAttachments.map((att) =>
                isImageAttachment(att) ? (
                  <div key={att.id} className="group relative shrink-0">
                    <RemoteImage
                      attachment={att}
                      variant="thumb"
                      onClick={() => openImagePreview(att)}
                    />
                    <button
                      type="button"
                      onClick={() =>
                        setPendingAttachments((prev) =>
                          prev.filter((p) => p.id !== att.id)
                        )
                      }
                      className="absolute -right-1.5 -top-1.5 flex size-5 items-center justify-center rounded-full border border-control-border bg-background text-control-placeholder opacity-0 transition-opacity hover:text-error group-hover:opacity-100"
                      aria-label={t("common.delete")}
                    >
                      <X className="size-3" />
                    </button>
                  </div>
                ) : (
                  <span
                    key={att.id}
                    className="group flex items-center gap-1.5 rounded-md border border-control-border bg-background px-2 py-1 text-xs text-main"
                  >
                    <span className="max-w-[160px] truncate">{att.name}</span>
                    <button
                      type="button"
                      onClick={() =>
                        setPendingAttachments((prev) =>
                          prev.filter((p) => p.id !== att.id)
                        )
                      }
                      className="text-control-placeholder hover:text-error transition-colors"
                      aria-label={t("common.delete")}
                    >
                      <X className="size-3" />
                    </button>
                  </span>
                )
              )}
            </div>
          )}
          <Textarea
            ref={textareaRef}
            className={cn(
              "block w-full resize-none border-0 bg-transparent px-3 py-2.5 text-sm text-main",
              "placeholder:text-control-placeholder focus:ring-0 focus:border-transparent",
              "max-h-[160px] min-h-[24px]"
            )}
            rows={1}
            placeholder={t("chat.thread-placeholder")}
            aria-controls={mentionState?.active ? MENTION_POPUP_ID : undefined}
            aria-activedescendant={
              mentionState?.active && mentionState.matched.length > 0
                ? `${MENTION_POPUP_ID}-opt-${mentionSelectedIndex}`
                : undefined
            }
            value={input}
            onChange={(e) => {
              const value = e.target.value;
              setInput(value);
              const pos = e.target.selectionStart ?? 0;
              setCursorPos(pos);
              const state = detectMention(value, pos, mentionTargets);
              setMentionState(state);
              setMentionSelectedIndex(0);
              if (state?.active) {
                const newMap: MentionTarget[] = [];
                const re = /(?:^|\s)@(\S+)/g;
                let m: RegExpExecArray | null;
                while ((m = re.exec(value)) !== null) {
                  const found = mentionTargets.find((t) => t.name === m![1]);
                  if (found) newMap.push(found);
                }
                setMentionMap(newMap);
              }
            }}
            onKeyDown={(e) => {
              if (mentionState?.active) {
                const total = mentionState.matched.length;
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setMentionSelectedIndex((idx) =>
                    idx + 1 < total ? idx + 1 : 0
                  );
                  return;
                }
                if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setMentionSelectedIndex((idx) =>
                    idx - 1 >= 0 ? idx - 1 : total - 1
                  );
                  return;
                }
                if (e.key === "Enter" || e.key === "Tab") {
                  if (total === 0) return;
                  e.preventDefault();
                  if (mentionState.matched[mentionSelectedIndex]) {
                    handleMentionSelect(
                      mentionState.matched[mentionSelectedIndex]
                    );
                  }
                  return;
                }
                if (e.key === "Escape") {
                  e.preventDefault();
                  setMentionState(null);
                  return;
                }
              }
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            onSelect={(e) => {
              const target = e.target as HTMLTextAreaElement;
              const pos = target.selectionStart ?? 0;
              setCursorPos(pos);
              const state = detectMention(target.value, pos, mentionTargets);
              setMentionState(state);
              setMentionSelectedIndex(0);
            }}
            disabled={sending}
          />
          <div className="flex items-center justify-between px-2.5 pb-1.5">
            <div className="flex items-center gap-2">
              <input
                ref={fileInputRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  if (e.target.files) handleFiles(e.target.files);
                  e.target.value = "";
                }}
              />
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                disabled={uploading || sending}
                className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors disabled:opacity-50"
                aria-label={t("channel.attach-file")}
              >
                {uploading ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Paperclip className="size-4" />
                )}
              </button>
            </div>
            <Button
              type="button"
              size="xs"
              onClick={handleSend}
              disabled={
                (!input.trim() && pendingAttachments.length === 0) || sending
              }
            >
              <Send className="size-3" />
              {t("common.send")}
            </Button>
          </div>
        </div>
        {mentionState?.active && textareaRef.current && (
          <MentionPopup
            id={MENTION_POPUP_ID}
            targets={mentionState.matched}
            query={mentionState.query}
            position={getCaretCoordinates(textareaRef.current, cursorPos)}
            selectedIndex={mentionSelectedIndex}
            onSelect={handleMentionSelect}
            onClose={() => setMentionState(null)}
          />
        )}
      </div>
    </aside>
  );
}

function ThreadHeader({
  title,
  channelName,
  channelId,
  rootMsg,
  onClose,
  onViewInChannel,
}: {
  title: string;
  channelName: string;
  channelId: string;
  rootMsg: ChatMessageUI | null;
  onClose: () => void;
  onViewInChannel: () => void;
}) {
  const { t } = useTranslation();
  const closeThread = useAppStore((s) => s.closeThread);
  const toggleTasksPanel = useAppStore((s) => s.toggleTasksPanel);
  const isTask = !!rootMsg?.task;
  const handleBackToTasks = () => {
    // Drill back from a task's thread to the channel's task board.
    closeThread();
    toggleTasksPanel(channelId);
  };
  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2.5">
      {isTask && (
        <button
          type="button"
          onClick={handleBackToTasks}
          className="flex items-center gap-1 text-xs text-control-placeholder hover:text-main transition-colors"
          aria-label={t("channelTask.back-to-tasks")}
        >
          <ArrowLeft className="size-3.5" />
          <span className="hidden sm:inline">
            {t("channelTask.back-to-tasks")}
          </span>
        </button>
      )}
      <div className="min-w-0 flex-1">
        <p className="text-sm font-semibold text-main truncate">
          {title} — #{channelName}
        </p>
      </div>
      <button
        type="button"
        onClick={onViewInChannel}
        className="flex items-center gap-1 text-xs text-control-placeholder hover:text-accent transition-colors"
      >
        <ExternalLink className="size-3.5" />
        <span className="hidden sm:inline">
          {t("chat.thread-view-in-channel")}
        </span>
      </button>
      <button
        type="button"
        onClick={onClose}
        className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
        aria-label={t("chat.thread-close")}
      >
        <X className="size-4" />
      </button>
    </div>
  );
}

const EMPTY_THREAD: ChatMessageUI[] = [];
