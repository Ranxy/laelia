import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import type { ApiProvider } from "@/types/proto-es/v1/api_provider_service_pb";
import { SettingsApiProvidersPage } from "./settings-api-providers";

const mock = vi.hoisted(() => ({
  listAPIProviders: vi.fn(),
  createAPIProvider: vi.fn(),
  updateAPIProvider: vi.fn(),
  deleteAPIProvider: vi.fn(),
  listAPIProviderModels: vi.fn(),
  listUsers: vi.fn(),
  listGroups: vi.fn(),
}));

vi.mock("@/connect", () => ({
  apiProviderServiceClient: {
    listAPIProviders: mock.listAPIProviders,
    createAPIProvider: mock.createAPIProvider,
    updateAPIProvider: mock.updateAPIProvider,
    deleteAPIProvider: mock.deleteAPIProvider,
    listAPIProviderModels: mock.listAPIProviderModels,
  },
  userServiceClient: { listUsers: mock.listUsers },
  groupServiceClient: { listGroups: mock.listGroups },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

vi.mock("@/lib/connect-errors", () => ({
  describeError: (err: unknown) =>
    err instanceof Error ? err.message : String(err),
}));

function provider(overrides?: Partial<ApiProvider>): ApiProvider {
  return {
    name: "apiProviders/deepseek",
    title: "DeepSeek",
    providerType: "deepseek",
    baseUrl: "https://api.deepseek.com",
    description: "Main provider",
    members: ["allUsers"],
    entries: [
      {
        name: "apiProviders/deepseek/entries/1",
        label: "chat",
        model: "deepseek-chat",
        maskedApiKey: "sk-***",
      },
    ],
    ...overrides,
  } as unknown as ApiProvider;
}

function renderPage() {
  return render(<SettingsApiProvidersPage />);
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.apiProviders.list",
        "laelia.apiProviders.create",
        "laelia.apiProviders.update",
      ],
    } as never,
  });
  mock.listAPIProviders.mockReset();
  mock.createAPIProvider.mockReset();
  mock.updateAPIProvider.mockReset();
  mock.deleteAPIProvider.mockReset();
  mock.listAPIProviderModels.mockReset();
  mock.listUsers.mockReset();
  mock.listGroups.mockReset();
  mock.listAPIProviders.mockResolvedValue({ apiProviders: [] });
  mock.createAPIProvider.mockResolvedValue({});
  mock.updateAPIProvider.mockResolvedValue({});
  mock.deleteAPIProvider.mockResolvedValue({});
  mock.listAPIProviderModels.mockResolvedValue({ models: [] });
  mock.listUsers.mockResolvedValue({ users: [] });
  mock.listGroups.mockResolvedValue({ groups: [] });
  toastMock.add.mockReset();
});

describe("settings-api-providers", () => {
  it("shows the permission notice without the list permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", title: "User", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.api-providers.not-allowed")
    ).toBeInTheDocument();
    expect(mock.listAPIProviders).not.toHaveBeenCalled();
  });

  it("renders the provider table", async () => {
    mock.listAPIProviders.mockResolvedValue({ apiProviders: [provider()] });

    renderPage();

    expect(await screen.findByText("DeepSeek")).toBeInTheDocument();
    expect(screen.getByText("deepseek")).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    renderPage();

    expect(
      await screen.findByText("settings.api-providers.no-providers")
    ).toBeInTheDocument();
  });

  it("creates a provider with a fetched model entry", async () => {
    mock.listAPIProviderModels.mockResolvedValue({
      models: [{ id: "deepseek-chat", name: "DeepSeek Chat" }],
    });
    renderPage();

    fireEvent.click(await screen.findByText("settings.api-providers.create"));

    const title = await screen.findByPlaceholderText(
      "settings.api-providers.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "DeepSeek" } });

    // Fetch models with a key.
    const keyInput = screen.getByPlaceholderText(
      "settings.api-providers.field-fetch-key-placeholder"
    );
    fireEvent.change(keyInput, { target: { value: "sk-test" } });
    fireEvent.click(
      screen.getByRole("button", {
        name: "settings.api-providers.fetch-models",
      })
    );

    await waitFor(() => {
      expect(mock.listAPIProviderModels).toHaveBeenCalledWith(
        expect.objectContaining({ apiKey: "sk-test" })
      );
    });

    // Toggle the model checkbox to add an entry.
    const checkbox = await screen.findByRole("checkbox", {
      name: "DeepSeek Chat",
    });
    fireEvent.click(checkbox);

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.createAPIProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          apiProvider: expect.objectContaining({
            title: "DeepSeek",
            entries: [
              expect.objectContaining({
                model: "deepseek-chat",
                apiKey: "sk-test",
              }),
            ],
          }),
        })
      );
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });

  it("creates a custom provider with a base URL", async () => {
    mock.listAPIProviderModels.mockResolvedValue({
      models: [{ id: "my-model", name: "My Model" }],
    });
    renderPage();

    fireEvent.click(await screen.findByText("settings.api-providers.create"));

    const title = await screen.findByPlaceholderText(
      "settings.api-providers.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "My Custom" } });

    // Switch the provider type to custom.
    const typeSelect = screen.getAllByRole("combobox")[0];
    fireEvent.click(typeSelect);
    const customItem = await screen.findByText(
      "settings.api-providers.type-custom"
    );
    fireEvent.pointerDown(customItem);
    fireEvent.pointerUp(customItem);
    fireEvent.click(customItem);

    // Base URL field appears and is required.
    const baseUrl = await screen.findByPlaceholderText(
      "settings.api-providers.field-base-url-placeholder"
    );
    fireEvent.change(baseUrl, { target: { value: "https://example.com/v1" } });

    // Fetch models with a key.
    const keyInput = screen.getByPlaceholderText(
      "settings.api-providers.field-fetch-key-placeholder"
    );
    fireEvent.change(keyInput, { target: { value: "sk-test" } });
    fireEvent.click(
      screen.getByRole("button", {
        name: "settings.api-providers.fetch-models",
      })
    );

    await waitFor(() => {
      expect(mock.listAPIProviderModels).toHaveBeenCalledWith(
        expect.objectContaining({
          providerType: "custom",
          baseUrl: "https://example.com/v1",
          apiKey: "sk-test",
        })
      );
    });

    const checkbox = await screen.findByRole("checkbox", {
      name: "My Model",
    });
    fireEvent.click(checkbox);

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.createAPIProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          apiProvider: expect.objectContaining({
            title: "My Custom",
            providerType: "custom",
            baseUrl: "https://example.com/v1",
            entries: [
              expect.objectContaining({
                model: "my-model",
                apiKey: "sk-test",
              }),
            ],
          }),
        })
      );
    });
  });

  it("requires a base URL for a custom provider", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("settings.api-providers.create"));

    const title = await screen.findByPlaceholderText(
      "settings.api-providers.field-title-placeholder"
    );
    fireEvent.change(title, { target: { value: "My Custom" } });

    const typeSelect = screen.getAllByRole("combobox")[0];
    fireEvent.click(typeSelect);
    const customItem = await screen.findByText(
      "settings.api-providers.type-custom"
    );
    fireEvent.pointerDown(customItem);
    fireEvent.pointerUp(customItem);
    fireEvent.click(customItem);

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "settings.api-providers.base-url-required",
        })
      );
    });
    expect(mock.createAPIProvider).not.toHaveBeenCalled();
  });

  it("requires a title when creating", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("settings.api-providers.create"));

    fireEvent.click(await screen.findByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "settings.api-providers.title-required",
        })
      );
    });
    expect(mock.createAPIProvider).not.toHaveBeenCalled();
  });

  it("edits a provider", async () => {
    mock.listAPIProviders.mockResolvedValue({ apiProviders: [provider()] });
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.edit"));

    const title = await screen.findByDisplayValue("DeepSeek");
    fireEvent.change(title, { target: { value: "DeepSeek Pro" } });

    fireEvent.click(screen.getByRole("button", { name: "common.save" }));

    await waitFor(() => {
      expect(mock.updateAPIProvider).toHaveBeenCalledWith(
        expect.objectContaining({
          apiProvider: expect.objectContaining({
            name: "apiProviders/deepseek",
            title: "DeepSeek Pro",
          }),
          updateMask: {
            paths: ["title", "base_url", "description", "entries", "members"],
          },
        })
      );
    });
  });

  it("deletes a provider after confirmation", async () => {
    mock.listAPIProviders.mockResolvedValue({ apiProviders: [provider()] });
    renderPage();

    fireEvent.click(await screen.findByLabelText("common.delete"));

    fireEvent.click(
      await screen.findByRole("button", { name: "common.delete" })
    );

    await waitFor(() => {
      expect(mock.deleteAPIProvider).toHaveBeenCalledWith({
        name: "apiProviders/deepseek",
      });
    });
    expect(toastMock.add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" })
    );
  });
});
