import type { ComponentProps } from "react";
import { cn } from "@/react/lib/utils";

function ColumnResizeHandle({
  className,
  ref,
  ...props
}: ComponentProps<"div">) {
  return (
    <div
      ref={ref}
      className={cn(
        "absolute right-0 top-0 h-full w-1 cursor-col-resize select-none",
        className
      )}
      {...props}
    />
  );
}

export { ColumnResizeHandle };
