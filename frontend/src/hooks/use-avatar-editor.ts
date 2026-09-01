import { useCallback, useState } from "react";
import { invalidateAvatar } from "@/lib/avatar-cache";
import { toastManager } from "@/lib/toast";

// useAvatarEditor wires the avatar upload/remove flow shared by the profile
// pages (own profile, human detail, agent profile): busy flag → upload/delete
// → invalidate the avatar cache → refresh the caller's view → success/error
// toast. The resize + RPC step is passed in because user and agent avatars go
// through different services; everything around it is identical.
export function useAvatarEditor({
  upload,
  remove,
  avatarName,
  refetch,
  messages,
}: {
  upload: (file: File) => Promise<unknown>;
  remove: (avatarName: string) => Promise<unknown>;
  // Resource name to invalidate after a change (e.g. "users/1/avatar").
  // Null when the subject has no avatar yet — uploading still works, removal
  // is disabled.
  avatarName: string | null;
  refetch: () => Promise<unknown>;
  messages: {
    uploadSuccess: string;
    uploadFailure: string;
    removeSuccess: string;
    removeFailure: string;
  };
}) {
  const [busy, setBusy] = useState(false);

  const onChange = useCallback(
    async (file: File | undefined) => {
      if (!file) return;
      setBusy(true);
      try {
        await upload(file);
        if (avatarName) invalidateAvatar(avatarName);
        await refetch();
        toastManager.add({ type: "success", title: messages.uploadSuccess });
      } catch (err) {
        toastManager.add({
          type: "error",
          title: messages.uploadFailure,
          description: err instanceof Error ? err.message : String(err),
        });
      } finally {
        setBusy(false);
      }
    },
    [
      upload,
      avatarName,
      refetch,
      messages.uploadSuccess,
      messages.uploadFailure,
    ]
  );

  const onRemove = useCallback(async () => {
    if (!avatarName) return;
    setBusy(true);
    try {
      await remove(avatarName);
      invalidateAvatar(avatarName);
      await refetch();
      toastManager.add({ type: "success", title: messages.removeSuccess });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: messages.removeFailure,
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setBusy(false);
    }
  }, [
    avatarName,
    remove,
    refetch,
    messages.removeSuccess,
    messages.removeFailure,
  ]);

  return { busy, onChange, onRemove };
}
