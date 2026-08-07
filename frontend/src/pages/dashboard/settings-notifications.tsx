import { Loader2, Network, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Card } from "@/components/profile-common";
import {
  PageLoading,
  PermissionNotice,
  SettingsPage,
} from "@/components/settings-page";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { notificationServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";

// The per-user desktop-notification toggle lives on /settings/profile; this
// page keeps only the workspace-level push delivery config: the outbound
// HTTP(S) proxy used when the server cannot reach browser push services
// directly. It is admin-only (laelia.pushConfig.update) — the server only
// returns the stored proxy to callers holding that permission, so a non-admin
// sees an empty value and the whole page is hidden by canEditProxy.
export function SettingsNotificationsPage() {
  const { t } = useTranslation();
  const canEditProxy = useHasPermission("laelia.pushConfig.update");
  const [loading, setLoading] = useState(true);
  const [proxy, setProxy] = useState("");
  const [proxyEnabled, setProxyEnabled] = useState(false);
  const [proxySaving, setProxySaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!canEditProxy) {
        if (!cancelled) setLoading(false);
        return;
      }
      try {
        const res = await notificationServiceClient.getPushConfig({});
        if (!cancelled) {
          setProxy(res.httpProxy ?? "");
          setProxyEnabled(!!res.httpProxy);
        }
      } catch {
        // treat a backend error as unset so the admin sees an empty form
        // rather than a half-broken page.
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [canEditProxy]);

  // handleToggleProxy turns the proxy on or off. Turning it on just reveals
  // the input (the value is saved explicitly via handleSaveProxy). Turning it
  // off immediately clears the stored proxy so delivery reverts to a direct
  // connection without an extra save step.
  async function handleToggleProxy(next: boolean) {
    if (next) {
      setProxyEnabled(true);
      return;
    }
    setProxyEnabled(false);
    setProxy("");
    setProxySaving(true);
    try {
      await notificationServiceClient.updatePushConfig({ httpProxy: "" });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.notifications.proxy-save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
      // revert the toggle so the UI reflects the still-stored proxy
      setProxyEnabled(true);
    } finally {
      setProxySaving(false);
    }
  }

  async function handleSaveProxy() {
    setProxySaving(true);
    try {
      await notificationServiceClient.updatePushConfig({ httpProxy: proxy });
      toastManager.add({
        type: "info",
        title: t("settings.notifications.proxy-saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.notifications.proxy-save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setProxySaving(false);
    }
  }

  if (!canEditProxy) {
    return (
      <PermissionNotice message={t("settings.notifications.not-allowed")} />
    );
  }

  return (
    <SettingsPage
      title={t("settings.notifications.title")}
      description={t("settings.notifications.description")}
    >
      {loading ? (
        <PageLoading message={t("settings.notifications.loading")} />
      ) : (
        <div className="mx-auto max-w-2xl">
          <Card title={t("settings.notifications.proxy-title")}>
            <div className="flex items-center justify-between">
              <div className="flex items-start gap-3">
                <Network className="mt-0.5 size-4 text-control-light" />
                <div>
                  <div className="text-sm font-medium text-main">
                    {t("settings.notifications.proxy-enable")}
                  </div>
                  <div className="mt-0.5 text-xs text-control-light">
                    {proxyEnabled
                      ? t("settings.notifications.enabled")
                      : t("settings.notifications.disabled")}
                  </div>
                </div>
              </div>
              <Switch
                checked={proxyEnabled}
                onCheckedChange={handleToggleProxy}
                disabled={proxySaving}
                size="md"
              />
            </div>

            {proxyEnabled && (
              <div>
                <p className="text-xs text-control-light">
                  {t("settings.notifications.proxy-description")}
                </p>
                <div className="mt-3 flex items-end gap-2">
                  <Input
                    value={proxy}
                    placeholder={t("settings.notifications.proxy-placeholder")}
                    onChange={(e) => setProxy(e.target.value)}
                    spellCheck={false}
                  />
                  <Button onClick={handleSaveProxy} disabled={proxySaving}>
                    {proxySaving ? (
                      <Loader2 className="size-4 animate-spin" />
                    ) : (
                      <Save className="size-4" />
                    )}
                    {t(
                      proxySaving
                        ? "settings.notifications.proxy-saving"
                        : "settings.notifications.proxy-save"
                    )}
                  </Button>
                </div>
              </div>
            )}
          </Card>
        </div>
      )}
    </SettingsPage>
  );
}
