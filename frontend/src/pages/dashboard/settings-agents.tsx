import { create } from "@bufbuild/protobuf";
import { Bot, Loader2, Server } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsPage } from "@/components/settings-page";
import { Switch } from "@/components/ui/switch";
import { settingServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
import {
  LlmAgentConfigSettingSchema,
  UserMcpConfigSettingSchema,
} from "@/types/proto-es/store/setting_pb";

// SettingsAgentsPage hosts workspace-level agent/LLM configuration. Today the
// single toggle controls whether users may self-provide an inline
// api_provider/api_key/model on a builtin-pi agent (in addition to the managed
// global API providers). The server defaults the toggle to enabled.
export function SettingsAgentsPage() {
  const { t } = useTranslation();
  const canUpdate = useHasPermission("laelia.settings.update");

  // Default to enabled to match the server's missing-row default; flip to the
  // actual value once the config loads.
  const [enabled, setEnabled] = useState(true);
  const [userMcpEnabled, setUserMcpEnabled] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [userMcpSaving, setUserMcpSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      settingServiceClient.getLlmAgentConfig({}),
      settingServiceClient.getUserMcpConfig({}),
    ])
      .then(([llmRes, mcpRes]) => {
        if (cancelled) return;
        setEnabled(llmRes.config?.allowUserSelfProvidedKeys ?? true);
        setUserMcpEnabled(mcpRes.config?.allowUserMcpServers ?? true);
        setLoaded(true);
      })
      .catch(() => {
        if (cancelled) return;
        setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleToggle(next: boolean) {
    setSaving(true);
    try {
      await settingServiceClient.updateLlmAgentConfig({
        config: create(LlmAgentConfigSettingSchema, {
          allowUserSelfProvidedKeys: next,
        }),
      });
      setEnabled(next);
      toastManager.add({ type: "success", title: t("settings.agents.saved") });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.agents.save-failed"),
        description: describeError(err),
      });
    } finally {
      setSaving(false);
    }
  }

  async function handleUserMcpToggle(next: boolean) {
    setUserMcpSaving(true);
    try {
      await settingServiceClient.updateUserMcpConfig({
        config: create(UserMcpConfigSettingSchema, {
          allowUserMcpServers: next,
        }),
      });
      setUserMcpEnabled(next);
      toastManager.add({
        type: "success",
        title: t("settings.agents.saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.agents.save-failed"),
        description: describeError(err),
      });
    } finally {
      setUserMcpSaving(false);
    }
  }

  return (
    <SettingsPage
      title={t("settings.agents.title")}
      description={t("settings.agents.description")}
    >
      {!loaded ? (
        <div className="flex items-center justify-center py-8 text-sm text-control-light">
          <Loader2 className="mr-2 size-4 animate-spin" />
          {t("common.loading")}
        </div>
      ) : (
        <div className="mx-auto max-w-2xl space-y-4">
          <div className="flex items-center justify-between rounded-md border border-control-border p-4">
            <div className="flex min-w-0 flex-1 items-start gap-3">
              <Bot className="mt-0.5 size-4 shrink-0 text-control-light" />
              <div className="min-w-0">
                <div className="text-sm font-medium text-main">
                  {t("settings.agents.self-provided-keys")}
                </div>
                <div className="mt-0.5 text-xs text-control-light">
                  {t("settings.agents.self-provided-keys-hint")}
                </div>
              </div>
            </div>
            <Switch
              checked={enabled}
              onCheckedChange={handleToggle}
              disabled={saving || !canUpdate}
              size="md"
              className="shrink-0"
            />
          </div>
          <div className="flex items-center justify-between rounded-md border border-control-border p-4">
            <div className="flex min-w-0 flex-1 items-start gap-3">
              <Server className="mt-0.5 size-4 shrink-0 text-control-light" />
              <div className="min-w-0">
                <div className="text-sm font-medium text-main">
                  {t("settings.agents.allow-user-mcp")}
                </div>
                <div className="mt-0.5 text-xs text-control-light">
                  {t("settings.agents.allow-user-mcp-hint")}
                </div>
              </div>
            </div>
            <Switch
              checked={userMcpEnabled}
              onCheckedChange={handleUserMcpToggle}
              disabled={userMcpSaving || !canUpdate}
              size="md"
              className="shrink-0"
            />
          </div>
        </div>
      )}
    </SettingsPage>
  );
}
