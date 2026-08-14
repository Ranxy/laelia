import { useTranslation } from "react-i18next";
import { Search } from "lucide-react";
import { cn } from "@/lib/utils";
import type { CommandEventFilter } from "./command-event-ledger";

export interface CommandEventToolbarProps {
  searchQuery: string;
  onSearchQueryChange: (value: string) => void;
  filter: CommandEventFilter;
  onFilterChange: (filter: CommandEventFilter) => void;
  className?: string;
}

const FILTERS: CommandEventFilter[] = [
  "all",
  "output",
  "tools",
  "diffs",
  "warnings",
  "compaction",
  "system",
];

const FILTER_LABEL_KEY: Record<CommandEventFilter, string> = {
  all: "command.filter-all",
  output: "command.filter-output",
  tools: "command.filter-tools",
  diffs: "command.filter-diffs",
  warnings: "command.filter-warnings",
  compaction: "command.filter-compaction",
  system: "command.filter-system",
};

export function CommandEventToolbar({
  searchQuery,
  onSearchQueryChange,
  filter,
  onFilterChange,
  className,
}: CommandEventToolbarProps) {
  const { t } = useTranslation();

  return (
    <div
      className={cn(
        "flex shrink-0 flex-wrap items-center gap-2 rounded border border-control-border bg-background px-2 py-1.5",
        className
      )}
    >
      <div className="flex min-w-0 flex-1 items-center gap-1.5 rounded border border-control-border bg-control-bg/50 px-2 py-1">
        <Search className="size-3 shrink-0 text-control-light" />
        <input
          type="search"
          value={searchQuery}
          onChange={(e) => onSearchQueryChange(e.target.value)}
          placeholder={t("command.search-events")}
          aria-label={t("command.search-events")}
          className="min-w-0 flex-1 bg-transparent text-xs text-control outline-none placeholder:text-control-light"
        />
      </div>

      <div className="flex items-center gap-1">
        {FILTERS.map((f) => (
          <button
            key={f}
            type="button"
            onClick={() => onFilterChange(f)}
            aria-pressed={filter === f}
            className={cn(
              "rounded px-2 py-1 text-[11px] font-medium text-control-light transition-colors hover:bg-control-bg hover:text-control",
              filter === f && "bg-accent/10 text-accent"
            )}
          >
            {t(FILTER_LABEL_KEY[f])}
          </button>
        ))}
      </div>
    </div>
  );
}
