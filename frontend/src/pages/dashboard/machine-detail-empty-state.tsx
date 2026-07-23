import { useTranslation } from "react-i18next";

export function MachineDetailEmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full items-center justify-center p-8">
      <p className="text-sm text-control-light">{t("machine.no-selection")}</p>
    </div>
  );
}
