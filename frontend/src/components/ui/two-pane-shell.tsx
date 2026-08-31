import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// Tailwind cannot see utilities assembled at runtime, so the responsive rail
// widths this shell needs are declared as literals here; callers pass the
// conventional unprefixed key (e.g. "w-72") and the shell applies its lg: twin.
const RAIL_LG_WIDTH = {
  "w-56": "lg:w-56",
  "w-60": "lg:w-60",
  "w-72": "lg:w-72",
  "w-80": "lg:w-80",
} as const;

export type TwoPaneRailWidth = keyof typeof RAIL_LG_WIDTH;

// Rail flex direction while the rail owns the full mobile width. ChatLayout's
// rail is a plain row there (its list manages its own column); every other
// shell is a column. Preserved from the copies — intentionally not unified.
export type TwoPaneRailMobileDirection = "row" | "column";

interface TwoPaneShellProps {
  /** Left rail content; stays inside the aside on every breakpoint. */
  rail: ReactNode;
  /** Detail pane content. */
  children: ReactNode;
  /** Whether a detail is open: mobile shows either rail or pane, never both. */
  detailOpen: boolean;
  /** Rail width on lg+, as the unprefixed width utility (e.g. "w-72"). */
  width: TwoPaneRailWidth;
  /** Rail flex direction while it owns the full mobile width (default "column"). */
  railMobileDirection?: TwoPaneRailMobileDirection;
  /** Extra aside classes for per-page backgrounds / overflow behavior. */
  railClassName?: string;
  /** Extra classes for the detail pane (e.g. "overflow-hidden"). */
  className?: string;
}

// TwoPaneShell is the shared responsive two-pane shell: a fixed-width left
// rail (always visible on lg+) and a main detail pane. On touch layouts
// exactly one pane is visible at a time, driven by `detailOpen` — the rail
// hides and the pane takes the full width while a detail is open, and the
// pane is the one hidden (until lg) when no detail is open. Differences the
// copies used to carry (width, mobile rail direction, rail/pane overflow and
// backgrounds) are explicit props, not silently unified.
export function TwoPaneShell({
  rail,
  children,
  detailOpen,
  width,
  railMobileDirection = "column",
  railClassName,
  className,
}: TwoPaneShellProps) {
  return (
    <div className="flex h-full w-full overflow-hidden">
      <aside
        className={cn(
          "shrink-0 border-r border-control-border",
          detailOpen
            ? cn("hidden", width, "lg:flex")
            : cn("flex w-full", RAIL_LG_WIDTH[width]),
          railMobileDirection === "row" ? "lg:flex-col" : "flex-col",
          railClassName
        )}
      >
        {rail}
      </aside>
      {/* Detail pane: mobile shows it only when a detail is open. */}
      <main
        className={cn(
          "min-w-0 flex-1",
          !detailOpen && "hidden lg:block",
          className
        )}
      >
        {children}
      </main>
    </div>
  );
}
