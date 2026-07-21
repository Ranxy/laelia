import { useState } from "react";
import { PixelAvatar } from "@/components/chat/pixel-avatar";
import { cn } from "@/lib/utils";

// Shared by both chat pages (DM + channel), thread panels, and comment asides
// so the avatar rendering stays in one place.
//
// - When `src` is present (a cached blob URL of the user's uploaded image),
//   render it as a cover-fit image, falling back to the pixel identicon if the
//   image fails to load.
// - Otherwise render a deterministic pixel identicon seeded by `seed` (a stable
//   user/agent id). This is the zero-bandwidth default avatar.
//
// `label`/`accent` are accepted for backwards compatibility with call sites that
// haven't been migrated yet; they only affect the legacy letter fallback path.
export function Avatar({
  src,
  seed,
  label,
  accent,
}: {
  src?: string | null;
  seed: string;
  label?: string;
  accent?: boolean;
}) {
  const [imgFailed, setImgFailed] = useState(false);
  const showImage = src && !imgFailed;

  if (showImage) {
    return (
      // eslint-disable-next-line jsx-a11y/img-redundant-alt -- alt is empty so
      // screen readers skip the decorative avatar; the adjacent header carries
      // the sender's name.
      <img
        src={src}
        alt=""
        className="size-8 shrink-0 rounded-full object-cover"
        onError={() => setImgFailed(true)}
      />
    );
  }

  return (
    <div
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold overflow-hidden",
        accent ? "bg-accent text-accent-foreground" : "bg-transparent"
      )}
    >
      {seed ? (
        <PixelAvatar seed={seed} size={32} />
      ) : (
        <span>{(label ?? "?").charAt(0).toUpperCase()}</span>
      )}
    </div>
  );
}

// Shared HH:MM time formatter for message row headers.
export function formatTime(date: Date): string {
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  return `${hours}:${minutes}`;
}
