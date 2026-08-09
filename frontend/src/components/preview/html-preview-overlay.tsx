import {
  Download,
  FileText,
  Loader2,
  MapPin,
  MessageSquare,
  Plus,
  X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { formatBytes } from "@/components/chat/file-card";
import { Button } from "@/components/ui/button";
import {
  getLayerRoot,
  LAYER_SURFACE_CLASS,
  usePreserveHigherLayerAccess,
} from "@/components/ui/layer";
import { downloadAttachment } from "@/lib/file-download";
import {
  buildHtmlPreviewDoc,
  htmlAnchorForSelection,
  parseHtmlAnchor,
  randomId,
} from "@/lib/html-file";
import type { CommentAnchor } from "@/lib/markdown-file";
import { useAppStore } from "@/stores";
import { HtmlCommentsAside, useHtmlComments } from "./html-comments-aside";

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

interface BridgeState {
  scrollX: number;
  scrollY: number;
  docWidth: number;
  docHeight: number;
  viewportWidth: number;
  viewportHeight: number;
}

const DEFAULT_STATE: BridgeState = {
  scrollX: 0,
  scrollY: 0,
  docWidth: 0,
  docHeight: 0,
  viewportWidth: 0,
  viewportHeight: 0,
};

// HtmlPreviewOverlay is the store-driven full-page preview for html
// attachments: a sandboxed iframe (srcDoc, sandbox="allow-scripts") renders
// the untrusted document with a bridge script inside. The parent and the
// bridge talk only through postMessage; the parent validates source, nonce
// and document epoch on every message. Phase 1 is preview-only (links open
// in a new tab, Esc closes, scroll state drives the overlay markers); the
// comment aside is Phase 2 and reuses the markdown comment plumbing
// (Attachment sectionAnchor/sectionId/quotedText, thread replies).
export function HtmlPreviewOverlay() {
  usePreserveHigherLayerAccess("overlay");
  const { t } = useTranslation();
  const active = useAppStore((s) => s.activePreview);
  const closeFilePreview = useAppStore((s) => s.closeFilePreview);

  const iframeRef = useRef<HTMLIFrameElement>(null);
  const flashTimerRef = useRef<number | null>(null);
  const locateCbsRef = useRef(new Map<string, (r: Rect | null) => void>());

  const [iframeReady, setIframeReady] = useState(false);
  const [commentsOpen, setCommentsOpen] = useState(false);
  const [scroll, setScroll] = useState<BridgeState>(DEFAULT_STATE);
  const [pendingAnchor, setPendingAnchor] = useState<CommentAnchor | null>(
    null
  );
  const [pendingRect, setPendingRect] = useState<Rect | null>(null);
  const [flash, setFlash] = useState<Rect | null>(null);
  const [located, setLocated] = useState<Record<string, Rect>>({});
  const [composerFocusKey, setComposerFocusKey] = useState(0);

  const attachmentId = active?.attachment.id ?? "";
  // nonce/epoch are per-open secrets: the bridge echoes them back, and the
  // epoch is only issued after the iframe loads, so a document that
  // navigates itself away can never speak for the original preview.
  const nonce = useMemo(() => randomId(), [attachmentId]);
  const epoch = useMemo(() => randomId(), [attachmentId]);
  const srcDoc = useMemo(
    () => buildHtmlPreviewDoc(active?.content ?? "", nonce),
    [active?.content, nonce]
  );

  const comments = useHtmlComments(
    active?.conversation ?? "",
    active?.rootMessageId ?? "",
    attachmentId,
    commentsOpen
  );

  // Reset per-open state when a different file is previewed.
  useEffect(() => {
    setIframeReady(false);
    setCommentsOpen(false);
    setPendingAnchor(null);
    setPendingRect(null);
    setFlash(null);
    setLocated({});
  }, [attachmentId]);

  const postToIframe = useCallback(
    (msg: Record<string, unknown>) => {
      iframeRef.current?.contentWindow?.postMessage(
        { slockAcBridge: 1, nonce, documentEpoch: epoch, ...msg },
        "*"
      );
    },
    [nonce, epoch]
  );

  // locateQuote asks the bridge to find the content rect of `quote`,
  // preferring the occurrence nearest to nearY. Used for jump targets and
  // comment pins; times out so a poisoned document can't hang the UI.
  const locateQuote = useCallback(
    (quote: string, nearY: number | null): Promise<Rect | null> =>
      new Promise((resolve) => {
        const requestId = randomId();
        locateCbsRef.current.set(requestId, resolve);
        postToIframe({
          type: "locate",
          requestId,
          quote: quote.slice(0, 500),
          nearY: nearY ?? "",
        });
        window.setTimeout(() => {
          if (locateCbsRef.current.delete(requestId)) resolve(null);
        }, 3000);
      }),
    [postToIframe]
  );

  const scrollTo = useCallback(
    (x: number, y: number) => postToIframe({ type: "scroll-to", x, y }),
    [postToIframe]
  );

  const flashRect = useCallback((rect: Rect) => {
    setFlash(rect);
    if (flashTimerRef.current) window.clearTimeout(flashTimerRef.current);
    flashTimerRef.current = window.setTimeout(() => setFlash(null), 2000);
  }, []);

  const handleLoad = useCallback(() => {
    setIframeReady(true);
    postToIframe({ type: "activate-document" });
  }, [postToIframe]);

  // Bridge messages: validate source + nonce + epoch before trusting anything.
  useEffect(() => {
    if (!active) return;
    const onMessage = (e: MessageEvent) => {
      const d = e.data;
      if (!d || typeof d !== "object") return;
      if (e.source !== iframeRef.current?.contentWindow) return;
      if (
        d.slockAcBridge !== 1 ||
        d.nonce !== nonce ||
        d.documentEpoch !== epoch
      )
        return;
      switch (d.type) {
        case "state":
          setScroll({
            scrollX: Number(d.scrollX) || 0,
            scrollY: Number(d.scrollY) || 0,
            docWidth: Number(d.docWidth) || 0,
            docHeight: Number(d.docHeight) || 0,
            viewportWidth: Number(d.viewportWidth) || 0,
            viewportHeight: Number(d.viewportHeight) || 0,
          });
          break;
        case "selection": {
          if (!commentsOpen) break;
          const rect = {
            x: Number(d.x),
            y: Number(d.y),
            w: Number(d.w),
            h: Number(d.h),
          };
          const anchor = htmlAnchorForSelection(
            String(d.text ?? ""),
            rect.y + rect.h / 2
          );
          if (anchor && Number.isFinite(rect.x)) {
            setPendingAnchor(anchor);
            setPendingRect(rect);
          }
          break;
        }
        case "selection-cleared":
          setPendingAnchor(null);
          setPendingRect(null);
          break;
        case "located": {
          const requestId = String(d.requestId ?? "");
          const cb = locateCbsRef.current.get(requestId);
          if (!cb) break;
          locateCbsRef.current.delete(requestId);
          const x = Number(d.x);
          const y = Number(d.y);
          cb(
            x >= 0 ? { x, y, w: Number(d.w) || 0, h: Number(d.h) || 0 } : null
          );
          break;
        }
        case "link-clicked": {
          const href = String(d.href ?? "");
          if (href) window.open(href, "_blank", "noopener,noreferrer");
          break;
        }
        case "esc":
          closeFilePreview();
          break;
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [active, nonce, epoch, commentsOpen, closeFilePreview]);

  // Esc closes the overlay (parent keydown; iframe Esc comes via the bridge).
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeFilePreview();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, closeFilePreview]);

  // Cross-scenario anchor jump: the preview was opened from a comment's
  // anchor chip; locate the quote (nearest to the stored content-y) and
  // scroll + flash it once the iframe is live.
  useEffect(() => {
    if (!active || active.kind !== "html" || !iframeReady) return;
    if (!active.scrollToAnchorId) return;
    const parsed = parseHtmlAnchor(active.scrollToAnchorId);
    if (!parsed) return;
    if (active.scrollToQuote) {
      void locateQuote(active.scrollToQuote, parsed.y).then((rect) => {
        if (!rect) {
          scrollTo(0, parsed.y);
          return;
        }
        scrollTo(rect.x, rect.y);
        flashRect(rect);
      });
    } else {
      scrollTo(0, parsed.y);
    }
  }, [active, iframeReady, locateQuote, scrollTo, flashRect]);

  // Locate every existing comment's quote once when the aside opens, so pins
  // can be drawn over the iframe at the right content positions.
  useEffect(() => {
    if (!commentsOpen || !iframeReady || !active) return;
    let cancelled = false;
    for (const m of comments) {
      const att = m.attachments?.find(
        (a) => a.sectionAnchor !== "" && a.id === active.attachment.id
      );
      if (!att?.quotedText) continue;
      const parsed = parseHtmlAnchor(att.sectionId ?? "");
      void locateQuote(att.quotedText, parsed?.y ?? null).then((rect) => {
        if (!cancelled && rect) {
          setLocated((prev) => ({ ...prev, [m.id]: rect }));
        }
      });
    }
    return () => {
      cancelled = true;
    };
  }, [commentsOpen, iframeReady, comments, active, locateQuote]);

  // Clicking a comment card in the aside: locate the quote and scroll+flash.
  const jumpToComment = useCallback(
    (sectionId: string, quote: string) => {
      if (!quote) return;
      const parsed = parseHtmlAnchor(sectionId);
      void locateQuote(quote, parsed?.y ?? null).then((rect) => {
        if (!rect) return;
        scrollTo(rect.x, rect.y);
        flashRect(rect);
      });
    },
    [locateQuote, scrollTo, flashRect]
  );

  const clearPendingAnchor = useCallback(() => {
    setPendingAnchor(null);
    setPendingRect(null);
  }, []);

  if (!active) return null;
  const { attachment } = active;

  return createPortal(
    <div
      className={`fixed inset-0 ${LAYER_SURFACE_CLASS} flex flex-col bg-background`}
    >
      {/* Top bar */}
      <div className="flex h-14 shrink-0 items-center gap-2 border-b border-control-border px-4">
        <FileText className="size-4 shrink-0 text-control-light" />
        <span className="truncate text-sm font-medium text-main">
          {attachment.name}
        </span>
        <span className="shrink-0 text-xs text-control-placeholder">
          {formatBytes(attachment.sizeBytes)}
        </span>
        <div className="flex-1" />
        {active.status === "ready" && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setCommentsOpen((v) => !v)}
            aria-pressed={commentsOpen}
            aria-label={t("preview.comments")}
            className="flex items-center gap-1.5 px-2.5 py-1.5"
          >
            <MessageSquare className="size-4" />
            <span className="hidden sm:inline">{t("preview.comments")}</span>
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          onClick={() => downloadAttachment(attachment)}
          aria-label={t("preview.download")}
          className="flex size-8 items-center justify-center p-0"
        >
          <Download className="size-4" />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={closeFilePreview}
          aria-label={t("common.close")}
          className="flex size-8 items-center justify-center p-0"
        >
          <X className="size-4" />
        </Button>
      </div>

      {/* Body */}
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1">
          {active.status === "loading" && (
            <Placeholder
              icon={
                <Loader2 className="size-5 animate-spin text-control-light" />
              }
              text={t("preview.loading")}
            />
          )}
          {active.status === "error" && (
            <Placeholder text={t("preview.error")} />
          )}
          {active.status === "too-large" && (
            <Placeholder
              text={t("preview.too-large", {
                size: formatBytes(attachment.sizeBytes),
              })}
              action={
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => downloadAttachment(attachment)}
                  className="flex items-center gap-1.5"
                >
                  <Download className="size-4" />
                  {t("preview.download")}
                </Button>
              }
            />
          )}
          {active.status === "ready" && (
            <>
              <iframe
                ref={iframeRef}
                title={attachment.name}
                sandbox="allow-scripts"
                srcDoc={srcDoc}
                referrerPolicy="no-referrer"
                onLoad={handleLoad}
                className="absolute inset-0 h-full w-full border-0 bg-white"
              />
              {flash && <FlashRect rect={flash} scroll={scroll} />}
              {commentsOpen &&
                Object.entries(located).map(([id, rect]) => (
                  <CommentPin
                    key={id}
                    rect={rect}
                    scroll={scroll}
                    onClick={() => scrollTo(rect.x, rect.y)}
                  />
                ))}
              {commentsOpen && pendingAnchor && pendingRect && (
                <button
                  type="button"
                  onClick={() => setComposerFocusKey((k) => k + 1)}
                  className="absolute z-10 flex items-center gap-1 rounded-full border border-control-border bg-background px-2.5 py-1 text-xs font-medium text-main shadow-md hover:bg-control-bg"
                  style={{
                    left: pendingRect.x + pendingRect.w / 2 - scroll.scrollX,
                    top: pendingRect.y + pendingRect.h - scroll.scrollY + 8,
                    transform: "translateX(-50%)",
                  }}
                >
                  <Plus className="size-3" />
                  {t("preview.html-add-comment")}
                </button>
              )}
            </>
          )}
        </div>
        {commentsOpen && active.status === "ready" && (
          <HtmlCommentsAside
            conversationId={active.conversationId}
            rootMessageId={active.rootMessageId}
            attachment={attachment}
            comments={comments}
            pendingAnchor={pendingAnchor}
            focusKey={composerFocusKey}
            onClearPendingAnchor={clearPendingAnchor}
            onJumpToComment={jumpToComment}
          />
        )}
      </div>
    </div>,
    getLayerRoot("overlay")
  );
}

function FlashRect({ rect, scroll }: { rect: Rect; scroll: BridgeState }) {
  return (
    <div
      className="pointer-events-none absolute z-10 border-2 border-accent bg-accent/15"
      style={{
        left: rect.x - scroll.scrollX,
        top: rect.y - scroll.scrollY,
        width: rect.w,
        height: rect.h,
      }}
    />
  );
}

function CommentPin({
  rect,
  scroll,
  onClick,
}: {
  rect: Rect;
  scroll: BridgeState;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label="comment"
      className="absolute z-10 cursor-pointer"
      style={{
        left: rect.x - scroll.scrollX,
        top: rect.y - scroll.scrollY,
        transform: "translate(-6px, -100%)",
      }}
    >
      <MapPin className="size-4 text-accent drop-shadow" />
    </button>
  );
}

function Placeholder({
  icon,
  text,
  action,
}: {
  icon?: React.ReactNode;
  text: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      {icon}
      <p className="text-sm text-control">{text}</p>
      {action}
    </div>
  );
}
