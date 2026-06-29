import type { RouteObject } from "react-router-dom";
import { Navigate } from "react-router-dom";
import { AgentWorkspaceLayout } from "@/app/layouts/agent-workspace-layout";
import { DashboardLayout } from "@/app/layouts/dashboard-layout";
import {
  AGENT_ROUTE_LIST,
  CHAT_ROUTE,
  CHAT_ROUTE_DETAIL,
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
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
          import("@/pages/dashboard/landing").then((m) => ({
            Component: m.LandingPage,
          })),
      },
      {
        path: "agents",
        handle: { name: AGENT_ROUTE_LIST },
        lazy: () =>
          import("@/pages/dashboard/agents").then((m) => ({
            Component: m.AgentsPage,
          })),
      },
      {
        path: "agents/:agentId",
        element: <AgentWorkspaceLayout />,
        children: [
          // Chat lives in the unified /chat page now; the agent workspace hosts
          // only the tasks/commands views.
          { index: true, element: <Navigate to="commands" replace /> },
          {
            path: "commands",
            handle: { name: COMMAND_ROUTE_LIST },
            lazy: () =>
              import("@/pages/dashboard/command-list").then((m) => ({
                Component: m.CommandListPage,
              })),
          },
          {
            path: "commands/:commandId",
            handle: { name: COMMAND_ROUTE_DETAIL },
            lazy: () =>
              import("@/pages/dashboard/command-detail").then((m) => ({
                Component: m.CommandDetailPage,
              })),
          },
        ],
      },
      {
        path: "chat",
        lazy: () =>
          import("@/pages/dashboard/chat-layout").then((m) => ({
            Component: m.ChatLayout,
          })),
        children: [
          {
            index: true,
            handle: { name: CHAT_ROUTE },
            lazy: () =>
              import("@/pages/dashboard/chat-conversation").then((m) => ({
                Component: m.ChatEmptyState,
              })),
          },
          {
            path: ":conversationId",
            handle: { name: CHAT_ROUTE_DETAIL },
            lazy: () =>
              import("@/pages/dashboard/chat-conversation").then((m) => ({
                Component: m.ChatConversationPage,
              })),
          },
        ],
      },
      {
        path: "settings",
        children: [
          { index: true, element: <Navigate to="storage" replace /> },
          {
            path: "storage",
            handle: { name: SETTINGS_ROUTE_STORAGE },
            lazy: () =>
              import("@/pages/dashboard/settings-storage").then((m) => ({
                Component: m.SettingsStoragePage,
              })),
          },
          {
            path: "users",
            handle: { name: SETTINGS_ROUTE_USERS },
            lazy: () =>
              import("@/pages/dashboard/user-list").then((m) => ({
                Component: m.UserListPage,
              })),
          },
        ],
      },
    ],
  },
];
