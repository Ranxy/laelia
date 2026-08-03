import { Loader2 } from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

// Shared scaffolding for the settings pages: the scroll container + title
// header (with optional trailing actions), the centered loading marker, and
// the "no permission" notice. Every settings page previously re-implemented
// these three blocks.

// PageLoading is the centered "spinner + Loading…" block used while a settings
// page's data is in flight. `message` overrides the default "Loading…" label.
export function PageLoading({ message }: { message?: string } = {}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center justify-center gap-2 py-16 text-control-light text-sm">
      <Loader2 className="size-4 animate-spin" />
      {message ?? t("common.loading")}
    </div>
  );
}

// PermissionNotice is the "you are not allowed to view this" fallback.
export function PermissionNotice({ message }: { message: string }) {
  return (
    <div className="h-full overflow-y-auto p-6">
      <p className="text-sm text-control-light">{message}</p>
    </div>
  );
}

// SettingsPage is the standard settings-page frame: full-height scroll area,
// title + optional description on the left, `actions` on the right, then the
// page content.
export function SettingsPage({
  title,
  description,
  actions,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-5 w-full">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-semibold text-main">{title}</h1>
          {description && (
            <p className="text-sm text-control-light">{description}</p>
          )}
        </div>
        {actions}
      </div>
      {children}
    </div>
  );
}
