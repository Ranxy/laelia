import type { RouteObject } from "react-router-dom";
import { Navigate, Outlet, useParams } from "react-router-dom";
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
  SETTINGS_ROUTE_API_PROVIDERS,
  SETTINGS_ROUTE_AUDIT,
  SETTINGS_ROUTE_CHAT,
  SETTINGS_ROUTE_GROUPS,
  SETTINGS_ROUTE_IAM,
  SETTINGS_ROUTE_NOTIFICATIONS,
  SETTINGS_ROUTE_PROFILE,
  SETTINGS_ROUTE_ROLES,
  SETTINGS_ROUTE_STORAGE,
  SETTINGS_ROUTE_USERS,
} from "../handles";

// AgentRouteRedirect preserves the legacy /agents/:agentId/** deep links
// (thread-panel, command-list, reminder-detail, machine-profile, etc.) by
// forwarding them to the canonical Members-embedded agent detail tree. The
// Members page now owns the agent detail layout; /agents exists only as a
// redirect so existing navigate(`/agents/...`) call sites keep working.
function AgentRouteRedirect() {
  const { agentId, "*": splat } = useParams<{ agentId: string; "*": string }>();
  const rest = splat ? `/${splat}` : "";
  return <Navigate to={`/members/agents/${agentId ?? ""}${rest}`} replace />;
}

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
        // Members owns the workspace directory (left rail) and the member
        // detail panes. The agent detail tree (AgentDetailLayout + its four
        // tab pages) is nested here so the route-name index resolves
        // AGENT_ROUTE_* / COMMAND_ROUTE_* / REMINDER_ROUTE_* under
        // /members/agents/:agentId — the left rail's tab navigation then
        // stays within Members. Defined before the legacy /agents redirect
        // so the index (first-registration-wins) picks these paths.
        path: "members",
        handle: { name: MEMBERS_ROUTE },
        lazy: () =>
          import("@/pages/dashboard/members").then((m) => ({
            Component: m.MembersPage,
          })),
        children: [
          {
            index: true,
            lazy: () =>
              import("@/components/selection-empty-state").then((m) => ({
                element: (
                  <m.SelectionEmptyState messageKey="members.no-selection" />
                ),
              })),
          },
          {
            path: "agents/:agentId",
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
          {
            path: "users/:userId",
            lazy: () =>
              import("@/pages/dashboard/human-detail").then((m) => ({
                Component: m.HumanDetailPage,
              })),
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
              import("@/components/selection-empty-state").then((m) => ({
                element: (
                  <m.SelectionEmptyState messageKey="machine.no-selection" />
                ),
              })),
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
        // Legacy /agents deep links redirect into the Members-embedded agent
        // detail tree. The index redirects to the Members directory; any
        // /agents/:agentId[/**] forwards to /members/agents/:agentId[/**].
        path: "agents",
        element: <Outlet />,
        children: [
          {
            index: true,
            element: <Navigate to="/members" replace />,
          },
          {
            path: ":agentId/*",
            element: <AgentRouteRedirect />,
          },
        ],
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
            path: "chat",
            handle: { name: SETTINGS_ROUTE_CHAT },
            lazy: () =>
              import("@/pages/dashboard/settings-chat").then((m) => ({
                Component: m.SettingsChatPage,
              })),
          },
          {
            path: "notifications",
            handle: { name: SETTINGS_ROUTE_NOTIFICATIONS },
            lazy: () =>
              import("@/pages/dashboard/settings-notifications").then((m) => ({
                Component: m.SettingsNotificationsPage,
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
          {
            path: "groups",
            handle: { name: SETTINGS_ROUTE_GROUPS },
            lazy: () =>
              import("@/pages/dashboard/settings-groups").then((m) => ({
                Component: m.SettingsGroupsPage,
              })),
          },
          {
            path: "api-providers",
            handle: { name: SETTINGS_ROUTE_API_PROVIDERS },
            lazy: () =>
              import("@/pages/dashboard/settings-api-providers").then((m) => ({
                Component: m.SettingsApiProvidersPage,
              })),
          },
          {
            path: "audit",
            handle: { name: SETTINGS_ROUTE_AUDIT },
            lazy: () =>
              import("@/pages/dashboard/settings-audit").then((m) => ({
                Component: m.SettingsAuditPage,
              })),
          },
        ],
      },
    ],
  },
];
