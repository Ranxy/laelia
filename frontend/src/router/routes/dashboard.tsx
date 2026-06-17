import type { RouteObject } from "react-router-dom";
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
        path: "agents/:agentId/commands",
        handle: { name: COMMAND_ROUTE_LIST },
        lazy: () =>
          import("@/react/pages/dashboard/command-list").then((m) => ({
            Component: m.CommandListPage,
          })),
      },
      {
        path: "agents/:agentId/commands/:commandId",
        handle: { name: COMMAND_ROUTE_DETAIL },
        lazy: () =>
          import("@/react/pages/dashboard/command-detail").then((m) => ({
            Component: m.CommandDetailPage,
          })),
      },
      {
        path: "agents/:agentId/chat",
        handle: { name: CHAT_ROUTE },
        lazy: () =>
          import("@/react/pages/dashboard/chat").then((m) => ({
            Component: m.ChatPage,
          })),
      },
    ],
  },
];
