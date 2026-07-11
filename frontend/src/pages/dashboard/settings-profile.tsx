import { Loader2, Save } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";

// ProfileForm mirrors the editable fields of the current user. The server is
// the source of truth; this seeds from `currentUser` and writes back only the
// fields that changed (diff-driven update_mask).
interface ProfileForm {
  title: string;
  email: string;
  phone: string;
  description: string;
}

export function SettingsProfilePage() {
  const { t } = useTranslation();
  const currentUser = useAppStore((s) => s.currentUser);
  const fetchCurrentUser = useAppStore((s) => s.fetchCurrentUser);
  const updateUser = useAppStore((s) => s.updateUser);
  const [form, setForm] = useState<ProfileForm>({
    title: "",
    email: "",
    phone: "",
    description: "",
  });
  const [saving, setSaving] = useState(false);

  // Seed from currentUser once it is available. Re-seeding on currentUser
  // change (e.g. after a save-driven refetch) keeps the form in sync without
  // clobbering in-progress edits, because the only currentUser change during
  // this page's life is our own save.
  useEffect(() => {
    if (!currentUser) return;
    setForm({
      title: currentUser.title,
      email: currentUser.email,
      phone: currentUser.phone,
      description: currentUser.description,
    });
  }, [currentUser]);

  if (!currentUser) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-control-light">
        <Loader2 className="mr-2 size-4 animate-spin" />
        {t("common.loading")}
      </div>
    );
  }

  async function handleSave() {
    if (!currentUser?.name) return;
    setSaving(true);
    try {
      const maskPaths: string[] = [];
      const fields: {
        title?: string;
        email?: string;
        phone?: string;
        description?: string;
      } = {};
      if (form.title !== currentUser.title) {
        maskPaths.push("title");
        fields.title = form.title;
      }
      if (form.email !== currentUser.email) {
        maskPaths.push("email");
        fields.email = form.email.trim();
      }
      if (form.phone !== currentUser.phone) {
        maskPaths.push("phone");
        fields.phone = form.phone;
      }
      if (form.description !== currentUser.description) {
        maskPaths.push("description");
        fields.description = form.description;
      }
      if (maskPaths.length === 0) {
        return;
      }
      await updateUser(currentUser.name, fields, maskPaths);
      // Refresh the cached current user so the user menu and rosters reflect
      // the new description without a full reload.
      await fetchCurrentUser();
      toastManager.add({ type: "success", title: t("settings.profile.saved") });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.profile.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  }

  const set = <K extends keyof ProfileForm>(key: K, value: ProfileForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  return (
    <div className="flex h-full overflow-y-auto flex-col">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-lg font-semibold text-main">
          {t("settings.profile.title")}
        </h1>
        <p className="mt-1 text-sm text-control-light">
          {t("settings.profile.description")}
        </p>

        <div className="mt-6 space-y-4">
          <Field label={t("user.field-title")}>
            <Input
              value={form.title}
              placeholder={t("user.field-title-placeholder")}
              onChange={(e) => set("title", e.target.value)}
            />
          </Field>
          <Field label={t("user.field-email")}>
            <Input
              value={form.email}
              placeholder={t("user.field-email-placeholder")}
              onChange={(e) => set("email", e.target.value)}
            />
          </Field>
          <Field label={t("user.field-phone")}>
            <Input
              value={form.phone}
              placeholder={t("user.field-phone-placeholder")}
              onChange={(e) => set("phone", e.target.value)}
            />
          </Field>
          <Field
            label={t("settings.profile.field-description")}
            hint={t("settings.profile.field-description-hint")}
          >
            <Textarea
              className="min-h-[100px]"
              placeholder={t("settings.profile.field-description-placeholder")}
              value={form.description}
              onChange={(e) => set("description", e.target.value)}
            />
          </Field>
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
