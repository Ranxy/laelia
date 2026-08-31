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
import { s3ConfigPaths } from "@/stores/setting";

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
        const cfg = await useAppStore.getState().fetchS3Config();
        if (cancelled) return;
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
        void showErrorToast(err, t("settings.s3.load-failed"));
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
      const cfg = await useAppStore.getState().updateS3Config(
        {
          endpoint: form.endpoint.trim(),
          region: form.region.trim(),
          bucket: form.bucket.trim(),
          accessKey: form.accessKey,
          // Send the masked value back when the user didn't edit the
          // secret; the backend interprets a "****" prefix as "leave
          // unchanged".
          secretKey: form.secretKey,
          forcePathStyle: form.forcePathStyle,
          useSsl: form.useSsl,
        },
        [...s3ConfigPaths]
      );
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
      void showErrorToast(err, t("settings.s3.save-failed"));
    } finally {
      setSaving(false);
    }
  }

  const set = <K extends keyof S3Form>(key: K, value: S3Form[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <SettingsPage
      title={t("settings.s3.title")}
      description={t("settings.s3.description")}
      contentWidth="mx-auto w-full max-w-2xl"
    >
      {loading ? (
        <PageLoading />
      ) : (
        <div className="space-y-4">
          <FieldRow label={t("settings.s3.endpoint")} htmlFor="s3-endpoint">
            <Input
              id="s3-endpoint"
              value={form.endpoint}
              placeholder={t("settings.s3.endpoint-placeholder")}
              onChange={(e) => set("endpoint", e.target.value)}
            />
          </FieldRow>
          <div className="grid grid-cols-2 gap-4">
            <FieldRow label={t("settings.s3.region")} htmlFor="s3-region">
              <Input
                id="s3-region"
                value={form.region}
                placeholder={t("settings.s3.region-placeholder")}
                onChange={(e) => set("region", e.target.value)}
              />
            </FieldRow>
            <FieldRow label={t("settings.s3.bucket")} htmlFor="s3-bucket">
              <Input
                id="s3-bucket"
                value={form.bucket}
                onChange={(e) => set("bucket", e.target.value)}
              />
            </FieldRow>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <FieldRow
              label={t("settings.s3.access-key")}
              htmlFor="s3-access-key"
            >
              <Input
                id="s3-access-key"
                value={form.accessKey}
                onChange={(e) => set("accessKey", e.target.value)}
                autoComplete="off"
              />
            </FieldRow>
            <FieldRow
              label={t("settings.s3.secret-key")}
              htmlFor="s3-secret-key"
              hint={
                isMasked(form.secretKey)
                  ? t("settings.s3.secret-masked")
                  : undefined
              }
            >
              <SecretInput
                id="s3-secret-key"
                value={form.secretKey}
                placeholder={t("settings.s3.secret-placeholder")}
                onChange={(e) => set("secretKey", e.target.value)}
              />
            </FieldRow>
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
    </SettingsPage>
  );
}
