import { type ReactNode, useEffect } from "react";
import { createPortal } from "react-dom";
import {
  getLayerRoot,
  LAYER_SURFACE_CLASS,
  usePreserveHigherLayerAccess,
} from "@/components/ui/layer";

// FilePreviewShell is the shared chrome of the three full-page preview
// overlays (html / markdown / image): a portal into the overlay layer, a
// fixed-inset surface, an h-14 top bar (icon + file name + size meta +
// action buttons), and an Esc-closes window keydown binding. The overlays
// keep only their differentiated body content. Overlay-specific classes are
// appended per caller so the emitted class lists stay identical to the
// pre-merge markup.
export function FilePreviewShell({
  icon,
  title,
  meta,
  barClassName,
  className,
  actions,
  onEscape,
  children,
}: {
  icon?: ReactNode;
  title: string;
  meta?: ReactNode;
  barClassName?: string;
  className?: string;
  actions?: ReactNode;
  onEscape: () => void;
  children: ReactNode;
}) {
  usePreserveHigherLayerAccess("overlay");

  // Esc closes. Bound only while mounted (the overlays mount only while the
  // preview is open).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onEscape();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onEscape]);

  return createPortal(
    <div
      className={`fixed inset-0 ${LAYER_SURFACE_CLASS} flex flex-col${className ? ` ${className}` : ""}`}
    >
      {/* Top bar */}
      <div
        className={`flex h-14 shrink-0 items-center gap-2 border-b border-control-border px-4${barClassName ? ` ${barClassName}` : ""}`}
      >
        {icon}
        <span className="truncate text-sm font-medium text-main">{title}</span>
        {meta != null && (
          <span className="shrink-0 text-xs text-control-placeholder">
            {meta}
          </span>
        )}
        <div className="flex-1" />
        {actions}
      </div>
      {children}
    </div>,
    getLayerRoot("overlay")
  );
}

// PreviewPlaceholder is the centered status view (loading / error / too
// large) shared by the html and markdown file overlays.
export function PreviewPlaceholder({
  icon,
  text,
  action,
}: {
  icon?: ReactNode;
  text: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      {icon}
      <p className="text-sm text-control">{text}</p>
      {action}
    </div>
  );
}
