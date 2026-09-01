import { create } from "@bufbuild/protobuf";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { renderWithQueryClient } from "@/test/query";
import { AgentSummarySchema } from "@/types/proto-es/v1/agent_pb";
import {
  AgentTeamMemberSchema,
  AgentTeamSchema,
} from "@/types/proto-es/v1/agent_team_service_pb";
import { State } from "@/types/proto-es/v1/common_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { TeamDetailPage } from "./team-detail";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@/lib/avatar-cache", () => ({
  useAvatar: () => null,
}));

const mock = vi.hoisted(() => ({
  getAgentTeam: vi.fn(),
  listAgentTeams: vi.fn(),
  createAgentTeam: vi.fn(),
  updateAgentTeam: vi.fn(),
  deleteAgentTeam: vi.fn(),
  listAgents: vi.fn(),
}));

vi.mock("@/connect", () => ({
  agentTeamServiceClient: {
    getAgentTeam: mock.getAgentTeam,
    listAgentTeams: mock.listAgentTeams,
    createAgentTeam: mock.createAgentTeam,
    updateAgentTeam: mock.updateAgentTeam,
    deleteAgentTeam: mock.deleteAgentTeam,
  },
  agentServiceClient: {
    listAgents: mock.listAgents,
  },
}));

const team = create(AgentTeamSchema, {
  name: "agentTeams/t1",
  title: "Ops Crew",
  description: "Runs the ops",
  teamPrompt: "Be terse",
  members: [
    create(AgentTeamMemberSchema, {
      agent: "agents/alpha",
      role: 1,
      responsibility: "leads",
    }),
  ],
});

const agent = create(AgentSummarySchema, {
  name: "agents/alpha",
  handle: "alpha",
  title: "Alpha",
  state: State.ACTIVE,
  owner: "users/1",
});

function seedStore() {
  useAppStore.setState({
    currentUser: { name: "users/1" } as unknown as User,
    agents: [agent],
    agentsLoading: false,
  });
}

function renderPage(initialEntry: string) {
  const router = createMemoryRouter(
    [
      {
        path: "/members/users/:userId/teams/:teamId",
        element: <TeamDetailPage />,
      },
      {
        path: "/members/users/:userId",
        element: <div data-testid="users-page" />,
      },
      { path: "/", element: <div data-testid="home" /> },
    ],
    { initialEntries: [initialEntry] }
  );
  return renderWithQueryClient(<RouterProvider router={router} />);
}

beforeEach(() => {
  seedStore();
  mock.getAgentTeam.mockReset();
  mock.listAgentTeams.mockReset();
  mock.createAgentTeam.mockReset();
  mock.updateAgentTeam.mockReset();
  mock.deleteAgentTeam.mockReset();
  mock.listAgents.mockReset();
  mock.listAgentTeams.mockResolvedValue({ agentTeams: [team] });
  mock.getAgentTeam.mockResolvedValue(team);
  mock.listAgents.mockResolvedValue({ agents: [agent] });
  mock.createAgentTeam.mockResolvedValue({});
  mock.updateAgentTeam.mockResolvedValue({});
  mock.deleteAgentTeam.mockResolvedValue({});
});

describe("TeamDetailPage", () => {
  it("renders the fetched team read-only with its members", async () => {
    renderPage("/members/users/1/teams/t1");

    // The fetched team seeds the read-only view.
    expect(await screen.findByText("Ops Crew")).toBeInTheDocument();
    // Edit mode starts off: the title field is not rendered as an input.
    expect(screen.queryByLabelText("settings.agentTeams.team-name")).toBeNull();
    // The leader member row shows the roster label for agents/alpha.
    expect(await screen.findByText("Alpha")).toBeInTheDocument();
  });

  it("saves an edit through updateAgentTeam and re-reads the team", async () => {
    renderPage("/members/users/1/teams/t1");
    await screen.findByText("Ops Crew");

    fireEvent.click(screen.getByText("settings.agentTeams.edit"));
    const titleInput = screen.getByLabelText("settings.agentTeams.team-name");
    fireEvent.change(titleInput, { target: { value: "Ops Crew v2" } });
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => expect(mock.updateAgentTeam).toHaveBeenCalledTimes(1));
    const req = mock.updateAgentTeam.mock.calls[0][0];
    expect(req.agentTeam.title).toBe("Ops Crew v2");
    expect(req.agentTeam.name).toBe("agentTeams/t1");
    expect(req.updateMask.paths).toContain("title");
    // Post-save the page re-reads through the Query keys (the old load()).
    await waitFor(() => expect(mock.getAgentTeam).toHaveBeenCalledTimes(2));
    // Back in read-only mode.
    expect(screen.queryByLabelText("settings.agentTeams.team-name")).toBeNull();
  });

  it("creates a team in create mode, auto-assigning the first leader", async () => {
    // The seeded agent is a member of t1, so a create page with an empty
    // directory offers it for the new team.
    mock.listAgentTeams.mockResolvedValue({ agentTeams: [] });
    renderPage("/members/users/1/teams/new");

    // Create mode starts editable.
    const titleInput = await screen.findByLabelText(
      "settings.agentTeams.team-name"
    );
    fireEvent.change(titleInput, { target: { value: "New Crew" } });
    // A team needs at least one leader: add a member first.
    const addButtons = screen.getAllByRole("button", {
      name: "settings.agentTeams.add-member",
    });
    fireEvent.click(addButtons[0]);
    fireEvent.click(screen.getByRole("button", { name: "common.create" }));

    await waitFor(() => expect(mock.createAgentTeam).toHaveBeenCalledTimes(1));
    const req = mock.createAgentTeam.mock.calls[0][0];
    expect(req.agentTeam.title).toBe("New Crew");
    expect(req.agentTeam.leaderAgent).toBe("agents/alpha");
    expect(req.agentTeam.members[0].role).toBe(1);
  });

  it("deletes the team after confirmation", async () => {
    renderPage("/members/users/1/teams/t1");
    await screen.findByText("Ops Crew");

    fireEvent.click(
      screen.getByRole("button", { name: "settings.agentTeams.delete" })
    );
    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => expect(mock.deleteAgentTeam).toHaveBeenCalledTimes(1));
    expect(mock.deleteAgentTeam).toHaveBeenCalledWith({
      name: "agentTeams/t1",
    });
    expect(await screen.findByTestId("users-page")).toBeInTheDocument();
  });
});
