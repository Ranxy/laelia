import { ImageIcon, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { commandServiceClient } from "@/connect";
import { cn } from "@/lib/utils";
import type { Attachment } from "@/types/proto-es/v1/command_pb";

// Module-level blob cache keyed by attachment id, mirroring avatar-cache: a
// channel full of images fetches each attachment's bytes at most once per
// session, so switching channels and back (which remounts every RemoteImage)
// does not re-download every image. Object URLs are intentionally NOT revoked
// on unmount — they're shared. The cap evicts the oldest entry so a long-lived
// image-heavy conversation doesn't pin every downloaded byte in memory.
const MAX_CACHED_IMAGES = 100;
const imageBlobUrls = new Map<string, string>();
const inflight = new Map<string, Promise<string | null>>();

function cacheImageUrl(id: string, url: string) {
  if (imageBlobUrls.size >= MAX_CACHED_IMAGES) {
    const oldest = imageBlobUrls.keys().next().value;
    if (oldest !== undefined) {
      const oldUrl = imageBlobUrls.get(oldest);
      if (oldUrl) URL.revokeObjectURL(oldUrl);
      imageBlobUrls.delete(oldest);
    }
  }
  imageBlobUrls.set(id, url);
}

async function getImageUrl(
  id: string,
  mimeType: string
): Promise<string | null> {
  const cached = imageBlobUrls.get(id);
  if (cached) return cached;
  const pending = inflight.get(id);
  if (pending) return pending;
  const promise = (async () => {
    try {
      const res = await commandServiceClient.downloadFile({ id });
      const blob = new Blob([new Uint8Array(res.data)], {
        type: mimeType || undefined,
      });
      const url = URL.createObjectURL(blob);
      cacheImageUrl(id, url);
      return url;
    } catch (err) {
      console.error("image fetch failed", err);
      return null;
    } finally {
      inflight.delete(id);
    }
  })();
  inflight.set(id, promise);
  return promise;
}

// RemoteImage fetches an attachment's bytes via downloadFile (cached per
// attachment id) and renders them as an <img>. Two layout variants:
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
}: {
  attachment: Attachment;
  variant: "thumb" | "inline";
  onClick?: () => void;
}) {
  const { t } = useTranslation();
  const [url, setUrl] = useState<string | null>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "error">(
    "loading"
  );

  useEffect(() => {
    let cancelled = false;
    setUrl(null);
    setStatus("loading");
    // The cached object URL is shared, so there's nothing to revoke on unmount
    // (and must not be — another mounted RemoteImage may still be using it).
    getImageUrl(attachment.id, attachment.mimeType ?? "").then((url) => {
      if (cancelled) return;
      if (url) {
        setUrl(url);
        setStatus("ready");
      } else {
        setStatus("error");
      }
    });
    return () => {
      cancelled = true;
    };
  }, [attachment.id, attachment.mimeType]);

  const interactive = !!onClick;

  if (variant === "thumb") {
    return (
      <div
        className={cn(
          "size-16 shrink-0 overflow-hidden rounded-md border border-control-border bg-control-bg/40 flex items-center justify-center",
          interactive && "cursor-pointer hover:border-accent"
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
