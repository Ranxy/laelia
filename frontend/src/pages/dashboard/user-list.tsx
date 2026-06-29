import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsList, TabsPanel, TabsTrigger } from "@/components/ui/tabs";
import { formatTimestamp } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { useAppStore } from "@/stores";
import { State } from "@/types/proto-es/v1/common_pb";
import { type User, UserType } from "@/types/proto-es/v1/user_service_pb";

type Tab = "active" | "trash";

export function UserListPage() {
  const { t } = useTranslation();
  const isAdmin = useAppStore((s) => s.currentUser?.workspaceAdmin ?? false);
  const currentUser = useAppStore((s) => s.currentUser);
  const users = useAppStore((s) => s.users);
  const usersLoading = useAppStore((s) => s.usersLoading);
  const deletedUsers = useAppStore((s) => s.deletedUsers);
  const deletedUsersLoading = useAppStore((s) => s.deletedUsersLoading);
  const [tab, setTab] = useState<Tab>("active");

  // Create-user sheet
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState("");
  const [title, setTitle] = useState("");
  const [titleManuallyEdited, setTitleManuallyEdited] = useState(false);
  const [phone, setPhone] = useState("");
  const [password, setPassword] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");

  // Auto-fill the title from the email local part, mirroring the sign-up page.
  // Stops once the user manually edits the title.
  useEffect(() => {
    if (titleManuallyEdited || !email.includes("@")) return;
    const parts = email.split("@")[0].replaceAll("_", ".").split(".");
    if (parts.length >= 2) {
      setTitle(
        `${parts[0].charAt(0).toUpperCase()}${parts[0].slice(1)} ${parts[1].charAt(0).toUpperCase()}${parts[1].slice(1)}`
      );
    } else if (parts[0].length > 0) {
      setTitle(parts[0].charAt(0).toUpperCase() + parts[0].slice(1));
    }
  }, [email, titleManuallyEdited]);

  // Edit-user sheet
  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<User | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [editEmail, setEditEmail] = useState("");
  const [editPhone, setEditPhone] = useState("");
  const [saving, setSaving] = useState(false);
  const [editError, setEditError] = useState("");

  // Reset-password dialog
  const [resetOpen, setResetOpen] = useState(false);
  const [resetTarget, setResetTarget] = useState<User | null>(null);
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [resetting, setResetting] = useState(false);
  const [resetError, setResetError] = useState("");

  // Delete-user dialog
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const [deleting, setDeleting] = useState(false);

  const loadActive = useCallback(() => {
    useAppStore.getState().fetchUsers({ showDeleted: false, pageSize: 100 });
  }, []);
  const loadTrash = useCallback(() => {
    useAppStore.getState().fetchUsers({ showDeleted: true, pageSize: 100 });
  }, []);

  useEffect(() => {
    loadActive();
  }, [loadActive]);

  useEffect(() => {
    if (tab === "trash") loadTrash();
  }, [tab, loadTrash]);

  const refreshBoth = useCallback(() => {
    loadActive();
    if (tab === "trash") loadTrash();
  }, [loadActive, loadTrash, tab]);

  function resetCreateForm() {
    setEmail("");
    setTitle("");
    setTitleManuallyEdited(false);
    setPhone("");
    setPassword("");
    setCreateError("");
  }

  async function handleCreate() {
    setCreateError("");
    if (!email.trim() || !title.trim() || !password.trim()) {
      setCreateError(t("user.create-required"));
      return;
    }
    if (!emailValid(email.trim())) {
      setCreateError(t("user.create-email-invalid"));
      return;
    }
    setCreating(true);
    try {
      await useAppStore.getState().createUser({
        email: email.trim(),
        title: title.trim(),
        phone: phone.trim(),
        password: password,
      });
      toastManager.add({ type: "success", title: t("user.created") });
      resetCreateForm();
      setCreateOpen(false);
      refreshBoth();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err));
    } finally {
      setCreating(false);
    }
  }

  function openEdit(user: User) {
    setEditTarget(user);
    setEditTitle(user.title);
    setEditEmail(user.email);
    setEditPhone(user.phone);
    setEditError("");
    setEditOpen(true);
  }

  async function handleSaveEdit() {
    if (!editTarget?.name) return;
    setEditError("");
    const maskPaths: string[] = [];
    const fields: { title?: string; email?: string; phone?: string } = {};
    if (editTitle !== editTarget.title) {
      maskPaths.push("title");
      fields.title = editTitle;
    }
    if (editEmail !== editTarget.email) {
      if (editEmail.trim() && !emailValid(editEmail.trim())) {
        setEditError(t("user.create-email-invalid"));
        return;
      }
      maskPaths.push("email");
      fields.email = editEmail.trim();
    }
    if (editPhone !== editTarget.phone) {
      maskPaths.push("phone");
      fields.phone = editPhone;
    }
    if (maskPaths.length === 0) {
      setEditOpen(false);
      return;
    }
    setSaving(true);
    try {
      await useAppStore
        .getState()
        .updateUser(editTarget.name, fields, maskPaths);
      toastManager.add({ type: "success", title: t("user.updated") });
      setEditOpen(false);
      refreshBoth();
    } catch (err) {
      setEditError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  function openReset(user: User) {
    setResetTarget(user);
    setNewPassword("");
    setConfirmPassword("");
    setResetError("");
    setResetOpen(true);
  }

  async function handleReset() {
    if (!resetTarget?.name) return;
    setResetError("");
    if (!newPassword) {
      setResetError(t("user.reset-password-required"));
      return;
    }
    if (newPassword !== confirmPassword) {
      setResetError(t("user.reset-password-mismatch"));
      return;
    }
    setResetting(true);
    try {
      await useAppStore.getState().resetPassword(resetTarget.name, newPassword);
      toastManager.add({ type: "success", title: t("user.password-changed") });
      setResetOpen(false);
      refreshBoth();
    } catch (err) {
      setResetError(err instanceof Error ? err.message : String(err));
    } finally {
      setResetting(false);
    }
  }

  async function handleConfirmDelete() {
    if (!deleteTarget?.name) return;
    setDeleting(true);
    try {
      await useAppStore.getState().deleteUser(deleteTarget.name);
      toastManager.add({ type: "success", title: t("user.deleted") });
      setDeleteOpen(false);
      setDeleteTarget(null);
      refreshBoth();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("user.delete-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setDeleting(false);
    }
  }

  async function handleRestore(user: User) {
    try {
      await useAppStore.getState().undeleteUser(user.name);
      toastManager.add({ type: "success", title: t("user.restored") });
      refreshBoth();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("user.restore-failed"),
        description: err instanceof Error ? err.message : String(err),
      });
    }
  }

  return (
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-4 w-full">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-main">{t("user.title")}</h1>
        {isAdmin && (
          <Button onClick={() => setCreateOpen(true)}>
            {t("user.create")}
          </Button>
        )}
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="active">{t("user.tab-active")}</TabsTrigger>
          <TabsTrigger value="trash">{t("user.tab-trash")}</TabsTrigger>
        </TabsList>

        <TabsPanel value="active">
          {usersLoading ? (
            <p className="text-control-light">{t("common.loading")}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("user.header-email")}</TableHead>
                  <TableHead>{t("user.header-title")}</TableHead>
                  <TableHead>{t("user.header-type")}</TableHead>
                  <TableHead>{t("user.header-state")}</TableHead>
                  <TableHead>{t("user.header-last-login")}</TableHead>
                  {isAdmin && <TableHead>{t("common.actions")}</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {users.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={isAdmin ? 6 : 5}
                      className="text-center text-control-light"
                    >
                      {t("user.no-data")}
                    </TableCell>
                  </TableRow>
                ) : (
                  users.map((user) => (
                    <TableRow key={user.name}>
                      <TableCell>{user.email}</TableCell>
                      <TableCell>{user.title || "-"}</TableCell>
                      <TableCell>{userTypeLabel(t, user.userType)}</TableCell>
                      <TableCell>
                        <StateBadge state={user.state} t={t} />
                      </TableCell>
                      <TableCell>
                        {formatTimestamp(user.profile?.lastLoginTime)}
                      </TableCell>
                      {isAdmin && (
                        <TableCell>
                          <div className="flex items-center gap-2">
                            {!isSpecialUser(user) && (
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => openEdit(user)}
                              >
                                {t("user.edit")}
                              </Button>
                            )}
                            {canResetPassword(user) && (
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => openReset(user)}
                              >
                                {t("user.reset-password")}
                              </Button>
                            )}
                            {!isSpecialUser(user) &&
                              !isSelf(user, currentUser) && (
                                <Button
                                  variant="outline"
                                  size="sm"
                                  onClick={() => {
                                    setDeleteTarget(user);
                                    setDeleteOpen(true);
                                  }}
                                >
                                  {t("common.delete")}
                                </Button>
                              )}
                          </div>
                        </TableCell>
                      )}
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          )}
        </TabsPanel>

        <TabsPanel value="trash">
          {deletedUsersLoading ? (
            <p className="text-control-light">{t("common.loading")}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("user.header-email")}</TableHead>
                  <TableHead>{t("user.header-title")}</TableHead>
                  <TableHead>{t("user.header-type")}</TableHead>
                  <TableHead>{t("user.header-state")}</TableHead>
                  {isAdmin && <TableHead>{t("common.actions")}</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {deletedUsers.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={isAdmin ? 5 : 4}
                      className="text-center text-control-light"
                    >
                      {t("user.no-data")}
                    </TableCell>
                  </TableRow>
                ) : (
                  deletedUsers.map((user) => (
                    <TableRow key={user.name}>
                      <TableCell>{user.email}</TableCell>
                      <TableCell>{user.title || "-"}</TableCell>
                      <TableCell>{userTypeLabel(t, user.userType)}</TableCell>
                      <TableCell>
                        <StateBadge state={user.state} t={t} />
                      </TableCell>
                      {isAdmin && (
                        <TableCell>
                          {!isSpecialUser(user) && (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handleRestore(user)}
                            >
                              {t("user.restore")}
                            </Button>
                          )}
                        </TableCell>
                      )}
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          )}
        </TabsPanel>
      </Tabs>

      {/* Create user */}
      <Sheet
        open={createOpen}
        onOpenChange={(next) => {
          setCreateOpen(next);
          if (!next) resetCreateForm();
        }}
      >
        <SheetContent width="narrow">
          <SheetTitle>{t("user.create-title")}</SheetTitle>
          <SheetDescription>{t("user.create-description")}</SheetDescription>
          <SheetBody>
            {createError && (
              <Alert
                variant="error"
                description={createError}
                className="mb-4"
              />
            )}
            <div className="flex flex-col gap-4">
              <FieldRow label={t("user.field-email")}>
                <Input
                  value={email}
                  placeholder={t("user.field-email-placeholder")}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </FieldRow>
              <FieldRow label={t("user.field-title")}>
                <Input
                  value={title}
                  placeholder={t("user.field-title-placeholder")}
                  onChange={(e) => {
                    setTitle(e.target.value);
                    setTitleManuallyEdited(e.target.value.trim().length > 0);
                  }}
                />
              </FieldRow>
              <FieldRow
                label={t("user.field-phone")}
                hint={t("user.field-phone-hint")}
              >
                <Input
                  value={phone}
                  placeholder={t("user.field-phone-placeholder")}
                  onChange={(e) => setPhone(e.target.value)}
                />
              </FieldRow>
              <FieldRow label={t("user.field-password")}>
                <Input
                  type="password"
                  value={password}
                  placeholder={t("user.field-password-placeholder")}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                />
              </FieldRow>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => {
                setCreateOpen(false);
                resetCreateForm();
              }}
              disabled={creating}
            >
              {t("common.cancel")}
            </Button>
            <Button
              disabled={
                creating || !email.trim() || !title.trim() || !password.trim()
              }
              onClick={handleCreate}
            >
              {creating ? t("common.creating") : t("common.create")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Edit user */}
      <Sheet
        open={editOpen}
        onOpenChange={(next) => {
          setEditOpen(next);
          if (!next) setEditTarget(null);
        }}
      >
        <SheetContent width="narrow">
          <SheetTitle>
            {t("user.edit-title", { title: editTarget?.title ?? "" })}
          </SheetTitle>
          <SheetDescription>{t("user.edit-description")}</SheetDescription>
          <SheetBody>
            {editError && (
              <Alert variant="error" description={editError} className="mb-4" />
            )}
            <div className="flex flex-col gap-4">
              <FieldRow label={t("user.field-title")}>
                <Input
                  value={editTitle}
                  onChange={(e) => {
                    setEditTitle(e.target.value);
                    setEditError("");
                  }}
                />
              </FieldRow>
              <FieldRow label={t("user.field-email")}>
                <Input
                  value={editEmail}
                  onChange={(e) => {
                    setEditEmail(e.target.value);
                    setEditError("");
                  }}
                />
              </FieldRow>
              <FieldRow label={t("user.field-phone")}>
                <Input
                  value={editPhone}
                  onChange={(e) => {
                    setEditPhone(e.target.value);
                    setEditError("");
                  }}
                />
              </FieldRow>
            </div>
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setEditOpen(false)}
              disabled={saving}
            >
              {t("common.cancel")}
            </Button>
            <Button disabled={saving} onClick={handleSaveEdit}>
              {saving ? t("common.saving") : t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {/* Reset password */}
      <AlertDialog
        open={resetOpen}
        onOpenChange={(next) => {
          setResetOpen(next);
          if (!next) setResetTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("user.reset-password-title", {
              title: resetTarget?.title ?? "",
            })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("user.reset-password-description")}
          </AlertDialogDescription>
          <div className="mt-4 flex flex-col gap-3">
            {resetError && <Alert variant="error" description={resetError} />}
            <Input
              type="password"
              placeholder={t("user.field-password-new")}
              value={newPassword}
              onChange={(e) => {
                setNewPassword(e.target.value);
                setResetError("");
              }}
              autoComplete="new-password"
            />
            <Input
              type="password"
              placeholder={t("user.field-password-confirm")}
              value={confirmPassword}
              onChange={(e) => {
                setConfirmPassword(e.target.value);
                setResetError("");
              }}
              autoComplete="new-password"
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={resetting}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button disabled={resetting} onClick={handleReset}>
              {resetting ? t("common.saving") : t("user.reset-password")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete user */}
      <AlertDialog
        open={deleteOpen}
        onOpenChange={(next) => {
          setDeleteOpen(next);
          if (!next) setDeleteTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogTitle>{t("user.delete-confirm-title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("user.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
              email: deleteTarget?.email ?? "",
            })}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={deleting}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button
              variant="destructive"
              disabled={deleting}
              onClick={handleConfirmDelete}
            >
              {deleting ? t("common.saving") : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function FieldRow({
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

function StateBadge({
  state,
  t,
}: {
  state: State | undefined;
  t: (key: string, options?: Record<string, unknown>) => string;
}) {
  if (state === State.DELETED) {
    return <Badge variant="warning">{t("user.state-deleted")}</Badge>;
  }
  return <Badge variant="success">{t("user.state-active")}</Badge>;
}

function userTypeLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  type: UserType | undefined
): string {
  switch (type) {
    case UserType.USER:
      return t("user.type-user");
    case UserType.SERVICE_ACCOUNT:
      return t("user.type-service-account");
    case UserType.SYSTEM_BOT:
      return t("user.type-system-bot");
    default:
      return "-";
  }
}

function emailValid(email: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
}

// userIdFromName extracts the numeric user id from a `users/{id}` resource
// name. Returns null for non-numeric names (e.g. email-keyed lookups).
function userIdFromName(name: string | undefined): number | null {
  if (!name) return null;
  const match = name.match(/^users\/(\d+)$/);
  return match ? Number(match[1]) : null;
}

// isSpecialUser reports a built-in account (id < 100, e.g. the seeded system
// bot at id=1) that must not be modified or deleted.
function isSpecialUser(user: User): boolean {
  const id = userIdFromName(user.name);
  return id !== null && id < 100;
}

// isSelf reports whether the row is the currently signed-in user, who must not
// delete their own account.
function isSelf(user: User, currentUser: User | null): boolean {
  return !!currentUser?.name && currentUser.name === user.name;
}

// canResetPassword reports whether reset-password applies to a row: only end
// users have a password (service accounts authenticate via service_key), and
// special accounts are locked down.
function canResetPassword(user: User): boolean {
  return user.userType === UserType.USER && !isSpecialUser(user);
}
