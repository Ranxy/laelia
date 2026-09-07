// Shared helpers for user-customizable machine parameters (catalog keys, see
// docs/plan/provisioner-machine-params-design.md): the label-key map used by
// both the provisioned create form and the machine profile provisioning card.
// An unknown (future) key falls back to the raw key name at the call site.
export const machineParamLabelKeys: Record<string, string> = {
  cpu: "machine.param.cpu",
  memory: "machine.param.memory",
  disk: "machine.param.disk",
  storage_class: "machine.param.storage-class",
};

export function machineParamLabelKey(key: string): string | undefined {
  return machineParamLabelKeys[key];
}
