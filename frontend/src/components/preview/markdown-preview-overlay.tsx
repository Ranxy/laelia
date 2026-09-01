import {
  Download,
  FileText,
  List,
  Loader2,
  MessageSquare,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { formatBytes } from "@/components/chat/file-card";
import { Button } from "@/components/ui/button";
import { downloadAttachment } from "@/lib/file-download";
import { MarkdownRenderer } from "@/lib/markdown";
import { buildOutline, type OutlineItem } from "@/lib/markdown-file";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { CommentsAside } from "./comments-aside";
import { FilePreviewShell, PreviewPlaceholder } from "./file-preview-shell";

// MarkdownPreviewOverlay is a single, store-driven full-page overlay that
// renders a markdown attachment for focused reading. It portals into the
// overlay layer (z-2500) and covers the viewport, so neither the composer
// nor the thread is visible while reading — that is the intent. An optional
// left outline drawer lists the document's headings; clicking jumps to the
// heading. Comments are Phase 2.
export function MarkdownPreviewOverlay() {
  const { t } = useTranslation();
  const active = useAppStore((s) => s.activePreview);
  const closeFilePreview = useAppStore((s) => s.closeFilePreview);

  const [outline, setOutline] = useState<OutlineItem[]>([]);
  const [outlineOpen, setOutlineOpen] = useState(true);
  const [commentsOpen, setCommentsOpen] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<number | null>(null);

  // Build the outline after both the static markdown render and any deferred
  // code-block work have had a chance to commit their DOM nodes. The second
  // frame is harmless for already-settled content and prevents missing late
  // headings when Streamdown yields during a static render.
  useEffect(() => {
    if (!active || active.status !== "ready") {
      setOutline([]);
      return;
    }
    const firstFrame = requestAnimationFrame(() => {
      const secondFrame = requestAnimationFrame(() => {
        if (!contentRef.current) return;
        setOutline(buildOutline(contentRef.current));
        if (active.scrollToAnchorId) {
          document
            .getElementById(active.scrollToAnchorId)
            ?.scrollIntoView({ block: "start", behavior: "smooth" });
        }
      });
      frameRef.current = secondFrame;
    });
    frameRef.current = firstFrame;
    return () => {
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current);
      }
    };
  }, [active]);

  if (!active) return null;
  const { attachment } = active;

  const jumpToSection = (id: string) =>
    document
      .getElementById(id)
      ?.scrollIntoView({ block: "start", behavior: "smooth" });

  return (
    <FilePreviewShell
      icon={<FileText className="size-4 shrink-0 text-control-light" />}
      title={attachment.name}
      meta={formatBytes(attachment.sizeBytes)}
      className="bg-background"
      onEscape={closeFilePreview}
      actions={
        <>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setOutlineOpen((v) => !v)}
            aria-pressed={outlineOpen}
            aria-label={t("preview.outline")}
            className="flex items-center gap-1.5 px-2.5 py-1.5"
          >
            <List className="size-4" />
            <span className="hidden sm:inline">{t("preview.outline")}</span>
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => downloadAttachment(attachment)}
            aria-label={t("preview.download")}
            className="flex size-8 items-center justify-center p-0"
          >
            <Download className="size-4" />
          </Button>
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
        {outlineOpen && outline.length > 0 && (
          <aside className="hidden w-60 shrink-0 overflow-y-auto border-r border-control-border px-3 py-4 md:block">
            <OutlineList items={outline} onJump={jumpToSection} />
          </aside>
        )}
        <div ref={contentRef} className="flex-1 overflow-y-auto">
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
            <MarkdownRenderer
              content={active.content}
              variant="preview"
              className="mx-auto max-w-4xl px-6 py-8"
            />
          )}
        </div>
        {commentsOpen && active.status === "ready" && (
          <CommentsAside
            conversation={active.conversation}
            conversationId={active.conversationId}
            rootMessageId={active.rootMessageId}
            attachment={attachment}
            contentRef={contentRef}
            outline={outline}
            onJumpToSection={jumpToSection}
          />
        )}
      </div>
    </FilePreviewShell>
  );
}

function OutlineList({
  items,
  onJump,
}: {
  items: OutlineItem[];
  onJump: (id: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <nav className="flex flex-col gap-0.5">
      <span className="px-2 pb-2 text-[11px] font-semibold uppercase tracking-wide text-control-light">
        {t("preview.outline")}
      </span>
      {items.map((it) => (
        <button
          key={it.id}
          type="button"
          onClick={() => onJump(it.id)}
          title={`§ ${it.number} ${it.text}`}
          className={cn(
            "truncate rounded-md px-2 py-1 text-left text-sm text-control-placeholder transition-colors hover:bg-control-bg hover:text-main"
          )}
          style={{ paddingLeft: `${(it.level - 1) * 12 + 8}px` }}
        >
          <span className="mr-1.5 text-control-light">{it.number}</span>
          {it.text}
        </button>
      ))}
    </nav>
  );
}
