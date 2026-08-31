import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// RailRow is one clickable row of a two-pane rail list (machines / members).
// It renders as a div with button semantics so a rail can nest interactive
// controls (e.g. the per-machine delete button) inside the row. The selected
// treatment — left accent border + control background — is the shared style
// the machines / members rails used to each copy byte for byte.
export function RailRow({
  selected,
  label,
  className,
  onSelect,
  children,
}: {
  selected: boolean;
  /** Accessible row name. */
  label: string;
  className?: string;
  onSelect: () => void;
  children: ReactNode;
}) {
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={label}
      className={cn(
        "group flex cursor-pointer items-center gap-2 px-3 py-2 transition-colors border-l-2",
        selected
          ? "border-l-accent bg-control-bg"
          : "border-l-transparent hover:bg-control-bg/60",
        className
      )}
      onClick={onSelect}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelect();
        }
      }}
    >
      {children}
    </div>
  );
}
