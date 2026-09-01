// Tests for the ADR-3 ③ light windowing (useWindowedMessageRange): a plain
// virtualizer-like mount window driven by measured heights, with the jsdom
// full-render fallback preserved (no mocks needed unless the test defines
// its own ResizeObserver to drive engagement).
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useWindowedMessageRange } from "./use-windowed-message-range";

type MockROCallback = (entries: unknown[]) => void;

class MockResizeObserver {
  static instances: MockResizeObserver[] = [];
  callback: MockROCallback;
  constructor(callback: MockROCallback) {
    this.callback = callback;
    MockResizeObserver.instances.push(this);
  }
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

const originalRO = (globalThis as { ResizeObserver?: typeof ResizeObserver })
  .ResizeObserver;

const rowIds = Array.from({ length: 200 }, (_, i) => `m-${i}`);

function setupContainer(height: number): {
  container: HTMLDivElement;
  fireResize: () => void;
} {
  const container = document.createElement("div");
  Object.defineProperty(container, "clientHeight", {
    value: height,
    configurable: true,
  });
  document.body.append(container);
  return {
    container,
    fireResize: () => {
      MockResizeObserver.instances.forEach((observer) => observer.callback([]));
    },
  };
}

describe("useWindowedMessageRange", () => {
  beforeEach(() => {
    MockResizeObserver.instances = [];
    (globalThis as { ResizeObserver?: unknown }).ResizeObserver =
      MockResizeObserver;
  });
  afterEach(() => {
    (globalThis as { ResizeObserver?: unknown }).ResizeObserver = originalRO;
    document.body.textContent = "";
  });

  it("renders everything before the container is measured", () => {
    const { container } = setupContainer(0);
    const { result } = renderHook(() =>
      useWindowedMessageRange({
        containerRef: { current: container },
        rowIds,
      })
    );
    expect(result.current.start).toBe(0);
    expect(result.current.end).toBe(Number.MAX_SAFE_INTEGER);
  });

  it("engages after a real measurement and bounds the mounted window", () => {
    const { container, fireResize } = setupContainer(500);
    const { result } = renderHook(() =>
      useWindowedMessageRange({
        containerRef: { current: container },
        rowIds,
      })
    );
    act(() => fireResize());
    expect(result.current.start).toBe(0);
    expect(result.current.end).toBeLessThanOrEqual(200);
  });

  it("slides the window toward the scroll position and keeps tall margins", () => {
    const { container, fireResize } = setupContainer(500);
    const { result, rerender } = renderHook(
      ({ force }: { force: boolean | undefined }) =>
        useWindowedMessageRange({
          containerRef: { current: container },
          rowIds,
          forceFullRender: force,
        }),
      { initialProps: { force: undefined as boolean | undefined } }
    );
    act(() => fireResize());
    container.scrollTop = 80_000;
    act(() => fireResize());
    rerender({ force: undefined });
    expect(result.current.end).toBe(200);
    expect(result.current.start).toBeGreaterThan(100);

    // While a jump is in flight the full tree must stay mounted.
    rerender({ force: true });
    expect(result.current.start).toBe(0);
    expect(result.current.end).toBe(Number.MAX_SAFE_INTEGER);
  });

  it("remembers mounted row heights for placeholder geometry", () => {
    const { container, fireResize } = setupContainer(500);
    const { result } = renderHook(() =>
      useWindowedMessageRange({
        containerRef: { current: container },
        rowIds,
      })
    );
    act(() => fireResize());
    const el = document.createElement("div");
    Object.defineProperty(el, "offsetHeight", {
      value: 234,
      configurable: true,
    });
    act(() => {
      result.current.rowRef("m-5")(el);
      result.current.rowRef("m-5")(null);
    });
    expect(result.current.placeholderHeight("m-5")).toBe(234);
    expect(result.current.placeholderHeight("m-unknown")).toBeGreaterThan(0);
  });

  it("does not engage when ResizeObserver is unavailable (jsdom default)", () => {
    delete (globalThis as { ResizeObserver?: unknown }).ResizeObserver;
    const { container, fireResize } = setupContainer(500);
    const { result } = renderHook(() =>
      useWindowedMessageRange({
        containerRef: { current: container },
        rowIds,
      })
    );
    // No observer instance could have been constructed.
    expect(MockResizeObserver.instances).toHaveLength(0);
    fireResize();
    expect(result.current.start).toBe(0);
    expect(result.current.end).toBe(Number.MAX_SAFE_INTEGER);
    vi.restoreAllMocks();
  });
});
