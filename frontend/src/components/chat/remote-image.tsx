import { ImageIcon, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { getImageBlob } from "@/lib/image-blob-cache";
import { cn } from "@/lib/utils";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// RemoteImage renders an attachment's bytes as an <img>. The bytes come from
// the shared image-blob cache (fetched at most once per session); each mount
// creates its own object URL from the cached Blob and revokes it on unmount,
// so evicting a cached blob never breaks a still-displayed image. Two layout
// variants:
//   - "thumb":  a fixed square thumbnail (composer draft chips), cover-fit.
//   - "inline": the published in-message image, contain-fit and capped to fit
//               the chat width/height; large images scale down.
// `onClick` (when provided) turns the image into a button — used to open the
// lightbox. While loading or on error, a placeholder of the same footprint is
// shown so the layout doesn't jump.
export function RemoteImage({
  attachment,
  variant,
  onClick,
  className,
}: {
  attachment: Attachment;
  variant: "thumb" | "inline";
  onClick?: () => void;
  className?: string;
}) {
  const { t } = useTranslation();
  const [url, setUrl] = useState<string | null>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading"
  );

  useEffect(() => {
    let cancelled = false;
    let createdUrl: string | null = null;
    setUrl(null);
    setStatus("loading");
    getImageBlob(attachment.id, attachment.mimeType ?? "").then((blob) => {
      if (cancelled) return;
      if (!blob) {
        setStatus("error");
        return;
      }
      createdUrl = URL.createObjectURL(blob);
      setUrl(createdUrl);
      setStatus("ready");
    });
    return () => {
      cancelled = true;
      // This URL was created for this mount only — revoke it on unmount so the
      // blob is released once the image leaves the screen.
      if (createdUrl) URL.revokeObjectURL(createdUrl);
    };
  }, [attachment.id, attachment.mimeType]);

  const interactive = !!onClick;

  if (variant === "thumb") {
    return (
      <div
        className={cn(
          "size-16 shrink-0 overflow-hidden rounded-md border border-control-border bg-control-bg/40 flex items-center justify-center",
          interactive && "cursor-pointer hover:border-accent",
          className
        )}
        onClick={onClick}
        role={interactive ? "button" : undefined}
        tabIndex={interactive ? 0 : undefined}
      >
        {status === "loading" && (
          <Loader2 className="size-4 animate-spin text-control-light" />
        )}
        {status === "error" && (
          <ImageIcon className="size-5 text-control-light" />
        )}
        {status === "ready" && url && (
          <img
            src={url}
            alt={attachment.name}
            className="h-full w-full object-cover"
          />
        )}
      </div>
    );
  }

  // inline
  return (
    <div
      className={cn(
        "flex items-center justify-center",
        interactive && "cursor-pointer"
      )}
      onClick={onClick}
      role={interactive ? "button" : undefined}
      tabIndex={interactive ? 0 : undefined}
    >
      {status === "loading" && (
        <div className="flex h-32 w-48 items-center justify-center rounded-lg bg-control-bg/40">
          <Loader2 className="size-5 animate-spin text-control-light" />
        </div>
      )}
      {status === "error" && (
        <div className="flex h-16 items-center justify-center rounded-lg bg-control-bg/40 px-4 text-xs text-control-light">
          <ImageIcon className="mr-1.5 size-4" />
          {t("preview.image-load-failed")}
        </div>
      )}
      {status === "ready" && url && (
        <img
          src={url}
          alt={attachment.name}
          className="block max-h-[400px] max-w-full rounded-lg object-contain"
        />
      )}
    </div>
  );
}
