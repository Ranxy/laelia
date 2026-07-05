import { useTranslation } from "react-i18next";

// Placeholder — full joined-channels list lands in a follow-up commit.
export function AgentChatPage() {
  const { t } = useTranslation();
  return (
    <div className="h-full overflow-y-auto p-6">
      <p className="text-sm text-control-light">{t("agent.tab-chat")}</p>
    </div>
  );
}
