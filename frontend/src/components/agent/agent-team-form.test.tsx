import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentSummary } from "@/types/proto-es/v1/agent_pb";
import { AgentTeamRole } from "@/types/proto-es/v1/agent_team_service_pb";
import {
  TeamFormFields,
  type TeamFormValues,
  type TeamMemberForm,
} from "./agent-team-form";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

// useAvatar would hit the avatar RPCs; stub it so rows render the pixel
// fallback without network noise.
vi.mock("@/lib/avatar-cache", () => ({
  useAvatar: () => null,
}));

function agent(handle: string, title: string): AgentSummary {
  return {
    name: `agents/${handle}`,
    handle,
    title,
    description: `${title} description`,
  } as never;
}

const AGENTS = [
  agent("alice", "Alice"),
  agent("bob", "Bob"),
  agent("carol", "Carol"),
];

function renderForm(
  members: Partial<TeamMemberForm>[],
  onUpdate: (index: number, patch: Partial<TeamMemberForm>) => void = vi.fn(),
  agents: AgentSummary[] = AGENTS
) {
  const form: TeamFormValues = {
    title: "Team",
    description: "",
    teamPrompt: "",
    members: members.map((m) => ({
      agent: "",
      role: AgentTeamRole.MEMBER,
      responsibility: "",
      ...m,
    })),
  };
  render(
    <TeamFormFields
      agents={agents}
      form={form}
      onAdd={vi.fn()}
      onRemove={vi.fn()}
      onUpdate={onUpdate}
      onChange={vi.fn()}
    />
  );
  return onUpdate;
}

// The member row mounts the agent picker first, then the role select.
function agentSelectTrigger() {
  return screen.getAllByRole("combobox")[0];
}

describe("AgentSelect on the shared Select primitive", () => {
  it("shows the selected agent in the trigger and picks another through the popup", async () => {
    const onUpdate = renderForm([{ agent: "agents/alice" }]);

    const trigger = agentSelectTrigger();
    expect(trigger).toHaveTextContent("Alice");

    fireEvent.click(trigger);
    const bob = await screen.findByRole("option", { name: /Bob/ });
    fireEvent.pointerDown(bob);
    fireEvent.pointerUp(bob);
    fireEvent.click(bob);

    expect(onUpdate).toHaveBeenCalledWith(0, { agent: "agents/bob" });
  });

  it("keeps the row's own agent selectable while excluding other rows' agents", async () => {
    renderForm([{ agent: "agents/alice" }, { agent: "agents/bob" }]);

    fireEvent.click(agentSelectTrigger());

    const options = await screen.findAllByRole("option");
    // Carol is free, Alice is this row's own pick; Bob belongs to row 1.
    expect(options.map((o) => o.textContent)).toEqual(
      expect.arrayContaining([
        expect.stringContaining("Alice"),
        expect.stringContaining("Carol"),
      ])
    );
    expect(screen.queryByRole("option", { name: /Bob/ })).toBeNull();
  });

  it("shows the empty note inside the popup when no agents are available", async () => {
    renderForm([{ agent: "" }], vi.fn(), []);

    fireEvent.click(agentSelectTrigger());

    expect(
      await screen.findByText("settings.agentTeams.no-agents")
    ).toBeInTheDocument();
  });
});
