import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/react/lib/utils";

type TooltipProps = ComponentProps<"div"> & {
  content?: ReactNode;
};

function Tooltip({ className, children, ref, ...props }: TooltipProps) {
  return (
    <div ref={ref} className={cn("relative inline-flex", className)} {...props}>
      {children}
    </div>
  );
}

export type { TooltipProps };
export { Tooltip };
