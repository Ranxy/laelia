import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { describeError } from "@/lib/connect-errors";
import { useAppStore } from "@/stores";
import { renderWithQueryClient } from "@/test/query";
import type { IdentityProvider } from "@/types/proto-es/v1/idp_service_pb";
import { IdentityProviderType } from "@/types/proto-es/v1/idp_service_pb";
import { SettingsIdentityProvidersPage } from "./settings-identity-providers";

const mock = vi.hoisted(() => ({
  listIdentityProviders: vi.fn(),
  createIdentityProvider: vi.fn(),
  updateIdentityProvider: vi.fn(),
  deleteIdentityProvider: vi.fn(),
}));

vi.mock("@/connect", () => ({
  identityProviderServiceClient: {
    listIdentityProviders: mock.listIdentityProviders,
    createIdentityProvider: mock.createIdentityProvider,
    updateIdentityProvider: mock.updateIdentityProvider,
    deleteIdentityProvider: mock.deleteIdentityProvider,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/lib/connect-errors", () => ({
  describeError: (err: unknown) => describeError(err),
}));

function idp(overrides?: Partial<IdentityProvider>): IdentityProvider {
  return {
    name: "identityProviders/github",
    title: "GitHub",
    domain: "github.example.com",
    type: IdentityProviderType.OAUTH2,
    config: {
      config: {
        case: "oauth2Config",
        value: {
          clientId: "cid-1",
          authUrl: "https://github.com/login/oauth/authorize",
          tokenUrl: "https://github.com/login/oauth/access_token",
          userInfoUrl: "https://api.github.com/user",
          scopes: ["openid", "email", "profile"],
          fieldMapping: { identifier: "email", displayName: "name" },
          skipTlsVerify: false,
          authStyle: 1,
        },
      },
    },
    ...overrides,
  } as unknown as IdentityProvider;
}

function renderPage() {
  return renderWithQueryClient(<SettingsIdentityProvidersPage />);
}

// The client-secret input only carries a placeholder in edit mode (create
// passes none), so it is located row-relative: its FieldRow label text is
// stable in both modes and the row holds exactly one textbox.
function secretField(): HTMLInputElement {
  const row = screen
    .getByText("settings.identity-providers.field-client-secret")
    .closest("div") as HTMLElement;
  return within(row).getByRole("textbox") as HTMLInputElement;
}

// Row action buttons are icon-only without aria-labels: [0] edit, [1] delete.
// Waits for the table row first — the list renders only after the query
// resolves, so a synchronous getByText would race the initial load.
async function openRowAction(index: number, domain = "github.example.com") {
  const row = (await screen.findByText(domain)).closest("tr") as HTMLElement;
  fireEvent.click(within(row).getAllByRole("button")[index]);
}

// Submits the create sheet via its footer button (scoped to the sheet dialog
// because the page header's create button shares the accessible name).
function submitCreate() {
  const dialog = screen.getByRole("dialog");
  fireEvent.click(
    within(dialog).getByRole("button", {
      name: "settings.identity-providers.create",
    })
  );
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.identityProviders.list",
        "laelia.identityProviders.create",
        "laelia.identityProviders.update",
        "laelia.identityProviders.delete",
      ],
    } as never,
  });
  mock.listIdentityProviders.mockReset();
  mock.createIdentityProvider.mockReset();
  mock.updateIdentityProvider.mockReset();
  mock.deleteIdentityProvider.mockReset();
  mock.listIdentityProviders.mockResolvedValue({ identityProviders: [] });
  mock.createIdentityProvider.mockResolvedValue({});
  mock.updateIdentityProvider.mockResolvedValue({});
  mock.deleteIdentityProvider.mockResolvedValue({});
  toastMock.add.mockReset();
});

describe("settings-identity-providers", () => {
  it("shows the permission notice without the identityProviders.list permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.identity-providers.not-allowed")
    ).toBeInTheDocument();
    expect(mock.listIdentityProviders).not.toHaveBeenCalled();
  });

  it("renders the provider table", async () => {
    mock.listIdentityProviders.mockResolvedValue({
      identityProviders: [idp()],
    });
    renderPage();

    expect(await screen.findByText("GitHub")).toBeInTheDocument();
    expect(
      screen.getByText("settings.identity-providers.type-oauth2")
    ).toBeInTheDocument();
    expect(screen.getByText("github.example.com")).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    renderPage();

    expect(
      await screen.findByText("settings.identity-providers.empty")
    ).toBeInTheDocument();
  });

  it("creates a provider with a slugified id and oauth2 config", async () => {
    renderPage();

    fireEvent.click(
      await screen.findByText("settings.identity-providers.create")
    );

    const title = await screen.findByPlaceholderText(
      "settings.identity-providers.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "GitHub Auth" } });
    fireEvent.change(
      screen.getByPlaceholderText(
        "settings.identity-providers.field-domain-placeholder"
      ),
      { target: { value: "Corp.Example.COM" } }
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "settings.identity-providers.field-client-id-placeholder"
      ),
      { target: { value: "cid-1" } }
    );
    fireEvent.change(secretField(), { target: { value: "sekret" } });
    fireEvent.change(
      screen.getByPlaceholderText(
        "settings.identity-providers.field-auth-url-placeholder"
      ),
      { target: { value: "https://auth.example.com/authorize" } }
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "settings.identity-providers.field-token-url-placeholder"
      ),
      { target: { value: "https://auth.example.com/token" } }
    );
    fireEvent.change(
      screen.getByPlaceholderText(
        "settings.identity-providers.field-user-info-url-placeholder"
      ),
      { target: { value: "https://user.example.com/me" } }
    );

    submitCreate();

    await waitFor(() => {
      expect(mock.createIdentityProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          identityProviderId: "github-auth",
          identityProvider: expect.objectContaining({
            title: "GitHub Auth",
            domain: "corp.example.com",
            type: IdentityProviderType.OAUTH2,
            config: {
              config: {
                case: "oauth2Config",
                value: expect.objectContaining({
                  clientId: "cid-1",
                  clientSecret: "sekret",
                  authUrl: "https://auth.example.com/authorize",
                  tokenUrl: "https://auth.example.com/token",
                  userInfoUrl: "https://user.example.com/me",
                  scopes: ["openid", "email", "profile"],
                  fieldMapping: { identifier: "email", displayName: "name" },
                  skipTlsVerify: false,
                  authStyle: 1, // IN_PARAMS
                }),
              },
            },
          }),
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "settings.identity-providers.created",
      })
    );
  });

  it("requires a title when creating", async () => {
    renderPage();
    fireEvent.click(
      await screen.findByText("settings.identity-providers.create")
    );

    submitCreate();

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "error",
          title: "settings.identity-providers.field-title",
        })
      );
    });
    expect(mock.createIdentityProvider).not.toHaveBeenCalled();
  });

  it("edits a provider and keeps the stored secret when untouched", async () => {
    mock.listIdentityProviders.mockResolvedValue({
      identityProviders: [idp()],
    });
    renderPage();

    await openRowAction(0);

    // Edit seeds the OAuth fields and shows the keep-existing secret
    // affordances while the secret input stays empty.
    expect(await screen.findByDisplayValue("cid-1")).toBeInTheDocument();
    const secret = screen.getByPlaceholderText(
      "settings.identity-providers.field-client-secret-placeholder"
    ) as HTMLInputElement;
    expect(secret.value).toBe("");
    expect(
      screen.getByText("settings.identity-providers.field-client-secret-kept")
    ).toBeInTheDocument();

    fireEvent.change(screen.getByDisplayValue("GitHub"), {
      target: { value: "GitHub SSO" },
    });

    // Save without touching the secret field.
    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateIdentityProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          identityProvider: expect.objectContaining({
            name: "identityProviders/github",
            title: "GitHub SSO",
            domain: "github.example.com",
            type: IdentityProviderType.OAUTH2,
            config: {
              config: {
                case: "oauth2Config",
                value: expect.objectContaining({
                  clientId: "cid-1",
                  // Untouched secret is sent empty; the server treats an
                  // empty secret as "keep the stored one" (the hint above).
                  clientSecret: "",
                  scopes: ["openid", "email", "profile"],
                  fieldMapping: { identifier: "email", displayName: "name" },
                  skipTlsVerify: false,
                  authStyle: 1,
                }),
              },
            },
          }),
          updateMask: { paths: ["title", "domain", "config"] },
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "settings.identity-providers.saved",
      })
    );
  });

  it("does not leak keep-existing secret hints into the create form (01-B4)", async () => {
    mock.listIdentityProviders.mockResolvedValue({
      identityProviders: [idp()],
    });
    renderPage();

    // Open edit on the seeded provider: keep-existing placeholder + hint.
    await openRowAction(0);
    expect(
      await screen.findByPlaceholderText(
        "settings.identity-providers.field-client-secret-placeholder"
      )
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.identity-providers.field-client-secret-kept")
    ).toBeInTheDocument();

    // Close the edit sheet through the sheet's built-in close button.
    fireEvent.click(screen.getByRole("button", { name: "common.close" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });

    // Open create: the fresh form must not show the edit-only affordances.
    fireEvent.click(
      screen.getByRole("button", {
        name: "settings.identity-providers.create",
      })
    );
    expect(
      await screen.findByPlaceholderText(
        "settings.identity-providers.field-title-placeholder"
      )
    ).toBeInTheDocument();
    const secret = secretField();
    expect(secret.value).toBe("");
    expect(secret.placeholder).toBe("");
    expect(
      screen.queryByText("settings.identity-providers.field-client-secret-kept")
    ).not.toBeInTheDocument();
  });

  it("deletes a provider after confirmation", async () => {
    mock.listIdentityProviders.mockResolvedValue({
      identityProviders: [idp()],
    });
    renderPage();

    await openRowAction(1);

    expect(
      await screen.findByText(
        "settings.identity-providers.delete-confirm-title"
      )
    ).toBeInTheDocument();
    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => {
      expect(mock.deleteIdentityProvider).toHaveBeenCalledWith({
        name: "identityProviders/github",
      });
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({
        type: "success",
        title: "settings.identity-providers.deleted",
      })
    );
  });

  it("refuses to edit a non-oauth2 provider", async () => {
    mock.listIdentityProviders.mockResolvedValue({
      identityProviders: [idp({ type: IdentityProviderType.LDAP })],
    });
    renderPage();

    await openRowAction(0);

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith({
        type: "error",
        title: "settings.identity-providers.unsupported-type",
      });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(mock.updateIdentityProvider).not.toHaveBeenCalled();
  });
});
