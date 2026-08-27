import { describe, expect, it } from "vitest";
import { isAgentOnline, peerPresenceOnline } from "./presence";

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

describe("peerPresenceOnline", () => {
  const agents = [
    { name: "agents/online", status: { state: 1 } },
    { name: "agents/offline", status: { state: 2 } },
  ];

  it("answers agent peers from the roster connection state", () => {
    expect(peerPresenceOnline("agents/online", true, agents, {})).toBe(true);
    expect(peerPresenceOnline("agents/offline", true, agents, {})).toBe(false);
    expect(peerPresenceOnline("agents/ghost", true, agents, {})).toBe(false);
  });

  it("answers user peers from the heartbeat map", () => {
    const onlineUsers = { "users/alice": true, "users/bob": false };
    expect(peerPresenceOnline("users/alice", false, agents, onlineUsers)).toBe(
      true
    );
    // An explicit offline heartbeat renders no badge (false), not undefined.
    expect(peerPresenceOnline("users/bob", false, agents, onlineUsers)).toBe(
      false
    );
    expect(peerPresenceOnline("users/ghost", false, agents, onlineUsers)).toBe(
      false
    );
  });

  it("returns undefined for an absent peer (no badge at all)", () => {
    expect(peerPresenceOnline(undefined, true, agents, {})).toBeUndefined();
    expect(peerPresenceOnline(undefined, false, agents, {})).toBeUndefined();
  });
});
