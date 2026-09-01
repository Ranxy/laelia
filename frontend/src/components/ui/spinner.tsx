import { cva, type VariantProps } from "class-variance-authority";
import { Loader2 } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

const spinnerVariants = cva("animate-spin", {
  variants: {
    size: {
      xs: "size-3",
      sm: "size-3.5",
      md: "size-4",
      lg: "size-5",
    },
  },
  defaultVariants: {
    size: "md",
  },
});

type SpinnerProps = Omit<ComponentProps<typeof Loader2>, "size"> &
  VariantProps<typeof spinnerVariants>;

/** Shared loading spinner. Render next to visible text ("Loading…") or an
 *  aria-label on the wrapping control so the state is announced. */
export function Spinner({ className, size, ...props }: SpinnerProps) {
  return (
    <Loader2 className={cn(spinnerVariants({ size, className }))} {...props} />
  );
}
