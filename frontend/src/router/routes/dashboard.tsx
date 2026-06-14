import type { RouteObject } from "react-router-dom";
import { DashboardLayout } from "@/react/app/layouts/dashboard-layout";

export const dashboardRoutes: RouteObject[] = [
  {
    element: <DashboardLayout />,
    children: [
      {
        index: true,
        handle: { name: "workspace.landing" },
        lazy: () =>
          import("@/react/pages/dashboard/landing").then((m) => ({
            Component: m.LandingPage,
          })),
      },
      {
        path: "agents",
        handle: { name: "agent.list" },
        lazy: () =>
          import("@/react/pages/dashboard/agents").then((m) => ({
            Component: m.AgentsPage,
          })),
      },
      {
        path: "agents/:agentId/commands",
        handle: { name: "command.list" },
        lazy: () =>
          import("@/react/pages/dashboard/command-list").then((m) => ({
            Component: m.CommandListPage,
          })),
      },
      {
        path: "agents/:agentId/commands/:commandId",
        handle: { name: "command.detail" },
        lazy: () =>
          import("@/react/pages/dashboard/command-detail").then((m) => ({
            Component: m.CommandDetailPage,
          })),
      },
    ],
  },
];
