import { fireEvent, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import { renderWithQueryClient } from "@/test/query";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";
import type { Provisioner } from "@/types/proto-es/v1/provisioner_pb";
import { SettingsProvisionerDetailPage } from "./settings-provisioner-detail";

const mock = vi.hoisted(() => ({
  getProvisioner: vi.fn(),
  listProvisioners: vi.fn(),
  listMachines: vi.fn(),
  getProvisionerIamPolicy: vi.fn(),
  setProvisionerIamPolicy: vi.fn(),
  listGroups: vi.fn(),
}));

vi.mock("@/connect", () => ({
  provisionerServiceClient: {
    getProvisioner: mock.getProvisioner,
    listProvisioners: mock.listProvisioners,
  },
  machineServiceClient: {
    listMachines: mock.listMachines,
  },
  iamServiceClient: {
    getProvisionerIamPolicy: mock.getProvisionerIamPolicy,
    setProvisionerIamPolicy: mock.setProvisionerIamPolicy,
  },
  groupServiceClient: {
    listGroups: mock.listGroups,
  },
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function provisioner(): Provisioner {
  return {
    name: "provisioners/p1",
    title: "Prod Cluster",
    backend: "kubernetes",
    description: "main cluster",
    status: {
      connected: true,
      version: "v0.0.9",
      autoUpgrade: true,
      retainData: true,
    },
    machineCount: 2,
    createdAt: { seconds: BigInt(1700000000), nanos: 0 },
  } as unknown as Provisioner;
}

function machine(id: string, title: string): MachineSummary {
  return {
    name: `machines/${id}`,
    title,
    createdAt: { seconds: BigInt(1700000100), nanos: 0 },
    provisioning: { phase: 3 }, // PROVISIONED
  } as unknown as MachineSummary;
}

function renderPage() {
  return renderWithQueryClient(
    <MemoryRouter initialEntries={["/settings/provisioners/p1"]}>
      <Routes>
        <Route
          path="/settings/provisioners/:provisionerId"
          element={<SettingsProvisionerDetailPage />}
        />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  useAppStore.getState().reset();
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      title: "Admin",
      permissions: ["laelia.provisioners.get"],
    } as never,
  });
  mock.getProvisioner.mockReset();
  mock.listProvisioners.mockReset();
  mock.listMachines.mockReset();
  mock.getProvisionerIamPolicy.mockReset();
  mock.setProvisionerIamPolicy.mockReset();
  mock.listGroups.mockReset();
  mock.getProvisioner.mockResolvedValue(provisioner());
  mock.listMachines.mockResolvedValue({
    machines: [machine("m1", "Machine One"), machine("m2", "Machine Two")],
    nextPageToken: "",
  });
  mock.getProvisionerIamPolicy.mockResolvedValue({
    policy: { bindings: [] },
    etag: "v1",
  });
  mock.listGroups.mockResolvedValue({ groups: [], nextPageToken: "" });
});

describe("settings-provisioner-detail", () => {
  it("shows the permission notice without the get permission", async () => {
    useAppStore.setState({
      currentUser: { name: "users/2", permissions: [] } as never,
    });

    renderPage();

    expect(
      await screen.findByText("settings.provisioners.not-allowed")
    ).toBeInTheDocument();
    expect(mock.getProvisioner).not.toHaveBeenCalled();
  });

  it("shows the provisioner's basic info and its machines", async () => {
    renderPage();

    expect(await screen.findByText("Prod Cluster")).toBeInTheDocument();
    expect(screen.getByText("kubernetes")).toBeInTheDocument();
    expect(
      screen.getByText("settings.provisioners.status-connected")
    ).toBeInTheDocument();
    expect(screen.getByText("v0.0.9")).toBeInTheDocument();
    expect(screen.getByText("common.yes")).toBeInTheDocument();

    expect(await screen.findByText("Machine One")).toBeInTheDocument();
    expect(screen.getByText("Machine Two")).toBeInTheDocument();

    await waitFor(() => {
      expect(mock.listMachines).toHaveBeenCalledTimes(1);
      expect(mock.listMachines.mock.calls[0][0].provisioner).toBe(
        "provisioners/p1"
      );
    });
  });

  it("shows an empty state when the provisioner has no machines", async () => {
    mock.listMachines.mockResolvedValue({ machines: [], nextPageToken: "" });

    renderPage();

    expect(
      await screen.findByText("settings.provisioner-detail.no-machines")
    ).toBeInTheDocument();
  });

  it("shows not-found when the provisioner is missing", async () => {
    mock.getProvisioner.mockResolvedValue(undefined);

    renderPage();

    expect(
      await screen.findByText("settings.provisioner-detail.not-found")
    ).toBeInTheDocument();
  });

  it("hides the access card without the admin permission", async () => {
    renderPage();

    await screen.findByText("Prod Cluster");

    expect(
      screen.queryByText("settings.provisioner-detail.access-title")
    ).not.toBeInTheDocument();
    expect(mock.getProvisionerIamPolicy).not.toHaveBeenCalled();
  });

  it("shows who can create machines and opens the manage sheet for an admin", async () => {
    useAppStore.setState({
      currentUser: {
        name: "users/1",
        title: "Admin",
        permissions: ["laelia.provisioners.get", "laelia.provisioners.delete"],
      } as never,
    });
    mock.getProvisionerIamPolicy.mockResolvedValue({
      policy: {
        bindings: [
          {
            role: "roles/provisionerMachineCreator",
            members: ["users/1", "groups/eng@example.com"],
          },
        ],
      },
      etag: "v1",
    });

    renderPage();

    expect(
      await screen.findByText("settings.provisioner-detail.access-title")
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(mock.getProvisionerIamPolicy).toHaveBeenCalledTimes(1);
      expect(mock.getProvisionerIamPolicy.mock.calls[0][0].name).toBe(
        "provisioners/p1"
      );
    });
    expect(await screen.findByText("users/1")).toBeInTheDocument();

    // Opening the manage sheet populates the bound members.
    fireEvent.click(
      screen.getByText("settings.provisioner-detail.access-manage")
    );
    expect(
      await screen.findByText("settings.provisioner-detail.access-manage-title")
    ).toBeInTheDocument();
  });
});
