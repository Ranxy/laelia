import { Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfirmActionDialog } from "@/components/settings/confirm-action-dialog";
import { ResourceSheet } from "@/components/settings/resource-sheet";
import {
  PageLoading,
  PermissionNotice,
  SettingsPage,
} from "@/components/settings-page";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FieldRow } from "@/components/ui/field-row";
import { Input } from "@/components/ui/input";
import { SecretInput } from "@/components/ui/secret-input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useCrudDialog } from "@/composables/use-crud-dialog";
import { useResourceQuery } from "@/composables/use-resource-query";
import { identityProviderServiceClient } from "@/connect";
import { slugify } from "@/lib/slug";
import { toastManager } from "@/lib/toast";
import { showErrorToast } from "@/lib/toast-errors";
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
  const oauth =
    p.config?.config?.case === "oauth2Config"
      ? p.config.config.value
      : undefined;
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

function typeLabel(
  type: IdentityProviderType,
  t: (k: string) => string
): string {
  if (type === IdentityProviderType.OAUTH2)
    return t("settings.identity-providers.type-oauth2");
  if (type === IdentityProviderType.OIDC) return "OIDC";
  if (type === IdentityProviderType.LDAP) return "LDAP";
  return "—";
}

export function SettingsIdentityProvidersPage() {
  const { t } = useTranslation();
  const canList = useHasPermission("laelia.identityProviders.list");
  const canCreate = useHasPermission("laelia.identityProviders.create");
  const canUpdate = useHasPermission("laelia.identityProviders.update");
  const canDelete = useHasPermission("laelia.identityProviders.delete");

  const idpQuery = useResourceQuery<IdentityProvider>({
    enabled: canList,
    queryKey: ["settings", "identityProviders"],
    queryFn: async (signal) =>
      (
        await identityProviderServiceClient.listIdentityProviders(
          {},
          { signal }
        )
      ).identityProviders ?? [],
    failureTitle: t("settings.identity-providers.load-failed"),
  });

  const crud = useCrudDialog<IdentityProvider>({
    // Post-mutation refresh of the resource list.
    onChanged: () => void idpQuery.reload(),
  });

  const providers = idpQuery.items;

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
    if (!form.clientId.trim())
      return t("settings.identity-providers.field-client-id");
    if (!form.authUrl.trim())
      return t("settings.identity-providers.field-auth-url");
    if (!form.tokenUrl.trim())
      return t("settings.identity-providers.field-token-url");
    if (!form.userInfoUrl.trim())
      return t("settings.identity-providers.field-user-info-url");
    return null;
  };

  const handleCreateForm = async (form: IdpForm) => {
    const missing = validateForm(form);
    if (missing) {
      toastManager.add({ type: "error", title: missing });
      return;
    }
    const identityProviderId = slugify(form.title.trim());
    if (!identityProviderId) {
      toastManager.add({
        type: "error",
        title: t("settings.identity-providers.invalid-id"),
      });
      return;
    }
    await crud.runCreate(
      async () => {
        await identityProviderServiceClient.createIdentityProvider({
          identityProviderId,
          identityProvider: {
            title: form.title.trim(),
            domain: form.domain.trim().toLowerCase(),
            type: IdentityProviderType.OAUTH2,
            config: buildOAuthConfig(form) as never,
          },
        });
      },
      {
        successTitle: t("settings.identity-providers.created"),
        onError: (err) => {
          void showErrorToast(
            err,
            t("settings.identity-providers.create-failed")
          );
        },
      }
    );
  };

  const handleSaveForm = async (form: IdpForm) => {
    const target = crud.editTarget;
    if (!target) return;
    const missing = validateForm(form);
    if (missing) {
      toastManager.add({ type: "error", title: missing });
      return;
    }
    await crud.runSave(
      async () => {
        await identityProviderServiceClient.updateIdentityProvider({
          identityProvider: {
            name: target.name,
            title: form.title.trim(),
            domain: form.domain.trim().toLowerCase(),
            type: target.type,
            config: buildOAuthConfig(form) as never,
          },
          updateMask: { paths: ["title", "domain", "config"] },
        });
      },
      {
        successTitle: t("settings.identity-providers.saved"),
        onError: (err) => {
          void showErrorToast(
            err,
            t("settings.identity-providers.save-failed")
          );
        },
      }
    );
  };

  const handleDelete = async () => {
    const target = crud.deleteTarget;
    if (!target) return;
    await crud.runDelete(
      async () => {
        await identityProviderServiceClient.deleteIdentityProvider({
          name: target.name,
        });
      },
      {
        successTitle: t("settings.identity-providers.deleted"),
        onError: (err) => {
          void showErrorToast(
            err,
            t("settings.identity-providers.delete-failed")
          );
        },
      }
    );
  };

  if (!canList) {
    return (
      <SettingsPage title={t("settings.identity-providers.title")}>
        <PermissionNotice
          message={t("settings.identity-providers.not-allowed")}
        />
      </SettingsPage>
    );
  }

  return (
    <SettingsPage
      title={t("settings.identity-providers.title")}
      description={t("settings.identity-providers.description")}
      actions={
        canCreate ? (
          <Button onClick={crud.openCreate}>
            <Plus className="size-4" />
            {t("settings.identity-providers.create")}
          </Button>
        ) : undefined
      }
    >
      <div className="mx-auto w-full max-w-3xl">
        {idpQuery.initialLoading ? (
          <PageLoading />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>
                  {t("settings.identity-providers.field-title")}
                </TableHead>
                <TableHead>{t("settings.identity-providers.type")}</TableHead>
                <TableHead>
                  {t("settings.identity-providers.field-domain")}
                </TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {providers.map((p) => (
                <TableRow key={p.name}>
                  <TableCell className="font-medium">{p.title}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">{typeLabel(p.type, t)}</Badge>
                  </TableCell>
                  <TableCell className="text-control-light">
                    {p.domain || "—"}
                  </TableCell>
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
                                title: t(
                                  "settings.identity-providers.unsupported-type"
                                ),
                              });
                              return;
                            }
                            crud.openEdit(p);
                          }}
                        >
                          <Pencil className="size-4" />
                        </Button>
                      )}
                      {canDelete && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => crud.openDelete(p)}
                        >
                          <Trash2 className="size-4 text-error" />
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {providers.length === 0 && !idpQuery.refreshing && (
                <TableRow>
                  <TableCell
                    colSpan={4}
                    className="text-center text-control-light py-8"
                  >
                    {t("settings.identity-providers.empty")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        )}
      </div>

      <ResourceSheet
        open={crud.createOpen}
        entity={null}
        title={t("settings.identity-providers.create-title")}
        description={t("settings.identity-providers.create-description")}
        submitting={crud.creating}
        submitLabel={t("settings.identity-providers.create")}
        onClose={crud.closeCreate}
        renderForm={({ formId }) => (
          <IdpFormFields
            entity={null}
            formId={formId}
            onSubmit={(form) => {
              void handleCreateForm(form);
            }}
          />
        )}
      />

      <ResourceSheet
        open={crud.editOpen}
        entity={crud.editTarget}
        title={(target) =>
          t("settings.identity-providers.edit-title", {
            title: target?.title ?? "",
          })
        }
        description={t("settings.identity-providers.edit-description")}
        submitting={crud.saving}
        onClose={crud.closeEdit}
        renderForm={({ entity, formId }) =>
          entity ? (
            <IdpFormFields
              entity={entity}
              formId={formId}
              onSubmit={(form) => {
                void handleSaveForm(form);
              }}
            />
          ) : null
        }
      />

      <ConfirmActionDialog
        open={crud.deleteOpen}
        onClose={crud.closeDelete}
        busy={crud.deleting}
        title={t("settings.identity-providers.delete-confirm-title")}
        description={t(
          "settings.identity-providers.delete-confirm-description",
          {
            title: crud.deleteTarget?.title ?? "",
          }
        )}
        onConfirm={() => {
          void handleDelete();
        }}
      />
    </SettingsPage>
  );
}

interface IdpFormFieldsProps {
  // null seeds an empty create form; an identity provider seeds its edit form.
  entity: IdentityProvider | null;
  formId: string;
  onSubmit: (form: IdpForm) => void;
}

// Inner form of the identity-provider drawer. Mounts fresh per open
// (ResourceSheet keys on the open sequence), so the create form never
// inherits the last-edited entity's "keep existing secret" placeholders and
// hints, which the previously shared renderFormFields leaked (01-B4).
function IdpFormFields({ entity, formId, onSubmit }: IdpFormFieldsProps) {
  const { t } = useTranslation();
  const [form, setForm] = useState<IdpForm>(() =>
    entity ? idpToForm(entity) : emptyForm()
  );

  return (
    <form
      id={formId}
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(form);
      }}
    >
      <FieldRow label={t("settings.identity-providers.field-title")}>
        <Input
          value={form.title}
          placeholder={t("settings.identity-providers.field-title-placeholder")}
          onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))}
        />
      </FieldRow>
      <FieldRow
        label={t("settings.identity-providers.field-domain")}
        hint={t("settings.identity-providers.field-domain-hint")}
      >
        <Input
          value={form.domain}
          placeholder={t(
            "settings.identity-providers.field-domain-placeholder"
          )}
          onChange={(e) => setForm((f) => ({ ...f, domain: e.target.value }))}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-client-id")}>
        <Input
          value={form.clientId}
          placeholder={t(
            "settings.identity-providers.field-client-id-placeholder"
          )}
          onChange={(e) => setForm((f) => ({ ...f, clientId: e.target.value }))}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-client-secret")}>
        <SecretInput
          value={form.clientSecret}
          placeholder={
            entity
              ? t("settings.identity-providers.field-client-secret-placeholder")
              : undefined
          }
          onChange={(e) =>
            setForm((f) => ({ ...f, clientSecret: e.target.value }))
          }
        />
        {entity && form.clientSecret === "" && (
          <p className="mt-1 text-xs text-control-light">
            {t("settings.identity-providers.field-client-secret-kept")}
          </p>
        )}
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-auth-url")}>
        <Input
          value={form.authUrl}
          placeholder={t(
            "settings.identity-providers.field-auth-url-placeholder"
          )}
          onChange={(e) => setForm((f) => ({ ...f, authUrl: e.target.value }))}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-token-url")}>
        <Input
          value={form.tokenUrl}
          placeholder={t(
            "settings.identity-providers.field-token-url-placeholder"
          )}
          onChange={(e) => setForm((f) => ({ ...f, tokenUrl: e.target.value }))}
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-user-info-url")}>
        <Input
          value={form.userInfoUrl}
          placeholder={t(
            "settings.identity-providers.field-user-info-url-placeholder"
          )}
          onChange={(e) =>
            setForm((f) => ({ ...f, userInfoUrl: e.target.value }))
          }
        />
      </FieldRow>
      <FieldRow label={t("settings.identity-providers.field-scopes")}>
        <Input
          value={form.scopes}
          placeholder={t(
            "settings.identity-providers.field-scopes-placeholder"
          )}
          onChange={(e) => setForm((f) => ({ ...f, scopes: e.target.value }))}
        />
      </FieldRow>
      <FieldRow
        label={t("settings.identity-providers.field-identifier-mapping")}
      >
        <Input
          value={form.identifier}
          placeholder={t(
            "settings.identity-providers.field-identifier-mapping-placeholder"
          )}
          onChange={(e) =>
            setForm((f) => ({ ...f, identifier: e.target.value }))
          }
        />
      </FieldRow>
      <FieldRow
        label={t("settings.identity-providers.field-display-name-mapping")}
      >
        <Input
          value={form.displayName}
          placeholder={t(
            "settings.identity-providers.field-display-name-mapping-placeholder"
          )}
          onChange={(e) =>
            setForm((f) => ({ ...f, displayName: e.target.value }))
          }
        />
      </FieldRow>
    </form>
  );
}
