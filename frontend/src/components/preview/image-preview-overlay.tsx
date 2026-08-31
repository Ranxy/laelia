import { Download, Loader2, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { downloadAttachment } from "@/lib/file-download";
import { useAppStore } from "@/stores";
import { FilePreviewShell } from "./file-preview-shell";

// ImagePreviewOverlay is the store-driven full-page lightbox for image
// attachments. It portals into the overlay layer (z-2500). The top bar keeps
// the normal page surface (file name + download + close); only the image
// stage below uses a dark translucent backdrop so a white image can't blend
// into the page background, and the image carries a faint ring + shadow so its
// silhouette is visible regardless of image color. Esc closes, clicking the
// dark stage closes, clicking the image does not (so it can be inspected).
export function ImagePreviewOverlay() {
  const { t } = useTranslation();
  const active = useAppStore((s) => s.activeImage);
  const close = useAppStore((s) => s.closeImagePreview);

  if (!active) return null;
  const { attachment, blobUrl, status } = active;

  return (
    <FilePreviewShell
      title={attachment.name}
      barClassName="bg-background"
      onEscape={close}
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => downloadAttachment(attachment)}
            aria-label={t("preview.download")}
            className="flex size-8 items-center justify-center p-0"
          >
            <Download className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={close}
            aria-label={t("common.close")}
            className="flex size-8 items-center justify-center p-0"
          >
            <X className="size-4" />
          </Button>
        </>
      }
    >
      {/* Image stage — dark translucent backdrop; click closes, image does not. */}
      <div
        className="flex min-h-0 flex-1 items-center justify-center bg-black/75 p-6"
        onClick={close}
      >
        {status === "loading" && (
          <Loader2 className="size-8 animate-spin text-white/70" />
        )}
        {status === "error" && (
          <p className="text-sm text-white/80">{t("preview.error")}</p>
        )}
        {status === "ready" && blobUrl && (
          <img
            src={blobUrl}
            alt={attachment.name}
            onClick={(e) => e.stopPropagation()}
            className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain shadow-2xl ring-1 ring-white/20"
          />
        )}
      </div>
    </FilePreviewShell>
  );
}
