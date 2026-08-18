import { Pencil, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { PermissionNotice, SettingsPage } from "@/components/settings-page";
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
import { identityProviderServiceClient } from "@/connect";
import { describeError } from "@/lib/connect-errors";
import { toastManager } from "@/lib/toast";
import { useHasPermission } from "@/stores/permissions";
import type { IdentityProvider } from "@/types/proto-es/v1/idp_service_pb";
import { IdentityProviderType } from "@/types/proto-es/v1/idp_service_pb";

interface IdpForm {
  title: string;
  domain: string;
  clientId: string;
  clientSecret: string;
  authUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  scopes: string;
  identifier: string;
  displayName: string;
}

function emptyForm(): IdpForm {
  return {
    title: "",
    domain: "",
    clientId: "",
    clientSecret: "",
    authUrl: "",
    tokenUrl: "",
    userInfoUrl: "",
    scopes: "openid email profile",
    identifier: "email",
    displayName: "name",
  };
}

function idpToForm(p: IdentityProvider): IdpForm {
  const oauth = p.config?.config?.case === "oauth2Config" ? p.config.config.value : undefined;
  return {
    title: p.title,
    domain: p.domain,
    clientId: oauth?.clientId ?? "",
    clientSecret: "", // server never returns the secret
    authUrl: oauth?.authUrl ?? "",
    tokenUrl: oauth?.tokenUrl ?? "",
    userInfoUrl: oauth?.userInfoUrl ?? "",
    scopes: (oauth?.scopes ?? []).join(" "),
    identifier: oauth?.fieldMapping?.identifier ?? "",
    displayName: oauth?.fieldMapping?.displayName ?? "",
  };
}

function typeLabel(t: IdentityProviderType, tFn: (k: string) => string): string {
  if (t === IdentityProviderType.OAUTH2) return tFn("settings.identity-providers.type-oauth2");
  if (t === IdentityProviderType.OIDC) return "OIDC";
  if (t === IdentityProviderType.LDAP) return "LDAP";
  return "—";
}

export function SettingsIdentityProvidersPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.identityProviders.list");
  const canCreate = useHasPermission("laelia.identityProviders.create");
  const canUpdate = useHasPermission("laelia.identityProviders.update");
  const canDelete = useHasPermission("laelia.identityProviders.delete");

  const [providers, setProviders] = useState<IdentityProvider[]>([]);
  const [loading, setLoading] = useState(true);

  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<IdpForm>(emptyForm());
  const [creating, setCreating] = useState(false);

  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<IdentityProvider | null>(null);
  const [editForm, setEditForm] = useState<IdpForm>(emptyForm());
  const [saving, setSaving] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<IdentityProvider | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await identityProviderServiceClient.listIdentityProviders({});
      setProviders(res.identityProviders ?? []);
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.identity-providers.load-failed"),
        description: describeError(err),
      });
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (canList) load();
    else setLoading(false);
  }, [canList, load]);

  const buildOAuthConfig = (form: IdpForm) => ({
    config: {
      case: "oauth2Config" as const,
      value: {
        clientId: form.clientId.trim(),
        clientSecret: form.clientSecret.trim(),
        authUrl: form.authUrl.trim(),
        tokenUrl: form.tokenUrl.trim(),
        userInfoUrl: form.userInfoUrl.trim(),
        scopes: form.scopes
          .split(/\s+/)
          .map((s) => s.trim())
          .filter(Boolean),
        fieldMapping: {
          identifier: form.identifier.trim() || "email",
          displayName: form.displayName.trim(),
        },
        skipTlsVerify: false,
        authStyle: 1, // IN_PARAMS
      },
    },
  });

  const validateForm = (form: IdpForm): string | null => {
    if (!form.title.trim()) return t("settings.identity-providers.field-title");
    if (!form.clientId.trim()) return t("settings.identity-providers.field-client-id");
    if (!form.authUrl.trim()) return t("settings.identity-providers.field-auth-url");
    if (!form.tokenUrl.trim()) return t("settings.identity-providers.field-token-url");
    if (!form.userInfoUrl.trim()) return t("settings.identity-providers.field-user-info-url");
    return null;
  };

  const create = async () => {
    const missing = validateForm(createForm);
    if (missing) {
      toastManager.add({ type: "error", title: missing });
      return;
    }
    const identityProviderId = createForm.title
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9-]/g, "-")
      .replace(/-+/g, "-")
      .replace(/^-|-$/g, "");
    if (!identityProviderId) {
      toastManager.add({
        type: "error",
        title: t("settings.identity-providers.invalid-id"),
      });
      return;
    }
    setCreating(true);
    try {
      await identityProviderServiceClient.createIdentityProvider({
        identityProviderId,
        identityProvider: {
          title: createForm.title.trim(),
          domain: createForm.domain.trim().toLowerCase(),
          type: IdentityProviderType.OAUTH2,
          config: buildOAuthConfig(createForm) as never,
        },
      });
      toastManager.add({
        type: "success",
        title: t("settings.identity-providers.created"),
      });
      setCreateOpen(false);
      setCreateForm(emptyForm());
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.identity-providers.create-failed"),
        description: describeError(err),
      });
    } finally {
      setCreating(false);
    }
  };

  const save = async () => {
    if (!editTarget) return;
    const missing = validateForm(editForm);
    if (missing) {
      toastManager.add({ type: "error", title: missing });
      return;
    }
    setSaving(true);
    try {
      await identityProviderServiceClient.updateIdentityProvider({
        identityProvider: {
          name: editTarget.name,
          title: editForm.title.trim(),
          domain: editForm.domain.trim().toLowerCase(),
          type: editTarget.type,
          config: buildOAuthConfig(editForm) as never,
        },
        updateMask: { paths: ["title", "domain", "config"] },
      });
      toastManager.add({
        type: "success",
        title: t("settings.identity-providers.saved"),
      });
      setEditOpen(false);
      setEditTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.identity-providers.save-failed"),
        description: describeError(err),
      });
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await identityProviderServiceClient.deleteIdentityProvider({
        name: deleteTarget.name,
      });
      toastManager.add({
        type: "success",
        title: t("settings.identity-providers.deleted"),
      });
      setDeleteOpen(false);
      setDeleteTarget(null);
      load();
    } catch (err) {
      toastManager.add({
        type: "error",
        title: t("settings.identity-providers.delete-failed"),
        description: describeError(err),
      });
    } finally {
      setDeleting(false);
    }
  };

  if (!canList) {
    return (
      <SettingsPage title={t("settings.identity-providers.title")}>
        <PermissionNotice message={t("settings.identity-providers.not-allowed")} />
      </SettingsPage>
    );
  }

  const renderFormFields = (form: IdpForm, setForm: (f: IdpForm) => void) => (
    <div className="flex flex-col gap-4">
      <FieldRow label={t("settings.identity-providers.field-title")}>
        <Input
          value={form.title}
          placeholder={t("settings.identity-providers.field-title-placeholder")}
          onChange={(e) => setForm({ ...form, title: e.target.value })}
        />
      </FieldRow>
      <FieldRow
        label={t("settings.identity-providers.field-domain")}
        hint={t("settings.identity-providers.field-domain-hint")}
      >
        <Input
          value={form.domain}
          placeholder={t("settings.identity-providers.field-domain-placeholder")}
          onChange={(e) => setForm({ ...form, domain: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-client-id")}>
        <Input
          value={form.clientId}
          placeholder={t("settings.identity-providers.field-client-id-placeholder")}
          onChange={(e) => setForm({ ...form, clientId: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-client-secret")}>
        <SecretInput
          value={form.clientSecret}
          placeholder={
            editTarget
              ? t("settings.identity-providers.field-client-secret-placeholder")
              : undefined
          }
          onChange={(e) => setForm({ ...form, clientSecret: e.target.value })}
        />
        {editTarget && form.clientSecret === "" && (
          <p className="mt-1 text-xs text-control-light">
            {t("settings.identity-providers.field-client-secret-kept")}
          </p>
        )}
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-auth-url")}>
        <Input
          value={form.authUrl}
          placeholder={t("settings.identity-providers.field-auth-url-placeholder")}
          onChange={(e) => setForm({ ...form, authUrl: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-token-url")}>
        <Input
          value={form.tokenUrl}
          placeholder={t("settings.identity-providers.field-token-url-placeholder")}
          onChange={(e) => setForm({ ...form, tokenUrl: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-user-info-url")}>
        <Input
          value={form.userInfoUrl}
          placeholder={t("settings.identity-providers.field-user-info-url-placeholder")}
          onChange={(e) => setForm({ ...form, userInfoUrl: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-scopes")}>
        <Input
          value={form.scopes}
          placeholder={t("settings.identity-providers.field-scopes-placeholder")}
          onChange={(e) => setForm({ ...form, scopes: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-identifier-mapping")}>
        <Input
          value={form.identifier}
          placeholder={t("settings.identity-providers.field-identifier-mapping-placeholder")}
          onChange={(e) => setForm({ ...form, identifier: e.target.value })}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-display-name-mapping")}>
        <Input
          value={form.displayName}
          placeholder={t("settings.identity-providers.field-display-name-mapping-placeholder")}
          onChange={(e) => setForm({ ...form, displayName: e.target.value })}
        />
      </FieldRow>
    </div>
  );

  return (
    <SettingsPage
      title={t("settings.identity-providers.title")}
      description={t("settings.identity-providers.description")}
      actions={
        canCreate ? (
          <Button
            onClick={() => {
              setCreateForm(emptyForm());
              setCreateOpen(true);
            }}
          >
            <Plus className="size-4" />
            {t("settings.identity-providers.create")}
          </Button>
        ) : undefined
      }
    >
      <div className="mx-auto w-full max-w-3xl">
        {loading ? (
          <p className="py-8 text-center text-sm text-control-light">
            {t("common.loading")}
          </p>
        ) : providers.length === 0 ? (
          <p className="py-8 text-center text-sm text-control-light">
            {t("settings.identity-providers.empty")}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("settings.identity-providers.field-title")}</TableHead>
                <TableHead>{t("settings.identity-providers.type")}</TableHead>
                <TableHead>{t("settings.identity-providers.field-domain")}</TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {providers.map((p) => (
                <TableRow key={p.name}>
                  <TableCell className="font-medium">{p.title}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">
                      {typeLabel(p.type, t)}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-control-light">{p.domain || "—"}</TableCell>
                  <TableCell>
                    <div className="flex items-center justify-end gap-1">
                      {canUpdate && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            if (p.type !== IdentityProviderType.OAUTH2) {
                              toastManager.add({
                                type: "error",
                                title: t("settings.identity-providers.unsupported-type"),
                              });
                              return;
                            }
                            setEditTarget(p);
                            setEditForm(idpToForm(p));
                            setEditOpen(true);
                          }}
                        >
                          <Pencil className="size-4" />
                        </Button>
                      )}
                      {canDelete && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setDeleteTarget(p);
                            setDeleteOpen(true);
                          }}
                        >
                          <Trash2 className="size-4 text-error" />
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>

      <Sheet open={createOpen} onOpenChange={setCreateOpen}>
        <SheetContent width="standard">
          <SheetHeader>
            <SheetTitle>{t("settings.identity-providers.create-title")}</SheetTitle>
            <SheetDescription>
              {t("settings.identity-providers.create-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>{renderFormFields(createForm, setCreateForm)}</SheetBody>
          <SheetFooter>
            <Button onClick={create} disabled={creating}>
              {creating ? "…" : t("settings.identity-providers.create")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <Sheet open={editOpen} onOpenChange={setEditOpen}>
        <SheetContent width="standard">
          <SheetHeader>
            <SheetTitle>
              {t("settings.identity-providers.edit-title", {
                title: editTarget?.title ?? "",
              })}
            </SheetTitle>
            <SheetDescription>
              {t("settings.identity-providers.edit-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody>{renderFormFields(editForm, setEditForm)}</SheetBody>
          <SheetFooter>
            <Button onClick={save} disabled={saving}>
              {saving ? "…" : t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("settings.identity-providers.delete-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("settings.identity-providers.delete-confirm-description", {
              title: deleteTarget?.title ?? "",
            })}
          </AlertDialogDescription>
          <AlertDialogFooter>
            <AlertDialogClose>{t("common.cancel")}</AlertDialogClose>
            <Button variant="destructive" onClick={remove} disabled={deleting}>
              {deleting ? "…" : t("common.delete")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsPage>
  );
}
