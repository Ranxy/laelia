import { create } from "@bufbuild/protobuf";
import { Loader2, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PageLoading } from "@/components/settings-page";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { SecretInput } from "@/components/ui/secret-input";
import { settingServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import { SettingValueSchema } from "@/types/proto-es/v1/setting_pb";

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
        const res = await settingServiceClient.getSetting({
          name: "settings/smtp_config",
        });
        if (cancelled) return;
        const v = res.value?.value;
        const cfg = v?.case === "smtpConfig" ? v.value : undefined;
        setForm({
          host: cfg?.host ?? "",
          port: cfg?.port ?? 587,
          username: cfg?.username ?? "",
          password: cfg?.password ?? "",
          from: cfg?.from ?? "",
          useTls: cfg?.useTls ?? true,
        });
      } catch (err) {
        toastManager.add({
          type: "error",
          title: t("settings.smtp.load-failed"),
          description: err instanceof Error ? err.message : String(err),
        });
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
      const res = await settingServiceClient.updateSetting({
        setting: {
          name: "settings/smtp_config",
          value: create(SettingValueSchema, {
            value: {
              case: "smtpConfig" as const,
              value: {
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
            },
          }),
        },
      });
      const v = res.value?.value;
      const cfg = v?.case === "smtpConfig" ? v.value : undefined;
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
      toastManager.add({
        type: "error",
        title: t("settings.smtp.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  }

  const set = <K extends keyof SmtpForm>(key: K, value: SmtpForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <div className="flex h-full overflow-y-auto flex-col">
      <div className="mx-auto w-full max-w-2xl px-4 pb-[calc(var(--mobile-tab-height)+var(--mobile-safe-bottom)+1rem)] pt-4 lg:px-6 lg:py-8">
        <h1 className="hidden text-lg font-semibold text-main lg:block">
          {t("settings.smtp.title")}
        </h1>
        <p className="hidden mt-1 text-sm text-control-light lg:block">
          {t("settings.smtp.description")}
        </p>

        {loading ? (
          <PageLoading />
        ) : (
          <div className="mt-6 space-y-4">
            <Field label={t("settings.smtp.host")}>
              <Input
                value={form.host}
                placeholder={t("settings.smtp.host-placeholder")}
                onChange={(e) => set("host", e.target.value)}
              />
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field label={t("settings.smtp.port")}>
                <Input
                  type="number"
                  min={1}
                  max={65535}
                  value={Number.isFinite(form.port) ? form.port : ""}
                  onChange={(e) => set("port", Number(e.target.value) || 0)}
                />
              </Field>
              <Field label={t("settings.smtp.from")}>
                <Input
                  value={form.from}
                  placeholder={t("settings.smtp.from-placeholder")}
                  onChange={(e) => set("from", e.target.value)}
                />
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Field label={t("settings.smtp.username")}>
                <Input
                  value={form.username}
                  onChange={(e) => set("username", e.target.value)}
                  autoComplete="off"
                />
              </Field>
              <Field
                label={t("settings.smtp.password")}
                hint={
                  isMasked(form.password)
                    ? t("settings.smtp.password-masked")
                    : undefined
                }
              >
                <SecretInput
                  value={form.password}
                  placeholder={t("settings.smtp.password-placeholder")}
                  onChange={(e) => set("password", e.target.value)}
                />
              </Field>
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
      </div>
    </div>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1.5 block text-xs font-medium text-control">
        {label}
      </span>
      {children}
      {hint && (
        <span className="mt-1 block text-xs text-control-placeholder">
          {hint}
        </span>
      )}
    </label>
  );
}
