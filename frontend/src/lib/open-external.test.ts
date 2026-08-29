import { afterEach, describe, expect, it, vi } from "vitest";
import { safeOpenExternal } from "@/lib/open-external";

describe("safeOpenExternal", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("TestSafeOpenExternal_AllowedSchemes: opens http/https/mailto and resolves relative hrefs", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);

    expect(safeOpenExternal("https://example.com/a?b=1")).toBe(true);
    expect(open).toHaveBeenLastCalledWith(
      "https://example.com/a?b=1",
      "_blank",
      "noopener,noreferrer"
    );

    expect(safeOpenExternal("http://example.com")).toBe(true);
    expect(open).toHaveBeenLastCalledWith(
      "http://example.com/",
      "_blank",
      "noopener,noreferrer"
    );

    expect(safeOpenExternal("mailto:ops@example.com")).toBe(true);
    expect(open).toHaveBeenLastCalledWith(
      "mailto:ops@example.com",
      "_blank",
      "noopener,noreferrer"
    );

    // Relative hrefs resolve against the app origin before opening.
    expect(safeOpenExternal("/docs/page.html")).toBe(true);
    expect(open).toHaveBeenLastCalledWith(
      `${window.location.origin}/docs/page.html`,
      "_blank",
      "noopener,noreferrer"
    );
  });

  it("TestSafeOpenExternal_BlockedSchemes: refuses script-ish schemes and unparsable input", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);

    expect(safeOpenExternal("javascript:alert(1)")).toBe(false);
    expect(safeOpenExternal("JaVaScRiPt:alert(1)")).toBe(false);
    expect(safeOpenExternal("data:text/html,<script>alert(1)</script>")).toBe(
      false
    );
    expect(safeOpenExternal("vbscript:msgbox")).toBe(false);
    expect(safeOpenExternal("file:///etc/passwd")).toBe(false);
    expect(safeOpenExternal("chrome-extension://abc/x")).toBe(false);
    expect(safeOpenExternal("  ")).toBe(false);
    expect(open).not.toHaveBeenCalled();
  });
});
