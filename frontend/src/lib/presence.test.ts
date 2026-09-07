import { describe, expect, it } from "vitest";
import { agentPeerOnline, formatLastSeen, isAgentOnline } from "./presence";

describe("isAgentOnline", () => {
  it("counts only the ONLINE connection state", () => {
    // 1=ONLINE, 2=OFFLINE, 3=ERROR, 4=KICKED, 5=STOPPED per the proto enum.
    expect(isAgentOnline({ name: "agents/a", status: { state: 1 } })).toBe(
      true
    );
    for (const state of [0, 2, 3, 4, 5]) {
      expect(isAgentOnline({ name: "agents/a", status: { state } })).toBe(
        false
      );
    }
    // A roster row without a status payload is not online.
    expect(isAgentOnline({ name: "agents/a" })).toBe(false);
  });
});

describe("agentPeerOnline", () => {
  const agents = [
    { name: "agents/online", status: { state: 1 } },
    { name: "agents/offline", status: { state: 2 } },
  ];

  it("answers agent peers from the roster connection state", () => {
    expect(agentPeerOnline("agents/online", agents)).toBe(true);
    expect(agentPeerOnline("agents/offline", agents)).toBe(false);
    expect(agentPeerOnline("agents/ghost", agents)).toBe(false);
  });

  it("answers false for an absent peer", () => {
    expect(agentPeerOnline(undefined, agents)).toBe(false);
  });
});

describe("formatLastSeen", () => {
  // The translator stands in for i18next: every phrase is number-agnostic
  // (compact unit style), so one catalog form per locale carries all counts.
  const t = (key: string, opts?: Record<string, unknown>) =>
    `${key}:${opts?.count ?? opts?.time ?? ""}`;

  it("says just-now inside a minute", () => {
    const now = new Date("2026-01-01T12:00:00Z");
    expect(formatLastSeen(new Date("2026-01-01T11:59:30Z"), t, now)).toBe(
      "chat.presence-last-just-now:"
    );
  });

  it("formats minutes, hours, and days with the elapsed count", () => {
    const now = new Date("2026-01-01T12:00:00Z");
    expect(formatLastSeen(new Date("2026-01-01T11:55:00Z"), t, now)).toBe(
      "chat.presence-last-minutes:5"
    );
    expect(formatLastSeen(new Date("2026-01-01T09:00:00Z"), t, now)).toBe(
      "chat.presence-last-hours:3"
    );
    expect(formatLastSeen(new Date("2025-12-28T12:00:00Z"), t, now)).toBe(
      "chat.presence-last-days:4"
    );
  });

  it("falls back to a locale date beyond a week", () => {
    const now = new Date("2026-01-01T12:00:00Z");
    const longAgo = new Date("2025-11-01T12:00:00Z");
    expect(formatLastSeen(longAgo, t, now)).toBe(
      `chat.presence-last-date:${longAgo.toLocaleDateString()}`
    );
  });
});
