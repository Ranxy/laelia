import { describe, expect, it, vi } from "vitest";
import {
  buildRouteNameIndex,
  resolvePath,
  setRouteNameIndex,
} from "./route-index";
import { dashboardRoutes } from "./routes/dashboard";

// The Members page now owns the agent detail tree (nested under /members), and
// /agents is a redirect stub. The route-name index keeps the first
// registration, so — as long as the members route precedes the agents route —
// the named agent/command/reminder routes must resolve under /members/agents.
// This guards against accidentally re-ordering the routes and silently
// sending tab navigation through the /agents redirect.
describe("dashboard route-name index", () => {
  const index = buildRouteNameIndex(dashboardRoutes);
  setRouteNameIndex(index);

  it("resolves the agent profile/chat routes under /members/agents", () => {
    expect(resolvePath("agent.profile", { agentId: "abc" })).toBe(
      "/members/agents/abc"
    );
    expect(resolvePath("agent.chat", { agentId: "abc" })).toBe(
      "/members/agents/abc/chat"
    );
    expect(resolvePath("agent.mcp", { agentId: "abc" })).toBe(
      "/members/agents/abc/mcp"
    );
  });

  it("resolves the command/reminder routes under /members/agents", () => {
    expect(resolvePath("command.list", { agentId: "abc" })).toBe(
      "/members/agents/abc/commands"
    );
    expect(
      resolvePath("command.detail", { agentId: "abc", commandId: "9" })
    ).toBe("/members/agents/abc/commands/9");
    expect(resolvePath("reminder.list", { agentId: "abc" })).toBe(
      "/members/agents/abc/reminders"
    );
    expect(
      resolvePath("reminder.detail", { agentId: "abc", reminderId: "7" })
    ).toBe("/members/agents/abc/reminders/7");
  });

  it("encodes params and appends the query string", () => {
    expect(
      resolvePath(
        "command.detail",
        { agentId: "agents/x", commandId: "a/b" },
        { tab: "summary", q: "a b" }
      )
    ).toBe("/members/agents/agents%2Fx/commands/a%2Fb?tab=summary&q=a+b");
  });

  it("falls back to / for an unregistered name", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(resolvePath("no.such.route")).toBe("/");
    expect(warn).toHaveBeenCalledOnce();
    warn.mockRestore();
  });

  it("throws when a required param is missing instead of leaving :id in the URL", () => {
    expect(() => resolvePath("command.detail", { agentId: "abc" })).toThrow(
      /Missing ":commandId" param/
    );
  });
});
