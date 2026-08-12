import { describe, expect, it } from "vitest";
import { AgentStatus_ConnectionState } from "@/types/proto-es/v1/agent_pb";
import { agentLifecycle, lifecycleLabel } from "./agents";

const ONLINE = AgentStatus_ConnectionState.ONLINE;
const OFFLINE = AgentStatus_ConnectionState.OFFLINE;

describe("agentLifecycle", () => {
  it("classifies an online configured agent as ready", () => {
    expect(
      agentLifecycle({ status: { state: ONLINE }, provider: "openai" })
    ).toBe("ready");
    expect(
      agentLifecycle({
        status: { state: ONLINE },
        executable: "/usr/bin/codex",
      })
    ).toBe("ready");
  });

  it("classifies an online agent without config as pending-config", () => {
    expect(agentLifecycle({ status: { state: ONLINE } })).toBe(
      "pending-config"
    );
  });

  it("classifies an offline configured agent as configured-offline", () => {
    expect(
      agentLifecycle({ status: { state: OFFLINE }, provider: "openai" })
    ).toBe("configured-offline");
  });

  it("classifies an offline unconfigured agent as waiting-connection", () => {
    expect(agentLifecycle({ status: { state: OFFLINE } })).toBe(
      "waiting-connection"
    );
    expect(agentLifecycle({})).toBe("waiting-connection");
  });

  it("reads nested info.acpConfig for the full Agent shape", () => {
    expect(
      agentLifecycle({
        status: { state: ONLINE },
        info: { acpConfig: { provider: "anthropic" } },
      })
    ).toBe("ready");
    expect(
      agentLifecycle({
        status: { state: OFFLINE },
        info: { acpConfig: { executable: "codex" } },
      })
    ).toBe("configured-offline");
  });

  it("prefers top-level provider/executable over nested config", () => {
    expect(
      agentLifecycle({
        status: { state: ONLINE },
        provider: "openai",
        info: { acpConfig: { provider: "anthropic" } },
      })
    ).toBe("ready");
  });
});

describe("lifecycleLabel", () => {
  it("maps each lifecycle state to its i18n key", () => {
    const t = (key: string) => key;
    expect(lifecycleLabel(t, "ready")).toBe("agent.lifecycle.ready");
    expect(lifecycleLabel(t, "pending-config")).toBe(
      "agent.lifecycle.pending-config"
    );
    expect(lifecycleLabel(t, "configured-offline")).toBe(
      "agent.lifecycle.configured-offline"
    );
    expect(lifecycleLabel(t, "waiting-connection")).toBe(
      "agent.lifecycle.waiting-connection"
    );
  });
});
