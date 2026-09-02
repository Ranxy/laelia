import { fireEvent, screen, waitFor } from "@testing-library/react";
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
}));

vi.mock("@/connect", () => ({
  provisionerServiceClient: {
    listProvisioners: mock.listProvisioners,
    createProvisioner: mock.createProvisioner,
    rotateProvisionerToken: mock.rotateProvisionerToken,
    deleteProvisioner: mock.deleteProvisioner,
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
  return renderWithQueryClient(<SettingsProvisionersPage />);
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
  mock.listProvisioners.mockResolvedValue({ provisioners: [] });
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

  it("deletes a provisioner and refreshes the table", async () => {
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
  });
});
