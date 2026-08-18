import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AgentService } from "@/types/proto-es/v1/agent_pb";
import { DeviceService } from "@/types/proto-es/v1/device_pb";
import { MachineService } from "@/types/proto-es/v1/machine_pb";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { UserService } from "@/types/proto-es/v1/user_service_pb";
import { CommandService } from "@/types/proto-es/v1/command_pb";
import { SettingService } from "@/types/proto-es/v1/setting_pb";
import { RoleService } from "@/types/proto-es/v1/role_service_pb";
import { IamService } from "@/types/proto-es/v1/iam_service_pb";
import { GroupService } from "@/types/proto-es/v1/group_service_pb";
import { ApiProviderService } from "@/types/proto-es/v1/api_provider_service_pb";
import { McpServerService } from "@/types/proto-es/v1/mcp_pb";
import { AuditLogService } from "@/types/proto-es/v1/audit_log_service_pb";
import { NotificationService } from "@/types/proto-es/v1/notification_pb";
import { IdentityProviderService } from "@/types/proto-es/v1/idp_service_pb";
import { createAuthInterceptor } from "./auth-interceptor";

// Guards against a stampede of concurrent 401s each triggering a redirect.
let authRedirecting = false;

/**
 * Default handler for a mid-session `Unauthenticated` (expired access cookie):
 * clears auth state and bounces to sign-in, preserving the current URL. The
 * store is imported dynamically to avoid a static import cycle — `stores/auth`
 * imports the clients below, so `@/connect` importing `@/stores` at module load
 * would be circular.
 */
async function onUnauthenticated() {
  if (authRedirecting) {
    return;
  }
  // On public auth surfaces (sign-in, OAuth callback, device login) a 401 is
  // expected during an in-flight login. In particular RootLayout's loadSession()
  // fires GetCurrentUser before the OAuth login completes; that 401 must not
  // reset the store or yank the user back to sign-in mid-login.
  if (
    window.location.pathname.startsWith("/auth/") ||
    window.location.pathname.startsWith("/oauth/callback") ||
    window.location.pathname.startsWith("/oauth/login") ||
    window.location.pathname.startsWith("/login/device")
  ) {
    return;
  }
  authRedirecting = true;
  try {
    const { useAppStore } = await import("@/stores");
    // Clear auth without calling the backend `logout` RPC (it would itself 401
    // and re-enter this handler). Also wipe every slice (same as logout) so a
    // mid-session expiry can't leave the previous principal's cached
    // messages/channels/rosters behind for the next login. Keep sessionLoaded
    // true so the guard does not re-show the initial spinner.
    useAppStore.getState().reset();
    useAppStore.setState({
      currentUser: null,
      isLoggedIn: false,
      sessionLoaded: true,
    });
    const redirect = encodeURIComponent(
      window.location.pathname + window.location.search
    );
    window.location.assign(`/auth/signin?redirect=${redirect}`);
  } finally {
    authRedirecting = false;
  }
}

const transport = createConnectTransport({
  baseUrl: import.meta.env.VITE_API_BASE_URL ?? "",
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
  interceptors: [createAuthInterceptor(onUnauthenticated)],
});

export const agentServiceClient = createClient(AgentService, transport);
export const deviceServiceClient = createClient(DeviceService, transport);
export const machineServiceClient = createClient(MachineService, transport);
export const authServiceClient = createClient(AuthService, transport);
export const userServiceClient = createClient(UserService, transport);
export const commandServiceClient = createClient(CommandService, transport);
export const settingServiceClient = createClient(SettingService, transport);
export const roleServiceClient = createClient(RoleService, transport);
export const iamServiceClient = createClient(IamService, transport);
export const groupServiceClient = createClient(GroupService, transport);
export const apiProviderServiceClient = createClient(
  ApiProviderService,
  transport
);
export const mcpServerServiceClient = createClient(McpServerService, transport);
export const auditLogServiceClient = createClient(AuditLogService, transport);
export const notificationServiceClient = createClient(
  NotificationService,
  transport
);

export const identityProviderServiceClient = createClient(
  IdentityProviderService,
  transport
);
