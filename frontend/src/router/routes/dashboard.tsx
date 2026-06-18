import type { RouteObject } from "react-router-dom";
import { Navigate } from "react-router-dom";
import { AgentWorkspaceLayout } from "@/react/app/layouts/agent-workspace-layout";
import { DashboardLayout } from "@/react/app/layouts/dashboard-layout";
import {
  AGENT_ROUTE_LIST,
  CHAT_ROUTE,
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  WORKSPACE_ROUTE_LANDING,
} from "../handles";

export const dashboardRoutes: RouteObject[] = [
  {
    element: <DashboardLayout />,
    children: [
      {
        index: true,
        handle: { name: WORKSPACE_ROUTE_LANDING },
        lazy: () =>
          import("@/react/pages/dashboard/landing").then((m) => ({
            Component: m.LandingPage,
          })),
      },
      {
        path: "agents",
        handle: { name: AGENT_ROUTE_LIST },
        lazy: () =>
          import("@/react/pages/dashboard/agents").then((m) => ({
            Component: m.AgentsPage,
          })),
      },
      {
        path: "agents/:agentId",
        element: <AgentWorkspaceLayout />,
        children: [
          { index: true, element: <Navigate to="chat" replace /> },
          {
            path: "chat",
            handle: { name: CHAT_ROUTE },
            lazy: () =>
              import("@/react/pages/dashboard/chat").then((m) => ({
                Component: m.ChatPage,
              })),
          },
          {
            path: "commands",
            handle: { name: COMMAND_ROUTE_LIST },
            lazy: () =>
              import("@/react/pages/dashboard/command-list").then((m) => ({
                Component: m.CommandListPage,
              })),
          },
          {
            path: "commands/:commandId",
            handle: { name: COMMAND_ROUTE_DETAIL },
            lazy: () =>
              import("@/react/pages/dashboard/command-detail").then((m) => ({
                Component: m.CommandDetailPage,
              })),
          },
        ],
      },
    ],
  },
];
