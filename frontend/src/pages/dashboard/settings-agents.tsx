import { create } from "@bufbuild/protobuf";
import { Bot, Loader2, Server, ShieldAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsPage } from "@/components/settings-page";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { settingServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
import {
  LlmAgentConfigSettingSchema,
  type McpIpPolicy,
  McpIpPolicy_Scope,
  McpIpPolicySchema,
  UserMcpConfigSettingSchema,
} from "@/types/proto-es/store/setting_pb";
import { SettingValueSchema } from "@/types/proto-es/v1/setting_pb";

// Preset deny list of internal / cloud-metadata ranges appended idempotently
// by the "add presets" button.
const DENY_PRESETS = [
  "0.0.0.0/8",
  "10.0.0.0/8",
  "100.64.0.0/10",
  "127.0.0.0/8",
  "169.254.0.0/16",
  "172.16.0.0/12",
  "192.168.0.0/16",
  "198.18.0.0/15",
  "224.0.0.0/4",
  "240.0.0.0/4",
  "::1/128",
  "fc00::/7",
  "fe80::/10",
];

function splitLines(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

// SettingsAgentsPage hosts workspace-level agent/LLM configuration: whether
// users may self-provide inline LLM credentials, whether users may configure
// personal MCP servers, and the MCP target IP allow/deny policy that bounds
// where those servers may connect (SSRF guard).
export function SettingsAgentsPage() {
  const { t } = useTranslation();
  const canUpdate = useHasPermission("laelia.settings.update");

  const [enabled, setEnabled] = useState(true);
  const [userMcpEnabled, setUserMcpEnabled] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [userMcpSaving, setUserMcpSaving] = useState(false);

  // Policy editor state. The textareas hold the in-progress CIDR lists; they
  // are pushed to the server on save (or when the MCP toggle changes).
  const [policyEnabled, setPolicyEnabled] = useState(false);
  const [policyScope, setPolicyScope] = useState<McpIpPolicy_Scope>(
    McpIpPolicy_Scope.USER_CREATED
  );
  const [allowText, setAllowText] = useState("");
  const [denyText, setDenyText] = useState("");

  // Confirm dialog state: enabling personal MCP servers requires an explicit
  // second step because it expands the SSRF surface.
  const [showMcpEnableConfirm, setShowMcpEnableConfirm] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      settingServiceClient.getSetting({ name: "settings/llm_agent_config" }),
      settingServiceClient.getSetting({ name: "settings/user_mcp_config" }),
    ])
      .then(([llmRes, mcpRes]) => {
        if (cancelled) return;
        const llmV = llmRes.value?.value;
        const mcpV = mcpRes.value?.value;
        setEnabled(
          llmV?.case === "llmAgentConfig"
            ? llmV.value.allowUserSelfProvidedKeys
            : true
        );
        setUserMcpEnabled(
          mcpV?.case === "userMcpConfig" ? mcpV.value.allowUserMcpServers : true
        );
        const p =
          mcpV?.case === "userMcpConfig" ? mcpV.value.mcpIpPolicy : undefined;
        setPolicyEnabled(p?.enabled ?? false);
        setPolicyScope(p?.scope ?? McpIpPolicy_Scope.USER_CREATED);
        setAllowText((p?.allowCidrs ?? []).join("\n"));
        setDenyText((p?.denyCidrs ?? []).join("\n"));
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

  function buildPolicy(): McpIpPolicy {
    return create(McpIpPolicySchema, {
      enabled: policyEnabled,
      scope: policyScope,
      allowCidrs: splitLines(allowText),
      denyCidrs: splitLines(denyText),
    });
  }

  async function persistUserMcp(nextEnabled: boolean) {
    setUserMcpSaving(true);
    try {
      await settingServiceClient.updateSetting({
        setting: {
          name: "settings/user_mcp_config",
          value: create(SettingValueSchema, {
            value: {
              case: "userMcpConfig" as const,
              value: create(UserMcpConfigSettingSchema, {
                allowUserMcpServers: nextEnabled,
                mcpIpPolicy: buildPolicy(),
              }),
            },
          }),
        },
      });
      setUserMcpEnabled(nextEnabled);
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

  async function handleToggle(next: boolean) {
    setSaving(true);
    try {
      await settingServiceClient.updateSetting({
        setting: {
          name: "settings/llm_agent_config",
          value: create(SettingValueSchema, {
            value: {
              case: "llmAgentConfig" as const,
              value: create(LlmAgentConfigSettingSchema, {
                allowUserSelfProvidedKeys: next,
              }),
            },
          }),
        },
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

  function handleUserMcpToggle(next: boolean) {
    if (next) {
      // Enabling expands the SSRF surface: require explicit confirmation.
      setShowMcpEnableConfirm(true);
      return;
    }
    void persistUserMcp(false);
  }

  function handlePolicySave() {
    void persistUserMcp(userMcpEnabled);
  }

  function addPresets() {
    const current = new Set(splitLines(denyText));
    const additions = DENY_PRESETS.filter((cidr) => !current.has(cidr));
    if (additions.length === 0) return;
    setDenyText([...splitLines(denyText), ...additions].join("\n"));
  }

  const bothEmpty = allowText.trim() === "" && denyText.trim() === "";

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

          {canUpdate && (
            <div className="rounded-md border border-control-border p-4">
              <div className="flex items-center justify-between">
                <div className="flex min-w-0 flex-1 items-start gap-3">
                  <ShieldAlert className="mt-0.5 size-4 shrink-0 text-control-light" />
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-main">
                      {t("settings.agents.ip-policy-title")}
                    </div>
                    <div className="mt-0.5 text-xs text-control-light">
                      {t("settings.agents.ip-policy-hint")}
                    </div>
                  </div>
                </div>
                <Switch
                  checked={policyEnabled}
                  onCheckedChange={setPolicyEnabled}
                  disabled={userMcpSaving}
                  size="md"
                  className="shrink-0"
                />
              </div>

              {policyEnabled && (
                <div className="mt-4 space-y-4">
                  <div className="text-xs text-control-light">
                    {t("settings.agents.ip-policy-scope")}
                  </div>
                  <RadioGroup
                    value={String(policyScope)}
                    onValueChange={(v) =>
                      setPolicyScope(Number(v) as McpIpPolicy_Scope)
                    }
                  >
                    <RadioGroupItem value={String(McpIpPolicy_Scope.ALL)}>
                      {t("settings.agents.ip-policy-scope-all")}
                    </RadioGroupItem>
                    <RadioGroupItem
                      value={String(McpIpPolicy_Scope.USER_CREATED)}
                    >
                      {t("settings.agents.ip-policy-scope-user")}
                    </RadioGroupItem>
                  </RadioGroup>

                  <div>
                    <div className="mb-1 text-xs text-control-light">
                      {t("settings.agents.ip-policy-allow-label")}
                    </div>
                    <Textarea
                      value={allowText}
                      onChange={(e) => setAllowText(e.target.value)}
                      placeholder={t(
                        "settings.agents.ip-policy-allow-placeholder"
                      )}
                      rows={4}
                      spellCheck={false}
                    />
                  </div>

                  <div>
                    <div className="mb-1 text-xs text-control-light">
                      {t("settings.agents.ip-policy-deny-label")}
                    </div>
                    <Textarea
                      value={denyText}
                      onChange={(e) => setDenyText(e.target.value)}
                      placeholder={t(
                        "settings.agents.ip-policy-deny-placeholder"
                      )}
                      rows={4}
                      spellCheck={false}
                    />
                    <div className="mt-1 flex items-center gap-2">
                      <Button variant="outline" size="xs" onClick={addPresets}>
                        {t("settings.agents.ip-policy-preset")}
                      </Button>
                    </div>
                  </div>

                  {bothEmpty && (
                    <div className="text-xs text-amber-600 dark:text-amber-400">
                      {t("settings.agents.ip-policy-empty-warning")}
                    </div>
                  )}

                  <div className="flex justify-end">
                    <Button
                      size="sm"
                      onClick={handlePolicySave}
                      disabled={userMcpSaving}
                    >
                      {userMcpSaving
                        ? t("common.saving")
                        : t("settings.agents.ip-policy-save")}
                    </Button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      <AlertDialog
        open={showMcpEnableConfirm}
        onOpenChange={setShowMcpEnableConfirm}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.agents.allow-user-mcp-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.agents.allow-user-mcp-confirm-description")}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline">{t("common.cancel")}</Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={userMcpSaving}
              onClick={() => {
                setShowMcpEnableConfirm(false);
                void persistUserMcp(true);
              }}
            >
              {t("settings.agents.allow-user-mcp-confirm-action")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsPage>
  );
}
