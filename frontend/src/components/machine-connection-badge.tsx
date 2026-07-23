import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";

// MachineConnectionBadge renders the machine's connection state. It mirrors
// ConnectionBadge but for the machine-scoped MachineStatus_ConnectionState
// enum (which adds a KICKED state).
export function MachineConnectionBadge({
  state,
}: {
  state?: MachineStatus_ConnectionState;
}) {
  const { t } = useTranslation();
  switch (state) {
    case MachineStatus_ConnectionState.ONLINE:
      return <Badge variant="success">{t("machine.status-online")}</Badge>;
    case MachineStatus_ConnectionState.ERROR:
      return <Badge variant="destructive">{t("machine.status-error")}</Badge>;
    case MachineStatus_ConnectionState.KICKED:
      return <Badge variant="destructive">{t("machine.status-kicked")}</Badge>;
    default:
      return <Badge variant="secondary">{t("machine.status-offline")}</Badge>;
  }
}
