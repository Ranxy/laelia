import { Loader2, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PageLoading, SettingsPage } from "@/components/settings-page";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import type { WorkspaceProfileSetting } from "@/types/proto-es/store/setting_pb";

interface GeneralForm {
  allowSignup: boolean;
  requireEmailVerification: boolean;
  enforceIdentityDomain: boolean;
  domains: string;
}

const EMPTY: GeneralForm = {
  allowSignup: true,
  requireEmailVerification: true,
  enforceIdentityDomain: false,
  domains: "",
};

// parseDomains splits a newline-separated suffix list, trimming whitespace,
// stripping a leading "@", lowercasing, and dropping empties — mirroring the
// backend normalization.
function parseDomains(raw: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of raw.split("\n")) {
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
  const [saved, setSaved] = useState<GeneralForm>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [savingSignup, setSavingSignup] = useState(false);
  const [savingDomain, setSavingDomain] = useState(false);
  const [savingEmailVerification, setSavingEmailVerification] = useState(false);
  const [savingDomains, setSavingDomains] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const profile = await useAppStore.getState().fetchWorkspaceProfile();
        if (cancelled) return;
        const next = {
          allowSignup: !(profile?.disallowSignup ?? false),
          requireEmailVerification: profile?.requireEmailVerification ?? true,
          enforceIdentityDomain: profile?.enforceIdentityDomain ?? false,
          domains: (profile?.domains ?? []).join("\n"),
        };
        setForm(next);
        setSaved(next);
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

  function applyProfile(profile: WorkspaceProfileSetting | undefined) {
    const next = {
      allowSignup: !(profile?.disallowSignup ?? false),
      requireEmailVerification: profile?.requireEmailVerification ?? true,
      enforceIdentityDomain: profile?.enforceIdentityDomain ?? false,
      domains: (profile?.domains ?? []).join("\n"),
    };
    setForm(next);
    setSaved(next);
  }

  // saveField sends a field-level update: only the mask-listed paths are
  // written server-side, so unrelated fields are never round-tripped.
  async function saveField(
    patch: Partial<WorkspaceProfileSetting>,
    paths: string[]
  ) {
    const profile = await useAppStore
      .getState()
      .updateWorkspaceProfile(patch, paths);
    applyProfile(profile);
  }

  async function handleToggleSignup(v: boolean) {
    const prev = form.allowSignup;
    setForm((f) => ({ ...f, allowSignup: v }));
    setSavingSignup(true);
    try {
      await saveField({ disallowSignup: !v }, [
        "value.workspace_profile.disallow_signup",
      ]);
    } catch (err) {
      setForm((f) => ({ ...f, allowSignup: prev }));
      toastManager.add({
        type: "error",
        title: t("settings.general.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSavingSignup(false);
    }
  }

  async function handleToggleDomain(v: boolean) {
    const prev = form.enforceIdentityDomain;
    setForm((f) => ({ ...f, enforceIdentityDomain: v }));
    setSavingDomain(true);
    try {
      await saveField({ enforceIdentityDomain: v }, [
        "value.workspace_profile.enforce_identity_domain",
      ]);
    } catch (err) {
      setForm((f) => ({ ...f, enforceIdentityDomain: prev }));
      toastManager.add({
        type: "error",
        title: t("settings.general.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSavingDomain(false);
    }
  }

  async function handleToggleEmailVerification(v: boolean) {
    const prev = form.requireEmailVerification;
    setForm((f) => ({ ...f, requireEmailVerification: v }));
    setSavingEmailVerification(true);
    try {
      await saveField({ requireEmailVerification: v }, [
        "value.workspace_profile.require_email_verification",
      ]);
    } catch (err) {
      setForm((f) => ({ ...f, requireEmailVerification: prev }));
      toastManager.add({
        type: "error",
        title: t("settings.general.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSavingEmailVerification(false);
    }
  }

  async function handleSaveDomains() {
    setSavingDomains(true);
    try {
      await saveField({ domains: parseDomains(form.domains) }, [
        "value.workspace_profile.domains",
      ]);
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
      setSavingDomains(false);
    }
  }

  const domainsDirty =
    parseDomains(form.domains).join("\n") !==
    parseDomains(saved.domains).join("\n");

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
              onCheckedChange={handleToggleSignup}
              disabled={savingSignup}
              size="md"
            />
          </div>

          {form.allowSignup && (
            <div className="flex items-center justify-between rounded-lg border border-control-border bg-background px-5 py-4 shadow-xs">
              <div>
                <div className="text-sm font-medium text-main">
                  {t("settings.general.require-email-verification")}
                </div>
                <div className="mt-0.5 text-xs text-control-light">
                  {t("settings.general.require-email-verification-description")}
                </div>
              </div>
              <Switch
                checked={form.requireEmailVerification}
                onCheckedChange={handleToggleEmailVerification}
                disabled={savingEmailVerification}
                size="md"
              />
            </div>
          )}

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
              onCheckedChange={handleToggleDomain}
              disabled={savingDomain}
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
              <Textarea
                id="general-domains"
                value={form.domains}
                placeholder={t("settings.general.domains-placeholder")}
                onChange={(e) => set("domains", e.target.value)}
                spellCheck={false}
                rows={5}
                className="mt-2"
              />
              <div className="mt-1.5 flex items-center justify-between gap-3">
                <p className="text-xs text-control-light">
                  {t("settings.general.domains-hint")}
                </p>
                <Button
                  size="sm"
                  onClick={handleSaveDomains}
                  disabled={savingDomains || !domainsDirty}
                >
                  {savingDomains ? (
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
      )}
    </SettingsPage>
  );
}
