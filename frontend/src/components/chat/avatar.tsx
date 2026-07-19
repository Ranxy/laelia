import { cn } from "@/lib/utils";

// Shared by both chat pages (DM + channel) so the avatar styling and label
// derivation stay in one place. The accent (current-user) style is selected by
// the explicit `accent` flag when provided, otherwise by the legacy `label ===
// "U"` convention; callers that know whether a row is the current user should
// pass `accent` explicitly rather than relying on the "U" sentinel, since a
// sender whose name starts with "U" would otherwise be mis-styled as the
// current user.
export function Avatar({ label, accent }: { label: string; accent?: boolean }) {
  const isOwn = accent ?? label === "U";
  return (
    <div
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
        isOwn
          ? "bg-accent text-accent-foreground"
          : "bg-control-bg text-control"
      )}
    >
      {label.charAt(0).toUpperCase()}
    </div>
  );
}

// Shared HH:MM time formatter for message row headers.
export function formatTime(date: Date): string {
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  return `${hours}:${minutes}`;
}
