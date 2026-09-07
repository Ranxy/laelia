import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
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
        "",
        undefined
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
        "registry.example.com/team/app:v1",
        undefined
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
        "",
        undefined
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

describe("MachineNewProvisionedPanel params", () => {
  function parametrizedProvisioner(): Provisioner {
    return provisioner("provisioners/p1", "Prod Cluster", {
      status: {
        connected: true,
        version: "v0.0.1",
        machineParams: [
          {
            key: "cpu",
            defaultValue: "1",
            minValue: "250m",
            maxValue: "8",
          },
          { key: "memory", defaultValue: "2Gi" },
          { key: "disk" }, // no default → short "default" placeholder
          { key: "gpu", defaultValue: "0" }, // unknown key renders raw
        ],
      },
    } as never);
  }

  function uncheckDefault(key: string) {
    fireEvent.click(screen.getByTestId(`use-default-${key}`));
  }

  // The param label names both the number input and the slider thumb's hidden
  // range input; the number input is pinned by its element id.
  function numberInput(key: string) {
    return screen.getByLabelText(`machine.param.${key}`, {
      selector: `#machine-new-provisioned-param-${key}`,
    });
  }

  function paramRow(key: string) {
    return within(screen.getByTestId(`param-row-${key}`));
  }

  it("renders slider rows seeded with the defaults and keeps unknown keys as text inputs", async () => {
    useAppStore.setState({ provisioners: [parametrizedProvisioner()] });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    expect(
      screen.getByText("machine.new.provisioned.params-title")
    ).toBeInTheDocument();

    // cpu: the admin bounds 250m–8 become the slider range; the default
    // (1 core) seeds the slider + input while the switch is on — and the
    // controls stay live so dragging takes over without unchecking first.
    const cpuSlider = screen.getByRole("slider", { name: "machine.param.cpu" });
    expect(cpuSlider).toHaveAttribute("min", "0.25");
    expect(cpuSlider).toHaveAttribute("max", "8");
    expect(cpuSlider).toHaveAttribute("aria-valuenow", "1");
    expect(cpuSlider).toBeEnabled();
    const cpuInput = numberInput("cpu");
    expect(cpuInput).toBeEnabled();
    expect(cpuInput).toHaveValue("1");
    expect(
      paramRow("cpu").getByText("machine.param.unit-cores")
    ).toBeInTheDocument();
    expect(screen.getByTestId("use-default-cpu")).toBeChecked();

    // memory: no admin bounds → the built-in 1–64Gi fallback; 2Gi default.
    const memorySlider = screen.getByRole("slider", {
      name: "machine.param.memory",
    });
    expect(memorySlider).toHaveAttribute("min", "1");
    expect(memorySlider).toHaveAttribute("max", "64");
    expect(memorySlider).toHaveAttribute("aria-valuenow", "2");
    expect(numberInput("memory")).toHaveValue("2");

    // disk: no default → the input shows the short placeholder (no unit
    // suffix crowding it) and the 1–500Gi fallback keeps the 10Gi-style
    // defaults reachable at step 1.
    const diskSlider = screen.getByRole("slider", {
      name: "machine.param.disk",
    });
    expect(diskSlider).toHaveAttribute("min", "1");
    expect(diskSlider).toHaveAttribute("max", "500");
    expect(diskSlider).toHaveAttribute("step", "1");
    expect(numberInput("disk")).toHaveAttribute(
      "placeholder",
      "machine.new.provisioned.param-default-short"
    );
    expect(paramRow("disk").queryByText("Gi")).not.toBeInTheDocument();

    // A key the frontend does not know renders as a raw text input.
    expect(screen.getByLabelText(/^gpu$/)).toHaveAttribute("placeholder", "0");
  });

  it("hides the parameter section for provisioners without a schema", async () => {
    useAppStore.setState({
      provisioners: [provisioner("provisioners/p1", "Prod Cluster")],
    });
    render(<MachineNewProvisionedPanel />);
    fireEvent.click(await screen.findByText("Prod Cluster"));
    expect(
      screen.queryByText("machine.new.provisioned.params-title")
    ).not.toBeInTheDocument();
  });

  it("dragging a slider takes the param over from its default and converts on submit", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({ provisioners: [parametrizedProvisioner()] });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });

    // Dragging the memory slider while the default switch is on flips the
    // switch off and submits the dragged value as "4Gi".
    fireEvent.change(paramRow("memory").getByRole("slider"), {
      target: { value: "4" },
    });
    expect(screen.getByTestId("use-default-memory")).not.toBeChecked();
    // Unknown keys stay free-text and submit verbatim.
    fireEvent.change(screen.getByLabelText(/^gpu$/), {
      target: { value: "1" },
    });
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        "",
        { memory: "4Gi", gpu: "1" }
      );
      expect(mockRouter.navigate).toHaveBeenCalledWith("/machines/m1");
    });
  });

  it("taking over cpu via the switch seeds the default, then edits submit", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({ provisioners: [parametrizedProvisioner()] });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });

    // Unchecking seeds the default (1 core); the user then edits it to 4.
    uncheckDefault("cpu");
    fireEvent.change(numberInput("cpu"), {
      target: { value: "4" },
    });
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        "",
        { cpu: "4" }
      );
      expect(mockRouter.navigate).toHaveBeenCalledWith("/machines/m1");
    });
  });

  it("typing an out-of-bounds value takes over and clamps at submit", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({ provisioners: [parametrizedProvisioner()] });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });
    // Typing into the live input flips the default switch off by itself.
    fireEvent.change(numberInput("cpu"), {
      target: { value: "999" },
    });
    expect(screen.getByTestId("use-default-cpu")).not.toBeChecked();
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        "",
        { cpu: "8" }
      );
    });
  });

  it("re-checking default reverts the input to the default value", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({ provisioners: [parametrizedProvisioner()] });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));

    // Typed takeover, then back to default: the box shows the default again.
    fireEvent.change(numberInput("cpu"), { target: { value: "5" } });
    fireEvent.click(screen.getByTestId("use-default-cpu"));
    expect(screen.getByTestId("use-default-cpu")).toBeChecked();
    expect(numberInput("cpu")).toHaveValue("1");

    // Dragged takeover, then back to default.
    fireEvent.change(paramRow("memory").getByRole("slider"), {
      target: { value: "4" },
    });
    fireEvent.click(screen.getByTestId("use-default-memory"));
    expect(numberInput("memory")).toHaveValue("2");

    // A param without a configured default clears to its short placeholder.
    fireEvent.change(numberInput("disk"), { target: { value: "50" } });
    fireEvent.click(screen.getByTestId("use-default-disk"));
    expect(numberInput("disk")).toHaveValue("");
    expect(numberInput("disk")).toHaveAttribute(
      "placeholder",
      "machine.new.provisioned.param-default-short"
    );
  });

  it("submits no params argument when every param keeps its default", async () => {
    mockedActions.provisionMachine.mockResolvedValue({
      name: "machines/m1",
      title: "Cloud Box",
    });
    useAppStore.setState({ provisioners: [parametrizedProvisioner()] });
    render(<MachineNewProvisionedPanel />);

    fireEvent.click(await screen.findByText("Prod Cluster"));
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "Cloud Box" },
    });
    fireEvent.click(screen.getByText("machine.new.provisioned.create"));

    await waitFor(() => {
      expect(mockedActions.provisionMachine).toHaveBeenCalledWith(
        "provisioners/p1",
        "Cloud Box",
        "",
        undefined
      );
    });
  });
});
