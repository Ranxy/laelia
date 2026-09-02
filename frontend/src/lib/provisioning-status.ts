import type { BadgeVariant } from "@/components/ui/badge";
import { ProvisioningPhase } from "@/types/proto-es/v1/machine_pb";

// ProvisioningPhase → i18n key / Badge variant lookups (design §5.2). The
// single consumer is the StatusBadge glue in
// components/provisioning-phase-badge.tsx; provisioningActive additionally
// drives the machine profile's poll-until-ONLINE loop.
const provisioningPhaseToI18nKey: Record<ProvisioningPhase, string> = {
  [ProvisioningPhase.UNSPECIFIED]: "machine.provisioning.phase-unknown",
  [ProvisioningPhase.PENDING]: "machine.provisioning.phase-pending",
  [ProvisioningPhase.PROVISIONING]: "machine.provisioning.phase-provisioning",
  [ProvisioningPhase.PROVISIONED]: "machine.provisioning.phase-provisioned",
  [ProvisioningPhase.FAILED]: "machine.provisioning.phase-failed",
  [ProvisioningPhase.DEPROVISIONING]:
    "machine.provisioning.phase-deprovisioning",
  [ProvisioningPhase.DELETED]: "machine.provisioning.phase-deleted",
};

const provisioningPhaseToVariant: Record<ProvisioningPhase, BadgeVariant> = {
  [ProvisioningPhase.UNSPECIFIED]: "default",
  [ProvisioningPhase.PENDING]: "warning",
  [ProvisioningPhase.PROVISIONING]: "warning",
  [ProvisioningPhase.PROVISIONED]: "success",
  [ProvisioningPhase.FAILED]: "error",
  [ProvisioningPhase.DEPROVISIONING]: "warning",
  [ProvisioningPhase.DELETED]: "default",
};

// provisioningActive reports whether the provisioning job is still moving: a
// machine in one of these phases keeps the machine profile polling until it
// comes ONLINE (or hits a terminal phase).
export function provisioningActive(
  phase: ProvisioningPhase | undefined
): boolean {
  return (
    phase === ProvisioningPhase.PENDING ||
    phase === ProvisioningPhase.PROVISIONING ||
    phase === ProvisioningPhase.PROVISIONED
  );
}

export { provisioningPhaseToI18nKey, provisioningPhaseToVariant };
