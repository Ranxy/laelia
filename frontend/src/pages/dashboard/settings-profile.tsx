import { create } from "@bufbuild/protobuf";
import {
  Bell,
  Keyboard,
  Languages,
  Loader2,
  Save,
  Trash2,
  Upload,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/chat/avatar";
import { Card } from "@/components/profile-common";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useAvatarEditor } from "@/composables/useAvatarEditor";
import { notificationServiceClient, userServiceClient } from "@/connect";
import { useAvatar } from "@/lib/avatar-cache";
import { resizeImageFile } from "@/lib/image-resize";
import { toastManager } from "@/lib/toast";
import {
  disableDesktopNotifications,
  enableDesktopNotifications,
  isDeviceSubscribed,
  webPushSupported,
} from "@/lib/web-push";
import { useAppStore } from "@/stores";
import {
  ChatPreferencesSchema,
  DeleteAvatarRequestSchema,
  PreferredLanguage,
  UploadAvatarRequestSchema,
} from "@/types/proto-es/v1/user_service_pb";

// ProfileForm mirrors the editable fields of the current user. The server is
// the source of truth; this seeds from `currentUser` and writes back only the
// fields that changed (diff-driven update_mask).
interface ProfileForm {
  title: string;
  email: string;
  phone: string;
  description: string;
}

type NotificationStatus =
  | "loading"
  | "unsupported"
  | "not-configured"
  | "denied"
  | "ready";

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
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Chat preferences. The server returns the default (enter_to_send = true)
  // when the user has never customized the preference, so a missing field is
  // the historic behavior, not "off". PreferredLanguage stays UNSPECIFIED
  // until set.
  const enterToSend = currentUser?.chatPreferences?.enterToSend ?? true;
  const preferredLanguage =
    currentUser?.chatPreferences?.preferredLanguage ??
    PreferredLanguage.UNSPECIFIED;
  const [chatSaving, setChatSaving] = useState(false);

  // Desktop notification state, derived from the server (see lib/web-push).
  const [notifStatus, setNotifStatus] = useState<NotificationStatus>("loading");
  const [notifEnabled, setNotifEnabled] = useState(false);
  const [notifBusy, setNotifBusy] = useState(false);
  // busyRef guards against a rapid double-click landing before the disabled
  // Switch re-renders; the toggle operation is async and must not re-enter.
  const busyRef = useRef(false);

  // The current user's principal id (the {user} segment of "users/{user}"),
  // used both as the pixel-avatar seed and to build the avatar resource name.
  const userId = currentUser?.name
    ? (currentUser.name.split("/")[1] ?? "")
    : "";
  const avatarName =
    currentUser?.avatar || (userId ? `users/${userId}/avatar` : undefined);
  const avatarSrc = useAvatar(avatarName);

  const {
    busy: avatarBusy,
    onChange: handleAvatarChange,
    onRemove: handleAvatarRemove,
  } = useAvatarEditor({
    avatarName: userId ? `users/${userId}/avatar` : null,
    upload: async (file) => {
      const { data, mimeType } = await resizeImageFile(file, 256, 0.9);
      await userServiceClient.uploadAvatar(
        create(UploadAvatarRequestSchema, { data, mimeType })
      );
    },
    remove: (name) =>
      userServiceClient.deleteAvatar(
        create(DeleteAvatarRequestSchema, { name })
      ),
    refetch: fetchCurrentUser,
    messages: {
      uploadSuccess: t("settings.profile.avatar-uploaded"),
      uploadFailure: t("settings.profile.avatar-upload-failed"),
      removeSuccess: t("settings.profile.avatar-removed"),
      removeFailure: t("settings.profile.avatar-remove-failed"),
    },
  });

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

  // Derive the desktop-notification status from the browser + server once.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const supported = webPushSupported();
      if (!supported) {
        if (!cancelled) setNotifStatus("unsupported");
        return;
      }
      let configEnabled = false;
      try {
        const res = await notificationServiceClient.getPushConfig({});
        configEnabled = res.enabled;
      } catch {
        // treat a backend error as "not configured" so the user sees a clear
        // message rather than a half-broken toggle.
      }
      if (cancelled) return;
      if (!configEnabled) {
        setNotifStatus("not-configured");
        return;
      }
      const permission = Notification.permission;
      if (permission === "denied") {
        setNotifStatus("denied");
        return;
      }
      setNotifStatus("ready");
      // The toggle reflects whether this browser's subscription is registered
      // server-side (ListPushSubscriptions), not a local intent flag.
      try {
        const subscribed = await isDeviceSubscribed();
        if (!cancelled) setNotifEnabled(subscribed);
      } catch {
        // list failed; keep the toggle off until the user acts.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

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

  // preferredLanguageLabel renders the localized label for a language value,
  // used by SelectValue (which by default would show the raw numeric value).
  function preferredLanguageLabel(lang: PreferredLanguage) {
    switch (lang) {
      case PreferredLanguage.ZH_CN:
        return t("settings.profile.chat.language.zh-CN");
      case PreferredLanguage.EN_US:
        return t("settings.profile.chat.language.en-US");
      case PreferredLanguage.JA_JP:
        return t("settings.profile.chat.language.ja-JP");
      default:
        return t("settings.profile.chat.language.auto");
    }
  }

  // Both chat preferences are saved as one chat_preferences message so saving
  // one never wipes the other.
  async function savePreferences(prefs: {
    enterToSend: boolean;
    preferredLanguage: PreferredLanguage;
  }) {
    if (!currentUser?.name) return;
    setChatSaving(true);
    try {
      await updateUser(
        currentUser.name,
        { chatPreferences: create(ChatPreferencesSchema, prefs) },
        ["chat_preferences"]
      );
      await fetchCurrentUser();
      toastManager.add({
        type: "success",
        title: t("settings.profile.chat.saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.profile.chat.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setChatSaving(false);
    }
  }

  async function handleEnterToggle(next: boolean) {
    await savePreferences({ enterToSend: next, preferredLanguage });
  }

  async function handleLanguageChange(next: PreferredLanguage) {
    await savePreferences({ enterToSend, preferredLanguage: next });
  }

  async function handleNotifToggle(next: boolean) {
    if (busyRef.current) return;
    busyRef.current = true;
    setNotifBusy(true);
    try {
      if (next) {
        await enableDesktopNotifications();
        setNotifStatus("ready");
      } else {
        await disableDesktopNotifications();
      }
    } catch (err) {
      const code = err instanceof Error ? err.message : String(err);
      if (code === "denied") {
        setNotifStatus("denied");
        toastManager.add({
          type: "warning",
          title: t("settings.profile.notifications.permission-denied"),
        });
      } else if (code === "not-configured") {
        setNotifStatus("not-configured");
      } else if (code === "unsupported") {
        setNotifStatus("unsupported");
      } else {
        toastManager.add({
          type: "error",
          title: t(
            next
              ? "settings.profile.notifications.enable-failed"
              : "settings.profile.notifications.disable-failed"
          ),
          description: code,
        });
      }
      return;
    } finally {
      busyRef.current = false;
      setNotifBusy(false);
    }
    // Re-derive the toggle from the server after the subscription changed; a
    // failure here just leaves the previous value.
    try {
      setNotifEnabled(await isDeviceSubscribed());
    } catch {
      // ignore
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

        <div className="mt-6 flex flex-col gap-6">
          <Card
            title={t("settings.profile.section-identity")}
            footer={
              <div className="flex justify-end">
                <Button onClick={handleSave} disabled={saving}>
                  {saving ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Save className="size-4" />
                  )}
                  {t("common.save")}
                </Button>
              </div>
            }
          >
            <div className="flex items-center gap-4">
              <Avatar seed={userId} src={avatarSrc} />
              <div className="min-w-0 flex-1">
                <div className="text-xs font-medium text-control">
                  {t("settings.profile.avatar")}
                </div>
                <p className="mt-0.5 text-xs text-control-placeholder">
                  {t("settings.profile.avatar-hint")}
                </p>
                <div className="mt-2 flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => fileInputRef.current?.click()}
                    disabled={avatarBusy}
                  >
                    {avatarBusy ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <Upload className="size-3.5" />
                    )}
                    {avatarBusy
                      ? t("settings.profile.avatar-uploading")
                      : t("settings.profile.avatar-upload")}
                  </Button>
                  {currentUser.avatar && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleAvatarRemove}
                      disabled={avatarBusy}
                    >
                      <Trash2 className="size-3.5" />
                      {t("settings.profile.avatar-remove")}
                    </Button>
                  )}
                  <input
                    ref={fileInputRef}
                    type="file"
                    accept="image/png,image/jpeg,image/webp,image/gif"
                    className="hidden"
                    onChange={(e) => {
                      void handleAvatarChange(e.target.files?.[0]);
                      e.target.value = "";
                    }}
                  />
                </div>
              </div>
            </div>

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
                placeholder={t(
                  "settings.profile.field-description-placeholder"
                )}
                value={form.description}
                onChange={(e) => set("description", e.target.value)}
              />
            </Field>
          </Card>

          <Card title={t("settings.profile.section-chat")}>
            <div className="flex items-center justify-between rounded-md border border-control-border p-4">
              <div className="flex min-w-0 flex-1 items-start gap-3">
                <Keyboard className="mt-0.5 size-4 shrink-0 text-control-light" />
                <div className="min-w-0">
                  <div className="text-sm font-medium text-main">
                    {t("settings.profile.chat.enter-to-send")}
                  </div>
                  <div className="mt-0.5 text-xs text-control-light">
                    {t("settings.profile.chat.enter-to-send-hint")}
                  </div>
                </div>
              </div>
              <Switch
                checked={enterToSend}
                onCheckedChange={handleEnterToggle}
                disabled={chatSaving}
                size="md"
                className="shrink-0"
              />
            </div>

            <div className="flex items-center justify-between rounded-md border border-control-border p-4">
              <div className="flex min-w-0 flex-1 items-start gap-3">
                <Languages className="mt-0.5 size-4 shrink-0 text-control-light" />
                <div className="min-w-0">
                  <div className="text-sm font-medium text-main">
                    {t("settings.profile.chat.preferred-language")}
                  </div>
                  <div className="mt-0.5 text-xs text-control-light">
                    {t("settings.profile.chat.preferred-language-hint")}
                  </div>
                </div>
              </div>
              <Select
                value={String(preferredLanguage)}
                onValueChange={(v) =>
                  void handleLanguageChange(Number(v) as PreferredLanguage)
                }
              >
                <SelectTrigger className="shrink-0">
                  <SelectValue>
                    {(value) =>
                      preferredLanguageLabel(Number(value) as PreferredLanguage)
                    }
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={String(PreferredLanguage.UNSPECIFIED)}>
                    {t("settings.profile.chat.language.auto")}
                  </SelectItem>
                  <SelectItem value={String(PreferredLanguage.ZH_CN)}>
                    {t("settings.profile.chat.language.zh-CN")}
                  </SelectItem>
                  <SelectItem value={String(PreferredLanguage.EN_US)}>
                    {t("settings.profile.chat.language.en-US")}
                  </SelectItem>
                  <SelectItem value={String(PreferredLanguage.JA_JP)}>
                    {t("settings.profile.chat.language.ja-JP")}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          </Card>

          <Card title={t("settings.profile.section-notifications")}>
            {notifStatus === "loading" ? (
              <div className="flex items-center gap-2 text-sm text-control-light">
                <Loader2 className="size-4 animate-spin" />
                {t("settings.profile.notifications.loading")}
              </div>
            ) : (
              <>
                {notifStatus === "unsupported" && (
                  <Notice>
                    {t("settings.profile.notifications.unsupported")}
                  </Notice>
                )}
                {notifStatus === "not-configured" && (
                  <Notice>
                    {t("settings.profile.notifications.not-configured")}
                  </Notice>
                )}
                {notifStatus === "denied" && (
                  <Notice>
                    {t("settings.profile.notifications.permission-denied")}
                  </Notice>
                )}

                <div className="flex items-center justify-between rounded-md border border-control-border p-4">
                  <div className="flex items-start gap-3">
                    <Bell className="mt-0.5 size-4 text-control-light" />
                    <div>
                      <div className="text-sm font-medium text-main">
                        {t("settings.profile.notifications.enable")}
                      </div>
                      <div className="mt-0.5 text-xs text-control-light">
                        {notifBusy ? (
                          <span className="inline-flex items-center gap-1.5">
                            <Loader2 className="size-3 animate-spin" />
                            {t("settings.profile.notifications.updating")}
                          </span>
                        ) : notifEnabled ? (
                          t("settings.profile.notifications.enabled")
                        ) : (
                          t("settings.profile.notifications.disabled")
                        )}
                      </div>
                    </div>
                  </div>
                  <Switch
                    checked={notifEnabled}
                    onCheckedChange={handleNotifToggle}
                    disabled={notifBusy || notifStatus !== "ready"}
                    size="md"
                  />
                </div>

                {notifStatus === "ready" && !notifEnabled && (
                  <p className="text-xs text-control-placeholder">
                    {t("settings.profile.notifications.permission-prompt")}
                  </p>
                )}
              </>
            )}
          </Card>
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

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-md border border-control-border bg-control-bg p-3 text-xs text-control">
      {children}
    </p>
  );
}
