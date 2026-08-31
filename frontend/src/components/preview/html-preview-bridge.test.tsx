import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  type HtmlPreviewBridge,
  type HtmlPreviewBridgeHandlers,
  type HtmlPreviewRect,
  useHtmlPreviewBridge,
} from "./html-preview-bridge";

// Harness renders the hook with a real iframe so iframeRef.contentWindow is
// live, and exports the bridge object through a captured ref for assertions.
let latest: HtmlPreviewBridge | null = null;

function Harness({
  handlers,
  resetKey,
}: {
  handlers: HtmlPreviewBridgeHandlers;
  resetKey?: string;
}) {
  latest = useHtmlPreviewBridge(handlers, resetKey);
  return <iframe ref={latest.iframeRef} title="preview" />;
}

function renderBridge(handlers: HtmlPreviewBridgeHandlers, resetKey?: string) {
  const view = render(<Harness handlers={handlers} resetKey={resetKey} />);
  const bridge = latest;
  if (!bridge) throw new Error("bridge not rendered");
  const iframe = view.container.querySelector("iframe");
  const win = iframe?.contentWindow ?? null;
  return { view, bridge, win };
}

// postFromBridge dispatches a message event as if it came from the iframe's
// window, carrying the bridge marker + per-open secrets the hook expects.
function postFromBridge(
  win: Window | null,
  bridge: HtmlPreviewBridge,
  payload: Record<string, unknown>,
  source?: MessageEventSource
) {
  window.dispatchEvent(
    new MessageEvent("message", {
      source: source ?? (win as MessageEventSource),
      data: {
        slockAcBridge: 1,
        nonce: bridge.nonce,
        documentEpoch: bridge.epoch,
        ...payload,
      },
    })
  );
}

describe("useHtmlPreviewBridge", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("TestBridgeValidationChain: marker + nonce + epoch + e.source all required", () => {
    const onEscape = vi.fn();
    const { bridge, win } = renderBridge({ onEscape });

    // Missing marker.
    window.dispatchEvent(
      new MessageEvent("message", {
        source: win as MessageEventSource,
        data: { nonce: bridge.nonce, documentEpoch: bridge.epoch, type: "esc" },
      })
    );
    // Wrong nonce.
    postFromBridge(win, bridge, { type: "esc", nonce: "nope" });
    // Wrong epoch.
    postFromBridge(win, bridge, { type: "esc", documentEpoch: "nope" });
    // Wrong source.
    postFromBridge(win, bridge, { type: "esc" }, window);
    // Non-object data.
    window.dispatchEvent(
      new MessageEvent("message", {
        source: win as MessageEventSource,
        data: "esc",
      })
    );
    expect(onEscape).not.toHaveBeenCalled();

    // All four checks pass -> delivered.
    act(() => postFromBridge(win, bridge, { type: "esc" }));
    expect(onEscape).toHaveBeenCalledTimes(1);
  });

  it("TestBridgeEscThrottle: repeats within 300ms are dropped (F-S2)", () => {
    const onEscape = vi.fn();
    const { bridge, win } = renderBridge({ onEscape });

    act(() => postFromBridge(win, bridge, { type: "esc" }));
    act(() => postFromBridge(win, bridge, { type: "esc" }));
    expect(onEscape).toHaveBeenCalledTimes(1);

    act(() => vi.advanceTimersByTime(299));
    act(() => postFromBridge(win, bridge, { type: "esc" }));
    expect(onEscape).toHaveBeenCalledTimes(1);

    act(() => vi.advanceTimersByTime(1));
    act(() => postFromBridge(win, bridge, { type: "esc" }));
    expect(onEscape).toHaveBeenCalledTimes(2);
  });

  it("TestBridgeStateDedup: unchanged geometry/scroll does not re-render (F-S2)", () => {
    const onState = vi.fn();
    const { bridge, win } = renderBridge({ onState });

    const state = {
      type: "state",
      scrollX: 3,
      scrollY: 4,
      docWidth: 100,
      docHeight: 200,
      viewportWidth: 50,
      viewportHeight: 60,
    };
    act(() => postFromBridge(win, bridge, state));
    act(() => postFromBridge(win, bridge, state));
    expect(onState).toHaveBeenCalledTimes(1);
    expect(onState).toHaveBeenLastCalledWith({
      scrollX: 3,
      scrollY: 4,
      docWidth: 100,
      docHeight: 200,
      viewportWidth: 50,
      viewportHeight: 60,
    });

    // A real geometry change is forwarded again.
    act(() => postFromBridge(win, bridge, { ...state, scrollY: 40 }));
    expect(onState).toHaveBeenCalledTimes(2);
    expect(onState).toHaveBeenLastCalledWith({
      scrollX: 3,
      scrollY: 40,
      docWidth: 100,
      docHeight: 200,
      viewportWidth: 50,
      viewportHeight: 60,
    });
  });

  it("TestBridgeLocate: resolves the reported rect and clears the timer", async () => {
    const { bridge, win } = renderBridge({});
    const posted: Array<Record<string, unknown>> = [];
    if (!win) throw new Error("iframe contentWindow missing in jsdom");
    win.postMessage = ((msg: unknown) => {
      posted.push(msg as Record<string, unknown>);
    }) as typeof win.postMessage;

    let result: HtmlPreviewRect | null | undefined;
    act(() => {
      void bridge.locateQuote("needle", 12).then((r) => {
        result = r;
      });
    });
    expect(posted).toHaveLength(1);
    expect(posted[0].type).toBe("locate");
    expect(posted[0].nonce).toBe(bridge.nonce);
    const requestId = String(posted[0].requestId);

    act(() =>
      postFromBridge(win, bridge, {
        type: "located",
        requestId,
        x: 10,
        y: 20,
        w: 30,
        h: 40,
      })
    );
    await act(async () => {});
    expect(result).toEqual({ x: 10, y: 20, w: 30, h: 40 });
  });

  it("TestBridgeLocateTimeout: a poisoned document resolves null after 3s (F-B9)", async () => {
    const { bridge } = renderBridge({});

    let result: HtmlPreviewRect | null | undefined;
    act(() => {
      void bridge.locateQuote("needle", null).then((r) => {
        result = r;
      });
    });
    act(() => vi.advanceTimersByTime(2999));
    expect(result).toBeUndefined();
    act(() => vi.advanceTimersByTime(1));
    await act(async () => {});
    expect(result).toBeNull();
  });

  it("TestBridgeLocateUnmountDrain: pending resolves null and timers clear on unmount (F-B9)", async () => {
    const { view, bridge } = renderBridge({});

    let result: HtmlPreviewRect | null | undefined;
    act(() => {
      void bridge.locateQuote("needle", null).then((r) => {
        result = r;
      });
    });
    expect(result).toBeUndefined();

    view.unmount();
    await act(async () => {});
    expect(result).toBeNull();
  });

  it("TestBridgePerOpenSecrets: resetKey re-issues nonce/epoch and drops the old secret (F-S3)", () => {
    const onEscape = vi.fn();
    const { view, bridge: first, win } = renderBridge({ onEscape }, "file-a");
    const firstNonce = first.nonce;
    const firstEpoch = first.epoch;

    view.rerender(<Harness handlers={{ onEscape }} resetKey="file-b" />);
    const second = latest as HtmlPreviewBridge;
    expect(second.nonce).not.toBe(firstNonce);
    expect(second.epoch).not.toBe(firstEpoch);

    // The old secret is no longer honored.
    act(() => postFromBridge(win, first, { type: "esc" }));
    expect(onEscape).not.toHaveBeenCalled();

    act(() => postFromBridge(win, second, { type: "esc" }));
    expect(onEscape).toHaveBeenCalledTimes(1);
  });
});
