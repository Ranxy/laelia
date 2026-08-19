import { create } from "@bufbuild/protobuf";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAppStore } from "@/stores";
import {
  MachineStatus_ConnectionState,
  MachineSummarySchema,
} from "@/types/proto-es/v1/machine_pb";
import { MachinesPage } from "./machines";

const mock = vi.hoisted(() => ({
  listMachines: vi.fn(),
  deleteMachine: vi.fn(),
  getWorkspaceInfo: vi.fn(),
}));

vi.mock("@/connect", () => ({
  machineServiceClient: {
    listMachines: mock.listMachines,
    deleteMachine: mock.deleteMachine,
  },
  settingServiceClient: {
    getWorkspaceInfo: mock.getWorkspaceInfo,
  },
}));

const tFn = (key: string, params?: Record<string, string | number>) => {
  if (!params) return key;
  const values = Object.values(params);
  return values.length > 0 ? `${key}:${values.join(":")}` : key;
};
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function machine(
  name: string,
  title: string,
  overrides?: Record<string, unknown>
) {
  return create(MachineSummarySchema, {
    name,
    title,
    agentCount: 1,
    status: { state: MachineStatus_ConnectionState.ONLINE },
    canDelete: true,
    ...overrides,
  });
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/machines"]}>
      <Routes>
        <Route path="/machines" element={<MachinesPage />}>
          <Route path="new" element={<div data-testid="new-page" />} />
          <Route path=":machineId" element={<div data-testid="detail" />} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      permissions: ["laelia.machines.create"],
    } as never,
    machines: [],
    machinesLoading: false,
  });
  mock.listMachines.mockReset();
  mock.deleteMachine.mockReset();
  mock.getWorkspaceInfo.mockReset();
  mock.getWorkspaceInfo.mockResolvedValue({ disallowUserCreateMachine: false });
});

describe("machines", () => {
  it("shows the loading hint while the list is being fetched", () => {
    mock.listMachines.mockReturnValue(new Promise(() => {}));

    renderPage();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
  });

  it("shows the empty hint when there are no machines", async () => {
    mock.listMachines.mockResolvedValue({ machines: [], nextPageToken: "" });

    renderPage();

    expect(await screen.findByText("common.no-data")).toBeInTheDocument();
  });

  it("renders machine rows with title, agent count and connection state", async () => {
    mock.listMachines.mockResolvedValue({
      machines: [
        machine("machines/m1", "Build box", { agentCount: 3 }),
        machine("machines/m2", "Idle box", {
          status: { state: MachineStatus_ConnectionState.ERROR },
        }),
      ],
      nextPageToken: "",
    });

    renderPage();

    expect(await screen.findByText("Build box")).toBeInTheDocument();
    expect(screen.getByText("Idle box")).toBeInTheDocument();
    expect(screen.getByText("machine.status-online")).toBeInTheDocument();
    expect(screen.getByText("machine.status-error")).toBeInTheDocument();
    expect(screen.getByText("machine.agent-count:3")).toBeInTheDocument();
  });

  it("navigates to the machine detail when a row is clicked", async () => {
    mock.listMachines.mockResolvedValue({
      machines: [machine("machines/m1", "Build box")],
      nextPageToken: "",
    });

    renderPage();
    fireEvent.click(
      await screen.findByRole("button", {
        name: "machine.row-open-detail:Build box",
      })
    );

    expect(screen.getByTestId("detail")).toBeInTheDocument();
  });

  it("navigates to the create-machine waiting page", async () => {
    mock.listMachines.mockResolvedValue({ machines: [], nextPageToken: "" });

    renderPage();
    await screen.findByText("common.no-data");
    fireEvent.click(
      screen.getAllByRole("button", { name: "machine.create" })[0]
    );

    expect(await screen.findByTestId("new-page")).toBeInTheDocument();
  });

  it("deletes a machine after confirmation", async () => {
    mock.listMachines.mockResolvedValue({
      machines: [machine("machines/m1", "Build box")],
      nextPageToken: "",
    });
    mock.deleteMachine.mockResolvedValue({});

    renderPage();
    await screen.findByText("Build box");
    fireEvent.click(screen.getByRole("button", { name: "common.delete" }));

    expect(
      screen.getByText("machine.delete-confirm-title")
    ).toBeInTheDocument();
    // The page is inert while the dialog is open, so the only visible
    // "delete" button is the dialog's confirm action.
    fireEvent.click(
      screen.getAllByRole("button", { name: "common.delete" })[0]
    );

    await waitFor(() => expect(mock.deleteMachine).toHaveBeenCalledTimes(1));
    const req = mock.deleteMachine.mock.calls[0][0] as { name: string };
    expect(req.name).toBe("machines/m1");
  });

  it("hides the create affordance without the create permission when user-created machines are disabled", async () => {
    useAppStore.setState({
      currentUser: {
        name: "users/2",
        permissions: [],
      } as never,
    });
    mock.getWorkspaceInfo.mockResolvedValue({
      disallowUserCreateMachine: true,
    });
    mock.listMachines.mockResolvedValue({ machines: [], nextPageToken: "" });

    renderPage();

    await screen.findByText("common.no-data");
    expect(
      screen.queryByRole("button", { name: "machine.create" })
    ).not.toBeInTheDocument();
    expect(screen.queryByTestId("create-machine-fab")).not.toBeInTheDocument();
  });

  it("shows the create affordance to ordinary users when user-created machines are enabled", async () => {
    useAppStore.setState({
      currentUser: {
        name: "users/2",
        permissions: [],
      } as never,
    });
    mock.getWorkspaceInfo.mockResolvedValue({
      disallowUserCreateMachine: false,
    });
    mock.listMachines.mockResolvedValue({ machines: [], nextPageToken: "" });

    renderPage();

    await screen.findByText("common.no-data");
    expect(
      screen.getAllByRole("button", { name: "machine.create" }).length
    ).toBeGreaterThan(0);
    expect(screen.getByTestId("create-machine-fab")).toBeInTheDocument();
  });
});
