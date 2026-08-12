import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setRouteNameIndex } from "@/router/route-index";
import { AgentWorkspacePage } from "./agent-workspace";

const mock = vi.hoisted(() => ({
  getAgent: vi.fn(),
}));

vi.mock("@/stores", () => {
  const state = {
    getAgent: mock.getAgent,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

// The page embeds the tree + file panel; stub them so the test focuses on the
// page's gate logic and layout.
vi.mock("@/components/workspace/workspace-tree", () => ({
  WorkspaceTree: () => <div data-testid="workspace-tree" />,
}));
vi.mock("@/components/workspace/workspace-file-panel", () => ({
  WorkspaceFilePanel: () => <div data-testid="workspace-file-panel" />,
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/members/agents/a1/workspace"]}>
      <Routes>
        <Route
          path="/members/agents/:agentId/workspace"
          element={<AgentWorkspacePage />}
        />
        <Route
          path="/members/agents/:agentId"
          element={<div data-testid="profile" />}
        />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.getAgent.mockReset();
  // The app builds this index from the real route table; mirror the entry the
  // page resolves so the redirect lands on the profile route.
  setRouteNameIndex(new Map([["agent.profile", "/members/agents/:agentId"]]));
});

describe("agent-workspace", () => {
  it("shows a spinner while the agent is being fetched", () => {
    mock.getAgent.mockReturnValue(new Promise(() => {}));

    const { container } = renderPage();

    expect(container.querySelector("svg.animate-spin")).toBeInTheDocument();
  });

  it("renders the tree and file panel for an editable agent", async () => {
    mock.getAgent.mockResolvedValue({
      name: "agents/a1",
      canEdit: true,
    });

    renderPage();

    expect(await screen.findByTestId("workspace-tree")).toBeInTheDocument();
    expect(screen.getByTestId("workspace-file-panel")).toBeInTheDocument();
  });

  it("redirects to the profile tab when the caller cannot edit", async () => {
    mock.getAgent.mockResolvedValue({
      name: "agents/a1",
      canEdit: false,
    });

    renderPage();

    expect(await screen.findByTestId("profile")).toBeInTheDocument();
  });
});
