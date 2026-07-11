import {
  Download,
  FileText,
  List,
  Loader2,
  MessageSquare,
  X,
} from "lucide-react";
import MarkdownRender from "markstream-react";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { formatBytes } from "@/components/chat/file-card";
import { Button } from "@/components/ui/button";
import {
  getLayerRoot,
  usePreserveHigherLayerAccess,
} from "@/components/ui/layer";
import { downloadAttachment } from "@/lib/file-download";
import { buildOutline, type OutlineItem } from "@/lib/markdown-file";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { CommentsAside } from "./comments-aside";

// MarkdownPreviewOverlay is a single, store-driven full-page overlay that
// renders a markdown attachment for focused reading. It portals into the
// overlay layer (z-2500) and covers the viewport, so neither the composer
// nor the thread is visible while reading — that is the intent. An optional
// left outline drawer lists the document's headings; clicking jumps to the
// heading. Comments are Phase 2.
export function MarkdownPreviewOverlay() {
  usePreserveHigherLayerAccess("overlay");
  const { t } = useTranslation();
  const active = useAppStore((s) => s.activePreview);
  const closeFilePreview = useAppStore((s) => s.closeFilePreview);

  const [outline, setOutline] = useState<OutlineItem[]>([]);
  const [outlineOpen, setOutlineOpen] = useState(true);
  const [commentsOpen, setCommentsOpen] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);

  // Esc closes the overlay. Bound only while open.
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeFilePreview();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, closeFilePreview]);

  // Build the outline once the markdown DOM is painted. The content is static
  // (`final`), so a single rAF after `status === "ready"` suffices. When the
  // preview was opened with a scrollToSectionId (cross-scenario anchor jump
  // from a comment card), scroll to that heading right after ids are assigned.
  useEffect(() => {
    if (!active || active.status !== "ready") {
      setOutline([]);
      return;
    }
    const id = requestAnimationFrame(() => {
      if (!contentRef.current) return;
      setOutline(buildOutline(contentRef.current));
      if (active.scrollToSectionId) {
        document
          .getElementById(active.scrollToSectionId)
          ?.scrollIntoView({ block: "start", behavior: "smooth" });
      }
    });
    return () => cancelAnimationFrame(id);
  }, [active?.status, active?.content, active?.scrollToSectionId]);

  if (!active) return null;
  const { attachment } = active;

  const jumpToSection = (id: string) =>
    document
      .getElementById(id)
      ?.scrollIntoView({ block: "start", behavior: "smooth" });

  return createPortal(
    <div className="fixed inset-0 z-10 flex flex-col bg-background">
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
      </div>

      {/* Body */}
      <div className="flex min-h-0 flex-1">
        {outlineOpen && outline.length > 0 && (
          <aside className="hidden w-60 shrink-0 overflow-y-auto border-r border-control-border px-3 py-4 md:block">
            <OutlineList items={outline} onJump={jumpToSection} />
          </aside>
        )}
        <div ref={contentRef} className="flex-1 overflow-y-auto">
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
            <div className="markstream-chat mx-auto max-w-4xl px-6 py-8">
              <MarkdownRender
                customId="md-preview"
                content={active.content}
                final
                fade
                batchRendering
                deferNodesUntilVisible={false}
              />
            </div>
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
    </div>,
    getLayerRoot("overlay")
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
