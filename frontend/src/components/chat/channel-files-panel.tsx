import { create } from "@bufbuild/protobuf";
import {
  CornerDownRight,
  Download,
  Eye,
  FileIcon,
  FolderOpen,
  Loader2,
  X,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { formatTime } from "@/components/chat/avatar";
import { formatBytes } from "@/components/chat/file-card";
import { RemoteImage } from "@/components/chat/remote-image";
import { EmptyState, LoadingState } from "@/components/chat/states";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { SearchInput } from "@/components/ui/search-input";
import { commandServiceClient } from "@/connect";
import { downloadAttachment } from "@/lib/file-download";
import { isHtmlAttachment, MAX_HTML_PREVIEW_BYTES } from "@/lib/html-file";
import { isImageAttachment } from "@/lib/image-file";
import {
  isMarkdownAttachment,
  MAX_MARKDOWN_PREVIEW_BYTES,
} from "@/lib/markdown-file";
import { useIsDesktop } from "@/lib/use-is-desktop";
import { cn } from "@/lib/utils";
import type {
  Attachment,
  ConversationFile,
} from "@/types/proto-es/v1/command_pb";
import { AttachmentSchema } from "@/types/proto-es/v1/command_pb";

export interface ChannelFilesPanelProps {
  channelId: string;
  channelTitle: string;
  onClose: () => void;
  onPreviewAttachment: (attachment: Attachment, rootMessageId: string) => void;
  onPreviewImage: (attachment: Attachment) => void;
  // onJumpToMessage navigates the channel chat to the message where the file
  // was attached (the last carrying position), loading only a focused window
  // around it instead of the whole history.
  onJumpToMessage: (cf: ConversationFile) => void;
}

const EMPTY_FILES: ConversationFile[] = [];

// Truncates a long file name in the middle while keeping the extension visible,
// e.g. "Gemini_Generated_Image_...xzs1.png". Returns the original name when it
// is short enough to fit.
function truncateFileName(name: string, maxLength = 36): string {
  if (name.length <= maxLength) return name;
  const dot = name.lastIndexOf(".");
  const ext = dot > 0 ? name.slice(dot) : "";
  const stemMax = Math.max(8, maxLength - ext.length - 3);
  const head = name.slice(0, stemMax);
  return `${head}…${ext}`;
}

// ChannelFilesPanel is the channel's files drawer: every file uploaded to the
// conversation, newest first, with the sender, send time, and the message text
// that carried it. The user can search by file name and open a preview or
// download directly from the list, matching the in-chat file affordances.
export function ChannelFilesPanel({
  channelId,
  channelTitle,
  onClose,
  onPreviewAttachment,
  onPreviewImage,
  onJumpToMessage,
}: ChannelFilesPanelProps) {
  const { t } = useTranslation();
  const [files, setFiles] = useState<ConversationFile[]>(EMPTY_FILES);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    commandServiceClient
      .listFiles({ conversation: `conversations/${channelId}` })
      .then((res) => {
        if (cancelled) return;
        const convFiles = res.conversationFiles ?? [];
        // Fall back to the plain file list when the enriched payload is
        // unavailable (e.g. an older backend), so the drawer still works.
        setFiles(
          convFiles.length > 0
            ? convFiles
            : (res.files ?? []).map((f) => ({ file: f }) as ConversationFile)
        );
        setLoading(false);
      })
      .catch(() => {
        if (cancelled) return;
        setFiles(EMPTY_FILES);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [channelId]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return files;
    return files.filter((f) =>
      (f.file?.originalName ?? "").toLowerCase().includes(q)
    );
  }, [files, query]);

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2.5">
        <FolderOpen className="size-4 text-control-placeholder" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold text-main truncate">
            {t("channelFiles.title")} — #{channelTitle}
          </p>
          <p className="text-[11px] text-control-placeholder">
            {t("channelFiles.count", { count: files.length })}
          </p>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="flex size-7 items-center justify-center rounded-md text-control-placeholder hover:text-main hover:bg-control-bg transition-colors"
          aria-label={t("channelFiles.close")}
        >
          <X className="size-4" />
        </button>
      </div>

      <div className="shrink-0 border-b border-control-border px-3 py-2">
        <SearchInput
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("channelFiles.search-placeholder")}
          aria-label={t("channelFiles.search-placeholder")}
        />
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="flex flex-col px-2 pt-2 pb-3">
          {loading && files.length === 0 && <LoadingState />}
          {!loading && filtered.length === 0 && (
            <EmptyState
              icon={FolderOpen}
              message={
                query.trim()
                  ? t("channelFiles.no-results")
                  : t("channelFiles.empty")
              }
            />
          )}
          {filtered.map((cf) => (
            <FileRow
              key={cf.file?.id ?? cf.messageId}
              cf={cf}
              onPreviewAttachment={onPreviewAttachment}
              onPreviewImage={onPreviewImage}
              onJumpToMessage={onJumpToMessage}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

function FileRow({
  cf,
  onPreviewAttachment,
  onPreviewImage,
  onJumpToMessage,
}: {
  cf: ConversationFile;
  onPreviewAttachment: (attachment: Attachment, rootMessageId: string) => void;
  onPreviewImage: (attachment: Attachment) => void;
  onJumpToMessage: (cf: ConversationFile) => void;
}) {
  const { t, i18n } = useTranslation();
  const isDesktop = useIsDesktop();
  const [busy, setBusy] = useState(false);
  const file = cf.file;
  if (!file) return null;

  const attachment = create(AttachmentSchema, {
    id: file.id,
    name: file.originalName,
    mimeType: file.mimeType,
    sizeBytes: file.sizeBytes,
  });

  const isImage = isImageAttachment(attachment);
  const previewable =
    isMarkdownAttachment(attachment) || isHtmlAttachment(attachment);
  const tooLarge =
    previewable &&
    (attachment.sizeBytes ?? 0n) >
      (isHtmlAttachment(attachment)
        ? MAX_HTML_PREVIEW_BYTES
        : MAX_MARKDOWN_PREVIEW_BYTES);
  const rootMessageId = cf.threadRoot || cf.messageId || file.id;
  const canJump = !!cf.messageId;
  const canPreview = isImage || (previewable && !tooLarge);
  // Reason shown when preview is unavailable, so the button is always visible
  // but clearly disabled with an explanation instead of disappearing.
  const previewDisabledReason = !canPreview
    ? tooLarge
      ? t("preview.too-large-tooltip")
      : t("channelFiles.preview-unsupported")
    : undefined;

  const handleDownload = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await downloadAttachment(attachment);
    } catch (err) {
      console.error("file download failed", err);
    } finally {
      setBusy(false);
    }
  };

  const handlePreview = () => {
    if (isImage) {
      onPreviewImage(attachment);
      return;
    }
    if (previewable && !tooLarge) {
      onPreviewAttachment(attachment, rootMessageId);
    }
  };

  const senderName = cf.senderName || t("channelFiles.unknown-sender");
  const timeSource = cf.messageCreatedAt ?? file.createdAt;
  const sentAt = timeSource
    ? formatTime(new Date(Number(timeSource.seconds) * 1000), i18n.language)
    : t("channelFiles.unknown-time");

  const row = (
    <div
      className={cn(
        "flex items-center gap-2.5 rounded-md px-2 py-1.5 text-sm transition-colors",
        "hover:bg-control-bg/50"
      )}
    >
      {isImage ? (
        <button
          type="button"
          onClick={() => canJump && onJumpToMessage(cf)}
          disabled={!canJump}
          className="shrink-0 rounded-md focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-default"
          title={canJump ? t("channelFiles.view-in-chat") : undefined}
        >
          <RemoteImage
            attachment={attachment}
            variant="thumb"
            className="size-10"
          />
        </button>
      ) : (
        <button
          type="button"
          onClick={() => canJump && onJumpToMessage(cf)}
          disabled={!canJump}
          className="flex size-10 shrink-0 items-center justify-center rounded-md bg-control-bg text-control transition-colors hover:bg-control-bg/70 focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-default"
          title={canJump ? t("channelFiles.view-in-chat") : undefined}
        >
          <FileIcon className="size-4.5" />
        </button>
      )}

      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-baseline gap-2">
          <button
            type="button"
            onClick={() => canJump && onJumpToMessage(cf)}
            disabled={!canJump}
            className="truncate font-medium text-main text-left hover:text-accent transition-colors disabled:cursor-default disabled:hover:text-main"
            title={canJump ? t("channelFiles.view-in-chat") : undefined}
          >
            {truncateFileName(file.originalName)}
          </button>
          <span className="shrink-0 text-[11px] text-control-placeholder">
            {file.mimeType || "file"} · {formatBytes(file.sizeBytes)}
          </span>
        </div>
        <div className="mt-0.5 flex min-w-0 items-center gap-2 text-[11px] text-control-placeholder">
          <span className="truncate">{senderName}</span>
          <span aria-hidden="true">·</span>
          <span className="shrink-0">{sentAt}</span>
          {cf.messageContent && (
            <span className="truncate text-control-light">
              {cf.messageContent}
            </span>
          )}
        </div>
      </div>

      <div className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          onClick={handlePreview}
          disabled={!canPreview || busy}
          title={previewDisabledReason ?? t("channelFiles.preview")}
          className={cn(
            "flex size-8 items-center justify-center rounded-md transition-colors",
            canPreview
              ? "text-control-placeholder hover:bg-control-bg hover:text-main"
              : "cursor-not-allowed text-control-placeholder/40"
          )}
          aria-label={t("channelFiles.preview")}
        >
          <Eye className="size-4" />
        </button>
        <button
          type="button"
          onClick={handleDownload}
          disabled={busy}
          title={t("channelFiles.download")}
          className="flex size-8 items-center justify-center rounded-md text-control-placeholder transition-colors hover:bg-control-bg hover:text-main disabled:opacity-50"
          aria-label={t("channelFiles.download")}
        >
          {busy ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Download className="size-4" />
          )}
        </button>
      </div>
    </div>
  );

  // Desktop gets a right-click menu with the same preview/download actions;
  // mobile renders the row directly (long-press is used elsewhere in the app).
  if (!isDesktop) return row;

  return (
    <ContextMenu>
      <ContextMenuTrigger className="block">{row}</ContextMenuTrigger>
      <ContextMenuContent>
        {canJump && (
          <>
            <ContextMenuItem onClick={() => onJumpToMessage(cf)}>
              <CornerDownRight className="size-4" />
              {t("channelFiles.view-in-chat")}
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuItem onClick={handlePreview} disabled={!canPreview}>
          <Eye className="size-4" />
          {canPreview
            ? t("channelFiles.preview")
            : tooLarge
              ? t("preview.too-large-tooltip")
              : t("channelFiles.preview-unsupported")}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem onClick={handleDownload}>
          <Download className="size-4" />
          {t("channelFiles.download")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
