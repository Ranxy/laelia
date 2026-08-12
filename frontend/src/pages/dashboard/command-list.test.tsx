import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import {
  createBrowserRouter,
  type RouteObject,
  RouterProvider,
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
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";
import type { User } from "@/types/proto-es/v1/user_service_pb";

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
        subtitle: "machines/m1",
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
