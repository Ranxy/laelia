import { afterEach, describe, expect, it, vi } from "vitest";
import { formatConversationListTime } from "./command-status";

// formatConversationListTime renders relative to the live clock, so pin it:
// 2026-08-11 15:30 local time.
const NOW = new Date(2026, 7, 11, 15, 30, 0, 0);

describe("formatConversationListTime", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("formats today as HH:MM", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const today = new Date(2026, 7, 11, 9, 5, 0, 0).getTime();
    expect(formatConversationListTime(today)).toBe("09:05");
  });

  it("formats earlier in the same year as M/D", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const earlier = new Date(2026, 2, 4, 23, 59, 0, 0).getTime();
    expect(formatConversationListTime(earlier)).toBe("3/4");
  });

  it("formats a previous year as YYYY/M/D", () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const lastYear = new Date(2025, 11, 31, 8, 0, 0, 0).getTime();
    expect(formatConversationListTime(lastYear)).toBe("2025/12/31");
  });

  it("returns empty for missing or invalid input", () => {
    expect(formatConversationListTime(undefined)).toBe("");
    expect(formatConversationListTime(Number.NaN)).toBe("");
  });
});
