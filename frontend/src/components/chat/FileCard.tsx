import { Download, FileIcon, Loader2 } from "lucide-react";
import { useState } from "react";
import { commandServiceClient } from "@/connect";
import { cn } from "@/react/lib/utils";
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

// FileCard renders a single attached file as a downloadable chip. The download
// goes through the CommandService.DownloadFile RPC (the same one agents use),
// which returns the bytes; they're handed to the browser as a blob. Auth rides
// on the Connect transport's cookie, so there's no separate URL/credential
// handling here.
export function FileCard({ attachment }: { attachment: Attachment }) {
  const [busy, setBusy] = useState(false);

  async function handleDownload() {
    if (busy) return;
    setBusy(true);
    try {
      const res = await commandServiceClient.downloadFile({
        id: attachment.id,
      });
      const mime = res.file?.mimeType || attachment.mimeType || undefined;
      // Copy the bytes into a fresh ArrayBuffer so the Blob constructor
      // accepts them under the strict ArrayBufferView<ArrayBuffer> lib typings.
      const ab = new ArrayBuffer(res.data.byteLength);
      new Uint8Array(ab).set(res.data);
      const blob = new Blob([ab], mime ? { type: mime } : undefined);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = res.file?.originalName || attachment.name || attachment.id;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      console.error("file download failed", err);
    } finally {
      setBusy(false);
    }
  }

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
          {attachment.mimeType || "file"} · {formatBytes(attachment.sizeBytes)}
        </span>
      </span>
      <Download className="size-3.5 shrink-0 text-current/60 group-hover:text-current/90" />
    </button>
  );
}
