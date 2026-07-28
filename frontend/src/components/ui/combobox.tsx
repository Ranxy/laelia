import { ChevronDown, Loader2 } from "lucide-react";
import type { KeyboardEvent } from "react";
import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import { Input } from "./input";

export interface ModelComboboxOption {
  id: string;
  name: string;
}

export interface ModelComboboxProps {
  value: string;
  onValueChange: (value: string) => void;
  options: ModelComboboxOption[];
  loading?: boolean;
  placeholder?: string;
  disabled?: boolean;
  emptyLabel?: string;
  className?: string;
}

// ModelCombobox is a searchable, free-text model picker. The input shows the
// current model id; as the user types, the dropdown filters the fetched options
// by id/name (case-insensitive). Picking an option sets the value to its id;
// typing a custom id the API did not return is also accepted (free-text
// fallback). Keyboard: ArrowUp/Down to move, Enter to pick the highlighted
// option (or commit the typed text), Escape to close.
export function ModelCombobox({
  value,
  onValueChange,
  options,
  loading = false,
  placeholder,
  disabled = false,
  emptyLabel,
  className,
}: ModelComboboxProps) {
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const query = value.trim().toLowerCase();
  const filtered =
    query === ""
      ? options
      : options.filter(
          (o) =>
            o.id.toLowerCase().includes(query) ||
            o.name.toLowerCase().includes(query)
        );

  // Close on outside click / Escape handled in onKeyDown.
  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: MouseEvent) {
      if (!containerRef.current?.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [open]);

  function choose(option: ModelComboboxOption) {
    onValueChange(option.id);
    setOpen(false);
    setHighlight(-1);
    inputRef.current?.blur();
  }

  function handleKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setOpen(true);
      setHighlight((h) => Math.min(h + 1, filtered.length - 1));
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      setHighlight((h) => Math.max(h - 1, 0));
      return;
    }
    if (e.key === "Enter") {
      if (open && highlight >= 0 && highlight < filtered.length) {
        e.preventDefault();
        choose(filtered[highlight]);
      } else {
        setOpen(false);
      }
      return;
    }
    if (e.key === "Escape") {
      setOpen(false);
      setHighlight(-1);
    }
  }

  return (
    <div ref={containerRef} className={cn("relative", className)}>
      <Input
        ref={inputRef}
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        autoComplete="off"
        spellCheck={false}
        onFocus={() => setOpen(true)}
        onChange={(e) => {
          onValueChange(e.target.value);
          setOpen(true);
          setHighlight(-1);
        }}
        onKeyDown={handleKeyDown}
        onBlur={() => {
          // Defer so a mousedown on an option can select before we close.
          window.setTimeout(() => setOpen(false), 120);
        }}
      />
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 size-3.5 -translate-y-1/2 opacity-50" />
      {open && (
        <div
          className="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-sm border border-control-border bg-background py-1 shadow-md"
          role="listbox"
        >
          {loading ? (
            <div className="flex items-center gap-2 px-2 py-1.5 text-xs text-control-placeholder">
              <Loader2 className="size-3.5 animate-spin" />
            </div>
          ) : filtered.length === 0 ? (
            <div className="px-2 py-1.5 text-xs text-control-placeholder">
              {emptyLabel ?? value}
            </div>
          ) : (
            filtered.map((option, i) => (
              <button
                key={option.id}
                type="button"
                onMouseDown={(e) => {
                  e.preventDefault();
                  choose(option);
                }}
                onMouseEnter={() => setHighlight(i)}
                className={cn(
                  "flex w-full items-start gap-2 px-2 py-1.5 text-left text-sm",
                  i === highlight ? "bg-control-bg" : "hover:bg-control-bg"
                )}
                title={option.id}
              >
                <span className="min-w-0 flex-1 truncate text-main">
                  {option.name}
                </span>
                {option.name !== option.id && (
                  <span className="shrink-0 text-xs text-control-placeholder">
                    {option.id}
                  </span>
                )}
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
}
