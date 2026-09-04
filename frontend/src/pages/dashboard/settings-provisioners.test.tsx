import { fireEvent, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { renderWithQueryClient } from "@/test/query";
import type { Provisioner } from "@/types/proto-es/v1/provisioner_pb";
import { SettingsProvisionersPage } from "./settings-provisioners";

const mock = vi.hoisted(() => ({
  listProvisioners: vi.fn(),
  createProvisioner: vi.fn(),
  rotateProvisionerToken: vi.fn(),
  deleteProvisioner: vi.fn(),
  getProvisioningConfig: vi.fn(),
  listMachines: vi.fn(),
}));

vi.mock("@/connect", () => ({
  provisionerServiceClient: {
    listProvisioners: mock.listProvisioners,
    createProvisioner: mock.createProvisioner,
    rotateProvisionerToken: mock.rotateProvisionerToken,
    deleteProvisioner: mock.deleteProvisioner,
  },
  settingServiceClient: {
    getSetting: mock.getProvisioningConfig,
  },
  machineServiceClient: {
    listMachines: mock.listMachines,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

const toastMock = vi.hoisted(() => ({ add: vi.fn() }));
vi.mock("@/lib/toast", () => ({ toastManager: toastMock }));

function provisioner(overrides?: Partial<Provisioner>): Provisioner {
  return {
    name: "provisioners/p1",
    title: "Prod Cluster",
    backend: "kubernetes",
    description: "main cluster",
    status: {
      connected: true,
      version: "v0.0.9",
      autoUpgrade: true,
    },
    machineCount: 3,
    createdAt: { seconds: BigInt(1700000000), nanos: 0 },
    ...overrides,
  } as unknown as Provisioner;
}

function renderPage() {
  return renderWithQueryClient(
    <MemoryRouter initialEntries={["/settings/provisioners"]}>
      <Routes>
        <Route
          path="/settings/provisioners"
          element={<SettingsProvisionersPage />}
        />
        {/* Row clicks navigate to the detail path; keep the page mounted so
            the rotate/delete actions stay reachable in the test. */}
        <Route
          path="/settings/provisioners/:provisionerId"
          element={<SettingsProvisionersPage />}
        />
        <Route
          path="/settings/provisioners/:provisionerId/cleanup"
          element={<div>cleanup-page</div>}
        />
      </Routes>
    </MemoryRouter>
  );
}

// provisioning config with the given runtime image.
function provisioningConfig(runtimeImage: string) {
  return {
    value: { value: { case: "provisioning", value: { runtimeImage } } },
  };
}

beforeEach(() => {
  useAppStore.getState().reset();
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: [
        "laelia.provisioners.get",
        "laelia.provisioners.create",
        "laelia.provisioners.delete",
      ],
    } as never,
  });
  mock.listProvisioners.mockReset();
  mock.createProvisioner.mockReset();
  mock.rotateProvisionerToken.mockReset();
  mock.deleteProvisioner.mockReset();
  mock.getProvisioningConfig.mockReset();
  mock.listMachines.mockReset();
  mock.listProvisioners.mockResolvedValue({ provisioners: [] });
  // No bound machines by default so the delete confirm stays enabled.
  mock.listMachines.mockResolvedValue({ machines: [], nextPageToken: "" });
  // A runtime image is configured by default so the non-gating tests exercise
  // the normal flow.
  mock.getProvisioningConfig.mockResolvedValue(
    provisioningConfig("registry.example.com/laelia/machine-runtime:1.2.3")
  );
  toastMock.add.mockReset();
});

describe("settings-provisioners", () => {
  it("shows the permission notice without the get permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.provisioners.not-allowed")
    ).toBeInTheDocument();
    expect(mock.listProvisioners).not.toHaveBeenCalled();
  });

  it("renders the provisioner table", async () => {
    mock.listProvisioners.mockResolvedValue({
      provisioners: [provisioner()],
    });

    renderPage();

    expect(await screen.findByText("Prod Cluster")).toBeInTheDocument();
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioners.status-connected")
    ).toBeInTheDocument();
    expect(screen.getByText("v0.0.9")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("disables add and prompts to configure the runtime image when none is set", async () => {
    mock.getProvisioningConfig.mockResolvedValue(provisioningConfig(""));

    renderPage();

    expect(
      await screen.findByText("settings.provisioners.runtime-image-required")
    ).toBeInTheDocument();
    const add = await screen.findByText("settings.provisioners.create");
    expect(add.closest("button")).toBeDisabled();
    expect(add.closest("button")).toHaveAccessibleName(
      "settings.provisioners.create"
    );
  });

  it("creates a provisioner and shows the one-time token dialog", async () => {
    mock.createProvisioner.mockResolvedValue({
      provisioner: provisioner(),
      token: "llprov_secret",
    });

    renderPage();

    fireEvent.click(await screen.findByText("settings.provisioners.create"));
    // FieldRow appends the required asterisk to the label text.
    fireEvent.change(
      screen.getByLabelText(/settings\.provisioners\.field-title/),
      { target: { value: "Prod Cluster" } }
    );
    fireEvent.click(screen.getByText("common.create"));

    await waitFor(() => {
      expect(mock.createProvisioner).toHaveBeenCalledTimes(1);
    });
    const req = mock.createProvisioner.mock.calls[0][0];
    expect(req.provisioner?.title).toBe("Prod Cluster");
    expect(req.provisioner?.backend).toBe("kubernetes");
    expect(req.provisioner?.description).toBe("");
    expect(
      await screen.findByDisplayValue("llprov_secret")
    ).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioners.token-warning")
    ).toBeInTheDocument();
  });

  it("refuses to create without a title", async () => {
    renderPage();

    fireEvent.click(await screen.findByText("settings.provisioners.create"));
    fireEvent.click(screen.getByText("common.create"));

    await waitFor(() => {
      expect(toastMock.add).toHaveBeenCalledWith({
        type: "error",
        title: "settings.provisioners.title-required",
      });
    });
    expect(mock.createProvisioner).not.toHaveBeenCalled();
  });

  it("rotates the token and shows the new one-time token", async () => {
    mock.listProvisioners.mockResolvedValue({
      provisioners: [provisioner()],
    });
    mock.rotateProvisionerToken.mockResolvedValue({ token: "llprov_rotated" });

    renderPage();

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.click(screen.getByLabelText("settings.provisioners.rotate"));
    fireEvent.click(screen.getByText("settings.provisioners.rotate"));

    await waitFor(() => {
      expect(mock.rotateProvisionerToken).toHaveBeenCalledTimes(1);
      expect(mock.rotateProvisionerToken.mock.calls[0][0].name).toBe(
        "provisioners/p1"
      );
    });
    expect(
      await screen.findByDisplayValue("llprov_rotated")
    ).toBeInTheDocument();
  });

  it("shows the machines-bound refusal inline when delete fails", async () => {
    mock.listProvisioners.mockResolvedValue({
      provisioners: [provisioner()],
    });
    mock.deleteProvisioner.mockRejectedValue(
      new Error("2 machines still bound")
    );

    renderPage();

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.click(screen.getByLabelText("common.delete"));
    fireEvent.click(screen.getByText("common.delete"));

    expect(
      await screen.findByText("2 machines still bound")
    ).toBeInTheDocument();
    // The dialog stays open so the refusal is visible next to the actions.
    expect(
      screen.getByText("settings.provisioners.delete-confirm-title")
    ).toBeInTheDocument();
  });

  it("lists the bound machines and disables delete while any exist", async () => {
    mock.listProvisioners.mockResolvedValue({
      provisioners: [provisioner()],
    });
    mock.listMachines.mockResolvedValue({
      machines: [
        { name: "machines/m1", title: "Machine One" },
        { name: "machines/m2", title: "Machine Two" },
      ],
      nextPageToken: "",
    });

    renderPage();

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.click(screen.getByLabelText("common.delete"));

    expect(
      await screen.findByText("settings.provisioners.delete-machines-bound")
    ).toBeInTheDocument();
    expect(screen.getByText("Machine One")).toBeInTheDocument();
    expect(screen.getByText("Machine Two")).toBeInTheDocument();
    const confirm = screen.getByText("common.delete").closest("button");
    expect(confirm).toBeDisabled();
    expect(mock.deleteProvisioner).not.toHaveBeenCalled();
  });

  it("deletes a provisioner, refreshes the table, and opens the cleanup guide", async () => {
    mock.listProvisioners.mockResolvedValue({
      provisioners: [provisioner()],
    });
    mock.deleteProvisioner.mockResolvedValue({});

    renderPage();

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.click(screen.getByLabelText("common.delete"));
    fireEvent.click(screen.getByText("common.delete"));

    await waitFor(() => {
      expect(mock.deleteProvisioner).toHaveBeenCalledTimes(1);
      expect(mock.deleteProvisioner.mock.calls[0][0].name).toBe(
        "provisioners/p1"
      );
      expect(toastMock.add).toHaveBeenCalledWith({
        type: "success",
        title: "settings.provisioners.deleted",
      });
    });
    // After deletion the user is taken to the full-page cleanup guide.
    expect(await screen.findByText("cleanup-page")).toBeInTheDocument();
  });
});
