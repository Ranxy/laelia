import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useEdgeDragToClose } from "@/hooks/use-edge-drag-to-close";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { cn } from "@/lib/utils";
import { Sheet, SheetContent, SheetTitle } from "./sheet";

interface SidePanelProps {
  /** Whether the panel is shown. Hosts that conditionally mount the panel
   *  can omit it (defaults to open). */
  open?: boolean;
  /** Close affordance in the header and the mobile drawer dismissal. When
   *  omitted the panel renders no close button of its own (the host surface
   *  owns the toggle). */
  onClose?: () => void;
  /** Plain-text accessible name: aria-label on the aside, sr-only SheetTitle
   *  in the mobile drawer. */
  label: string;
  /** Leading header icon. */
  icon?: ReactNode;
  /** Header title, truncated, takes the free width. */
  title?: ReactNode;
  /** Right-aligned header meta (timestamp, size, ...). */
  meta?: ReactNode;
  /** Extra header affordances rendered before the close button. */
  actions?: ReactNode;
  /** Rows pinned between header and scrollable body (tabs, toolbars). */
  toolbar?: ReactNode;
  /** Pinned bottom region (composers, footers). */
  footer?: ReactNode;
  /** Mobile: present as a swipe-to-close right-edge Sheet instead of the
   *  in-place aside (which would cover the content it overlays). Desktop
   *  always renders the in-place aside. */
  mobileSheet?: boolean;
  /** Surface classes for the aside: positioning plus width plus card look. */
  className?: string;
  /** Header surface overrides (e.g. a tinted strip behind the title). */
  headerClassName?: string;
  children: ReactNode;
}

function SidePanelHeader({
  icon,
  title,
  meta,
  actions,
  onClose,
  className,
}: {
  icon?: ReactNode;
  title?: ReactNode;
  meta?: ReactNode;
  actions?: ReactNode;
  onClose?: () => void;
  className?: string;
}) {
  const { t } = useTranslation();
  return (
    <div
      className={cn(
        "flex shrink-0 items-center gap-2 border-b border-control-border px-3 py-2",
        className
      )}
    >
      {icon}
      {/* Always rendered: the flex-1 spacer pushes meta/actions right even
          when there is no title. */}
      <span className="min-w-0 flex-1 truncate text-xs font-semibold text-main">
        {title}
      </span>
      {meta && (
        <span className="shrink-0 text-[10px] text-control-light">{meta}</span>
      )}
      {actions}
      {onClose && (
        <button
          type="button"
          onClick={onClose}
          aria-label={t("common.close")}
          className="flex size-5 shrink-0 items-center justify-center rounded text-control-light hover:bg-control-bg hover:text-control"
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}

// SidePanel is the shared shell for titled right-side panels that live INSIDE
// a host surface (next to a ledger, inside a preview overlay) — as opposed to
// Sheet, the app-level resource drawer. It owns the header row (icon, title,
// meta, close), an optional pinned toolbar, the scrollable body, and an
// optional pinned footer. With `mobileSheet`, narrow viewports get the
// ChatDrawerSheet treatment (right-edge Sheet with the edge drag-to-close and
// history-sentinel semantics) instead of an in-place aside covering the
// content it overlays.
export function SidePanel({
  open = true,
  onClose,
  label,
  icon,
  title,
  meta,
  actions,
  toolbar,
  footer,
  mobileSheet = false,
  className,
  headerClassName,
  children,
}: SidePanelProps) {
  const isDesktop = useIsDesktop();
  const { setPopup, setOverlay } = useEdgeDragToClose({
    open: mobileSheet && !isDesktop && open,
    onClose: onClose ?? (() => {}),
  });

  const header = (
    <SidePanelHeader
      icon={icon}
      title={title}
      meta={meta}
      actions={actions}
      onClose={onClose}
      className={headerClassName}
    />
  );

  const body = (
    <>
      {toolbar}
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
      {footer && (
        <div className="shrink-0 border-t border-control-border">{footer}</div>
      )}
    </>
  );

  if (!(mobileSheet && !isDesktop)) {
    if (!open) return null;
    return (
      <aside
        aria-label={label}
        className={cn(
          "flex min-h-0 flex-col overflow-hidden border-l border-control-border bg-background",
          className
        )}
      >
        {header}
        {body}
      </aside>
    );
  }

  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose?.()}>
      <SheetContent width="medium" ref={setPopup} overlayRef={setOverlay}>
        <SheetTitle className="sr-only">{label}</SheetTitle>
        {header}
        {body}
      </SheetContent>
    </Sheet>
  );
}
