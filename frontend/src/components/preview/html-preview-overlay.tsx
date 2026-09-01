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
import { useTranslation } from "react-i18next";
import { formatBytes } from "@/components/chat/file-card";
import { Button } from "@/components/ui/button";
import { downloadAttachment } from "@/lib/file-download";
import {
  buildHtmlPreviewDoc,
  htmlAnchorForSelection,
  parseHtmlAnchor,
} from "@/lib/html-file";
import type { CommentAnchor } from "@/lib/markdown-file";
import { safeOpenExternal } from "@/lib/open-external";
import { useAppStore } from "@/stores";
import { usePreviewComments } from "./comments-panel";
import { FilePreviewShell, PreviewPlaceholder } from "./file-preview-shell";
import { HtmlCommentsAside } from "./html-comments-aside";
import {
  type HtmlPreviewDocState,
  type HtmlPreviewRect,
  useHtmlPreviewBridge,
} from "./html-preview-bridge";

const DEFAULT_STATE: HtmlPreviewDocState = {
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
// and document epoch on every message (see useHtmlPreviewBridge). Phase 1 is
// preview-only (links open in a new tab, Esc closes, scroll state drives the
// overlay markers); the comment aside is Phase 2 and reuses the markdown
// comment plumbing (Attachment sectionAnchor/sectionId/quotedText, thread
// replies).
export function HtmlPreviewOverlay() {
  const { t } = useTranslation();
  const active = useAppStore((s) => s.activePreview);
  const closeFilePreview = useAppStore((s) => s.closeFilePreview);

  const flashTimerRef = useRef<number | null>(null);

  const [iframeReady, setIframeReady] = useState(false);
  const [commentsOpen, setCommentsOpen] = useState(false);
  const [scroll, setScroll] = useState<HtmlPreviewDocState>(DEFAULT_STATE);
  const [pendingAnchor, setPendingAnchor] = useState<CommentAnchor | null>(
    null
  );
  const [pendingRect, setPendingRect] = useState<HtmlPreviewRect | null>(null);
  const [flash, setFlash] = useState<HtmlPreviewRect | null>(null);
  const [located, setLocated] = useState<Record<string, HtmlPreviewRect>>({});
  const [composerFocusKey, setComposerFocusKey] = useState(0);

  const attachmentId = active?.attachment.id ?? "";

  // Reset per-open state when a different file is previewed. The bridge's
  // nonce/epoch reset runs from its own effect over the same key.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset keyed on the previewed attachment; the body only clears state.
  useEffect(() => {
    setIframeReady(false);
    setCommentsOpen(false);
    setPendingAnchor(null);
    setPendingRect(null);
    setFlash(null);
    setLocated({});
  }, [attachmentId]);

  const comments = usePreviewComments(
    active?.conversation ?? "",
    active?.rootMessageId ?? "",
    attachmentId,
    commentsOpen
  );

  const bridge = useHtmlPreviewBridge(
    {
      onState: setScroll,
      onSelection: (msg) => {
        if (!commentsOpen) return;
        const rect = { x: msg.x, y: msg.y, w: msg.w, h: msg.h };
        const anchor = htmlAnchorForSelection(msg.text, rect.y + rect.h / 2);
        if (anchor && Number.isFinite(rect.x)) {
          setPendingAnchor(anchor);
          setPendingRect(rect);
        }
      },
      onSelectionCleared: () => {
        setPendingAnchor(null);
        setPendingRect(null);
      },
      onLinkClick: (href) => {
        // Bridge payloads come from untrusted preview documents; only
        // allow-listed schemes may reach window.open.
        if (!safeOpenExternal(href)) {
          console.warn("[html-preview] blocked link with rejected scheme");
        }
      },
      onEscape: closeFilePreview,
    },
    attachmentId
  );
  const { nonce, activate, scrollTo, locateQuote } = bridge;

  const srcDoc = useMemo(
    () => buildHtmlPreviewDoc(active?.content ?? "", nonce),
    [active?.content, nonce]
  );

  // F-B9: the flash timeout must not outlive the overlay.
  useEffect(
    () => () => {
      if (flashTimerRef.current) window.clearTimeout(flashTimerRef.current);
    },
    []
  );

  const flashRect = useCallback((rect: HtmlPreviewRect) => {
    setFlash(rect);
    if (flashTimerRef.current) window.clearTimeout(flashTimerRef.current);
    flashTimerRef.current = window.setTimeout(() => setFlash(null), 2000);
  }, []);

  // F-B9: locate promises settle asynchronously (bridge reply or the 3s
  // timeout), so their consumers must not touch state after the overlay
  // unmounted. The ref flips on unmount; the jumpToComment callback reads it.
  const mountedRef = useRef(true);
  useEffect(
    () => () => {
      mountedRef.current = false;
    },
    []
  );

  // Cross-scenario anchor jump: the preview was opened from a comment's
  // anchor chip; locate the quote (nearest to the stored content-y) and
  // scroll + flash it once the iframe is live.
  useEffect(() => {
    if (!active || active.kind !== "html" || !iframeReady) return;
    if (!active.scrollToAnchorId) return;
    const parsed = parseHtmlAnchor(active.scrollToAnchorId);
    if (!parsed) return;
    // Guard against overlay unmount / attachment switch while the locate
    // promise is still pending.
    let cancelled = false;
    if (active.scrollToQuote) {
      void locateQuote(active.scrollToQuote, parsed.y).then((rect) => {
        if (cancelled) return;
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
    return () => {
      cancelled = true;
    };
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
        // The promise can outlive the overlay (bridge timeout up to 3s);
        // never touch state after unmount (F-B9).
        if (!mountedRef.current || !rect) return;
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

  return (
    <FilePreviewShell
      icon={<FileText className="size-4 shrink-0 text-control-light" />}
      title={attachment.name}
      meta={formatBytes(attachment.sizeBytes)}
      className="bg-background"
      onEscape={closeFilePreview}
      actions={
        <>
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
        </>
      }
    >
      {/* Body */}
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1">
          {active.status === "loading" && (
            <PreviewPlaceholder
              icon={
                <Loader2 className="size-5 animate-spin text-control-light" />
              }
              text={t("preview.loading")}
            />
          )}
          {active.status === "error" && (
            <PreviewPlaceholder text={t("preview.error")} />
          )}
          {active.status === "too-large" && (
            <PreviewPlaceholder
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
                ref={bridge.iframeRef}
                title={attachment.name}
                sandbox="allow-scripts"
                srcDoc={srcDoc}
                referrerPolicy="no-referrer"
                onLoad={() => {
                  setIframeReady(true);
                  activate();
                }}
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
    </FilePreviewShell>
  );
}

function FlashRect({
  rect,
  scroll,
}: {
  rect: HtmlPreviewRect;
  scroll: HtmlPreviewDocState;
}) {
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
  rect: HtmlPreviewRect;
  scroll: HtmlPreviewDocState;
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
