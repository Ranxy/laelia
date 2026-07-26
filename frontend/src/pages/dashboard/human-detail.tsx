import { create } from "@bufbuild/protobuf";
import { Check, Loader2, Pencil, Trash2, Upload, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { Avatar } from "@/components/chat/avatar";
import { ConnectionBadge } from "@/components/connection-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  iamServiceClient,
  roleServiceClient,
  userServiceClient,
} from "@/connect";
import { invalidateAvatar, useAvatar } from "@/lib/avatar-cache";
import { resizeImageFile } from "@/lib/image-resize";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/auth";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import {
  DeleteAvatarRequestSchema,
  UploadAvatarRequestSchema,
} from "@/types/proto-es/v1/user_service_pb";

// HumanDetailPage is the right-pane profile for a human member, opened from
// the Members directory. It reuses the existing design system and data paths:
// the User comes from the drained `users` roster (no per-page GetUser), the
// avatar from the shared avatar cache, description edits go through the
// `updateUser` store mutation, and role badges come from the workspace IAM
// policy + role list (same calls as settings-iam). The "Created Agents" list
// filters the agent roster by `created_by`, which is now surfaced on
// AgentSummary so this is an O(n) client-side filter, not an N+1 of GetAgent.
export function HumanDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { userId } = useParams<{ userId: string }>();
  const users = useAppStore((s) => s.users);
  const fetchUsers = useAppStore((s) => s.fetchUsers);
  const currentUser = useAppStore((s) => s.currentUser);
  const agents = useAppStore((s) => s.agents);
  const updateUser = useAppStore((s) => s.updateUser);
  const fetchCurrentUser = useAppStore((s) => s.fetchCurrentUser);
  const canUpdateUsers = useHasPermission("laelia.users.update");
  const canGetPolicy = useHasPermission("laelia.iam.getPolicy");

  const user = useMemo(
    () => users.find((u) => u.name === `users/${userId ?? ""}`) ?? null,
    [users, userId]
  );

  // Ensure the roster is loaded on a deep link (MembersPage also loads it,
  // but a direct /members/users/:id entry still mounts the parent, so this is
  // mostly a safety net for empty rosters).
  useEffect(() => {
    if (users.length === 0) void fetchUsers({ pageSize: 100 });
  }, [users.length, fetchUsers]);

  const isSelf =
    !!currentUser?.name && currentUser.name === `users/${userId ?? ""}`;
  const canEditDescription = isSelf || canUpdateUsers;

  // Avatar: prefer the recorded resource name, fall back to the synthesized
  // name so useAvatar probes the cache for users without an uploaded image.
  const avatarName =
    user?.avatar || (userId ? `users/${userId}/avatar` : undefined);
  const avatarSrc = useAvatar(avatarName);

  // Description inline editor.
  const [editingDescription, setEditingDescription] = useState(false);
  const [descriptionDraft, setDescriptionDraft] = useState("");
  const [savingDescription, setSavingDescription] = useState(false);

  // Avatar upload (self only).
  const [avatarBusy, setAvatarBusy] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Role badges from the workspace IAM policy. Fetched only when the caller
  // may read the policy; otherwise the Role row is hidden.
  const [roleTitles, setRoleTitles] = useState<string[]>([]);
  useEffect(() => {
    if (!canGetPolicy || !user) return;
    let active = true;
    (async () => {
      try {
        const [policyRes, rolesRes] = await Promise.all([
          iamServiceClient.getWorkspaceIamPolicy({}),
          roleServiceClient.listRoles({}),
        ]);
        if (!active) return;
        const roleById = new Map<string, Role>();
        for (const r of rolesRes.roles ?? []) roleById.set(r.name, r);
        const titles: string[] = [];
        for (const binding of policyRes.policy?.bindings ?? []) {
          if (binding.members.includes(user.name)) {
            const role = roleById.get(binding.role);
            titles.push(role?.title || binding.role.replace(/^roles\//, ""));
          }
        }
        setRoleTitles(titles);
      } catch {
        if (active) setRoleTitles([]);
      }
    })();
    return () => {
      active = false;
    };
    // Depend on the user's resource name (stable across roster refetches) so a
    // description save that refreshes the roster does not re-fetch the policy.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canGetPolicy, user?.name]);

  if (!user) {
    return (
      <div className="h-full overflow-y-auto p-6">
        <p className="text-sm text-control-light">{t("common.loading")}</p>
      </div>
    );
  }

  const createdAgents = agents.filter((a) => a.createdBy === user.name);

  async function handleAvatarChange(file: File | undefined) {
    if (!file || !userId) return;
    setAvatarBusy(true);
    try {
      const { data, mimeType } = await resizeImageFile(file, 256, 0.9);
      await userServiceClient.uploadAvatar(
        create(UploadAvatarRequestSchema, { data, mimeType })
      );
      invalidateAvatar(`users/${userId}/avatar`);
      // Refresh both the session (currentUser.avatar) and the roster row.
      await fetchCurrentUser();
      await fetchUsers({ pageSize: 100 }, { silent: true });
      toastManager.add({
        type: "success",
        title: t("members.human.avatar-uploaded"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("members.human.avatar-upload-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setAvatarBusy(false);
    }
  }

  async function handleAvatarRemove() {
    if (!userId) return;
    setAvatarBusy(true);
    try {
      await userServiceClient.deleteAvatar(
        create(DeleteAvatarRequestSchema, { name: `users/${userId}/avatar` })
      );
      invalidateAvatar(`users/${userId}/avatar`);
      await fetchCurrentUser();
      await fetchUsers({ pageSize: 100 }, { silent: true });
      toastManager.add({
        type: "success",
        title: t("members.human.avatar-removed"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("members.human.avatar-remove-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setAvatarBusy(false);
    }
  }

  function startEditDescription() {
    setDescriptionDraft(user?.description ?? "");
    setEditingDescription(true);
  }

  async function saveDescription() {
    if (!user?.name) return;
    setSavingDescription(true);
    try {
      await updateUser(user.name, { description: descriptionDraft }, [
        "description",
      ]);
      await fetchUsers({ pageSize: 100 }, { silent: true });
      setEditingDescription(false);
      toastManager.add({
        type: "success",
        title: t("members.human.description-saved"),
      });
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("members.human.description-save-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setSavingDescription(false);
    }
  }

  return (
    <div className="h-full overflow-y-auto">
      {/* Header: avatar + name + handle. */}
      <div className="flex items-start gap-4 px-5 py-5">
        <div className="relative shrink-0">
          <Avatar seed={userId ?? user.title} src={avatarSrc} />
          {isSelf && (
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              disabled={avatarBusy}
              className="absolute inset-0 flex size-8 items-center justify-center rounded-full bg-black/40 opacity-0 transition-opacity hover:opacity-100 disabled:opacity-70"
              aria-label={t("members.human.avatar-upload")}
              title={t("members.human.avatar-upload")}
            >
              {avatarBusy ? (
                <Loader2 className="size-3.5 animate-spin text-white" />
              ) : (
                <Upload className="size-3.5 text-white" />
              )}
            </button>
          )}
          {isSelf && user.avatar && (
            <button
              type="button"
              onClick={handleAvatarRemove}
              disabled={avatarBusy}
              className="absolute -right-1 -bottom-1 flex size-5 items-center justify-center rounded-full border border-control-border bg-background text-control-light hover:text-error"
              aria-label={t("members.human.avatar-remove")}
              title={t("members.human.avatar-remove")}
            >
              <Trash2 className="size-3" />
            </button>
          )}
          {isSelf && (
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
          )}
        </div>
        <div className="min-w-0 flex-1">
          <div className="min-w-0 truncate text-lg font-bold leading-tight text-main">
            {user.title || user.email}
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <span className="min-w-0 truncate text-sm font-mono text-control-light">
              {t("members.human.handle", { id: userId ?? "" })}
            </span>
            {isSelf && (
              <span className="shrink-0 text-sm text-control font-mono">
                {t("members.human.you")}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Description. */}
      <div className="border-t border-control-border px-5 py-4">
        <div className="mb-1 flex items-center gap-2">
          <div className="text-xs font-bold uppercase text-control tracking-widest">
            {t("members.human.description")}
          </div>
          {canEditDescription && !editingDescription && (
            <button
              type="button"
              onClick={startEditDescription}
              className="text-control-light hover:text-main transition-colors"
              aria-label={t("members.human.edit-description")}
              title={t("members.human.edit-description")}
            >
              <Pencil className="size-3" />
            </button>
          )}
        </div>
        {editingDescription ? (
          <div className="flex flex-col gap-2">
            <Textarea
              className="min-h-[80px]"
              value={descriptionDraft}
              onChange={(e) => setDescriptionDraft(e.target.value)}
              placeholder={t("user.field-description-placeholder")}
              disabled={savingDescription}
            />
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                onClick={saveDescription}
                disabled={savingDescription}
              >
                <Check className="size-3.5" />
                {t("members.human.save")}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setEditingDescription(false)}
                disabled={savingDescription}
              >
                <X className="size-3.5" />
                {t("members.human.cancel")}
              </Button>
            </div>
          </div>
        ) : user.description ? (
          <p className="whitespace-pre-wrap text-sm text-main">
            {user.description}
          </p>
        ) : (
          <p className="text-sm italic text-control-light">
            {t("members.human.no-description")}
          </p>
        )}
      </div>

      {/* Info: role + email. */}
      <div className="border-t border-control-border px-5 py-4">
        <div className="mb-3 text-xs font-bold uppercase text-control tracking-widest">
          {t("members.human.info")}
        </div>
        <div className="flex flex-col gap-3">
          {canGetPolicy && (
            <div>
              <div className="mb-1 text-xs text-control-light">
                {t("members.human.role")}
              </div>
              <div className="flex flex-wrap gap-1.5">
                {roleTitles.length === 0 ? (
                  <span className="text-sm text-control-light">—</span>
                ) : (
                  roleTitles.map((title) => (
                    <Badge key={title} variant="secondary">
                      {title}
                    </Badge>
                  ))
                )}
              </div>
            </div>
          )}
          <div>
            <div className="mb-1 text-xs text-control-light">
              {t("members.human.email")}
            </div>
            <div className="text-sm text-main font-mono break-all">
              {user.email}
            </div>
          </div>
        </div>
      </div>

      {/* Created agents. */}
      <div className="border-t border-control-border px-5 py-4">
        <div className="mb-3 flex items-center gap-2">
          <div className="text-xs font-bold uppercase text-control tracking-widest">
            {t("members.human.created-agents")}
          </div>
          <span className="font-mono text-xs text-control-light">
            {createdAgents.length}
          </span>
        </div>
        {createdAgents.length === 0 ? (
          <p className="text-sm text-control-light">
            {t("members.human.no-created-agents")}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {createdAgents.map((agent) => {
              const id = agent.name.replace(/^agents\//, "");
              return (
                <button
                  key={agent.name}
                  type="button"
                  onClick={() => navigate(`/members/agents/${id}`)}
                  className="flex items-center gap-3 rounded-md border border-control-border bg-background px-3 py-2 text-left transition-colors hover:border-accent/40 hover:bg-control-bg/60"
                >
                  <Avatar seed={id || agent.title} />
                  <div className="min-w-0 flex-1 flex flex-col gap-0.5">
                    <span className="truncate text-sm font-medium text-main">
                      {agent.title}
                    </span>
                    {agent.provider && (
                      <span className="truncate text-xs text-control-light font-mono">
                        {agent.provider}
                      </span>
                    )}
                  </div>
                  <ConnectionBadge state={agent.status?.state} />
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
