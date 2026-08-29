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
// `accent` is accepted for backwards compatibility with call sites that
// haven't been migrated yet; it only affects the legacy fallback styling.
//
// `online` renders the standard chat-app presence badge: a green dot pinned to
// the avatar's bottom-right corner (ringed by the background color so it reads
// on top of the image). `undefined` renders no dot at all — offline avatars
// stay plain, and call sites that don't care keep passing nothing.
export function Avatar({
  src,
  seed,
  accent,
  size = 8,
  online,
  title,
}: {
  src?: string | null;
  seed: string;
  accent?: boolean;
  size?: 6 | 7 | 8 | 10 | 12 | 14 | 16;
  online?: boolean;
  // Tooltip carried by the badge wrapper (e.g. the localized "Online" label).
  title?: string;
}) {
  const [imgFailed, setImgFailed] = useState(false);
  const showImage = src && !imgFailed;
  const sizeClass = `size-${size}`;
  const core = showImage ? (
    // alt is empty so screen readers skip the decorative avatar; the adjacent
    // header carries the sender's name.
    <img
      src={src}
      alt=""
      className={cn("shrink-0 rounded-full object-cover", sizeClass)}
      onError={() => setImgFailed(true)}
    />
  ) : (
    <div
      className={cn(
        "flex shrink-0 items-center justify-center rounded-full text-xs font-semibold overflow-hidden",
        sizeClass,
        accent ? "bg-accent text-accent-foreground" : "bg-transparent"
      )}
    >
      {seed ? <PixelAvatar seed={seed} size={size * 4} /> : <span>?</span>}
    </div>
  );

  // Only a true online flag wraps + badges; false/undefined keep the plain
  // avatar (offline peers show no dot).
  if (!online) return core;

  return (
    <span
      className={cn("relative inline-flex shrink-0", sizeClass)}
      title={title}
    >
      {core}
      <span
        data-testid="presence-badge"
        className={cn(
          "absolute right-0 bottom-0 rounded-full bg-success ring-2 ring-background",
          // The dot scales with the avatar: ~1/3 of a 32px avatar, smaller on
          // the 24px variant used in compact lists.
          size <= 6 ? "size-2" : "size-2.5"
        )}
      />
    </span>
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
