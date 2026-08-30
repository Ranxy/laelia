import { ListTodo, Loader2, Paperclip, Send, X } from "lucide-react";
import { type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { MentionPopup } from "@/components/chat/mention-popup";
import { RemoteImage } from "@/components/chat/remote-image";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  type ComposerDraft,
  useChatComposer,
} from "@/composables/use-chat-composer";
import { type MentionTarget } from "@/composables/useMentionTargets";
import { getCaretCoordinates } from "@/lib/caret-position";
import { filesFromClipboard } from "@/lib/clipboard-file";
import { isImageAttachment } from "@/lib/image-file";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// ChatComposer is the shared message composer: one implementation of the
// upload state machine, optimistic send pipeline, mention popup, auto-resize
// textarea, and per-surface draft cache, used by both the channel page (size
// "main") and the thread panel (size "compact"). Behavior that differs between
// the two surfaces is parameterized below; the markup is otherwise identical
// so the two call sites cannot drift again.
export interface ChatComposerProps {
  channelId: string;
  rootMessageId?: string;
  draftKey: string;
  // Per-surface draft cache, owned by the host page/panel.
  draftsRef: RefObject<Map<string, ComposerDraft>>;
  enterToSend: boolean;
  mentionTargets: MentionTarget[];
  // Per-instance DOM id for the mention popup listbox (aria wiring).
  popupId: string;
  placeholder: string;
  // "main" = channel page composer (taller textarea, task toggle, keybinding
  // hint); "compact" = thread panel composer.
  size: "main" | "compact";
  // taskEnabled renders the as-task toggle and sends the asTask flag.
  taskEnabled?: boolean;
  // Fired once the optimistic row is appended (thread panel sticks to bottom).
  onSendStart?: () => void;
}

// ComposerAttachments renders the pending-upload progress chips and the
// completed-attachment chips (shared by both surfaces; identical markup).
function ComposerAttachments({
  uploads,
  pendingAttachments,
  onRemoveUpload,
  onRemoveAttachment,
  onPreviewImage,
  onDeleteLabel,
}: {
  uploads: ReturnType<typeof useChatComposer>["uploads"];
  pendingAttachments: Attachment[];
  onRemoveUpload: (id: string) => void;
  onRemoveAttachment: (id: string) => void;
  onPreviewImage: (att: Attachment) => void;
  onDeleteLabel: string;
}) {
  return (
    <>
      {uploads.map((u) =>
        u.error ? (
          <span
            key={u.id}
            className="flex items-center gap-1.5 rounded-md border border-error/40 bg-error/5 px-2 py-1 text-xs text-error"
          >
            <span className="max-w-[120px] truncate">{u.name}</span>
            <span className="text-error/80">{u.error}</span>
            <button
              type="button"
              onClick={() => onRemoveUpload(u.id)}
              className="text-error/60 hover:text-error transition-colors"
              aria-label={onDeleteLabel}
            >
              <X className="size-3" />
            </button>
          </span>
        ) : (
          <span
            key={u.id}
            className="flex items-center gap-1.5 rounded-md border border-control-border bg-background px-2 py-1 text-xs text-main"
          >
            <Loader2 className="size-3 animate-spin" />
            <span className="max-w-[120px] truncate">{u.name}</span>
            <span className="text-control-placeholder">{u.progress}%</span>
          </span>
        )
      )}
      {pendingAttachments.map((att) =>
        isImageAttachment(att) ? (
          <div key={att.id} className="group relative shrink-0">
            <RemoteImage
              attachment={att}
              variant="thumb"
              onClick={() => onPreviewImage(att)}
            />
            <button
              type="button"
              onClick={() => onRemoveAttachment(att.id)}
              className="absolute -right-1.5 -top-1.5 flex size-5 items-center justify-center rounded-full border border-control-border bg-background text-control-placeholder opacity-0 transition-opacity hover:text-error group-hover:opacity-100"
              aria-label={onDeleteLabel}
            >
              <X className="size-3" />
            </button>
            <div className="pointer-events-none absolute left-full top-1/2 z-20 ml-2 max-w-[240px] -translate-y-1/2 truncate rounded-md border border-control-border bg-background px-2 py-1 text-xs text-main opacity-0 shadow-lg transition-opacity group-hover:opacity-100">
              {att.name}
            </div>
          </div>
        ) : (
          <span
            key={att.id}
            className="group flex items-center gap-1.5 rounded-md border border-control-border bg-background px-2 py-1 text-xs text-main"
          >
            <span className="max-w-[160px] truncate">{att.name}</span>
            <button
              type="button"
              onClick={() => onRemoveAttachment(att.id)}
              className="text-control-placeholder hover:text-error transition-colors"
              aria-label={onDeleteLabel}
            >
              <X className="size-3" />
            </button>
          </span>
        )
      )}
    </>
  );
}

export function ChatComposer(props: ChatComposerProps) {
  const {
    channelId,
    rootMessageId,
    draftKey,
    draftsRef,
    enterToSend,
    mentionTargets,
    popupId,
    placeholder,
    size,
    taskEnabled,
    onSendStart,
  } = props;
  const { t } = useTranslation();
  const isDesktop = useIsDesktop();
  const openImagePreview = useAppStore((s) => s.openImagePreview);
  const composer = useChatComposer({
    channelId,
    threadRootId: rootMessageId,
    draftKey,
    draftsRef,
    enterToSend,
    mentionTargets,
    taskEnabled,
    onSendStart,
    textareaMaxHeight: size === "main" ? 200 : 160,
  });

  const textarea =
    size === "main"
      ? cn(
          "block w-full resize-none border-0 bg-transparent px-4 py-3 text-sm text-main",
          "placeholder:text-control-placeholder focus:ring-0 focus:border-transparent",
          "max-h-[200px] min-h-[24px]"
        )
      : cn(
          "block w-full resize-none border-0 bg-transparent px-3 py-2.5 text-sm text-main",
          "placeholder:text-control-placeholder focus:ring-0 focus:border-transparent",
          "max-h-[160px] min-h-[24px]"
        );

  const footerPadding = size === "main" ? "px-3 pb-2" : "px-2.5 pb-1.5";

  return (
    <div
      className="rounded-2xl border border-control-border bg-control-bg/40 focus-within:border-accent focus-within:bg-background transition-colors"
      onDragOver={(e) => {
        e.preventDefault();
      }}
      onDrop={(e) => {
        e.preventDefault();
        if (e.dataTransfer.files.length > 0)
          composer.addFiles(e.dataTransfer.files);
      }}
      onPaste={(e) => {
        // Pasting a clipboard image (screenshot, copied image) uploads it
        // like a picked file. Only preventDefault when real files were found,
        // so text paste keeps inserting into the textarea.
        const files = filesFromClipboard(e.clipboardData);
        if (files.length > 0) {
          e.preventDefault();
          void composer.addFiles(files);
        }
      }}
    >
      {(composer.pendingAttachments.length > 0 ||
        composer.uploads.length > 0) && (
        <div className="flex flex-wrap gap-1.5 px-3 pt-2">
          <ComposerAttachments
            uploads={composer.uploads}
            pendingAttachments={composer.pendingAttachments}
            onRemoveUpload={composer.removeUpload}
            onRemoveAttachment={composer.removeAttachment}
            onPreviewImage={openImagePreview}
            onDeleteLabel={t("common.delete")}
          />
        </div>
      )}
      <Textarea
        ref={composer.textareaRef}
        className={textarea}
        rows={1}
        placeholder={placeholder}
        aria-controls={composer.mentionState?.active ? popupId : undefined}
        aria-activedescendant={
          composer.mentionState?.active &&
          composer.mentionState.matched.length > 0
            ? `${popupId}-opt-${composer.mentionSelectedIndex}`
            : undefined
        }
        value={composer.input}
        onChange={(e) => {
          composer.handleTextareaChange(
            e.target.value,
            e.target.selectionStart ?? 0
          );
        }}
        onKeyDown={composer.handleTextareaKeyDown}
        onSelect={(e) => {
          const target = e.target as HTMLTextAreaElement;
          composer.handleTextareaSelect(target.selectionStart ?? 0);
        }}
      />
      <div className={cn("flex items-center justify-between", footerPadding)}>
        <div className="flex items-center gap-2">
          <input
            ref={composer.fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(e) => {
              if (e.target.files) void composer.addFiles(e.target.files);
              e.target.value = "";
            }}
          />
          <button
            type="button"
            onClick={() => composer.fileInputRef.current?.click()}
            disabled={composer.uploads.length > 0 || composer.sending}
            className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors disabled:opacity-50"
            aria-label={t("channel.attach-file")}
          >
            {composer.uploads.length > 0 ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Paperclip className="size-4" />
            )}
          </button>
          {taskEnabled && size === "main" && (
            <button
              type="button"
              onClick={() => composer.setAsTask((v) => !v)}
              aria-pressed={composer.asTask}
              disabled={composer.sending}
              className={cn(
                "flex h-7 items-center gap-1 rounded-md px-2 text-xs transition-colors disabled:opacity-50 lg:ml-0",
                "ml-2",
                composer.asTask
                  ? "bg-accent/15 text-accent"
                  : "text-control-placeholder hover:text-main hover:bg-control-bg"
              )}
              aria-label={t("channelTask.as-task")}
              title={t("channelTask.as-task-hint")}
            >
              <ListTodo className="size-3.5" />
              <span className="sm:hidden lg:inline">
                {t("channelTask.as-task")}
              </span>
            </button>
          )}
          {size === "main" && isDesktop && (
            <span className="text-xs text-control-placeholder">
              {t(enterToSend ? "chat.send-hint" : "chat.send-hint-inverted")}
            </span>
          )}
        </div>
        <Button
          type="button"
          size="xs"
          onClick={() => void composer.send()}
          disabled={
            (!composer.input.trim() &&
              composer.pendingAttachments.length === 0) ||
            composer.sending
          }
        >
          <Send className="size-3" />
          {t("common.send")}
        </Button>
      </div>
      {composer.mentionState?.active && composer.textareaRef.current && (
        <MentionPopup
          id={popupId}
          targets={composer.mentionState.matched}
          query={composer.mentionState.query}
          position={getCaretCoordinates(
            composer.textareaRef.current,
            composer.cursorPos
          )}
          selectedIndex={composer.mentionSelectedIndex}
          onSelect={composer.insertMention}
          onClose={composer.resetMentionState}
        />
      )}
    </div>
  );
}
