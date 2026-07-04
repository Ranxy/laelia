import type { Attachment } from "@/types/proto-es/v1/command_pb";

// 10 MiB: above this we do not attempt to render a markdown file in-browser.
// The threshold is checked against attachment.sizeBytes before fetching, so
// an oversized file never triggers a download just to be refused.
export const MAX_MARKDOWN_PREVIEW_BYTES = 10n * 1024n * 1024n;

const MARKDOWN_NAME_RE = /\.(md|markdown|mdx)$/i;
const MARKDOWN_MIME = new Set(["text/markdown", "text/x-markdown"]);

// isMarkdownAttachment reports whether the attachment is plausibly markdown,
// judged by name extension first (uploads often carry no mime type) and then
// by the declared mime type.
export function isMarkdownAttachment(att: Attachment): boolean {
  if (att.mimeType && MARKDOWN_MIME.has(att.mimeType)) return true;
  return MARKDOWN_NAME_RE.test(att.name ?? "");
}

// isMarkdownPreviewable is true only when the file is markdown AND within the
// in-browser preview size limit. Oversized markdown still offers download.
export function isMarkdownPreviewable(att: Attachment): boolean {
  return (
    isMarkdownAttachment(att) &&
    (att.sizeBytes ?? 0n) <= MAX_MARKDOWN_PREVIEW_BYTES
  );
}

// slugify turns heading text into a URL-safe id fragment. Empty results fall
// back to "section" so we always produce a usable id.
export function slugify(text: string): string {
  const slug = text
    .toLowerCase()
    .trim()
    .replace(/[^\w\s-]/g, "")
    .replace(/[\s_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return slug || "section";
}

export interface OutlineItem {
  level: number; // 1..6
  text: string; // heading text with any leading doc number stripped
  id: string; // DOM id assigned to the heading
  number: string; // auto section number, e.g. "1", "1.1", "2", "2.1"
}

// A leading "1.1 " / "2.1 " on a heading is stripped from outline display so
// we don't double-number documents that already embed their own numbering.
const LEADING_NUMBER_RE = /^\d+(\.\d+)*\s+/;

// buildOutline scans rendered headings inside `container`, assigns each a
// stable DOM id, and returns outline entries with auto-numbered sections.
// Numbering is relative to the shallowest heading level present, so a document
// that starts at H2 (with H1 as a title) still numbers its first section "1".
// Must run after the markdown renderer has produced DOM (preview content is
// static `final`, so one rAF after render suffices). Idempotent: ids are
// reassigned deterministically by heading index.
export function buildOutline(container: HTMLElement): OutlineItem[] {
  const headings = Array.from(
    container.querySelectorAll("h1, h2, h3, h4, h5, h6")
  ) as HTMLHeadingElement[];
  if (headings.length === 0) return [];
  const levels = headings.map((h) => Number(h.tagName.slice(1)));
  const minLevel = Math.min(...levels);
  const counters: number[] = [];
  const items: OutlineItem[] = [];
  headings.forEach((h, i) => {
    const level = levels[i];
    const rel = level - minLevel; // 0-based depth
    counters[rel] = (counters[rel] ?? 0) + 1;
    // Truncate deeper-level counters so numbering resets under the new parent.
    counters.length = rel + 1;
    const number = counters.slice(0, rel + 1).join(".");
    const raw = (h.textContent ?? "").trim();
    const text = raw.replace(LEADING_NUMBER_RE, "");
    const id = `md-${i}-${slugify(text)}`;
    h.id = id;
    items.push({ level, text, id, number });
  });
  return items;
}
