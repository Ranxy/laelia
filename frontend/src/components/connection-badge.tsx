import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";

interface ConnectionBadgeProps {
  state?: AgentStatus_ConnectionState;
}

function ConnectionBadge({ state }: ConnectionBadgeProps) {
  const { t } = useTranslation();
  switch (state) {
    case AgentStatus_ConnectionState.ONLINE:
      return <Badge variant="success">{t("agent.status-online")}</Badge>;
    case AgentStatus_ConnectionState.ERROR:
      return <Badge variant="destructive">{t("agent.status-error")}</Badge>;
    default:
      return <Badge variant="secondary">{t("agent.status-offline")}</Badge>;
  }
}

export { ConnectionBadge };
