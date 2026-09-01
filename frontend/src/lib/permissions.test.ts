import { describe, expect, it } from "vitest";
import {
  ALL_PERMISSIONS,
  PERMISSION_GROUPS,
  permissionLabel,
} from "./permissions";

describe("permission catalog", () => {
  it("derives ALL_PERMISSIONS from the groups in display order", () => {
    expect(ALL_PERMISSIONS).toEqual(
      PERMISSION_GROUPS.flatMap((g) => g.permissions)
    );
  });

  it("keeps every permission unique across the catalog", () => {
    const seen = new Set<string>();
    for (const perm of ALL_PERMISSIONS) {
      expect(seen.has(perm)).toBe(false);
      seen.add(perm);
    }
  });

  it("names every permission with the laelia.<resource>.<verb> shape", () => {
    for (const perm of ALL_PERMISSIONS) {
      expect(perm).toMatch(/^laelia\.[a-zA-Z]+\.[a-zA-Z]+$/);
    }
  });

  it("derives a non-empty verb-only label for every permission", () => {
    for (const perm of ALL_PERMISSIONS) {
      const label = permissionLabel(perm);
      expect(label.length).toBeGreaterThan(0);
      expect(label).not.toContain("laelia");
      expect(perm.endsWith(label)).toBe(true);
    }
  });

  it("keeps every group non-empty", () => {
    for (const group of PERMISSION_GROUPS) {
      expect(group.permissions.length).toBeGreaterThan(0);
    }
  });
});
