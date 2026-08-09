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

interface S3Form {
  endpoint: string;
  region: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  forcePathStyle: boolean;
  useSsl: boolean;
}

const EMPTY: S3Form = {
  endpoint: "",
  region: "",
  bucket: "",
  accessKey: "",
  secretKey: "",
  forcePathStyle: false,
  useSsl: true,
};

// isMasked reports whether a secret value is the server-returned mask
// ("****…" or empty). The backend treats a masked secret as "unchanged".
function isMasked(secret: string): boolean {
  return secret === "" || secret.startsWith("****");
}

export function SettingsStoragePage() {
  const { t } = useTranslation();
  const [form, setForm] = useState<S3Form>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await settingServiceClient.getS3Config({});
        if (cancelled) return;
        const cfg = res.config;
        setForm({
          endpoint: cfg?.endpoint ?? "",
          region: cfg?.region ?? "",
          bucket: cfg?.bucket ?? "",
          accessKey: cfg?.accessKey ?? "",
          secretKey: cfg?.secretKey ?? "",
          forcePathStyle: cfg?.forcePathStyle ?? false,
          useSsl: cfg?.useSsl ?? true,
        });
      } catch (err) {
        toastManager.add({
          type: "error",
          title: t("settings.s3.load-failed"),
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
      const res = await settingServiceClient.updateS3Config({
        config: {
          endpoint: form.endpoint.trim(),
          region: form.region.trim(),
          bucket: form.bucket.trim(),
          accessKey: form.accessKey,
          // Send the masked value back when the user didn't edit the secret;
          // the backend interprets a "****" prefix as "leave unchanged".
          secretKey: form.secretKey,
          forcePathStyle: form.forcePathStyle,
          useSsl: form.useSsl,
        },
      });
      const cfg = res.config;
      setForm({
        endpoint: cfg?.endpoint ?? "",
        region: cfg?.region ?? "",
        bucket: cfg?.bucket ?? "",
        accessKey: cfg?.accessKey ?? "",
        secretKey: cfg?.secretKey ?? "",
        forcePathStyle: cfg?.forcePathStyle ?? false,
        useSsl: cfg?.useSsl ?? true,
      });
      toastManager.add({
        type: "success",
        title: t("settings.s3.saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.s3.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  }

  const set = <K extends keyof S3Form>(key: K, value: S3Form[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <div className="flex h-full overflow-y-auto flex-col">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-lg font-semibold text-main">
          {t("settings.s3.title")}
        </h1>
        <p className="mt-1 text-sm text-control-light">
          {t("settings.s3.description")}
        </p>

        {loading ? (
          <PageLoading />
        ) : (
          <div className="mt-6 space-y-4">
            <Field label={t("settings.s3.endpoint")}>
              <Input
                value={form.endpoint}
                placeholder={t("settings.s3.endpoint-placeholder")}
                onChange={(e) => set("endpoint", e.target.value)}
              />
            </Field>
            <div className="grid grid-cols-2 gap-4">
              <Field label={t("settings.s3.region")}>
                <Input
                  value={form.region}
                  placeholder={t("settings.s3.region-placeholder")}
                  onChange={(e) => set("region", e.target.value)}
                />
              </Field>
              <Field label={t("settings.s3.bucket")}>
                <Input
                  value={form.bucket}
                  onChange={(e) => set("bucket", e.target.value)}
                />
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Field label={t("settings.s3.access-key")}>
                <Input
                  value={form.accessKey}
                  onChange={(e) => set("accessKey", e.target.value)}
                  autoComplete="off"
                />
              </Field>
              <Field
                label={t("settings.s3.secret-key")}
                hint={
                  isMasked(form.secretKey)
                    ? t("settings.s3.secret-masked")
                    : undefined
                }
              >
                <SecretInput
                  value={form.secretKey}
                  placeholder={t("settings.s3.secret-placeholder")}
                  onChange={(e) => set("secretKey", e.target.value)}
                />
              </Field>
            </div>
            <div className="flex flex-col gap-3 pt-2">
              <label className="flex items-center gap-2.5 text-sm text-main">
                <Checkbox
                  checked={form.forcePathStyle}
                  onCheckedChange={(v) => set("forcePathStyle", v)}
                  size="md"
                />
                {t("settings.s3.force-path-style")}
              </label>
              <label className="flex items-center gap-2.5 text-sm text-main">
                <Checkbox
                  checked={form.useSsl}
                  onCheckedChange={(v) => set("useSsl", v)}
                  size="md"
                />
                {t("settings.s3.use-ssl")}
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
