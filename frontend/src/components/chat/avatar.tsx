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
  size = 8,
}: {
  src?: string | null;
  seed: string;
  label?: string;
  accent?: boolean;
  size?: 6 | 7 | 8 | 10 | 12 | 14 | 16;
}) {
  const [imgFailed, setImgFailed] = useState(false);
  const showImage = src && !imgFailed;
  const sizeClass = `size-${size}`;

  if (showImage) {
    return (
      // eslint-disable-next-line jsx-a11y/img-redundant-alt -- alt is empty so
      // screen readers skip the decorative avatar; the adjacent header carries
      // the sender's name.
      <img
        src={src}
        alt=""
        className={cn("shrink-0 rounded-full object-cover", sizeClass)}
        onError={() => setImgFailed(true)}
      />
    );
  }

  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-center rounded-full text-xs font-semibold overflow-hidden",
        sizeClass,
        accent ? "bg-accent text-accent-foreground" : "bg-transparent"
      )}
    >
      {seed ? (
        <PixelAvatar seed={seed} size={size * 4} />
      ) : (
        <span>{(label ?? "?").charAt(0).toUpperCase()}</span>
      )}
    </div>
  );
}

// Shared time formatter for message row headers. Today's messages show the
// time only; older messages include the date (plus the year when it differs
// from the current year) so history rows are distinguishable by day. Both
// parts follow the active locale (12/24-hour clock, date order).
export function formatTime(date: Date, locale: string): string {
  const now = new Date();
  const time = new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
  if (
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()
  ) {
    return time;
  }
  const datePart = new Intl.DateTimeFormat(locale, {
    month: "numeric",
    day: "numeric",
    ...(date.getFullYear() !== now.getFullYear() && { year: "numeric" }),
  }).format(date);
  return `${datePart} ${time}`;
}
