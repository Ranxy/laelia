import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import {
  createBrowserRouter,
  MemoryRouter,
  Route,
  type RouteObject,
  RouterProvider,
  Routes,
  redirect,
} from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RootLayout } from "@/app/root-layout";
import { RouterErrorBoundary } from "@/router/error-boundary";
import { rootGuard } from "@/router/guard";
import { buildRouteNameIndex, setRouteNameIndex } from "@/router/route-index";
import { authRoutes } from "@/router/routes/auth";
import { dashboardRoutes } from "@/router/routes/dashboard";
import { useAppStore } from "@/stores";
import type { ChatMessage, Command } from "@/types/proto-es/v1/command_pb";
import { CommandStatus } from "@/types/proto-es/v1/command_pb";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";
import { CommandListPage } from "./command-list";

// Regression: command rows (and the detail back button) must navigate to the
// canonical /members/agents/... routes. The legacy /agents/... redirect route
// unmounts the whole Members tree and remounts it, re-triggering
// fetchMembers/fetchMachines — which looks like a full page reload.

function seedStore() {
  useAppStore.setState({
    currentUser: { name: "users/1", title: "U" } as unknown as User,
    isLoggedIn: true,
    sessionLoaded: true,
    loadSession: vi.fn(async () => {}),
    members: [
      {
        kind: "agent",
        name: "agents/a",
        title: "Agent A",
        subtitle: "Owner Name",
        connectionState: 2,
      },
    ],
    membersLoading: false,
    membersError: false,
    fetchMembers: vi.fn(async () => undefined),
    machines: [
      { name: "machines/m1", title: "M1" } as unknown as MachineSummary,
    ],
    machinesLoading: false,
    fetchMachines: vi.fn(async () => undefined),
    commands: [
      {
        name: "agents/a/commands/c1",
        status: 3,
        finalSummary: "done",
      } as unknown as Command,
    ],
    commandsLoading: false,
    activeOutputs: {},
    activeEvents: {},
    listCommands: vi.fn(async () => undefined),
    getCommand: vi.fn(async () => undefined),
    watchCommand: vi.fn(async () => true),
    watchCommandEvents: vi.fn(async () => true),
    cancelCommand: vi.fn(async () => ({}) as Command),
    getOrCreateConversation: vi.fn(async () => "conversations/1"),
    fetchChannels: vi.fn(async () => {}),
    sendChatMessage: vi.fn(async () => ({}) as ChatMessage),
  });
}

function buildRealRouter() {
  const allRoutes: RouteObject[] = [
    {
      element: <RootLayout />,
      errorElement: <RouterErrorBoundary />,
      loader: ({ request }: { request: Request }) =>
        rootGuard({ url: new URL(request.url) }),
      children: [
        ...authRoutes,
        ...dashboardRoutes,
        { path: "*", loader: () => redirect("/") },
      ],
    },
  ];
  setRouteNameIndex(buildRouteNameIndex(allRoutes));
  return createBrowserRouter(allRoutes);
}

describe("command row click navigation", () => {
  beforeEach(() => {
    seedStore();
    window.history.replaceState(null, "", "/members/agents/a/commands");
  });

  it("navigates to the canonical detail route without remounting members", async () => {
    const router = buildRealRouter();
    render(<RouterProvider router={router} />);

    await waitFor(
      () => {
        expect(screen.getByText("done")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
    const fetchMembers = useAppStore.getState().fetchMembers as ReturnType<
      typeof vi.fn
    >;
    expect(fetchMembers).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByText("done").closest("tr")!);

    await waitFor(
      () => {
        expect(useAppStore.getState().watchCommand).toHaveBeenCalled();
      },
      { timeout: 3000 }
    );
    expect(window.location.pathname).toBe("/members/agents/a/commands/c1");
    expect(fetchMembers).toHaveBeenCalledTimes(1);
  });

  it("back button returns to the canonical list route without remounting members", async () => {
    const router = buildRealRouter();
    render(<RouterProvider router={router} />);

    await waitFor(
      () => {
        expect(screen.getByText("done")).toBeInTheDocument();
      },
      { timeout: 3000 }
    );
    const fetchMembers = useAppStore.getState().fetchMembers as ReturnType<
      typeof vi.fn
    >;

    fireEvent.click(screen.getByText("done").closest("tr")!);
    await waitFor(
      () => {
        expect(useAppStore.getState().watchCommand).toHaveBeenCalled();
      },
      { timeout: 3000 }
    );

    const back = screen
      .getAllByRole("button", { name: /back/i })
      .find((b) => b.textContent?.includes("←"))!;
    fireEvent.click(back);
    await waitFor(
      () => {
        expect(window.location.pathname).toBe("/members/agents/a/commands");
      },
      { timeout: 3000 }
    );
    expect(fetchMembers).toHaveBeenCalledTimes(1);
  });
});

describe("command list page", () => {
  function renderListPage() {
    return render(
      <MemoryRouter initialEntries={["/members/agents/a/commands"]}>
        <Routes>
          <Route
            path="/members/agents/:agentId/commands"
            element={<CommandListPage />}
          />
          <Route
            path="/members/agents/:agentId/commands/:commandId"
            element={<div data-testid="detail" />}
          />
        </Routes>
      </MemoryRouter>
    );
  }

  function cmd(name: string, summary: string): Command {
    return {
      name,
      status: CommandStatus.COMPLETED,
      finalSummary: summary,
    } as unknown as Command;
  }

  beforeEach(() => {
    seedStore();
  });

  it("shows the loading row while commands are being fetched", () => {
    useAppStore.setState({ commands: [], commandsLoading: true });

    renderListPage();

    expect(screen.getByText("Loading...")).toBeInTheDocument();
  });

  it("shows the empty state and opens the new-task sheet from it", async () => {
    useAppStore.setState({ commands: [], commandsLoading: false });

    renderListPage();

    expect(
      await screen.findByText("No tasks yet", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Create your first task" })
    );
    expect(
      await screen.findByText("Send a task to agent a", {}, { timeout: 3000 })
    ).toBeInTheDocument();
  });

  it("filters the list by status", async () => {
    const listCommands = vi.fn(async () => ({
      commands: [],
      nextPageToken: "",
    }));
    useAppStore.setState({
      commands: [],
      commandsLoading: false,
      listCommands,
    });

    renderListPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Running" }, { timeout: 3000 })
    );
    await waitFor(
      () =>
        expect(listCommands).toHaveBeenCalledWith(
          "agents/a",
          expect.objectContaining({ status: CommandStatus.RUNNING })
        ),
      { timeout: 3000 }
    );
  });

  it("paginates with next/prev", async () => {
    // The real store listCommands writes the fetched page into the store; the
    // mock mirrors that so the table re-renders per page.
    const listCommands = vi.fn(
      async (
        _agent: string,
        params?: { pageSize?: number; pageToken?: string; status?: number }
      ) => {
        const page =
          params?.pageToken === "tok2"
            ? {
                commands: [cmd("agents/a/commands/c2", "two")],
                nextPageToken: "",
              }
            : {
                commands: [cmd("agents/a/commands/c1", "one")],
                nextPageToken: "tok2",
              };
        useAppStore.setState({ commands: page.commands });
        return page;
      }
    );
    useAppStore.setState({
      commands: [cmd("agents/a/commands/c1", "one")],
      commandsLoading: false,
      listCommands,
    });

    renderListPage();

    expect(
      await screen.findByText("one", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(screen.getByText("Page 1")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(
      await screen.findByText("two", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(screen.getByText("Page 2")).toBeInTheDocument();
    expect(listCommands).toHaveBeenCalledWith(
      "agents/a",
      expect.objectContaining({ pageToken: "tok2" })
    );

    fireEvent.click(screen.getByRole("button", { name: "Prev" }));
    expect(
      await screen.findByText("one", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(screen.getByText("Page 1")).toBeInTheDocument();
  });

  it("sends a new task from the sheet", async () => {
    const sendChatMessage = vi.fn(async () => ({}) as ChatMessage);
    const listCommands = vi.fn(async () => ({
      commands: [],
      nextPageToken: "",
    }));
    useAppStore.setState({
      commands: [],
      commandsLoading: false,
      sendChatMessage,
      listCommands,
    });

    renderListPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "New Task" }, { timeout: 3000 })
    );
    const textarea = await screen.findByPlaceholderText(
      /Read config\.yaml/,
      {},
      { timeout: 3000 }
    );
    fireEvent.change(textarea, { target: { value: "list ports" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(
      () =>
        expect(sendChatMessage).toHaveBeenCalledWith("agents/a", "list ports"),
      { timeout: 3000 }
    );
    await waitFor(
      () =>
        expect(
          screen.queryByPlaceholderText(/Read config\.yaml/)
        ).not.toBeInTheDocument(),
      { timeout: 3000 }
    );
  });

  it("expands the final summary in a sheet", async () => {
    useAppStore.setState({
      commands: [cmd("agents/a/commands/c1", "long summary text")],
      commandsLoading: false,
    });

    renderListPage();

    fireEvent.click(
      await screen.findByRole(
        "button",
        { name: "Show final summary" },
        { timeout: 3000 }
      )
    );
    // The sheet title joins the table header, confirming the sheet opened.
    expect(
      await screen.findAllByText("Final Summary", {}, { timeout: 3000 })
    ).toHaveLength(2);
    expect(screen.getAllByText(/long summary text/).length).toBeGreaterThan(0);
  });

  it("opens the detail page when a row is activated with the keyboard", async () => {
    useAppStore.setState({
      commands: [cmd("agents/a/commands/c1", "done")],
      commandsLoading: false,
    });

    renderListPage();

    const row = (
      await screen.findByText("done", {}, { timeout: 3000 })
    ).closest("tr")!;
    fireEvent.keyDown(row, { key: "Enter" });
    expect(
      await screen.findByTestId("detail", {}, { timeout: 3000 })
    ).toBeInTheDocument();
  });
});
