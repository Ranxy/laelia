import type { ComponentProps } from "react";
import { cn } from "@/react/lib/utils";

// The grab target is a 12px-wide invisible strip straddling the column border
// (right-[-6px] + w-3 centers it on the edge) so the affordance is easy to hit;
// the visible 3px bar inside it signals the resizer. z-10 keeps it above cell
// content so the pointer stays over the handle, not the neighbouring cell.
function ColumnResizeHandle({
  className,
  ref,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      ref={ref}
      className={cn(
        "absolute right-[-6px] top-0 z-10 flex h-full w-3 cursor-col-resize select-none items-center justify-center",
        className
      )}
      {...props}
    >
      <div className="h-full w-[3px] rounded-full bg-block-border" />
    </div>
  );
}

export { ColumnResizeHandle };
