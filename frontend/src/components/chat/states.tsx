import { Loader2, type LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

type DivProps = ComponentProps<"div">;

// Shared "spinner + label" loading marker. Several list pages (chat,
// channel-chat, channel-list) duplicate the same layout, so it lives here.
export function LoadingState({
  className,
  ...props
}: DivProps & { children?: ReactNode }) {
  const { t } = useTranslation();
  return (
    <div
      className={cn(
        "flex items-center justify-center gap-2 py-12 text-control-light text-sm",
        className
      )}
      {...props}
    >
      <Loader2 className="size-4 animate-spin" />
      {props.children ?? t("common.loading")}
    </div>
  );
}

// Shared "icon + message + optional action" empty-state marker. The icon and
// action let callers compose a channel-specific empty state without
// reimplementing the centered layout.
export function EmptyState({
  icon: Icon,
  message,
  action,
  className,
}: {
  icon: LucideIcon;
  message: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 py-24 text-center",
        className
      )}
    >
      <div className="flex size-14 items-center justify-center rounded-full bg-control-bg text-control-light">
        <Icon className="size-6" />
      </div>
      <p className="text-control-light text-sm">{message}</p>
      {action}
    </div>
  );
}
