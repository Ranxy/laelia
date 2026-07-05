import { useTranslation } from "react-i18next";

export function AgentDetailEmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex h-full items-center justify-center p-8">
      <p className="text-sm text-control-light">{t("agent.no-selection")}</p>
    </div>
  );
}
