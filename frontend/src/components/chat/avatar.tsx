import { cn } from "@/lib/utils";

// Shared by both chat pages (DM + channel) so the avatar styling and label
// derivation stay in one place. `label === "U"` marks the current user; every
// other label renders the agent/sender identity's first character.
export function Avatar({ label }: { label: string }) {
  return (
    <div
      className={cn(
        "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
        label === "U"
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
