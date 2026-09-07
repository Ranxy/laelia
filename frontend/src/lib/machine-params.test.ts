import { describe, expect, test } from "vitest";
import {
  formatQuantityDisplay,
  machineParamLabelKey,
  machineParamQuantity,
  machineParamSliderRange,
  parseQuantityMilli,
  roundQuantityDisplay,
} from "./machine-params";

describe("machineParamLabelKey", () => {
  test("maps catalog keys and leaves unknown keys unmapped", () => {
    expect(machineParamLabelKey("cpu")).toBe("machine.param.cpu");
    expect(machineParamLabelKey("storage_class")).toBe(
      "machine.param.storage-class"
    );
    expect(machineParamLabelKey("gpu")).toBeUndefined();
  });
});

describe("parseQuantityMilli", () => {
  test("parses the suffix subset the manager accepts", () => {
    expect(parseQuantityMilli("500m")).toBe(500);
    expect(parseQuantityMilli("2")).toBe(2000);
    expect(parseQuantityMilli(" 2Gi ")).toBe(2 * 1_073_741_824_000);
    expect(parseQuantityMilli("1536Mi")).toBe(1536 * 1_048_576_000);
    expect(parseQuantityMilli("1.5")).toBe(1500);
    expect(parseQuantityMilli("3k")).toBe(3_000_000);
  });

  test("rejects malformed and overflowing values", () => {
    expect(parseQuantityMilli("abc")).toBeNull();
    expect(parseQuantityMilli("2K")).toBeNull(); // SI uses lowercase k
    expect(parseQuantityMilli("-1")).toBeNull();
    expect(parseQuantityMilli("1e3")).toBeNull();
    expect(parseQuantityMilli("")).toBeNull();
    expect(parseQuantityMilli("1Pi")).toBeNull(); // beyond float-safe milli
  });
});

describe("machineParamSliderRange", () => {
  test("builds a cores range for cpu from admin bounds", () => {
    expect(machineParamSliderRange("cpu", "250m", "8")).toEqual({
      min: 0.25,
      max: 8,
      step: 0.25,
      unit: expect.objectContaining({ toQuantity: expect.any(Function) }),
    });
  });

  test("fills missing sides from the built-in fallback range", () => {
    const range = machineParamSliderRange("memory", "512Mi", undefined);
    expect(range?.min).toBe(0.5);
    expect(range?.max).toBe(64); // fallback ceiling
    expect(range?.step).toBe(1);
  });

  test("uses the full fallback when the admin configured no bounds", () => {
    expect(machineParamSliderRange("disk", undefined, undefined)).toEqual({
      min: 1,
      max: 500,
      step: 1,
      unit: expect.objectContaining({ toQuantity: expect.any(Function) }),
    });
  });

  test("keeps the slider usable under degenerate admin bounds", () => {
    const range = machineParamSliderRange("cpu", "4", "4");
    expect(range?.min).toBe(4);
    expect(range?.max).toBe(4.25); // one step wide
  });

  test("returns null for string and unknown keys", () => {
    expect(
      machineParamSliderRange("storage_class", undefined, undefined)
    ).toBeNull();
    expect(machineParamSliderRange("gpu", "1", "8")).toBeNull();
  });
});

describe("formatQuantityDisplay", () => {
  test("humanizes quantities into the param's display unit", () => {
    expect(formatQuantityDisplay("cpu", "500m")).toBe("0.5");
    expect(formatQuantityDisplay("cpu", "2")).toBe("2");
    expect(formatQuantityDisplay("memory", "2Gi")).toBe("2");
    expect(formatQuantityDisplay("disk", "1536Mi")).toBe("1.5");
  });

  test("returns null for empty, unparseable, or string params", () => {
    expect(formatQuantityDisplay("cpu", undefined)).toBeNull();
    expect(formatQuantityDisplay("cpu", "fast")).toBeNull();
    expect(formatQuantityDisplay("storage_class", "fast")).toBeNull();
  });
});

describe("machineParamQuantity", () => {
  test("serializes cores as bare integers or millicores", () => {
    expect(machineParamQuantity("cpu", 2)).toBe("2");
    expect(machineParamQuantity("cpu", 0.5)).toBe("500m");
    expect(machineParamQuantity("cpu", 2.25)).toBe("2250m");
  });

  test("serializes sizes as Gi with trimmed decimals", () => {
    expect(machineParamQuantity("memory", 8)).toBe("8Gi");
    expect(machineParamQuantity("disk", 1.5)).toBe("1.5Gi");
    expect(machineParamQuantity("disk", 0.30000000000000004)).toBe("0.3Gi");
  });

  test("returns null for keys without a quantity unit", () => {
    expect(machineParamQuantity("storage_class", 1)).toBeNull();
  });
});

describe("roundQuantityDisplay", () => {
  test("trims float noise from slider arithmetic", () => {
    expect(roundQuantityDisplay(0.30000000000000004)).toBe(0.3);
    expect(roundQuantityDisplay(1.23456)).toBe(1.235);
  });
});
