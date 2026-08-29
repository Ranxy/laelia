import { ArrowDown, ArrowUp, ArrowUpDown } from "lucide-react";
import { type ComponentPropsWithoutRef, useRef } from "react";
import { ColumnResizeHandle } from "@/components/ui/column-resize-handle";
import { cn } from "@/lib/utils";

function Table({ className, ...props }: ComponentPropsWithoutRef<"table">) {
  return (
    <table
      className={cn("w-full caption-bottom text-sm", className)}
      {...props}
    />
  );
}

function TableHeader({
  className,
  ...props
}: ComponentPropsWithoutRef<"thead">) {
  return <thead className={cn("[&_tr]:border-b", className)} {...props} />;
}

interface TableBodyProps extends ComponentPropsWithoutRef<"tbody"> {
  striped?: boolean;
}

function TableBody({ className, striped = true, ...props }: TableBodyProps) {
  return (
    <tbody
      className={cn(
        "[&_tr:last-child]:border-0",
        striped && "[&_tr:nth-child(even)]:bg-control-bg/50",
        className
      )}
      {...props}
    />
  );
}

interface TableRowProps extends ComponentPropsWithoutRef<"tr"> {
  striped?: boolean;
}

function TableRow({ className, striped = true, ...props }: TableRowProps) {
  return (
    <tr
      data-striped={striped ? undefined : "false"}
      className={cn(
        "border-b border-block-border transition-colors hover:bg-control-bg/60 data-[state=selected]:!bg-control-bg",
        !striped && "!bg-transparent",
        className
      )}
      {...props}
    />
  );
}

export type TableHeadSortDirection = "asc" | "desc";

interface TableHeadProps extends ComponentPropsWithoutRef<"th"> {
  /** Show a sort indicator and make the header clickable. */
  sortable?: boolean;
  /** Whether this column is the currently active sort column. */
  sortActive?: boolean;
  /** Current direction when `sortActive` is true. */
  sortDir?: TableHeadSortDirection;
  /** Called when the user clicks to toggle sort. */
  onSort?: () => void;
  /** Render a drag-to-resize handle on the right edge. */
  resizable?: boolean;
  /** Called when the user starts dragging the resize handle. */
  onResizeStart?: (e: React.MouseEvent) => void;
}

function TableHead({
  className,
  children,
  sortable,
  sortActive,
  sortDir,
  onSort,
  resizable,
  onResizeStart,
  onClick,
  onMouseDown,
  ...props
}: TableHeadProps) {
  // A resize drag ends with the browser synthesizing a click on (or bubbling
  // through) this header; suppress the sort toggle for it. The flag reads as
  // "a resize is pending its trailing click" and is re-armed by the next real
  // press on the header.
  const resizeJustEnded = useRef(false);
  return (
    <th
      className={cn(
        "h-10 px-4 py-2 text-left align-middle font-medium text-control-light",
        sortable && "cursor-pointer select-none hover:text-control",
        resizable && "relative",
        className
      )}
      onMouseDown={(e) => {
        // A genuine press anywhere on the header re-arms sorting.
        resizeJustEnded.current = false;
        onMouseDown?.(e);
      }}
      onClick={(e) => {
        onClick?.(e);
        const justResized = resizeJustEnded.current;
        resizeJustEnded.current = false;
        if (sortable && !e.defaultPrevented && !justResized) onSort?.();
      }}
      {...props}
    >
      {sortable ? (
        <span className="inline-flex items-center gap-x-1">
          {children}
          <SortIndicator active={!!sortActive} dir={sortDir} />
        </span>
      ) : (
        children
      )}
      {resizable && onResizeStart && (
        <ColumnResizeHandle
          onPointerDown={(e) => e.stopPropagation()}
          onMouseDown={(e) => {
            resizeJustEnded.current = true;
            e.stopPropagation();
            onResizeStart?.(e);
          }}
          onClick={(e) => e.stopPropagation()}
        />
      )}
    </th>
  );
}

function SortIndicator({
  active,
  dir,
}: {
  active: boolean;
  dir?: TableHeadSortDirection;
}) {
  const Icon = active ? (dir === "asc" ? ArrowUp : ArrowDown) : ArrowUpDown;
  return (
    <Icon
      className={cn(
        "w-3.5 h-3.5",
        active ? "text-accent" : "text-control-placeholder"
      )}
    />
  );
}

function TableCell({ className, ...props }: ComponentPropsWithoutRef<"td">) {
  return (
    <td
      className={cn("px-4 py-3 align-middle text-sm text-control", className)}
      {...props}
    />
  );
}

export { Table, TableBody, TableCell, TableHead, TableHeader, TableRow };
