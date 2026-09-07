import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router-dom";
import { Card } from "@/components/profile-common";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { NumberInput } from "@/components/ui/number-input";
import { Slider } from "@/components/ui/slider";
import { useWorkspacePolicy } from "@/hooks/use-workspace-policy";
import { describeError } from "@/lib/connect-errors";
import {
  formatQuantityDisplay,
  type MachineParamSliderRange,
  machineParamLabelKey,
  machineParamQuantity,
  machineParamQuantityUnits,
  machineParamSliderRange,
  roundQuantityDisplay,
} from "@/lib/machine-params";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type {
  MachineParamSpec,
  Provisioner,
} from "@/types/proto-es/v1/provisioner_pb";

function clampToRange(n: number, range: MachineParamSliderRange): number {
  return Math.min(Math.max(n, range.min), range.max);
}

// QuantityParamRow renders one QUANTITY parameter: a "use default" switch
// (on for untouched fields, which keep tracking the provisioner's configured
// default) plus a linked slider + number input in human units (cores / Gi).
// The slider range comes from the provisioner's declared bounds, falling back
// to the built-in per-key range when the admin configured none. The controls
// stay live even while the switch is on: dragging the slider or typing a
// number takes the param over and flips the switch off.
function QuantityParamRow({
  param,
  label,
  range,
  value,
  useDefault,
  onValueChange,
  onUseDefaultChange,
}: {
  param: MachineParamSpec;
  label: string;
  range: MachineParamSliderRange;
  value: string | undefined;
  useDefault: boolean;
  onValueChange: (key: string, value: string) => void;
  onUseDefaultChange: (key: string, useDefault: boolean) => void;
}) {
  const { t } = useTranslation();
  const inputId = `machine-new-provisioned-param-${param.key}`;
  const unit = machineParamQuantityUnits[param.key];
  // Bumped each time the switch flips back on so the number input remounts
  // and re-reads the default: Base UI's NumberField freezes its visible text
  // while its input-sync ref is unset (until blur), so a controlled value
  // change alone can leave stale typed text in the box.
  const [defaultEpoch, setDefaultEpoch] = useState(0);
  const defaultDisplay = param.defaultValue
    ? formatQuantityDisplay(param.key, param.defaultValue)
    : null;
  // Position the slider/input take over from: the clamped default, or the
  // range floor when the default is unset or unparseable.
  const seed = clampToRange(
    defaultDisplay !== null ? Number(defaultDisplay) : range.min,
    range
  );
  // With the switch on, the input mirrors the default (empty + a short
  // "default" placeholder when the provisioner reports none); off, it edits
  // the value.
  const current = useDefault
    ? defaultDisplay !== null
      ? Number(defaultDisplay)
      : null
    : roundQuantityDisplay(Number(value ?? seed));
  return (
    <div className="flex flex-col gap-1" data-testid={`param-row-${param.key}`}>
      <div className="flex items-center justify-between gap-2">
        <label htmlFor={inputId} className="text-sm font-medium text-control">
          {label}
        </label>
        <label className="flex cursor-pointer items-center gap-1.5 text-xs text-control-light">
          <Checkbox
            checked={useDefault}
            data-testid={`use-default-${param.key}`}
            aria-label={t("machine.new.provisioned.param-use-default-aria", {
              param: label,
            })}
            onCheckedChange={(next) => {
              onUseDefaultChange(param.key, next);
              if (next) {
                setDefaultEpoch((epoch) => epoch + 1);
              } else if (!value) {
                onValueChange(param.key, String(seed));
              }
            }}
            size="sm"
          />
          {t("machine.new.provisioned.param-use-default")}
        </label>
      </div>
      <div className="flex items-center gap-3">
        <Slider
          className="flex-1"
          value={current ?? seed}
          min={range.min}
          max={range.max}
          step={range.step}
          aria-label={label}
          onValueChange={(v) => {
            if (useDefault) onUseDefaultChange(param.key, false);
            onValueChange(param.key, String(roundQuantityDisplay(v)));
          }}
        />
        <NumberInput
          key={`default-${defaultEpoch}`}
          id={inputId}
          className="w-28"
          value={current}
          min={range.min}
          max={range.max}
          step={range.step}
          placeholder={
            useDefault && defaultDisplay === null
              ? t("machine.new.provisioned.param-default-short")
              : undefined
          }
          suffix={
            current !== null
              ? unit?.labelKey
                ? t(unit.labelKey)
                : "Gi"
              : undefined
          }
          onValueChange={(v) => {
            if (v !== null) {
              if (useDefault) onUseDefaultChange(param.key, false);
              onValueChange(param.key, String(roundQuantityDisplay(v)));
            }
          }}
        />
      </div>
      <div className="flex justify-between text-xs text-control-light">
        <span>{roundQuantityDisplay(range.min)}</span>
        <span>{roundQuantityDisplay(range.max)}</span>
      </div>
    </div>
  );
}

// ParamInputs renders one control per schema-declared parameter. QUANTITY
// params (cpu/memory/disk) render as slider rows above; STRING and unknown
// future keys stay free-text inputs whose empty value tracks the default.
function ParamInputs({
  params,
  values,
  useDefaults,
  onChange,
  onUseDefaultChange,
}: {
  params: MachineParamSpec[];
  values: Record<string, string>;
  useDefaults: Record<string, boolean>;
  onChange: (key: string, value: string) => void;
  onUseDefaultChange: (key: string, useDefault: boolean) => void;
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
        const label = labelKey ? t(labelKey) : param.key;
        const range = machineParamSliderRange(
          param.key,
          param.minValue,
          param.maxValue
        );
        if (!range) {
          return (
            <div key={param.key} className="flex flex-col gap-1">
              <label
                htmlFor={`machine-new-provisioned-param-${param.key}`}
                className="text-sm font-medium text-control"
              >
                {label}
              </label>
              <Input
                id={`machine-new-provisioned-param-${param.key}`}
                value={values[param.key] ?? ""}
                placeholder={
                  param.defaultValue ||
                  t("machine.new.provisioned.param-default")
                }
                onChange={(e) => {
                  onChange(param.key, e.target.value);
                }}
                spellCheck={false}
              />
            </div>
          );
        }
        return (
          <QuantityParamRow
            key={param.key}
            param={param}
            label={label}
            range={range}
            value={values[param.key]}
            useDefault={useDefaults[param.key] ?? true}
            onValueChange={onChange}
            onUseDefaultChange={onUseDefaultChange}
          />
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
  // Per-machine parameter values keyed by catalog key, in display units for
  // quantity params (cores / Gi); quantity keys with the default switch on
  // (the initial state, tracked in useParamDefault) stay unsubmitted.
  const [paramValues, setParamValues] = useState<Record<string, string>>({});
  const [useParamDefault, setUseParamDefault] = useState<
    Record<string, boolean>
  >({});
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
      // Slider params with the default switch on never submit; overridden
      // quantity values clamp to the declared range and convert back to k8s
      // quantity strings ("0.5" cores → "500m"). String and unknown keys keep
      // the free-text rule: non-empty trimmed values submit verbatim.
      const machineParams: Record<string, string> = {};
      for (const param of selectedSchema) {
        const range = machineParamSliderRange(
          param.key,
          param.minValue,
          param.maxValue
        );
        if (range && (useParamDefault[param.key] ?? true)) continue;
        const raw = paramValues[param.key]?.trim();
        if (!raw) continue;
        const parsed = Number(raw);
        if (range && Number.isFinite(parsed)) {
          const quantity = machineParamQuantity(
            param.key,
            clampToRange(parsed, range)
          );
          if (quantity) machineParams[param.key] = quantity;
          continue;
        }
        machineParams[param.key] = raw;
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
                useDefaults={useParamDefault}
                onChange={(key, value) => {
                  setParamValues((prev) => ({ ...prev, [key]: value }));
                }}
                onUseDefaultChange={(key, useDefault) => {
                  setUseParamDefault((prev) => ({
                    ...prev,
                    [key]: useDefault,
                  }));
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
