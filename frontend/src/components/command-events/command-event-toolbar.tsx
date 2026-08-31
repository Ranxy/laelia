import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { SearchInput } from "@/components/ui/search-input";
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

// Search runs over the full merged output content, so every keystroke is a
// full-corpus scan (08 F-R7/F-P3): keep the input controlled by local state
// and push upstream only after a 250ms debounce, mirroring the chat search
// panels' cadence.
const SEARCH_DEBOUNCE_MS = 250;

export function CommandEventToolbar({
  searchQuery,
  onSearchQueryChange,
  filter,
  onFilterChange,
  className,
}: CommandEventToolbarProps) {
  const { t } = useTranslation();
  const [inputValue, setInputValue] = useState(searchQuery);

  // The parent owns the committed query (resets on command switch): external
  // changes win over the stale local value.
  useEffect(() => {
    setInputValue(searchQuery);
  }, [searchQuery]);

  useEffect(() => {
    if (inputValue === searchQuery) return;
    const timer = window.setTimeout(() => {
      onSearchQueryChange(inputValue);
    }, SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [inputValue, searchQuery, onSearchQueryChange]);

  return (
    <div
      className={cn(
        "flex shrink-0 flex-wrap items-center gap-2 rounded border border-control-border bg-background px-2 py-1.5",
        className
      )}
    >
      <SearchInput
        type="search"
        value={inputValue}
        onChange={(e) => setInputValue(e.target.value)}
        placeholder={t("command.search-events")}
        aria-label={t("command.search-events")}
        className="h-7 min-w-0 border-control-border bg-control-bg/50 text-xs"
      />

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
