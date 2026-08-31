import type { StatusBadgeEntry } from "@/components/ui/status-badge";
import { StatusBadge } from "@/components/ui/status-badge";
import { MachineStatus_ConnectionState } from "@/types/proto-es/v1/machine_pb";

// MachineConnectionBadge renders the machine's connection state. It mirrors
// ConnectionBadge but for the machine-scoped MachineStatus_ConnectionState
// enum (which adds a KICKED state).
const machineConnectionEntry: Partial<
  Record<MachineStatus_ConnectionState, StatusBadgeEntry>
> = {
  [MachineStatus_ConnectionState.ONLINE]: {
    variant: "success",
    labelKey: "machine.status-online",
  },
  [MachineStatus_ConnectionState.ERROR]: {
    variant: "error",
    labelKey: "machine.status-error",
  },
  [MachineStatus_ConnectionState.KICKED]: {
    variant: "error",
    labelKey: "machine.status-kicked",
  },
};

export function MachineConnectionBadge({
  state,
}: {
  state?: MachineStatus_ConnectionState;
}) {
  return (
    <StatusBadge
      mapping={machineConnectionEntry}
      status={state}
      fallback={{ variant: "secondary", labelKey: "machine.status-offline" }}
    />
  );
}
