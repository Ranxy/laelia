import { describe, expect, it, vi } from "vitest";

// The centralized markdown setup registers the code_block custom component
// exactly once. We spy on markstream-react's setCustomComponents to assert it
// is invoked a single time even when the setup module is imported more than
// once in the same module graph (e.g. via two chat pages).
//
// vi.mock factories are hoisted above the test body and cannot close over
// test-scope `let`s, so the counter lives in vi.hoisted which evaluates before
// the mock factory.
const counter = vi.hoisted(() => ({ calls: 0 }));

vi.mock("markstream-react", () => ({
  MarkdownCodeBlockNode: () => null,
  setCustomComponents: () => {
    counter.calls += 1;
  },
}));

describe("markdown setup", () => {
  it("TestMarkdownSetup_RegisteredOnce: setCustomComponents called once across imports", async () => {
    vi.resetModules();
    // Import the setup module — registration runs the first time.
    await import("@/lib/markdown");
    expect(counter.calls).toBe(1);

    // Re-importing must NOT trigger a second registration (module is cached;
    // the `registered` guard also protects against HMR double-eval).
    await import("@/lib/markdown");
    expect(counter.calls).toBe(1);
  });
});
