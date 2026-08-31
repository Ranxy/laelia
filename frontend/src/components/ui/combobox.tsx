import { ChevronDown, Loader2 } from "lucide-react";
import type { FocusEvent, KeyboardEvent } from "react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/utils";
import { Input } from "./input";
import { getLayerRoot, LAYER_SURFACE_CLASS } from "./layer";
import { POPUP_SURFACE_CLASS } from "./positioned-popup";

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
  // Default (true): the popup renders through the shared overlay layer root,
  // so clipped/stacked ancestors (e.g. a SheetBody scroll container) cannot
  // cut it off. false keeps the popup anchored locally under the input —
  // the legacy in-container rendering for callers that must stay inline.
  portal?: boolean;
}

// Keep in sync with the popup's `max-h-60`.
const POPUP_MAX_HEIGHT_PX = 240;
// Anchor gap between the trigger and the popup.
const POPUP_GAP_PX = 4;

// ModelCombobox is a searchable, free-text model picker. The input shows the
// current model id; as the user types, the dropdown filters the fetched options
// by id/name (case-insensitive). Picking an option sets the value to its id;
// typing a custom id the API did not return is also accepted (free-text
// fallback). Keyboard: ArrowUp/Down to move, Enter to pick the highlighted
// option (or commit the typed text), Escape to close. The popup follows the
// `portal` prop; dismissal uses outside-click (pointerdown) and focus leaving
// the control — no deferred timers.
export function ModelCombobox({
  value,
  onValueChange,
  options,
  loading = false,
  placeholder,
  disabled = false,
  emptyLabel,
  className,
  portal = true,
}: ModelComboboxProps) {
  const [open, setOpen] = useState(false);
  const [highlight, setHighlight] = useState(-1);
  const containerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const popupRef = useRef<HTMLDivElement>(null);

  const query = value.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      query === ""
        ? options
        : options.filter(
            (o) =>
              o.id.toLowerCase().includes(query) ||
              o.name.toLowerCase().includes(query)
          ),
    [options, query]
  );

  // Close when the press lands outside the trigger and the (portaled) popup.
  useEffect(() => {
    if (!open) return;
    function onPointerDown(e: Event) {
      const target = e.target as Node | null;
      if (
        !containerRef.current?.contains(target) &&
        !popupRef.current?.contains(target)
      ) {
        setOpen(false);
      }
    }
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [open]);

  // Only the portal path is fixed-positioned against the viewport; the local
  // (portal={false}) path flows under the input with plain `absolute` layout.
  useLayoutEffect(() => {
    if (!open || !portal) return;
    const update = () => {
      const anchor = containerRef.current;
      const popup = popupRef.current;
      if (!anchor || !popup) return;
      const rect = anchor.getBoundingClientRect();
      const popupHeight = Math.min(
        popup.offsetHeight || POPUP_MAX_HEIGHT_PX,
        POPUP_MAX_HEIGHT_PX
      );
      const spaceBelow = window.innerHeight - rect.bottom;
      // Flip above the trigger when the popup cannot fit below and there is
      // more room above than below.
      const openUp =
        spaceBelow < popupHeight + POPUP_GAP_PX && rect.top > spaceBelow;
      const usableHeight = Math.max(
        0,
        (openUp ? rect.top : spaceBelow) - POPUP_GAP_PX
      );
      const top = openUp
        ? rect.top - POPUP_GAP_PX - Math.min(popupHeight, usableHeight)
        : rect.bottom + POPUP_GAP_PX;
      popup.style.left = `${rect.left}px`;
      popup.style.top = `${top}px`;
      popup.style.width = `${rect.width}px`;
      popup.style.maxHeight = `${Math.min(POPUP_MAX_HEIGHT_PX, usableHeight)}px`;
    };
    update();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [open, portal]);

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

  // Close immediately when focus leaves the control entirely (e.g. Tab).
  // A mousedown on an option never blurs the input (mousedown is
  // preventDefault-ed), so no deferred-close timing compensation is needed.
  function handleBlur(e: FocusEvent<HTMLInputElement>) {
    const next = e.relatedTarget as Node | null;
    if (
      !containerRef.current?.contains(next) &&
      !popupRef.current?.contains(next)
    ) {
      setOpen(false);
    }
  }

  // Shared option-list body for the portal and local rendering paths.
  function listboxBody() {
    return loading ? (
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
    );
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
        onBlur={handleBlur}
      />
      <ChevronDown className="pointer-events-none absolute right-2 top-1/2 size-3.5 -translate-y-1/2 opacity-50" />
      {open &&
        (portal ? (
          // Shared overlay portal: no ancestor scroll container can clip the
          // popup; fixed position is pinned under the trigger by the effect.
          createPortal(
            <div
              ref={popupRef}
              className={cn(
                "fixed max-h-60 overflow-auto",
                POPUP_SURFACE_CLASS,
                LAYER_SURFACE_CLASS
              )}
              role="listbox"
            >
              {listboxBody()}
            </div>,
            getLayerRoot("overlay")
          )
        ) : (
          // Legacy in-container path: flows under the input inside the
          // relative wrapper. LAYER_SURFACE_CLASS keeps the stacking local
          // and policy-clean instead of the retired raw z-50.
          <div
            ref={popupRef}
            className={cn(
              "absolute mt-1 max-h-60 w-full overflow-auto",
              POPUP_SURFACE_CLASS,
              LAYER_SURFACE_CLASS
            )}
            role="listbox"
          >
            {listboxBody()}
          </div>
        ))}
    </div>
  );
}
