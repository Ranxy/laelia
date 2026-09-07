import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mockRouter = vi.hoisted(() => ({ navigate: vi.fn() }));
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockRouter.navigate,
}));

// The panel reads the custom-image switch from the public workspace policy
// (GetWorkspaceInfo); tests drive it through this mocked hook.
const mockedPolicy = vi.hoisted(() => ({
  signupDisallowed: false,
  requireEmailVerification: false,
  enforceIdentityDomain: false,
  allowedDomains: [] as string[],
  userCreateMachineDisallowed: false,
  allowCustomImages: true,
}));
vi.mock("@/hooks/use-workspace-policy", () => ({
  useWorkspacePolicy: () => mockedPolicy,
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
  // Permissive default: the custom-image field stays visible unless a test
  // flips the mocked workspace policy to allowCustomImages: false.
  mockedPolicy.allowCustomImages = true;
});

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("MachineNewPage tabs", () => {
  it("defaults to the provisioned tab when the permission is held and a provisioner is registered", async () => {
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewPage />);
    expect(
      await screen.findByText("machine.new.provisioned.pick-title")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("machine.new.step-install")
    ).not.toBeInTheDocument();
  });

  it("defaults to the self-hosted tab when the permission is held but no provisioner is registered", async () => {
    render(<MachineNewPage />);
    expect(
      await screen.findByText("machine.new.step-install")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("machine.new.provisioned.pick-title")
    ).not.toBeInTheDocument();
    // The provisioned tab is still reachable (where the empty state shows).
    expect(screen.getByText("machine.new.tab-provisioned")).toBeInTheDocument();
  });

  it("defaults to the self-hosted tab when provisioners exist but none are connected", async () => {
    useAppStore.setState({
      provisioners: [
        provisioner("provisioners/p1", "Prod Cluster", {
          status: { connected: false, version: "" } as never,
        }),
      ],
    });
    render(<MachineNewPage />);
    expect(
      await screen.findByText("machine.new.step-install")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("machine.new.provisioned.pick-title")
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

  it("shows the provisioned tab for a per-provisioner grant without the workspace permission", async () => {
    // A normal user bound to roles/provisionerMachineCreator on a provisioner
    // holds laelia.provisioners.provision per-resource, which is absent from the
    // workspace-scope permission set. The roster (filtered by the backend to
    // provisioners the caller may provision on) is what surfaces the tab.
    setSession([]);
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewPage />);
    expect(
      await screen.findByText("machine.new.tab-provisioned")
    ).toBeInTheDocument();
    expect(
      screen.getByText("machine.new.provisioned.pick-title")
    ).toBeInTheDocument();
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

  it("lists provisioners with backend and connection badges and creates once valid", async () => {
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
    // Offline provisioners are disabled and explained by the offline hint.
    expect(
      screen.getByText("machine.new.provisioned.offline-hint")
    ).toBeInTheDocument();
    const edgeOption = screen
      .getAllByRole("radio")
      .find((el) => el.textContent?.includes("Edge"));
    expect(edgeOption).toBeDisabled();

    const create = screen.getByText("machine.new.provisioned.create");
    // Clicking without a selection prompts instead of silently doing nothing.
    fireEvent.click(create);
    expect(
      screen.getByText("machine.new.provisioned.select-provisioner")
    ).toBeInTheDocument();
    expect(mockedActions.provisionMachine).not.toHaveBeenCalled();

    // An offline provisioner can't be selected, so it must never reach create.
    fireEvent.click(screen.getByText("Edge"));
    fireEvent.click(create);
    expect(
      screen.getByText("machine.new.provisioned.select-provisioner")
    ).toBeInTheDocument();
    expect(mockedActions.provisionMachine).not.toHaveBeenCalled();

    // Selecting a provisioner but leaving the name blank prompts for a name.
    fireEvent.click(screen.getByText("Prod Cluster"));
    fireEvent.click(create);
    expect(
      screen.getByText("machine.new.provisioned.enter-name")
    ).toBeInTheDocument();
    expect(mockedActions.provisionMachine).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "  Cloud Box  " },
    });
    fireEvent.click(create);

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        ""
      );
      expect(mockRouter.navigate).toHaveBeenCalledWith("/machines/m1");
    });
  });

  it("passes the trimmed custom runtime image through to provisionMachine", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });
    fireEvent.change(
      screen.getByLabelText("machine.new.provisioned.custom-image-label"),
      { target: { value: "  registry.example.com/team/app:v1  " } }
    );
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        "registry.example.com/team/app:v1"
      );
      expect(mockRouter.navigate).toHaveBeenCalledWith("/machines/m1");
    });
  });

  it("hides the custom image field when the workspace disables custom images", async () => {
    mockedPolicy.allowCustomImages = false;
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewProvisionedPanel />);

    expect(await screen.findByText("Prod Cluster")).toBeInTheDocument();
    await waitFor(() => {
      expect(
        screen.queryByLabelText("machine.new.provisioned.custom-image-label")
      ).not.toBeInTheDocument();
    });
    // Creating without the field still works (workspace default image).
    fireEvent.click(screen.getByText("Prod Cluster"));
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));
    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        ""
      );
    });
  });

  it("rejects creating when the selected provisioner goes offline after selection", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });

    // The provisioner disconnects after the user picked it.
    act(() => {
      useAppStore.setState({
        provisioners: [
          provisioner("provisioners/p1", "Prod Cluster", {
            status: { connected: false, version: "" } as never,
          }),
        ],
      });
    });

    fireEvent.click(screen.getByText("machine.new.provisioned.create"));
    expect(
      screen.getByText("machine.new.provisioned.provisioner-offline")
    ).toBeInTheDocument();
    expect(mockedActions.provisionMachine).not.toHaveBeenCalled();
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
