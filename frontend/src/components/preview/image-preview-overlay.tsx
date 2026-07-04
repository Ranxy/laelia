import { Download, Loader2, X } from "lucide-react";
import { useEffect } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  getLayerRoot,
  usePreserveHigherLayerAccess,
} from "@/components/ui/layer";
import { downloadAttachment } from "@/lib/file-download";
import { useAppStore } from "@/stores";

// ImagePreviewOverlay is the store-driven full-page lightbox for image
// attachments. It portals into the overlay layer (z-2500) and covers the
// viewport with a darkened backdrop; the image is centered and scaled to fit
// within 85vh / 90vw. Esc closes, clicking the backdrop closes, clicking the
// image does not (so it can be inspected). A download button is provided for
// the raw file.
export function ImagePreviewOverlay() {
  usePreserveHigherLayerAccess("overlay");
  const { t } = useTranslation();
  const active = useAppStore((s) => s.activeImage);
  const close = useAppStore((s) => s.closeImagePreview);

  // Esc closes. Bound only while open.
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, close]);

  if (!active) return null;
  const { attachment, blobUrl, status } = active;

  return createPortal(
    <div
      className="fixed inset-0 z-10 flex flex-col bg-background/95"
      onClick={close}
    >
      {/* Top bar — stopPropagation so backdrop click doesn't close via it. */}
      <div
        className="flex h-14 shrink-0 items-center gap-2 border-b border-control-border px-4"
        onClick={(e) => e.stopPropagation()}
      >
        <span className="truncate text-sm font-medium text-main">
          {attachment.name}
        </span>
        <div className="flex-1" />
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
      </div>

      {/* Image stage — backdrop click closes; image click does not. */}
      <div
        className="flex min-h-0 flex-1 items-center justify-center p-6"
        onClick={close}
      >
        {status === "loading" && (
          <Loader2 className="size-8 animate-spin text-control-light" />
        )}
        {status === "error" && (
          <p className="text-sm text-control">{t("preview.error")}</p>
        )}
        {status === "ready" && blobUrl && (
          <img
            src={blobUrl}
            alt={attachment.name}
            onClick={(e) => e.stopPropagation()}
            className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
          />
        )}
      </div>
    </div>,
    getLayerRoot("overlay")
  );
}
