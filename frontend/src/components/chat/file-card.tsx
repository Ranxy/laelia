import { Download, FileIcon, FileText, Loader2 } from "lucide-react";
import { useState } from "react";
import { downloadAttachment } from "@/lib/file-download";
import { cn } from "@/lib/utils";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// formatBytes renders a human-readable byte count.
export function formatBytes(bytes: number | bigint): string {
  const n = typeof bytes === "bigint" ? Number(bytes) : bytes;
  if (!n || n < 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value >= 10 || i === 0 ? Math.round(value) : value.toFixed(1)} ${units[i]}`;
}

export interface FileCardProps {
  attachment: Attachment;
  // When provided, the card body opens a preview instead of downloading.
  onPreview?: () => void;
  // When set, the preview entry is disabled and this string is shown as the
  // reason (e.g. "file too large to preview"). Download remains available.
  previewDisabledReason?: string;
}

// FileCard renders a single attached file. By default the whole card is a
// download button (the historical behavior). When `onPreview` is provided —
// for previewable markdown — the body becomes a preview trigger and a small
// download icon button is split out on the right so both actions are
// reachable independently.
export function FileCard({
  attachment,
  onPreview,
  previewDisabledReason,
}: FileCardProps) {
  const [busy, setBusy] = useState(false);
  const previewable = !!onPreview;
  const disabled = !!previewDisabledReason;

  async function handleDownload(e?: React.MouseEvent) {
    e?.stopPropagation();
    if (busy) return;
    setBusy(true);
    try {
      await downloadAttachment(attachment);
    } catch (err) {
      console.error("file download failed", err);
    } finally {
      setBusy(false);
    }
  }

  // Non-previewable: keep the original single-button download card exactly.
  if (!previewable) {
    return (
      <button
        type="button"
        onClick={handleDownload}
        className={cn(
          "group flex items-center gap-2.5 rounded-lg border border-current/15 bg-current/5",
          "px-2.5 py-2 text-sm transition-colors hover:bg-current/10 text-current",
          "mt-1.5 max-w-full text-left"
        )}
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-current/10 text-current/70">
          {busy ? (
            <Loader2 className="size-3.5 animate-spin" />
          ) : (
            <FileIcon className="size-3.5" />
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium text-current">
            {attachment.name}
          </span>
          <span className="text-[11px] text-current/60">
            {attachment.mimeType || "file"} ·{" "}
            {formatBytes(attachment.sizeBytes)}
          </span>
        </span>
        <Download className="size-3.5 shrink-0 text-current/60 group-hover:text-current/90" />
      </button>
    );
  }

  return (
    <div
      className={cn(
        "group mt-1.5 flex max-w-full items-center gap-2 rounded-lg border border-current/15 bg-current/5 px-2.5 py-2 text-sm text-current transition-colors",
        !disabled && "hover:bg-current/10",
        disabled && "opacity-60"
      )}
    >
      <button
        type="button"
        disabled={disabled}
        title={previewDisabledReason}
        onClick={onPreview}
        className="flex min-w-0 flex-1 items-center gap-2.5 text-left disabled:cursor-not-allowed"
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-current/10 text-current/70">
          <FileText className="size-3.5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium text-current">
            {attachment.name}
          </span>
          <span className="text-[11px] text-current/60">
            {attachment.mimeType || "file"} ·{" "}
            {formatBytes(attachment.sizeBytes)}
          </span>
        </span>
      </button>
      <button
        type="button"
        onClick={handleDownload}
        disabled={busy}
        aria-label="download"
        className="flex size-7 shrink-0 items-center justify-center rounded-md text-current/60 transition-colors hover:bg-current/10 hover:text-current/90 disabled:opacity-50"
      >
        {busy ? (
          <Loader2 className="size-3.5 animate-spin" />
        ) : (
          <Download className="size-3.5" />
        )}
      </button>
    </div>
  );
}
