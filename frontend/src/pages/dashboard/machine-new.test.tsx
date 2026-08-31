import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const mockRouter = vi.hoisted(() => ({ navigate: vi.fn() }));
vi.mock("react-router-dom", () => ({
  useNavigate: () => mockRouter.navigate,
}));

vi.mock("@/components/copyable-command", () => ({
  CopyableCommand: () => <div />,
}));

import { MachineNewPage } from "./machine-new";
import { useAppStore } from "@/stores";
import type { MachineSummary } from "@/types/proto-es/v1/machine_pb";

const mockedActions = vi.hoisted(() => ({
  fetchMachines: vi.fn(),
  getMachine: vi.fn(),
  updateMachine: vi.fn(),
}));

// Created "just now" so the page's pageOpenTime anchor counts it as fresh.
function freshSummary(name: string, title: string): MachineSummary {
  return {
    name,
    title,
    status: 1,
    createdBy: "users/1",
    createdAt: { seconds: BigInt(Math.floor(Date.now() / 1000) + 5), nanos: 0 },
  } as unknown as MachineSummary;
}

function fullMachine(name: string) {
  return {
    name,
    title: "pending-name",
    info: {
      hostname: "dev-box",
      os: "linux",
      arch: "amd64",
      ip: "10.0.0.5",
    },
  };
}

beforeEach(() => {
  useAppStore.getState().reset();
  useAppStore.setState({
    currentUser: {
      name: "users/1",
      handle: "users/1",
      title: "Ran",
      permissions: [],
    } as never,
    machines: [],
    fetchMachines: mockedActions.fetchMachines,
    getMachine: mockedActions.getMachine,
    updateMachine: mockedActions.updateMachine,
  });
  mockedActions.fetchMachines.mockResolvedValue(undefined);
  mockRouter.navigate.mockReset();
  mockedActions.getMachine.mockReset();
  mockedActions.updateMachine.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

describe("MachineNewPage", () => {
  it("polls for machines while waiting", async () => {
    render(<MachineNewPage />);
    expect(await screen.findByText("machine.new.waiting")).toBeInTheDocument();
    await waitFor(() => {
      expect(mockedActions.fetchMachines).toHaveBeenCalled();
    });
  });

  it("detects a freshly approved machine and shows its host info", async () => {
    mockedActions.getMachine.mockResolvedValue(fullMachine("machines/m1"));
    render(<MachineNewPage />);

    // The approval poll lands the new machine in the store.
    useAppStore.setState({ machines: [freshSummary("machines/m1", "m1")] });

    expect(
      (await screen.findAllByText("dev-box")).length
    ).toBeGreaterThan(0);
    expect(mockedActions.getMachine).toHaveBeenCalledWith("machines/m1");
    expect(screen.getByLabelText("machine.new.name-label")).toBeInTheDocument();
  });

  it("dismisses a candidate permanently — deny does not revive it", async () => {
    const summary = freshSummary("machines/m1", "m1");
    mockedActions.getMachine.mockResolvedValue(fullMachine("machines/m1"));
    render(<MachineNewPage />);

    useAppStore.setState({ machines: [summary] });
    await screen.findAllByText("dev-box");

    // "not mine" clears the candidate…
    fireEvent.click(screen.getByText("machine.new.not-mine"));
    expect(await screen.findByText("machine.new.waiting")).toBeInTheDocument();

    // …and a later poll returning the same machine must not revive it.
    useAppStore.setState({ machines: [summary] });
    expect(screen.getByText("machine.new.waiting")).toBeInTheDocument();
  });

  it("confirms the rename, navigates to the machine profile and stops as saving", async () => {
    const summary = freshSummary("machines/m1", "m1");
    mockedActions.getMachine.mockResolvedValue(fullMachine("machines/m1"));
    mockedActions.updateMachine.mockResolvedValue(undefined);
    render(<MachineNewPage />);

    useAppStore.setState({ machines: [summary] });
    const confirm = await screen.findByText("machine.new.confirm");

    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "  My Box  " },
    });
    fireEvent.click(confirm);

    await waitFor(() => {
      expect(mockedActions.updateMachine).toHaveBeenCalledWith(
        "machines/m1",
        "My Box"
      );
      expect(mockRouter.navigate).toHaveBeenCalledWith("/machines/m1");
    });
  });

  it("surfaces a save failure as an inline error instead of navigating", async () => {
    const summary = freshSummary("machines/m1", "m1");
    mockedActions.getMachine.mockResolvedValue(fullMachine("machines/m1"));
    mockedActions.updateMachine.mockRejectedValue(new Error("denied"));
    render(<MachineNewPage />);

    useAppStore.setState({ machines: [summary] });
    await screen.findAllByText("dev-box");
    fireEvent.click(screen.getByText("machine.new.confirm"));

    expect(await screen.findByText("denied")).toBeInTheDocument();
    expect(mockRouter.navigate).not.toHaveBeenCalled();

    // Editing the name clears the error (name change resets the error state).
    fireEvent.change(screen.getByLabelText("machine.new.name-label"), {
      target: { value: "x" },
    });
    expect(screen.queryByText("denied")).toBeNull();
  });
});