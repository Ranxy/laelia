import type { RouteObject } from "react-router-dom";
import { Navigate, Outlet } from "react-router-dom";
import { DashboardLayout } from "@/app/layouts/dashboard-layout";
import {
  ACTIVITY_ROUTE,
  ACTIVITY_ROUTE_DETAIL,
  AGENT_ROUTE_CHAT,
  AGENT_ROUTE_PROFILE,
  CHAT_ROUTE,
  CHAT_ROUTE_DETAIL,
  COMMAND_ROUTE_DETAIL,
  COMMAND_ROUTE_LIST,
  MACHINE_ROUTE_LIST,
  MACHINE_ROUTE_PROFILE,
  MEMBERS_ROUTE,
  REMINDER_ROUTE_DETAIL,
  REMINDER_ROUTE_LIST,
  SETTINGS_ROUTE_IAM,
  SETTINGS_ROUTE_PROFILE,
  SETTINGS_ROUTE_ROLES,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
} from "../handles";

export const dashboardRoutes: RouteObject[] = [
  {
    element: <DashboardLayout />,
    children: [
      {
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
        path: "activity",
        handle: { name: ACTIVITY_ROUTE },
        lazy: () =>
          import("@/pages/dashboard/activity-layout").then((m) => ({
            Component: m.ActivityLayout,
          })),
        children: [
          {
            path: ":messageId",
            handle: { name: ACTIVITY_ROUTE_DETAIL },
            lazy: () =>
              import("@/pages/dashboard/activity-detail").then((m) => ({
                Component: m.ActivityDetail,
              })),
          },
        ],
      },
      {
        path: "agents",
        // The Agents list page is gone (replaced by Members + Machines). This
        // route now exists only to host per-agent detail pages reached from a
        // member row or a machine roster link; the index redirects to Members.
        element: <Outlet />,
        children: [
          {
            index: true,
            element: <Navigate to="/members" replace />,
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
                path: "reminders",
                handle: { name: REMINDER_ROUTE_LIST },
                lazy: () =>
                  import("@/pages/dashboard/reminder-list").then((m) => ({
                    Component: m.ReminderListPage,
                  })),
              },
              {
                path: "reminders/:reminderId",
                handle: { name: REMINDER_ROUTE_DETAIL },
                lazy: () =>
                  import("@/pages/dashboard/reminder-detail").then((m) => ({
                    Component: m.ReminderDetailPage,
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
        path: "machines",
        lazy: () =>
          import("@/pages/dashboard/machines").then((m) => ({
            Component: m.MachinesPage,
          })),
        children: [
          {
            index: true,
            handle: { name: MACHINE_ROUTE_LIST },
            lazy: () =>
              import("@/pages/dashboard/machine-detail-empty-state").then(
                (m) => ({
                  Component: m.MachineDetailEmptyState,
                })
              ),
          },
          {
            path: ":machineId",
            lazy: () =>
              import("@/app/layouts/machine-detail-layout").then((m) => ({
                Component: m.MachineDetailLayout,
              })),
            children: [
              {
                index: true,
                handle: { name: MACHINE_ROUTE_PROFILE },
                lazy: () =>
                  import("@/pages/dashboard/machine-profile").then((m) => ({
                    Component: m.MachineProfilePage,
                  })),
              },
            ],
          },
        ],
      },
      {
        path: "members",
        handle: { name: MEMBERS_ROUTE },
        lazy: () =>
          import("@/pages/dashboard/members").then((m) => ({
            Component: m.MembersPage,
          })),
      },
      {
        path: "settings",
        children: [
          { index: true, element: <Navigate to="storage" replace /> },
          {
            path: "profile",
            handle: { name: SETTINGS_ROUTE_PROFILE },
            lazy: () =>
              import("@/pages/dashboard/settings-profile").then((m) => ({
                Component: m.SettingsProfilePage,
              })),
          },
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
          {
            path: "roles",
            handle: { name: SETTINGS_ROUTE_ROLES },
            lazy: () =>
              import("@/pages/dashboard/settings-roles").then((m) => ({
                Component: m.SettingsRolesPage,
              })),
          },
          {
            path: "iam",
            handle: { name: SETTINGS_ROUTE_IAM },
            lazy: () =>
              import("@/pages/dashboard/settings-iam").then((m) => ({
                Component: m.SettingsIamPage,
              })),
          },
        ],
      },
    ],
  },
];
