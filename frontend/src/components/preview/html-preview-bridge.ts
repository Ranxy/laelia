import {
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { randomId } from "@/lib/html-file";

export interface HtmlPreviewRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface HtmlPreviewDocState {
  scrollX: number;
  scrollY: number;
  docWidth: number;
  docHeight: number;
  viewportWidth: number;
  viewportHeight: number;
}

// Discriminated union of every message the sandboxed bridge script (see
// buildHtmlPreviewDoc) posts to the parent. Nothing is delivered to callers
// before the single validation below (marker + per-open nonce + parent-issued
// document epoch + e.source) accepts it.
export type HtmlPreviewBridgeMessage =
  | ({ type: "state" } & HtmlPreviewDocState)
  | ({ type: "selection"; text: string } & HtmlPreviewRect)
  | { type: "selection-cleared" }
  | { type: "located"; requestId: string; rect: HtmlPreviewRect | null }
  | { type: "link-clicked"; href: string }
  | { type: "esc" };

export interface HtmlPreviewBridgeHandlers {
  onState?: (state: HtmlPreviewDocState) => void;
  onSelection?: (
    msg: Extract<HtmlPreviewBridgeMessage, { type: "selection" }>
  ) => void;
  onSelectionCleared?: () => void;
  onLinkClick?: (href: string) => void;
  onEscape?: () => void;
}

export interface HtmlPreviewBridge {
  // Attach to the sandboxed iframe rendering buildHtmlPreviewDoc output.
  iframeRef: RefObject<HTMLIFrameElement | null>;
  nonce: string;
  epoch: string;
  // activate issues the document epoch; call it from the iframe's onLoad.
  activate: () => void;
  locateQuote: (
    quote: string,
    nearY: number | null
  ) => Promise<HtmlPreviewRect | null>;
  scrollTo: (x: number, y: number) => void;
}

// A pending locate request (bridge "locate" -> "located"). Timers are tracked
// alongside the callback so unmount / re-open can drain both (F-B9).
interface PendingLocate {
  resolve: (rect: HtmlPreviewRect | null) => void;
  timer: number;
}

const LOCATE_TIMEOUT_MS = 3000;
// An untrusted document can dispatch Escape programmatically to churn the
// overlay; forward at most one per window (F-S2).
const ESC_THROTTLE_MS = 300;

// useHtmlPreviewBridge is the single parent-side implementation of the
// sandboxed-iframe preview bridge (previously duplicated in
// HtmlPreviewOverlay and workspace HtmlFileView). It validates every message
// against marker + per-open secrets + iframe source, manages locate request
// timeouts, and drains pending requests on unmount (each resolves null, all
// timers cleared — F-B9). `resetKey` (the attachment id) rotates the
// nonce/epoch per open so they are true per-open secrets (F-S3).
export function useHtmlPreviewBridge(
  handlers: HtmlPreviewBridgeHandlers,
  resetKey?: string
): HtmlPreviewBridge {
  const iframeRef = useRef<HTMLIFrameElement>(null);

  // nonce/epoch are per-open secrets: the bridge echoes them back, and the
  // epoch is only issued after the iframe loads, so a document that
  // navigates itself away can never speak for the original preview.
  const [nonce, setNonce] = useState(() => randomId());
  const [epoch, setEpoch] = useState(() => randomId());

  const locateCbsRef = useRef(new Map<string, PendingLocate>());
  const handlersRef = useRef(handlers);
  useEffect(() => {
    handlersRef.current = handlers;
  });

  // F-S2 parent half: drop state messages whose geometry/scroll is unchanged
  // so a message storm cannot re-render the host page.
  const lastStateRef = useRef<HtmlPreviewDocState | null>(null);
  const lastEscAtRef = useRef(0);

  // Reset per-open secrets when the previewed file changes. The first run
  // (initial mount) keeps the initializer's values so the iframe loads
  // srcDoc exactly once.
  const prevResetKeyRef = useRef(resetKey);
  useEffect(() => {
    if (prevResetKeyRef.current === resetKey) return;
    prevResetKeyRef.current = resetKey;
    setNonce(randomId());
    setEpoch(randomId());
    // Pending locates belong to the previous document/epoch; settle them so
    // a stale rect can never land in the new session.
    drainPendingLocates(locateCbsRef.current);
  }, [resetKey]);

  // F-B9: unmount must settle pending locates — resolve(null) each callback
  // and clear every timeout.
  useEffect(() => {
    const pending = locateCbsRef.current;
    return () => drainPendingLocates(pending);
  }, []);

  const postToIframe = useCallback(
    (msg: Record<string, unknown>) => {
      iframeRef.current?.contentWindow?.postMessage(
        { slockAcBridge: 1, nonce, documentEpoch: epoch, ...msg },
        "*"
      );
    },
    [nonce, epoch]
  );

  const activate = useCallback(
    () => postToIframe({ type: "activate-document" }),
    [postToIframe]
  );

  // locateQuote asks the bridge to find the content rect of `quote`,
  // preferring the occurrence nearest to nearY. Used for jump targets and
  // comment pins; times out so a poisoned document can't hang the UI.
  const locateQuote = useCallback(
    (quote: string, nearY: number | null): Promise<HtmlPreviewRect | null> =>
      new Promise((resolve) => {
        const requestId = randomId();
        const timer = window.setTimeout(() => {
          if (locateCbsRef.current.delete(requestId)) resolve(null);
        }, LOCATE_TIMEOUT_MS);
        locateCbsRef.current.set(requestId, { resolve, timer });
        postToIframe({
          type: "locate",
          requestId,
          quote: quote.slice(0, 500),
          nearY: nearY ?? "",
        });
      }),
    [postToIframe]
  );

  const scrollTo = useCallback(
    (x: number, y: number) => postToIframe({ type: "scroll-to", x, y }),
    [postToIframe]
  );

  // Bridge messages: validate source + marker + nonce + epoch before
  // trusting anything. Handler identities are read through a ref so the
  // listener does not need to churn on every render.
  useEffect(() => {
    const onMessage = (e: MessageEvent) => {
      const win = iframeRef.current?.contentWindow;
      if (!win || e.source !== win) return;
      const d = e.data;
      if (!d || typeof d !== "object") return;
      const m = d as Record<string, unknown>;
      if (
        m.slockAcBridge !== 1 ||
        m.nonce !== nonce ||
        m.documentEpoch !== epoch
      )
        return;
      const handle = handlersRef.current;
      switch (m.type) {
        case "state": {
          const next: HtmlPreviewDocState = {
            scrollX: Number(m.scrollX) || 0,
            scrollY: Number(m.scrollY) || 0,
            docWidth: Number(m.docWidth) || 0,
            docHeight: Number(m.docHeight) || 0,
            viewportWidth: Number(m.viewportWidth) || 0,
            viewportHeight: Number(m.viewportHeight) || 0,
          };
          const last = lastStateRef.current;
          if (
            last &&
            last.scrollX === next.scrollX &&
            last.scrollY === next.scrollY &&
            last.docWidth === next.docWidth &&
            last.docHeight === next.docHeight &&
            last.viewportWidth === next.viewportWidth &&
            last.viewportHeight === next.viewportHeight
          )
            return;
          lastStateRef.current = next;
          handle.onState?.(next);
          return;
        }
        case "selection":
          handle.onSelection?.({
            type: "selection",
            text: String(m.text ?? ""),
            x: Number(m.x),
            y: Number(m.y),
            w: Number(m.w),
            h: Number(m.h),
          });
          return;
        case "selection-cleared":
          handle.onSelectionCleared?.();
          return;
        case "located": {
          const requestId = String(m.requestId ?? "");
          const entry = locateCbsRef.current.get(requestId);
          if (!entry) return;
          locateCbsRef.current.delete(requestId);
          window.clearTimeout(entry.timer);
          const x = Number(m.x);
          const y = Number(m.y);
          entry.resolve(
            x >= 0 ? { x, y, w: Number(m.w) || 0, h: Number(m.h) || 0 } : null
          );
          return;
        }
        case "link-clicked": {
          const href = String(m.href ?? "");
          if (href) handle.onLinkClick?.(href);
          return;
        }
        case "esc": {
          const now = Date.now();
          if (now - lastEscAtRef.current < ESC_THROTTLE_MS) return;
          lastEscAtRef.current = now;
          handle.onEscape?.();
          return;
        }
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [nonce, epoch]);

  return {
    iframeRef,
    nonce,
    epoch,
    activate,
    locateQuote,
    scrollTo,
  };
}

function drainPendingLocates(pending: Map<string, PendingLocate>) {
  for (const [, entry] of pending) {
    window.clearTimeout(entry.timer);
    entry.resolve(null);
  }
  pending.clear();
}
