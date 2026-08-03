import { Bell, Loader2, Network, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { notificationServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import {
  disableDesktopNotifications,
  enableDesktopNotifications,
  getStoredEnabled,
  PUSH_ENABLED_KEY,
  webPushSupported,
} from "@/lib/web-push";
import { useHasPermission } from "@/stores/permissions";

type Status = "loading" | "unsupported" | "not-configured" | "denied" | "ready";

export function SettingsNotificationsPage() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<Status>("loading");
  const [enabled, setEnabled] = useState(false);
  const [busy, setBusy] = useState(false);

  // Admin-only outbound HTTP proxy for push delivery. The server only returns
  // the stored proxy to callers holding laelia.pushConfig.update, so a non-admin
  // sees an empty value and the whole section is hidden by canEditProxy. The
  // toggle mirrors the desktop-notifications switch: off hides the input and
  // immediately clears the stored proxy; on reveals the input for the admin to
  // fill and save.
  const canEditProxy = useHasPermission("laelia.pushConfig.update");
  const [proxy, setProxy] = useState("");
  const [proxyEnabled, setProxyEnabled] = useState(false);
  const [proxySaving, setProxySaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const supported = webPushSupported();
      if (!supported) {
        if (!cancelled) setStatus("unsupported");
        return;
      }
      let configEnabled = false;
      try {
        const res = await notificationServiceClient.getPushConfig({});
        configEnabled = res.enabled;
        if (!cancelled) {
          setProxy(res.httpProxy ?? "");
          setProxyEnabled(!!res.httpProxy);
        }
      } catch {
        // treat a backend error as "not configured" so the user sees a clear
        // message rather than a half-broken toggle.
      }
      if (cancelled) return;
      if (!configEnabled) {
        setStatus("not-configured");
        return;
      }
      const permission = Notification.permission;
      if (permission === "denied") {
        setStatus("denied");
        return;
      }
      setStatus("ready");
      setEnabled(getStoredEnabled() && permission === "granted");
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleToggle(next: boolean) {
    setBusy(true);
    try {
      if (next) {
        await enableDesktopNotifications();
        setEnabled(true);
        setStatus("ready");
      } else {
        await disableDesktopNotifications();
        setEnabled(false);
      }
    } catch (err) {
      const code = err instanceof Error ? err.message : String(err);
      if (code === "denied") {
        setStatus("denied");
        toastManager.add({
          type: "warning",
          title: t("settings.notifications.permission-denied"),
        });
      } else if (code === "not-configured") {
        setStatus("not-configured");
      } else if (code === "unsupported") {
        setStatus("unsupported");
      } else {
        toastManager.add({
          type: "error",
          title: t(
            next
              ? "settings.notifications.enable-failed"
              : "settings.notifications.disable-failed"
          ),
          description: code,
        });
      }
    } finally {
      setBusy(false);
    }
  }

  // Refresh the toggle when the stored intent changes elsewhere (e.g. another
  // tab). storage events fire cross-tab; same-tab writes are handled inline.
  useEffect(() => {
    function onStorage(event: StorageEvent) {
      if (event.key === PUSH_ENABLED_KEY) {
        setEnabled(event.newValue === "true");
      }
    }
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  // handleToggleProxy turns the proxy on or off. Turning it on just reveals the
  // input (the value is saved explicitly via handleSaveProxy). Turning it off
  // immediately clears the stored proxy so delivery reverts to a direct
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

  return (
    <div className="flex h-full overflow-y-auto flex-col">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-lg font-semibold text-main">
          {t("settings.notifications.title")}
        </h1>
        <p className="mt-1 text-sm text-control-light">
          {t("settings.notifications.description")}
        </p>

        {status === "loading" ? (
          <div className="flex items-center justify-center gap-2 py-16 text-control-light text-sm">
            <Loader2 className="size-4 animate-spin" />
            {t("settings.notifications.loading")}
          </div>
        ) : (
          <div className="mt-6 space-y-4">
            {status === "unsupported" && (
              <Notice>{t("settings.notifications.unsupported")}</Notice>
            )}
            {status === "not-configured" && (
              <Notice>{t("settings.notifications.not-configured")}</Notice>
            )}
            {status === "denied" && (
              <Notice>{t("settings.notifications.permission-denied")}</Notice>
            )}

            <div className="flex items-center justify-between rounded-md border border-control-border p-4">
              <div className="flex items-start gap-3">
                <Bell className="mt-0.5 size-4 text-control-light" />
                <div>
                  <div className="text-sm font-medium text-main">
                    {t("settings.notifications.enable")}
                  </div>
                  <div className="mt-0.5 text-xs text-control-light">
                    {enabled
                      ? t("settings.notifications.enabled")
                      : t("settings.notifications.disabled")}
                  </div>
                </div>
              </div>
              <Switch
                checked={enabled}
                onCheckedChange={handleToggle}
                disabled={busy || status !== "ready"}
                size="md"
              />
            </div>

            {status === "ready" && !enabled && (
              <p className="text-xs text-control-placeholder">
                {t("settings.notifications.permission-prompt")}
              </p>
            )}

            {canEditProxy && (
              <div className="rounded-md border border-control-border p-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-start gap-3">
                    <Network className="mt-0.5 size-4 text-control-light" />
                    <div>
                      <div className="text-sm font-medium text-main">
                        {t("settings.notifications.proxy-title")}
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
                  <div className="mt-3">
                    <p className="mb-2 text-xs text-control-light">
                      {t("settings.notifications.proxy-description")}
                    </p>
                    <div className="flex items-end gap-2">
                      <Input
                        value={proxy}
                        placeholder={t(
                          "settings.notifications.proxy-placeholder"
                        )}
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
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-md border border-control-border bg-control-bg p-3 text-xs text-control">
      {children}
    </p>
  );
}
