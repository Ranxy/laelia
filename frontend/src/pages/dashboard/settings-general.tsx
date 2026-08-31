import { Loader2, Save } from "lucide-react";
import { type Dispatch, type SetStateAction, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PageLoading, SettingsPage } from "@/components/settings-page";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
import { useAppStore } from "@/stores";
import type { WorkspaceProfileSetting } from "@/types/proto-es/store/setting_pb";

interface GeneralForm {
  externalUrl: string;
  allowSignup: boolean;
  requireEmailVerification: boolean;
  enforceIdentityDomain: boolean;
  domains: string;
  allowUserCreateMachine: boolean;
}

const EMPTY: GeneralForm = {
  externalUrl: "",
  allowSignup: true,
  requireEmailVerification: false,
  enforceIdentityDomain: false,
  domains: "",
  allowUserCreateMachine: true,
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

// ToggleField is a boolean GeneralForm field driven by one of the four
// workspace toggles.
type ToggleField =
  | "allowSignup"
  | "requireEmailVerification"
  | "enforceIdentityDomain"
  | "allowUserCreateMachine";

// useSettingToggle backs one boolean workspace toggle: it flips form[key]
// optimistically, tracks that key's saving flag, and on failure reverts the
// form field and shows the shared save-failed toast. patchFn maps the
// requested value onto the wire patch — the disallow* fields invert
// (disallowSignup = !v) while the others pass the value through — and commit
// is the page's field-level save (saveField → settingServiceClient
// .updateSetting with the toggle's mask path).
function useSettingToggle(
  key: ToggleField,
  paths: string[],
  patchFn: (v: boolean) => Partial<WorkspaceProfileSetting>,
  form: GeneralForm,
  setForm: Dispatch<SetStateAction<GeneralForm>>,
  commit: (
    patch: Partial<WorkspaceProfileSetting>,
    paths: string[]
  ) => Promise<void>
) {
  const { t } = useTranslation();
  const [saving, setSaving] = useState(false);

  async function handleToggle(v: boolean) {
    const prev = form[key];
    setForm((f) => ({ ...f, [key]: v }));
    setSaving(true);
    try {
      await commit(patchFn(v), [...paths]);
    } catch (err) {
      setForm((f) => ({ ...f, [key]: prev }));
      void showErrorToast(err, t("settings.general.save-failed"));
    } finally {
      setSaving(false);
    }
  }

  return { saving, handleToggle };
}

export function SettingsGeneralPage() {
  const { t } = useTranslation();
  const [form, setForm] = useState<GeneralForm>(EMPTY);
  const [saved, setSaved] = useState<GeneralForm>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [savingDomains, setSavingDomains] = useState(false);
  const [savingExternalUrl, setSavingExternalUrl] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const profile = await useAppStore.getState().fetchWorkspaceProfile();
        if (cancelled) return;
        const next = {
          externalUrl: profile?.externalUrl ?? "",
          allowSignup: !(profile?.disallowSignup ?? false),
          requireEmailVerification: profile?.requireEmailVerification ?? false,
          enforceIdentityDomain: profile?.enforceIdentityDomain ?? false,
          domains: (profile?.domains ?? []).join("\n"),
          allowUserCreateMachine: !(
            profile?.disallowUserCreateMachine ?? false
          ),
        };
        setForm(next);
        setSaved(next);
      } catch (err) {
        void showErrorToast(err, t("settings.general.load-failed"));
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
      externalUrl: profile?.externalUrl ?? "",
      allowSignup: !(profile?.disallowSignup ?? false),
      requireEmailVerification: profile?.requireEmailVerification ?? false,
      enforceIdentityDomain: profile?.enforceIdentityDomain ?? false,
      domains: (profile?.domains ?? []).join("\n"),
      allowUserCreateMachine: !(profile?.disallowUserCreateMachine ?? false),
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

  // The four boolean workspace toggles share one handler engine: each call
  // gets its own saving flag while the optimistic flip/revert stays identical.
  const { saving: savingSignup, handleToggle: handleToggleSignup } =
    useSettingToggle(
      "allowSignup",
      ["value.workspace_profile.disallow_signup"],
      (v) => ({ disallowSignup: !v }),
      form,
      setForm,
      saveField
    );
  const {
    saving: savingEmailVerification,
    handleToggle: handleToggleEmailVerification,
  } = useSettingToggle(
    "requireEmailVerification",
    ["value.workspace_profile.require_email_verification"],
    (v) => ({ requireEmailVerification: v }),
    form,
    setForm,
    saveField
  );
  const {
    saving: savingUserCreateMachine,
    handleToggle: handleToggleUserCreateMachine,
  } = useSettingToggle(
    "allowUserCreateMachine",
    ["value.workspace_profile.disallow_user_create_machine"],
    (v) => ({ disallowUserCreateMachine: !v }),
    form,
    setForm,
    saveField
  );
  const { saving: savingDomain, handleToggle: handleToggleDomain } =
    useSettingToggle(
      "enforceIdentityDomain",
      ["value.workspace_profile.enforce_identity_domain"],
      (v) => ({ enforceIdentityDomain: v }),
      form,
      setForm,
      saveField
    );

  async function handleSaveExternalUrl() {
    setSavingExternalUrl(true);
    try {
      await saveField({ externalUrl: form.externalUrl.trim() }, [
        "value.workspace_profile.external_url",
      ]);
      toastManager.add({
        type: "success",
        title: t("settings.general.saved"),
      });
    } catch (err) {
      void showErrorToast(err, t("settings.general.save-failed"));
    } finally {
      setSavingExternalUrl(false);
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
      void showErrorToast(err, t("settings.general.save-failed"));
    } finally {
      setSavingDomains(false);
    }
  }

  const externalUrlDirty = form.externalUrl.trim() !== saved.externalUrl.trim();
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
          <div className="rounded-lg border border-control-border bg-background p-5 shadow-xs">
            <label
              htmlFor="general-external-url"
              className="block text-sm font-medium text-main"
            >
              {t("settings.general.external-url")}
            </label>
            <Input
              id="general-external-url"
              value={form.externalUrl}
              placeholder={t("settings.general.external-url-placeholder")}
              onChange={(e) => set("externalUrl", e.target.value)}
              spellCheck={false}
              className="mt-2"
            />
            <div className="mt-1.5 flex items-center justify-between gap-3">
              <p className="text-xs text-control-light">
                {t("settings.general.external-url-hint")}
              </p>
              <Button
                size="sm"
                onClick={handleSaveExternalUrl}
                disabled={savingExternalUrl || !externalUrlDirty}
              >
                {savingExternalUrl ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <Save className="size-4" />
                )}
                {t("common.save")}
              </Button>
            </div>
          </div>

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
                {t("settings.general.allow-user-create-machine")}
              </div>
              <div className="mt-0.5 text-xs text-control-light">
                {t("settings.general.allow-user-create-machine-description")}
              </div>
            </div>
            <Switch
              checked={form.allowUserCreateMachine}
              onCheckedChange={handleToggleUserCreateMachine}
              disabled={savingUserCreateMachine}
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
