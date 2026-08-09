import { Loader2, Users } from "lucide-react";
import {
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
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
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import { SearchInput } from "@/components/ui/search-input";
import { SecretInput } from "@/components/ui/secret-input";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
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
import { Textarea } from "@/components/ui/textarea";
import { formatTimestamp } from "@/lib/command-status";
import { toastManager } from "@/lib/toast";
import { buildUserFilter } from "@/lib/user-filter";
import { useAppStore } from "@/stores";
import { useHasPermission } from "@/stores/permissions";
import { State } from "@/types/proto-es/v1/common_pb";
import { type User, UserType } from "@/types/proto-es/v1/user_service_pb";

type Tab = "active" | "trash";

// UserTable renders the shared user-roster table shell (email/title/type/state
// columns + optional last-login and actions columns). The active and trash
// tabs previously duplicated this ~150-line markup; they now differ only in
// the columns they enable and the per-row action buttons.
function UserTable({
  users,
  loading,
  showLastLogin,
  actionsColumn,
  emptyMessage,
  renderActions,
}: {
  users: User[];
  loading: boolean;
  showLastLogin: boolean;
  actionsColumn: boolean;
  emptyMessage: string;
  renderActions: (user: User) => ReactNode;
}) {
  const { t } = useTranslation();
  const colCount = 4 + (showLastLogin ? 1 : 0) + (actionsColumn ? 1 : 0);
  if (loading) {
    return (
      <div className="flex items-center justify-center gap-2 py-16 text-control-light text-sm">
        <Loader2 className="size-4 animate-spin" />
        {t("common.loading")}
      </div>
    );
  }
  return (
    <div className="rounded-xs border border-control-border bg-background shadow-xs overflow-hidden">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-[25%]">{t("user.header-email")}</TableHead>
            <TableHead className="w-[15%]">{t("user.header-title")}</TableHead>
            <TableHead className="w-[10%]">{t("user.header-type")}</TableHead>
            <TableHead className="w-[10%]">{t("user.header-state")}</TableHead>
            {showLastLogin && (
              <TableHead className="w-[20%]">
                {t("user.header-last-login")}
              </TableHead>
            )}
            {actionsColumn && (
              <TableHead className="w-[20%]">{t("common.actions")}</TableHead>
            )}
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={colCount}
                className="text-center text-control-light py-12"
              >
                {emptyMessage}
              </TableCell>
            </TableRow>
          ) : (
            users.map((user) => (
              <TableRow key={user.name}>
                <TableCell className="align-top">{user.email}</TableCell>
                <TableCell className="align-top">{user.title || "-"}</TableCell>
                <TableCell className="align-top">
                  {userTypeLabel(t, user.userType)}
                </TableCell>
                <TableCell className="align-top">
                  <StateBadge state={user.state} t={t} />
                </TableCell>
                {showLastLogin && (
                  <TableCell className="align-top">
                    {formatTimestamp(user.profile?.lastLoginTime)}
                  </TableCell>
                )}
                {actionsColumn && (
                  <TableCell className="align-top">
                    {renderActions(user)}
                  </TableCell>
                )}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}

export function UserListPage() {
  const { t } = useTranslation();
  // Gate each user-management affordance on the exact permission its RPC
  // requires: laelia.users.create (CreateUser), laelia.users.update (UpdateUser
  // / reset password), laelia.users.delete (DeleteUser / UndeleteUser). A
  // custom role may hold any subset, so the UI must not offer an action the
  // server will 403. canManageUsers drives the active-table actions column
  // (which holds both update and delete controls).
  const canCreateUsers = useHasPermission("laelia.users.create");
  const canUpdateUsers = useHasPermission("laelia.users.update");
  const canDeleteUsers = useHasPermission("laelia.users.delete");
  const canManageUsers = canUpdateUsers || canDeleteUsers;
  const currentUser = useAppStore((s) => s.currentUser);
  const users = useAppStore((s) => s.users);
  const usersLoading = useAppStore((s) => s.usersLoading);
  const deletedUsers = useAppStore((s) => s.deletedUsers);
  const deletedUsersLoading = useAppStore((s) => s.deletedUsersLoading);
  const [tab, setTab] = useState<Tab>("active");
  const [searchQuery, setSearchQuery] = useState("");

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
  const [editDescription, setEditDescription] = useState("");
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

  // silent skips the loading flag so a refetch swaps the rows in place without
  // unmounting the table (headers/frame) to show the spinner.
  const loadActive = useCallback((filter: string, silent = false) => {
    useAppStore
      .getState()
      .fetchUsers({ showDeleted: false, pageSize: 100, filter }, { silent });
  }, []);
  const loadTrash = useCallback((filter: string, silent = false) => {
    useAppStore
      .getState()
      .fetchUsers({ showDeleted: true, pageSize: 100, filter }, { silent });
  }, []);

  // Debounced load of the visible tab. The first call runs immediately so the
  // list appears without waiting for the debounce; later typing or tab changes
  // refetch once the query settles. Every refetch after the initial mount is
  // silent so the table frame stays put and only the rows update.
  const initialLoadDone = useRef(false);
  useEffect(() => {
    const run = () => {
      const filter = buildUserFilter(searchQuery);
      if (tab === "trash") loadTrash(filter, initialLoadDone.current);
      else loadActive(filter, initialLoadDone.current);
    };
    if (!initialLoadDone.current) {
      initialLoadDone.current = true;
      run();
      return;
    }
    const timer = setTimeout(run, 250);
    return () => clearTimeout(timer);
  }, [searchQuery, tab, loadActive, loadTrash]);

  const refreshBoth = useCallback(() => {
    const filter = buildUserFilter(searchQuery);
    loadActive(filter, true);
    if (tab === "trash") loadTrash(filter, true);
  }, [loadActive, loadTrash, searchQuery, tab]);

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
    setEditDescription(user.description);
    setEditError("");
    setEditOpen(true);
  }

  async function handleSaveEdit() {
    if (!editTarget?.name) return;
    setEditError("");
    const maskPaths: string[] = [];
    const fields: {
      title?: string;
      email?: string;
      phone?: string;
      description?: string;
    } = {};
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
    if (editDescription !== editTarget.description) {
      maskPaths.push("description");
      fields.description = editDescription;
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
    <div className="h-full overflow-y-auto p-6 flex flex-col gap-5 w-full">
      <div className="flex items-start justify-between gap-4">
        <div className="hidden lg:flex flex-col gap-1">
          <h1 className="text-xl font-semibold text-main flex items-center gap-2">
            <Users className="size-5 text-accent" />
            {t("user.title")}
          </h1>
        </div>
        {canCreateUsers && (
          <Button onClick={() => setCreateOpen(true)}>
            {t("user.create")}
          </Button>
        )}
      </div>

      <div className="max-w-sm">
        <SearchInput
          placeholder={t("user.search-placeholder")}
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
        />
      </div>

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)}>
        <TabsList>
          <TabsTrigger value="active">{t("user.tab-active")}</TabsTrigger>
          <TabsTrigger value="trash">{t("user.tab-trash")}</TabsTrigger>
        </TabsList>

        <TabsPanel value="active">
          <UserTable
            users={users}
            loading={usersLoading}
            showLastLogin
            actionsColumn={canManageUsers}
            emptyMessage={t("user.no-data")}
            renderActions={(user) => (
              <div className="flex items-center gap-2">
                {canUpdateUsers && !isSpecialUser(user) && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => openEdit(user)}
                  >
                    {t("user.edit")}
                  </Button>
                )}
                {canUpdateUsers && canResetPassword(user) && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => openReset(user)}
                  >
                    {t("user.reset-password")}
                  </Button>
                )}
                {canDeleteUsers &&
                  !isSpecialUser(user) &&
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
            )}
          />
        </TabsPanel>

        <TabsPanel value="trash">
          <UserTable
            users={deletedUsers}
            loading={deletedUsersLoading}
            showLastLogin={false}
            actionsColumn={canDeleteUsers}
            emptyMessage={t("user.no-data")}
            renderActions={(user) =>
              !isSpecialUser(user) && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => handleRestore(user)}
                >
                  {t("user.restore")}
                </Button>
              )
            }
          />
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
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>{t("user.create-title")}</SheetTitle>
            <SheetDescription>{t("user.create-description")}</SheetDescription>
          </SheetHeader>
          <SheetBody>
            {createError && (
              <Alert
                variant="error"
                description={createError}
                className="mb-2"
              />
            )}
            <div className="flex flex-col gap-5">
              <FieldRow label={t("user.field-email")} htmlFor="create-email">
                <Input
                  id="create-email"
                  value={email}
                  placeholder={t("user.field-email-placeholder")}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </FieldRow>
              <FieldRow label={t("user.field-title")} htmlFor="create-title">
                <Input
                  id="create-title"
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
                htmlFor="create-phone"
              >
                <Input
                  id="create-phone"
                  value={phone}
                  placeholder={t("user.field-phone-placeholder")}
                  onChange={(e) => setPhone(e.target.value)}
                />
              </FieldRow>
              <FieldRow
                label={t("user.field-password")}
                htmlFor="create-password"
              >
                <SecretInput
                  id="create-password"
                  value={password}
                  placeholder={t("user.field-password-placeholder")}
                  onChange={(e) => setPassword(e.target.value)}
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
        <SheetContent width="medium">
          <SheetHeader>
            <SheetTitle>
              {t("user.edit-title", { title: editTarget?.title ?? "" })}
            </SheetTitle>
            <SheetDescription>{t("user.edit-description")}</SheetDescription>
          </SheetHeader>
          <SheetBody>
            {editError && (
              <Alert variant="error" description={editError} className="mb-2" />
            )}
            <div className="flex flex-col gap-5">
              <FieldRow label={t("user.field-title")} htmlFor="edit-title">
                <Input
                  id="edit-title"
                  value={editTitle}
                  onChange={(e) => {
                    setEditTitle(e.target.value);
                    setEditError("");
                  }}
                />
              </FieldRow>
              <FieldRow label={t("user.field-email")} htmlFor="edit-email">
                <Input
                  id="edit-email"
                  value={editEmail}
                  onChange={(e) => {
                    setEditEmail(e.target.value);
                    setEditError("");
                  }}
                />
              </FieldRow>
              <FieldRow label={t("user.field-phone")} htmlFor="edit-phone">
                <Input
                  id="edit-phone"
                  value={editPhone}
                  onChange={(e) => {
                    setEditPhone(e.target.value);
                    setEditError("");
                  }}
                />
              </FieldRow>
              <FieldRow
                label={t("user.field-description")}
                hint={t("user.field-description-hint")}
                htmlFor="edit-description"
              >
                <Textarea
                  id="edit-description"
                  className="min-h-[80px]"
                  placeholder={t("user.field-description-placeholder")}
                  value={editDescription}
                  onChange={(e) => {
                    setEditDescription(e.target.value);
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
            <SecretInput
              placeholder={t("user.field-password-new")}
              value={newPassword}
              onChange={(e) => {
                setNewPassword(e.target.value);
                setResetError("");
              }}
            />
            <SecretInput
              placeholder={t("user.field-password-confirm")}
              value={confirmPassword}
              onChange={(e) => {
                setConfirmPassword(e.target.value);
                setResetError("");
              }}
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
