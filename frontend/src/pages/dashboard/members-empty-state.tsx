import { useTranslation } from "react-i18next";

// MembersEmptyState is the right-pane placeholder shown at /members before a
// member is selected. Mirrors MachineDetailEmptyState.
export function MembersEmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full items-center justify-center p-8">
      <p className="text-sm text-control-light">{t("members.no-selection")}</p>
    </div>
  );
}
