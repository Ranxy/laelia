import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DeviceLoginStatus } from "@/types/proto-es/v1/device_pb";
import { DeviceLoginPage } from "./device-login";

// Poll-governance tests for the device approval page: terminal stop, failure
// backoff, success reset, the ≥3-failures unreachable rule, and the visibility
// (hidden tab) pause. Fake timers drive the self-scheduling poll loop.
const mock = vi.hoisted(() => ({
  getDeviceLoginStatus: vi.fn(),
  approveDeviceLogin: vi.fn(),
  logout: vi.fn(),
}));

vi.mock("@/connect", () => ({
  deviceServiceClient: {
    getDeviceLoginStatus: mock.getDeviceLoginStatus,
    approveDeviceLogin: mock.approveDeviceLogin,
  },
}));

vi.mock("@/stores", () => {
  const state = {
    currentUser: null,
    logout: mock.logout,
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

vi.mock("@/lib/avatar-cache", () => ({
  useAvatar: () => undefined,
}));

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

function statusResponse(status: DeviceLoginStatus) {
  return {
    status,
    hostname: "dev-1",
    os: "linux",
    arch: "arm64",
    ip: "",
    reauthExisting: false,
    machineTitle: "",
    machineOwner: "",
    denialReason: "",
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/login/device?user_code=ABCD-EFGH"]}>
      <Routes>
        <Route path="/login/device" element={<DeviceLoginPage />} />
        <Route path="/auth/signin" element={<div data-testid="signin" />} />
      </Routes>
    </MemoryRouter>
  );
}

// Override jsdom's read-only document.hidden for the visibility tests; the own
// property is deleted in afterEach so the prototype getter is restored.
function setHidden(hidden: boolean) {
  Object.defineProperty(document, "hidden", {
    configurable: true,
    get: () => hidden,
  });
}

function fireVisibilityChange() {
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
}

// Flush promise continuations inside act (poll responses resolve on
// microtasks; fake timers don't advance those).
async function flushMicrotasks() {
  await act(async () => {
    await Promise.resolve();
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  mock.getDeviceLoginStatus.mockReset();
  mock.approveDeviceLogin.mockReset();
  mock.logout.mockReset();
  mock.logout.mockResolvedValue(undefined);
});

afterEach(() => {
  Reflect.deleteProperty(document, "hidden");
  vi.useRealTimers();
});

describe("device-login poll governance", () => {
  it("stops polling once the status reaches a terminal state", async () => {
    mock.getDeviceLoginStatus
      .mockResolvedValueOnce(statusResponse(DeviceLoginStatus.PENDING))
      .mockResolvedValue(statusResponse(DeviceLoginStatus.EXPIRED));

    renderPage();
    await flushMicrotasks();
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(1);
    expect(screen.getByText("dev-1")).toBeInTheDocument();

    // The next tick observes EXPIRED — a terminal status — and must not
    // schedule anything afterwards: advancing 60s produces no further polls.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(2);
    expect(screen.getByText("auth.device-login.expired")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(60000);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(2);
  });

  it("backs off on consecutive failures, caps at 15s and resets after a success", async () => {
    mock.getDeviceLoginStatus.mockRejectedValue(new Error("down"));

    renderPage();
    await flushMicrotasks();
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(1);

    // Failure #1 → next tick at 6s (2×3s), not 3s.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5900);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(2);

    // Failure #2 → next tick at 12s (t=18). Failure #3 makes the server
    // unreachable (≥3 consecutive failures without data).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(12000);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(3);
    expect(
      screen.getByText("auth.device-login.unreachable")
    ).toBeInTheDocument();

    // Failure #4 → the cap: min(3×2³, 15s)=15s, so 14.9s later there is still
    // no extra call.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(14900);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(3);

    // A success resets the streak and returns data: the full-screen
    // unreachable card is dropped and the next tick goes back to 3s.
    mock.getDeviceLoginStatus.mockResolvedValue(
      statusResponse(DeviceLoginStatus.PENDING)
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(200);
    });
    // The success tick; the loop keeps running (no restart on a status
    // change) and the next tick lands back at 3s, not on a doubled delay.
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(4);
    expect(screen.getByText("dev-1")).toBeInTheDocument();
    expect(
      screen.queryByText("auth.device-login.unreachable")
    ).not.toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2500);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(4);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(600);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(5);
  });

  it("keeps rendered data and shows the stale notice instead of the unreachable card once data has loaded", async () => {
    mock.getDeviceLoginStatus
      .mockResolvedValueOnce(statusResponse(DeviceLoginStatus.PENDING))
      .mockRejectedValue(new Error("down"));

    renderPage();
    await flushMicrotasks(); // poll #1 succeeds → device data shown
    await act(async () => {
      await vi.advanceTimersByTimeAsync(21000); // failures at t=3s, 9s, 21s
    });

    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(4);
    // Data survived; the page degrades to a stale notice, not a full swap.
    expect(screen.getByText("dev-1")).toBeInTheDocument();
    expect(
      screen.getByText("auth.device-login.stale-data")
    ).toBeInTheDocument();
    expect(
      screen.queryByText("auth.device-login.unreachable")
    ).not.toBeInTheDocument();
  });

  it("pauses polling while the tab is hidden and polls immediately on return", async () => {
    mock.getDeviceLoginStatus.mockResolvedValue(
      statusResponse(DeviceLoginStatus.PENDING)
    );

    renderPage();
    await flushMicrotasks();
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(1);

    setHidden(true);
    fireVisibilityChange();

    // No polls while hidden, even across several 3s windows.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60000);
    });
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(1);

    // Returning to the foreground polls immediately.
    setHidden(false);
    fireVisibilityChange();
    await flushMicrotasks();
    expect(mock.getDeviceLoginStatus).toHaveBeenCalledTimes(2);
  });
});
