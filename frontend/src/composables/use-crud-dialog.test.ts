import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toastManager } from "@/lib/toast";
import { useCrudDialog } from "./use-crud-dialog";

vi.mock("@/lib/toast", () => ({ toastManager: { add: vi.fn() } }));

const toastMock = vi.mocked(toastManager.add);

describe("useCrudDialog", () => {
  beforeEach(() => {
    toastMock.mockReset();
  });

  it("opens edit with a target and retains it after close", async () => {
    const { result } = renderHook(() => useCrudDialog<{ name: string }>());

    const target = { name: "groups/1" };
    act(() => result.current.openEdit(target));
    expect(result.current.editOpen).toBe(true);
    expect(result.current.editTarget).toBe(target);

    act(() => result.current.closeEdit());
    // Retained on close so the drawer title survives the close animation.
    expect(result.current.editOpen).toBe(false);
    expect(result.current.editTarget).toBe(target);
  });

  it("runCreate: toasts success, closes, fires onChanged", async () => {
    const onChanged = vi.fn();
    const { result } = renderHook(() => useCrudDialog({ onChanged }));

    act(() => result.current.openCreate());
    const body = vi.fn().mockResolvedValue(undefined);
    let outcome = false;
    await act(async () => {
      outcome = await result.current.runCreate(body, {
        successTitle: "Created",
      });
    });

    expect(outcome).toBe(true);
    expect(body).toHaveBeenCalledTimes(1);
    expect(toastMock).toHaveBeenCalledWith({
      type: "success",
      title: "Created",
    });
    expect(result.current.createOpen).toBe(false);
    expect(onChanged).toHaveBeenCalledTimes(1);
    expect(result.current.creating).toBe(false);
  });

  it("runSave failure keeps the sheet open and reports through onError", async () => {
    const onChanged = vi.fn();
    const { result } = renderHook(() => useCrudDialog({ onChanged }));

    const body = vi.fn().mockRejectedValue(new Error("denied"));
    const onError = vi.fn();
    let outcome = true;
    await act(async () => {
      outcome = await result.current.runSave(body, { onError });
    });

    expect(outcome).toBe(false);
    expect(onError).toHaveBeenCalledTimes(1);
    expect(toastMock).not.toHaveBeenCalled();
    expect(result.current.editOpen).toBe(false); // was never opened
    expect(onChanged).not.toHaveBeenCalled();
    expect(result.current.saving).toBe(false);
  });

  it("guards against double submit while a run is in flight", async () => {
    const { result } = renderHook(() => useCrudDialog());
    let release: (() => void) | undefined;
    const body = vi.fn().mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          release = resolve;
        })
    );

    let first = false;
    await act(async () => {
      const pending = result.current.runCreate(body).then((ok) => {
        first = ok;
      });
      // Second submit while the first is in flight must be rejected.
      const second = await result.current.runCreate(body);
      expect(second).toBe(false);
      expect(body).toHaveBeenCalledTimes(1);
      release?.();
      await pending;
    });
    expect(first).toBe(true);
    expect(result.current.creating).toBe(false);

    // Busy state cleared — a subsequent submit goes through.
    const body2 = vi.fn().mockResolvedValue(undefined);
    await act(async () => {
      const ok = await result.current.runCreate(body2);
      expect(ok).toBe(true);
    });
  });
});
