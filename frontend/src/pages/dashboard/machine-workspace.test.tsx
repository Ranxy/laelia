import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setRouteNameIndex } from "@/router/route-index";
import type { MachineWorkspaceSummary } from "@/types/proto-es/v1/machine_pb";
import { MachineWorkspacePage } from "./machine-workspace";

const mock = vi.hoisted(() => ({
  getMachine: vi.fn(),
  listMachineWorkspaces: vi.fn(),
}));

vi.mock("@/stores", () => {
  const state = {
    getMachine: mock.getMachine,
    listMachineWorkspaces: mock.listMachineWorkspaces,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/machines/m1/workspace"]}>
      <Routes>
        <Route
          path="/machines/:machineId/workspace"
          element={<MachineWorkspacePage />}
        />
        <Route
          path="/machines/:machineId"
          element={<div data-testid="profile" />}
        />
      </Routes>
    </MemoryRouter>
  );
}

function ws(directoryName: string, totalSizeBytes: number, fileCount: number) {
  return {
    directoryName,
    totalSizeBytes,
    fileCount,
    lastModified: { seconds: 0n, nanos: 0 },
  } as unknown as MachineWorkspaceSummary;
}

beforeEach(() => {
  mock.getMachine.mockReset();
  mock.listMachineWorkspaces.mockReset();
  setRouteNameIndex(new Map([["machine.profile", "/machines/:machineId"]]));
});

describe("machine-workspace", () => {
  it("redirects to the profile tab when the caller cannot manage", async () => {
    mock.getMachine.mockResolvedValue({
      name: "machines/m1",
      canManage: false,
    });

    renderPage();

    expect(await screen.findByTestId("profile")).toBeInTheDocument();
  });

  it("shows the empty hint when the machine has no workspaces", async () => {
    mock.getMachine.mockResolvedValue({ name: "machines/m1", canManage: true });
    mock.listMachineWorkspaces.mockResolvedValue([]);

    renderPage();

    expect(
      await screen.findByText("workspace.no-workspaces")
    ).toBeInTheDocument();
  });

  it("renders workspace rows with size and file count", async () => {
    mock.getMachine.mockResolvedValue({ name: "machines/m1", canManage: true });
    mock.listMachineWorkspaces.mockResolvedValue([
      ws("/srv/laelia", 2048, 42),
      ws("/data", 1024, 7),
    ]);

    renderPage();

    expect(await screen.findByText("/srv/laelia")).toBeInTheDocument();
    expect(screen.getByText("/data")).toBeInTheDocument();
    expect(screen.getByText("42")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
  });

  it("shows the load error alert when listing fails", async () => {
    mock.getMachine.mockResolvedValue({ name: "machines/m1", canManage: true });
    mock.listMachineWorkspaces.mockRejectedValue(new Error("boom"));

    renderPage();

    expect(await screen.findByText("workspace.load-error")).toBeInTheDocument();
  });

  it("reloads the list when the refresh button is clicked", async () => {
    mock.getMachine.mockResolvedValue({ name: "machines/m1", canManage: true });
    mock.listMachineWorkspaces.mockResolvedValue([ws("/srv", 1, 1)]);

    renderPage();
    await screen.findByText("/srv");
    fireEvent.click(screen.getByRole("button", { name: "workspace.refresh" }));

    await waitFor(() =>
      expect(mock.listMachineWorkspaces).toHaveBeenCalledTimes(2)
    );
  });
});
