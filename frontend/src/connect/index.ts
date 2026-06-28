import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AgentService } from "@/types/proto-es/v1/agent_pb";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { UserService } from "@/types/proto-es/v1/user_service_pb";
import { CommandService } from "@/types/proto-es/v1/command_pb";
import { SettingService } from "@/types/proto-es/v1/setting_pb";
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
  authRedirecting = true;
  try {
    const { useAppStore } = await import("@/stores");
    // Clear auth without calling the backend `logout` RPC (it would itself 401
    // and re-enter this handler). Keep sessionLoaded true so the guard does not
    // re-show the initial spinner.
    useAppStore.setState({
      token: null,
      currentUser: null,
      isLoggedIn: false,
      sessionLoaded: true,
    });
    if (!window.location.pathname.startsWith("/auth/")) {
      const redirect = encodeURIComponent(
        window.location.pathname + window.location.search
      );
      window.location.assign(`/auth/signin?redirect=${redirect}`);
    }
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
export const authServiceClient = createClient(AuthService, transport);
export const userServiceClient = createClient(UserService, transport);
export const commandServiceClient = createClient(CommandService, transport);
export const settingServiceClient = createClient(SettingService, transport);
