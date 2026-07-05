import type { RouteObject } from "react-router-dom";
import { Navigate } from "react-router-dom";
import { DashboardLayout } from "@/app/layouts/dashboard-layout";
import {
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_LIST,
  AGENT_ROUTE_PROFILE,
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
        lazy: () =>
          import("@/pages/dashboard/agents").then((m) => ({
            Component: m.AgentsPage,
          })),
        children: [
          {
            index: true,
            handle: { name: AGENT_ROUTE_LIST },
            lazy: () =>
              import("@/pages/dashboard/agent-detail-empty-state").then(
                (m) => ({
                  Component: m.AgentDetailEmptyState,
                })
              ),
          },
          {
            path: ":agentId",
            lazy: () =>
              import("@/app/layouts/agent-detail-layout").then((m) => ({
                Component: m.AgentDetailLayout,
              })),
            children: [
              {
                index: true,
                handle: { name: AGENT_ROUTE_PROFILE },
                lazy: () =>
                  import("@/pages/dashboard/agent-profile").then((m) => ({
                    Component: m.AgentProfilePage,
                  })),
              },
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
              {
                path: "chat",
                handle: { name: AGENT_ROUTE_CHAT },
                lazy: () =>
                  import("@/pages/dashboard/agent-chat").then((m) => ({
                    Component: m.AgentChatPage,
                  })),
              },
            ],
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
