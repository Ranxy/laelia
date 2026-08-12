import { create } from "@bufbuild/protobuf";
import { Loader2, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PageLoading, SettingsPage } from "@/components/settings-page";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { settingServiceClient } from "@/connect";
import { toastManager } from "@/lib/toast";
import { SettingValueSchema } from "@/types/proto-es/v1/setting_pb";

interface GeneralForm {
  allowSignup: boolean;
  enforceIdentityDomain: boolean;
  domains: string;
}

const EMPTY: GeneralForm = {
  allowSignup: true,
  enforceIdentityDomain: false,
  domains: "",
};

// parseDomains splits a comma-separated suffix list, trimming whitespace,
// stripping a leading "@", lowercasing, and dropping empties — mirroring the
// backend normalization.
function parseDomains(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split(",")) {
    const d = part.trim().replace(/^@/, "").toLowerCase();
    if (d === "" || seen.has(d)) continue;
    seen.add(d);
    out.push(d);
  }
  return out;
}

export function SettingsGeneralPage() {
  const { t } = useTranslation();
  const [form, setForm] = useState<GeneralForm>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await settingServiceClient.getSetting({
          name: "settings/workspace_profile",
        });
        if (cancelled) return;
        const v = res.value?.value;
        const s = v?.case === "workspaceProfile" ? v.value : undefined;
        setForm({
          allowSignup: !(s?.disallowSignup ?? false),
          enforceIdentityDomain: s?.enforceIdentityDomain ?? false,
          domains: (s?.domains ?? []).join(", "),
        });
      } catch (err) {
        toastManager.add({
          type: "error",
          title: t("settings.general.load-failed"),
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
          name: "settings/workspace_profile",
          value: create(SettingValueSchema, {
            value: {
              case: "workspaceProfile" as const,
              value: {
                disallowSignup: !form.allowSignup,
                enforceIdentityDomain: form.enforceIdentityDomain,
                domains: parseDomains(form.domains),
              },
            },
          }),
        },
      });
      const v = res.value?.value;
      const s = v?.case === "workspaceProfile" ? v.value : undefined;
      setForm({
        allowSignup: !(s?.disallowSignup ?? false),
        enforceIdentityDomain: s?.enforceIdentityDomain ?? false,
        domains: (s?.domains ?? []).join(", "),
      });
      toastManager.add({
        type: "success",
        title: t("settings.general.saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.general.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  }

  const set = <K extends keyof GeneralForm>(key: K, value: GeneralForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <SettingsPage
      title={t("settings.general.title")}
      description={t("settings.general.description")}
    >
      {loading ? (
        <PageLoading />
      ) : (
        <div className="mx-auto max-w-2xl space-y-4">
          <div className="flex items-center justify-between rounded-lg border border-control-border bg-background px-5 py-4 shadow-xs">
            <div>
              <div className="text-sm font-medium text-main">
                {t("settings.general.allow-signup")}
              </div>
              <div className="mt-0.5 text-xs text-control-light">
                {t("settings.general.allow-signup-description")}
              </div>
            </div>
            <Switch
              checked={form.allowSignup}
              onCheckedChange={(v) => set("allowSignup", v)}
              size="md"
            />
          </div>

          <div className="flex items-center justify-between rounded-lg border border-control-border bg-background px-5 py-4 shadow-xs">
            <div>
              <div className="text-sm font-medium text-main">
                {t("settings.general.enforce-domain")}
              </div>
              <div className="mt-0.5 text-xs text-control-light">
                {t("settings.general.enforce-domain-description")}
              </div>
            </div>
            <Switch
              checked={form.enforceIdentityDomain}
              onCheckedChange={(v) => set("enforceIdentityDomain", v)}
              size="md"
            />
          </div>

          {form.enforceIdentityDomain && (
            <div className="rounded-lg border border-control-border bg-background p-5 shadow-xs">
              <label
                htmlFor="general-domains"
                className="block text-sm font-medium text-main"
              >
                {t("settings.general.domains")}
              </label>
              <Input
                id="general-domains"
                value={form.domains}
                placeholder={t("settings.general.domains-placeholder")}
                onChange={(e) => set("domains", e.target.value)}
                spellCheck={false}
                className="mt-2"
              />
              <p className="mt-1.5 text-xs text-control-light">
                {t("settings.general.domains-hint")}
              </p>
            </div>
          )}

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
