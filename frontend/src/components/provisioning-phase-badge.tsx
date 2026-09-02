import type { StatusBadgeEntry } from "@/components/ui/status-badge";
import { mergeStatusMapping, StatusBadge } from "@/components/ui/status-badge";
import {
  provisioningPhaseToI18nKey,
  provisioningPhaseToVariant,
} from "@/lib/provisioning-status";
import { ProvisioningPhase } from "@/types/proto-es/v1/machine_pb";

const provisioningPhaseEntry = mergeStatusMapping(
  provisioningPhaseToVariant,
  provisioningPhaseToI18nKey
);
const provisioningPhaseFallback: StatusBadgeEntry = {
  variant: "default",
  labelKey: "machine.provisioning.phase-unknown",
};

// ProvisioningPhaseBadge renders a Machine/MachineSummary provisioning phase
// as the shared enum pill (machine profile card, machines list rows).
function ProvisioningPhaseBadge({
  phase,
  className,
}: {
  phase: ProvisioningPhase | undefined;
  className?: string;
}) {
  return (
    <StatusBadge
      mapping={provisioningPhaseEntry}
      status={phase}
      fallback={provisioningPhaseFallback}
      className={className}
    />
  );
}

export { ProvisioningPhaseBadge };
