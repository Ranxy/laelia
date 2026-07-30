import { create } from "@bufbuild/protobuf";
import { Keyboard, Loader2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { ChatPreferencesSchema } from "@/types/proto-es/v1/user_service_pb";

// SettingsChatPage exposes per-user chat composer preferences. The single
// toggle today inverts the Enter / Shift+Enter keybinding. The value lives on
// the current user (server-stored), so saving calls `updateUser` with the
// `chat_preferences` mask path and then `fetchCurrentUser()` to refresh the
// cached identity — both composers read the preference reactively from the
// store, so the change takes effect immediately without a reload.
export function SettingsChatPage() {
  const { t } = useTranslation();
  const currentUser = useAppStore((s) => s.currentUser);
  const updateUser = useAppStore((s) => s.updateUser);
  const fetchCurrentUser = useAppStore((s) => s.fetchCurrentUser);

  // The server returns the default (enter_to_send = true) when the user has
  // never customized the preference, so a missing field is the historic
  // behavior, not "off".
  const enterToSend = currentUser?.chatPreferences?.enterToSend ?? true;
  const [saving, setSaving] = useState(false);

  async function handleToggle(next: boolean) {
    if (!currentUser?.name) return;
    setSaving(true);
    try {
      await updateUser(
        currentUser.name,
        {
          chatPreferences: create(ChatPreferencesSchema, { enterToSend: next }),
        },
        ["chat_preferences"]
      );
      await fetchCurrentUser();
      toastManager.add({ type: "success", title: t("settings.chat.saved") });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.chat.save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSaving(false);
    }
  }

  if (!currentUser) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-control-light">
        <Loader2 className="mr-2 size-4 animate-spin" />
        {t("common.loading")}
      </div>
    );
  }

  return (
    <div className="flex h-full overflow-y-auto flex-col">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-lg font-semibold text-main">
          {t("settings.chat.title")}
        </h1>
        <p className="mt-1 text-sm text-control-light">
          {t("settings.chat.description")}
        </p>

        <div className="mt-6 space-y-4">
          <div className="flex items-center justify-between rounded-md border border-control-border p-4">
            <div className="flex min-w-0 flex-1 items-start gap-3">
              <Keyboard className="mt-0.5 size-4 shrink-0 text-control-light" />
              <div className="min-w-0">
                <div className="text-sm font-medium text-main">
                  {t("settings.chat.enter-to-send")}
                </div>
                <div className="mt-0.5 text-xs text-control-light">
                  {t("settings.chat.enter-to-send-hint")}
                </div>
              </div>
            </div>
            <Switch
              checked={enterToSend}
              onCheckedChange={handleToggle}
              disabled={saving}
              size="md"
              className="shrink-0"
            />
          </div>
        </div>
      </div>
    </div>
  );
}
