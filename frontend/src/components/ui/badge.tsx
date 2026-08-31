import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full px-3 py-0.5 text-sm font-medium",
  {
    variants: {
      variant: {
        default: "bg-control-bg text-control",
        secondary: "bg-accent/10 text-accent",
        error: "bg-error/10 text-error",
        warning: "bg-warning/10 text-warning",
        success: "bg-success/10 text-success",
      },
      size: {
        md: "",
        sm: "px-1.5 py-0 text-[10px] leading-4",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "md",
    },
  }
);

type BadgeProps = ComponentProps<"span"> & VariantProps<typeof badgeVariants>;
type BadgeVariant = NonNullable<BadgeProps["variant"]>;

function Badge({ className, variant, size, ...props }: BadgeProps) {
  return (
    <span
      className={cn(badgeVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export type { BadgeProps, BadgeVariant };
export { Badge };
