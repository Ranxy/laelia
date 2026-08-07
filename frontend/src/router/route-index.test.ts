import { describe, expect, it } from "vitest";
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
});
