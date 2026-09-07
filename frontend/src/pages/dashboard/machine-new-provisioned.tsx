import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Card } from "@/components/profile-common";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useWorkspacePolicy } from "@/hooks/use-workspace-policy";
import { describeError } from "@/lib/connect-errors";
import { machineParamLabelKey } from "@/lib/machine-params";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type {
  MachineParamSpec,
  Provisioner,
} from "@/types/proto-es/v1/provisioner_pb";

// ParamInputs renders one input per schema-declared parameter. Inputs start
// empty: the default is the placeholder, so untouched fields keep tracking
// the provisioner's admin-configured defaults. Only non-empty values submit.
function ParamInputs({
  params,
  values,
  onChange,
}: {
  params: MachineParamSpec[];
  values: Record<string, string>;
  onChange: (key: string, value: string) => void;
}) {
  const { t } = useTranslation();
  if (params.length === 0) return null;
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm font-medium text-control">
        {t("machine.new.provisioned.params-title")}
      </p>
      {params.map((param) => {
        const labelKey = machineParamLabelKey(param.key);
        return (
          <div key={param.key} className="flex flex-col gap-1">
            <label
              htmlFor={`machine-new-provisioned-param-${param.key}`}
              className="text-sm font-medium text-control"
            >
              {labelKey ? t(labelKey) : param.key}
              {(param.minValue || param.maxValue) && (
                <span className="ml-2 text-xs font-normal text-control-light">
                  {t("machine.new.provisioned.param-range", {
                    range: `${param.minValue || "·"} – ${param.maxValue || "·"}`,
                  })}
                </span>
              )}
            </label>
            <Input
              id={`machine-new-provisioned-param-${param.key}`}
              value={values[param.key] ?? ""}
              placeholder={
                param.defaultValue || t("machine.new.provisioned.param-default")
              }
              onChange={(e) => {
                onChange(param.key, e.target.value);
              }}
              spellCheck={false}
            />
          </div>
        );
      })}
      <p className="text-xs text-control-light">
        {t("machine.new.provisioned.params-hint")}
      </p>
    </div>
  );
}

// MachineNewProvisionedPanel is the "Provisioned" tab of the create-machine
// page: pick a provisioner, name the machine, and the provisioner creates the
// workload while the user is taken to the machine profile. No install command,
// no device-code approval — the pod authenticates itself via its seeded
// machine.json.
export function MachineNewProvisionedPanel() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const provisioners = useAppStore((s) => s.provisioners);
  const provisionersLoading = useAppStore((s) => s.provisionersLoading);
  const fetchProvisioners = useAppStore((s) => s.fetchProvisioners);

  const [selected, setSelected] = useState("");
  const [title, setTitle] = useState("");
  // Optional per-machine runtime image; empty = the workspace default.
  const [runtimeImage, setRuntimeImage] = useState("");
  // Per-machine parameter values keyed by catalog key; empty = the
  // provisioner's configured default applies.
  const [paramValues, setParamValues] = useState<Record<string, string>>({});
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  // The custom-image field is only offered when the workspace allows custom
  // runtime images. The provisioning setting itself is admin-only, so the
  // public GetWorkspaceInfo mirror (allowCustomImages) is what every user can
  // read; the backend still enforces the switch at ProvisionMachine time.
  const { allowCustomImages } = useWorkspacePolicy();

  useEffect(() => {
    void fetchProvisioners({ pageSize: 100 });
  }, [fetchProvisioners]);

  // The picker only offers connected provisioners; offline ones are shown but
  // disabled. The backend validates liveness again at ProvisionMachine time.
  const connected = (p: Provisioner): boolean => p.status?.connected ?? false;

  // The schema of the currently selected provisioner: the create form renders
  // one input per declared parameter, even while the provisioner is offline
  // (the backend still fails fast at ProvisionMachine time).
  const selectedSchema =
    provisioners.find((p) => p.name === selected)?.status?.machineParams ?? [];

  async function handleCreate() {
    if (!selected) {
      setError(t("machine.new.provisioned.select-provisioner"));
      return;
    }
    const picked = provisioners.find((p) => p.name === selected);
    if (picked && !connected(picked)) {
      setError(t("machine.new.provisioned.provisioner-offline"));
      return;
    }
    if (!title.trim()) {
      setError(t("machine.new.provisioned.enter-name"));
      return;
    }
    setCreating(true);
    setError("");
    try {
      // Only non-empty values submit: an untouched field keeps tracking the
      // provisioner's configured default.
      const machineParams: Record<string, string> = {};
      for (const param of selectedSchema) {
        const value = paramValues[param.key]?.trim();
        if (value) machineParams[param.key] = value;
      }
      const hasParams = Object.keys(machineParams).length > 0;
      const machine = await useAppStore
        .getState()
        .provisionMachine(
          selected,
          title.trim(),
          runtimeImage.trim(),
          hasParams ? machineParams : undefined
        );
      navigate(`/machines/${machine.name.replace(/^machines\//, "")}`);
    } catch (err) {
      setError(describeError(err));
      setCreating(false);
    }
  }

  if (provisionersLoading && provisioners.length === 0) {
    return (
      <div className="flex items-center gap-2 py-8 text-sm text-control-light">
        <Loader2 className="size-4 animate-spin" />
        {t("common.loading")}
      </div>
    );
  }

  if (provisioners.length === 0) {
    return (
      <Card title={t("machine.new.provisioned.pick-title")}>
        <Alert
          variant="info"
          description={t("machine.new.provisioned.empty")}
        />
      </Card>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <Card title={t("machine.new.provisioned.pick-title")}>
        <div className="flex flex-col gap-3">
          <p className="text-sm text-control-light">
            {t("machine.new.provisioned.pick-hint")}
          </p>
          {provisioners.some((p) => !connected(p)) && (
            <p className="text-xs text-control-light">
              {t("machine.new.provisioned.offline-hint")}
            </p>
          )}
          <div className="flex flex-col gap-2" role="radiogroup">
            {provisioners.map((p) => {
              const isConnected = connected(p);
              const isSelected = p.name === selected;
              return (
                <button
                  key={p.name}
                  type="button"
                  role="radio"
                  aria-checked={isSelected}
                  disabled={!isConnected}
                  data-testid="provisioner-option"
                  onClick={() => {
                    setSelected(p.name);
                    setError("");
                  }}
                  className={cn(
                    "flex items-center gap-3 rounded-md border px-4 py-3 text-left transition-colors",
                    !isConnected &&
                      "cursor-not-allowed border-control-border opacity-60",
                    isConnected && isSelected && "border-accent bg-accent/5",
                    isConnected &&
                      !isSelected &&
                      "border-control-border hover:bg-control-bg/60"
                  )}
                >
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span
                      className={cn(
                        "truncate text-sm font-medium text-main",
                        !isConnected && "opacity-75"
                      )}
                    >
                      {p.title}
                    </span>
                    {p.description && (
                      <span className="truncate text-xs text-control-light">
                        {p.description}
                      </span>
                    )}
                  </div>
                  <Badge variant="secondary">{p.backend}</Badge>
                  {isConnected ? (
                    <Badge variant="success">
                      {t("machine.new.provisioned.connected")}
                    </Badge>
                  ) : (
                    <Badge variant="default">
                      {t("machine.new.provisioned.offline")}
                    </Badge>
                  )}
                </button>
              );
            })}
          </div>
        </div>
      </Card>

      <Card title={t("machine.new.provisioned.machine-title")}>
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <label
              htmlFor="machine-new-provisioned-title"
              className="text-sm font-medium text-control"
            >
              {t("machine.new.name-label")}
            </label>
            <Input
              id="machine-new-provisioned-title"
              value={title}
              placeholder={t("machine.new.name-placeholder")}
              onChange={(e) => {
                setTitle(e.target.value);
                setError("");
              }}
            />
          </div>
          {selectedSchema.length > 0 && (
            <div className="border-t border-control-border pt-4">
              <ParamInputs
                params={selectedSchema}
                values={paramValues}
                onChange={(key, value) => {
                  setParamValues((prev) => ({ ...prev, [key]: value }));
                }}
              />
            </div>
          )}
          {allowCustomImages && (
            <div className="flex flex-col gap-1">
              <label
                htmlFor="machine-new-provisioned-runtime-image"
                className="text-sm font-medium text-control"
              >
                {t("machine.new.provisioned.custom-image-label")}
              </label>
              <Input
                id="machine-new-provisioned-runtime-image"
                value={runtimeImage}
                placeholder={t(
                  "machine.new.provisioned.custom-image-placeholder"
                )}
                onChange={(e) => {
                  setRuntimeImage(e.target.value);
                  setError("");
                }}
                spellCheck={false}
              />
              <p className="text-xs text-control-light">
                {t("machine.new.provisioned.custom-image-hint")}
              </p>
            </div>
          )}
          {error && <Alert variant="error" description={error} />}
          <div className="flex items-center justify-between gap-3">
            <p className="text-xs text-control-light">
              {t("machine.new.provisioned.create-hint")}
            </p>
            <Button disabled={creating} onClick={() => void handleCreate()}>
              {creating && <Loader2 className="size-4 animate-spin" />}
              {t("machine.new.provisioned.create")}
            </Button>
          </div>
        </div>
      </Card>
    </div>
  );
}
