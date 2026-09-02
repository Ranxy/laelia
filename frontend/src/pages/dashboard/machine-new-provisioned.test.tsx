import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mockRouter = vi.hoisted(() => ({ navigate: vi.fn() }));
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockRouter.navigate,
}));

import { useAppStore } from "@/stores";
import type { Provisioner } from "@/types/proto-es/v1/provisioner_pb";
import { MachineNewPage } from "./machine-new";
import { MachineNewProvisionedPanel } from "./machine-new-provisioned";

const mockedActions = vi.hoisted(() => ({
  fetchProvisioners: vi.fn(),
  provisionMachine: vi.fn(),
}));

function provisioner(
  name: string,
  title: string,
  overrides?: Partial<Provisioner>
): Provisioner {
  return {
    name,
    title,
    backend: "kubernetes",
    description: "",
    status: { connected: true, version: "v0.0.1" },
    machineCount: 2,
    ...overrides,
  } as unknown as Provisioner;
}

function setSession(permissions: string[]) {
  useAppStore.setState({
    currentUser: { name: "users/1", permissions } as never,
    provisioners: [],
    provisionersLoading: false,
    fetchProvisioners: mockedActions.fetchProvisioners,
    provisionMachine: mockedActions.provisionMachine,
  });
}

beforeEach(() => {
  useAppStore.getState().reset();
  setSession(["laelia.provisioners.provision"]);
  mockedActions.fetchProvisioners.mockReset();
  mockedActions.provisionMachine.mockReset();
  mockRouter.navigate.mockReset();
  mockedActions.fetchProvisioners.mockResolvedValue(undefined);
});

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("MachineNewPage tabs", () => {
  it("defaults to the provisioned tab when the permission is held", async () => {
    render(<MachineNewPage />);
    expect(
      await screen.findByText("machine.new.provisioned.pick-title")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("machine.new.step-install")
    ).not.toBeInTheDocument();
  });

  it("renders only the self-hosted flow without the provision permission", async () => {
    setSession([]);
    render(<MachineNewPage />);
    expect(
      await screen.findByText("machine.new.step-install")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("machine.new.tab-provisioned")
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText("machine.new.provisioned.pick-title")
    ).not.toBeInTheDocument();
  });
});

describe("MachineNewProvisionedPanel", () => {
  it("shows the empty state when no provisioner is registered", async () => {
    render(<MachineNewProvisionedPanel />);
    expect(
      await screen.findByText("machine.new.provisioned.empty")
    ).toBeInTheDocument();
    expect(mockedActions.fetchProvisioners).toHaveBeenCalled();
  });

  it("lists provisioners with backend and connection badges and creates on click", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({
      provisioners: [
        provisioner("provisioners/p1", "Prod Cluster"),
        provisioner("provisioners/p2", "Edge", {
          status: { connected: false, version: "" } as never,
        }),
      ],
    });
    render(<MachineNewProvisionedPanel />);

    expect(await screen.findByText("Prod Cluster")).toBeInTheDocument();
    expect(screen.getByText("Edge")).toBeInTheDocument();
    // Both connection states render.
    expect(
      screen.getAllByText("machine.new.provisioned.connected").length
    ).toBe(1);
    expect(screen.getAllByText("machine.new.provisioned.offline").length).toBe(
      1
    );

    // Create is gated on a selection + a title.
    const create = screen.getByText("machine.new.provisioned.create");
    expect(create).toBeDisabled();
    fireEvent.click(screen.getByText("Prod Cluster"));
    expect(create).toBeDisabled();
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "  Cloud Box  " },
    });
    expect(create).not.toBeDisabled();
    fireEvent.click(create);

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box"
      );
      expect(mockRouter.navigate).toHaveBeenCalledWith("/machines/m1");
    });
  });

  it("surfaces a provisioning failure inline without navigating", async () => {
    mockedActions.provisionMachine.mockRejectedValue(
      new Error("runtime image not configured")
    );
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));

    expect(
      await screen.findByText("runtime image not configured")
    ).toBeInTheDocument();
    expect(mockRouter.navigate).not.toHaveBeenCalled();
  });
});
