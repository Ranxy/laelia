import { Loader2, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PageLoading, SettingsPage } from "@/components/settings-page";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import { SecretInput } from "@/components/ui/secret-input";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { useAppStore } from "@/stores";
import { smtpConfigPaths } from "@/stores/setting";

interface SmtpForm {
  host: string;
  port: number;
  username: string;
  password: string;
  from: string;
  useTls: boolean;
}

const EMPTY: SmtpForm = {
  host: "",
  port: 587,
  username: "",
  password: "",
  from: "",
  useTls: true,
};

// isMasked reports whether a secret value is the server-returned mask
// ("****…" or empty). The backend treats a masked secret as "unchanged".
function isMasked(secret: string): boolean {
  return secret === "" || secret.startsWith("****");
}

export function SettingsSmtpPage() {
  const { t } = useTranslation();
  const [form, setForm] = useState<SmtpForm>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const cfg = await useAppStore.getState().fetchSmtpConfig();
        if (cancelled) return;
        setForm({
          host: cfg?.host ?? "",
          port: cfg?.port ?? 587,
          username: cfg?.username ?? "",
          password: cfg?.password ?? "",
          from: cfg?.from ?? "",
          useTls: cfg?.useTls ?? true,
        });
      } catch (err) {
        void showErrorToast(err, t("settings.smtp.load-failed"));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [t]);

  async function handleSave() {
    setSaving(true);
    try {
      const cfg = await useAppStore.getState().updateSmtpConfig(
        {
          host: form.host.trim(),
          port: form.port,
          username: form.username.trim(),
          // Send the masked value back when the user didn't edit the
          // password; the backend interprets a "****" prefix as "leave
          // unchanged".
          password: form.password,
          from: form.from.trim(),
          useTls: form.useTls,
        },
        [...smtpConfigPaths]
      );
      setForm({
        host: cfg?.host ?? "",
        port: cfg?.port ?? 587,
        username: cfg?.username ?? "",
        password: cfg?.password ?? "",
        from: cfg?.from ?? "",
        useTls: cfg?.useTls ?? true,
      });
      toastManager.add({
        type: "success",
        title: t("settings.smtp.saved"),
      });
    } catch (err) {
      void showErrorToast(err, t("settings.smtp.save-failed"));
    } finally {
      setSaving(false);
    }
  }

  const set = <K extends keyof SmtpForm>(key: K, value: SmtpForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <SettingsPage
      title={t("settings.smtp.title")}
      description={t("settings.smtp.description")}
      contentWidth="mx-auto w-full max-w-2xl"
    >
      {loading ? (
        <PageLoading />
      ) : (
        <div className="space-y-4">
          <FieldRow label={t("settings.smtp.host")} htmlFor="smtp-host">
            <Input
              id="smtp-host"
              value={form.host}
              placeholder={t("settings.smtp.host-placeholder")}
              onChange={(e) => set("host", e.target.value)}
            />
          </FieldRow>
          <div className="grid grid-cols-2 gap-4">
            <FieldRow label={t("settings.smtp.port")} htmlFor="smtp-port">
              <Input
                id="smtp-port"
                type="number"
                min={1}
                max={65535}
                value={Number.isFinite(form.port) ? form.port : ""}
                onChange={(e) => set("port", Number(e.target.value) || 0)}
              />
            </FieldRow>
            <FieldRow label={t("settings.smtp.from")} htmlFor="smtp-from">
              <Input
                id="smtp-from"
                value={form.from}
                placeholder={t("settings.smtp.from-placeholder")}
                onChange={(e) => set("from", e.target.value)}
              />
            </FieldRow>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <FieldRow
              label={t("settings.smtp.username")}
              htmlFor="smtp-username"
            >
              <Input
                id="smtp-username"
                value={form.username}
                onChange={(e) => set("username", e.target.value)}
                autoComplete="off"
              />
            </FieldRow>
            <FieldRow
              label={t("settings.smtp.password")}
              htmlFor="smtp-password"
              hint={
                isMasked(form.password)
                  ? t("settings.smtp.password-masked")
                  : undefined
              }
            >
              <SecretInput
                id="smtp-password"
                value={form.password}
                placeholder={t("settings.smtp.password-placeholder")}
                onChange={(e) => set("password", e.target.value)}
              />
            </FieldRow>
          </div>
          <div className="flex flex-col gap-3 pt-2">
            <label className="flex items-center gap-2.5 text-sm text-main">
              <Checkbox
                checked={form.useTls}
                onCheckedChange={(v) => set("useTls", v)}
                size="md"
              />
              {t("settings.smtp.use-tls")}
            </label>
          </div>
          <div className="flex justify-end pt-2">
            <Button onClick={handleSave} disabled={saving}>
              {saving ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Save className="size-4" />
              )}
              {t("common.save")}
            </Button>
          </div>
        </div>
      )}
    </SettingsPage>
  );
}
