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

// ===== Quantity sliders =====
//
// QUANTITY params are k8s quantity strings ("500m", "2Gi") — unusable as
// slider coordinates. These helpers mirror backend/common/quantity (the
// milli-unit subset) so the create form can parse the provisioner-declared
// defaults/bounds into display units (CPU cores, binary Gi) and serialize a
// slider choice back into a string the manager's validator accepts.

// Suffix → milli-units per one unit ("Gi" = 2^30 units = 2^30·1000 milli).
const MILLI_PER_UNIT: Record<string, number> = {
  m: 1,
  "": 1_000,
  k: 1_000_000,
  M: 1_000_000_000,
  G: 1e12,
  T: 1e15,
  P: 1e18,
  Ki: 1_024_000,
  Mi: 1_048_576_000,
  Gi: 1_073_741_824_000,
  Ti: 1.099511627776e15,
  Pi: 1.125899906842624e18,
};

// Parses a k8s quantity into milli-units; null when unparseable or beyond the
// float-safe range (e.g. "1Pi") so callers can fall back to raw display.
export function parseQuantityMilli(value: string): number | null {
  const match = /^(\d+(?:\.\d+)?)\s*([a-zA-Z]*)$/.exec(value.trim());
  if (!match) return null;
  const per = MILLI_PER_UNIT[match[2]];
  if (per === undefined) return null;
  const milli = Number(match[1]) * per;
  return Number.isFinite(milli) && Math.abs(milli) <= Number.MAX_SAFE_INTEGER
    ? milli
    : null;
}

// Trims float noise to at most 3 decimals: 0.30000000000000004 → 0.3.
function round3(n: number): number {
  return Math.round(n * 1000) / 1000;
}

// Rounds a display-unit number (slider/number-input output) for state and
// display so float artifacts never reach a quantity string.
export function roundQuantityDisplay(n: number): number {
  return round3(n);
}

interface QuantityUnit {
  // milli-units → display-unit number (CPU cores, binary Gi).
  toDisplay(milli: number): number;
  // display-unit number → quantity string the manager accepts.
  toQuantity(n: number): string;
  // Locale key of the display unit rendered as the input suffix.
  labelKey?: string;
}

const CORES: QuantityUnit = {
  toDisplay: (milli) => milli / 1000,
  // Integral cores submit bare ("2"); sub-core values as millicores ("500m").
  toQuantity: (n) =>
    Number.isInteger(n) ? String(n) : `${Math.round(n * 1000)}m`,
  labelKey: "machine.param.unit-cores",
};

const GIBI: QuantityUnit = {
  toDisplay: (milli) => milli / MILLI_PER_UNIT.Gi,
  // Gi with up to 3 decimals ("8Gi", "1.5Gi").
  toQuantity: (n) => `${round3(n)}Gi`,
};

// QUANTITY params the create form renders as sliders, keyed by catalog key.
export const machineParamQuantityUnits: Record<string, QuantityUnit> = {
  cpu: CORES,
  memory: GIBI,
  disk: GIBI,
};

// Slider fallback ranges (display units) when the provisioner reports no
// admin-configured bounds; a configured bound overrides its side.
const FALLBACK_RANGES: Record<
  string,
  { min: string; max: string; step: number }
> = {
  cpu: { min: "250m", max: "16", step: 0.25 },
  memory: { min: "1Gi", max: "64Gi", step: 1 },
  // Step 1 so the typical 10Gi default stays reachable when dragging from
  // the 1Gi floor.
  disk: { min: "1Gi", max: "500Gi", step: 1 },
};

export interface MachineParamSliderRange {
  min: number;
  max: number;
  step: number;
  unit: QuantityUnit;
}

// Resolves the slider range for a QUANTITY spec: admin bounds win per side,
// the built-in range fills the rest. Null for STRING/unknown params and
// unparseable values — those stay free-text inputs.
export function machineParamSliderRange(
  key: string,
  minValue: string | undefined,
  maxValue: string | undefined
): MachineParamSliderRange | null {
  const unit = machineParamQuantityUnits[key];
  const fallback = FALLBACK_RANGES[key];
  if (!unit || !fallback) return null;
  const minMilli =
    parseQuantityMilli(minValue ?? "") ?? parseQuantityMilli(fallback.min);
  const maxMilli =
    parseQuantityMilli(maxValue ?? "") ?? parseQuantityMilli(fallback.max);
  if (minMilli === null || maxMilli === null) return null;
  const min = unit.toDisplay(minMilli);
  const max = unit.toDisplay(maxMilli);
  // Degenerate admin bounds (min ≥ max) get a one-step-wide range so the
  // slider stays usable; the number input clamps to the same bounds.
  if (min >= max)
    return { min, max: round3(min + fallback.step), step: fallback.step, unit };
  const range = max - min;
  const step = range < fallback.step ? round3(range / 10) : fallback.step;
  return { min, max, step, unit };
}

// Formats a quantity in a param's display unit ("2", "1.5") for the value
// chip next to the "use default" switch; null renders the raw string.
export function formatQuantityDisplay(
  key: string,
  value: string | undefined
): string | null {
  const unit = machineParamQuantityUnits[key];
  if (!unit || value === undefined) return null;
  const milli = parseQuantityMilli(value);
  return milli === null ? null : String(round3(unit.toDisplay(milli)));
}

// Serializes a display-unit number into the quantity string submitted for a
// catalog key ("0.5" cores → "500m", "8" Gi → "8Gi").
export function machineParamQuantity(
  key: string,
  display: number
): string | null {
  const unit = machineParamQuantityUnits[key];
  return unit ? unit.toQuantity(round3(display)) : null;
}
