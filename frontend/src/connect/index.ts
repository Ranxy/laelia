import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { AgentService } from "@/types/proto-es/v1/agent_pb";
import { AuthService } from "@/types/proto-es/v1/auth_service_pb";
import { UserService } from "@/types/proto-es/v1/user_service_pb";
import { CommandService } from "@/types/proto-es/v1/command_pb";
import { SettingService } from "@/types/proto-es/v1/setting_pb";

const transport = createConnectTransport({
  baseUrl: import.meta.env.VITE_API_BASE_URL ?? "",
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});

export const agentServiceClient = createClient(AgentService, transport);
export const authServiceClient = createClient(AuthService, transport);
export const userServiceClient = createClient(UserService, transport);
export const commandServiceClient = createClient(CommandService, transport);
export const settingServiceClient = createClient(SettingService, transport);
