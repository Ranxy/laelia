import { MarkdownCodeBlockNode, setCustomComponents } from "markstream-react";

// Centralizes the markstream-react custom component registry so both chat pages
// share the exact same registration. Calling setCustomComponents at module
// top-level mutates a global registry; importing this module from each page
// guarantees the registration runs exactly once and is never overwritten by a
// second import (which would happen if each page registered independently).
//
// The `registered` guard is a belt-and-suspenders against double-import (e.g.
// under Vite HMR or lazy route prefetch) so the registry is only touched on the
// very first evaluation of this module.
let registered = false;

// Renders an inline @mention badge inside agent markdown. markstream emits a
// CustomComponentNode for the <mention> tag (enabled per-render via
// customHtmlTags); this component turns it into a styled chip carrying the
// mention ref as data-* attributes. A delegated click handler on the message
// bubble reads those attributes and dispatches onMentionClick. Keyboard
// activation forwards to the same delegated click. No <a href> is used so the
// browser never surfaces the mention ref as a hover tooltip / status preview.
//
// `markstreamDisplay: "inline"` keeps the node inline within its paragraph so
// the badge flows with the surrounding prose.
// biome-ignore lint/suspicious/noExplicitAny: markstream custom component API is loosely typed
function MentionChip({ node }: any) {
  const attrs: Record<string, string> = {};
  const raw = (node as { attrs?: unknown }).attrs;
  if (Array.isArray(raw)) {
    for (const entry of raw) {
      if (Array.isArray(entry)) {
        attrs[entry[0]] = String(entry[1]);
      } else if (entry && typeof entry === "object") {
        const e = entry as { name?: string; value?: unknown };
        if (e.name) attrs[e.name] = String(e.value ?? "");
      }
    }
  } else if (raw && typeof raw === "object") {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      attrs[k] = String(v);
    }
  }
  const name = attrs.name ?? "";
  // label is the cosmetic display text (display name, or "name(handle)" for
  // same-named members); it never participates in matching or click dispatch,
  // which both use the handle in `name`.
  const label = attrs.label ?? name;
  return (
    <span
      data-mtype={attrs.type}
      data-mid={attrs.id}
      data-mname={name}
      role="button"
      tabIndex={0}
      className="mention-chip"
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          (e.currentTarget as HTMLElement).click();
        }
      }}
    >
      @{label}
    </span>
  );
}
MentionChip.markstreamDisplay = "inline" as const;

function registerMarkdownComponents(): void {
  if (registered) return;
  registered = true;
  setCustomComponents({
    // biome-ignore lint/suspicious/noExplicitAny: markstream custom component API is loosely typed
    code_block: ({ node, isDark, ctx }: any) => (
      // Keep only the copy action; the other header controls are unused and
      // their collapse/expand handling is currently broken in markstream-react.
      <MarkdownCodeBlockNode
        node={node}
        isDark={isDark}
        stream={ctx?.codeBlockStream}
        {...(ctx?.codeBlockProps ?? {})}
        showCollapseButton={false}
        showExpandButton={false}
        showPreviewButton={false}
        showFontSizeButtons={false}
        enableFontSizeControl={false}
      />
    ),
    mention: MentionChip,
  });
}

// Side-effect import: any module importing this file gets the registration
// applied exactly once. Markstream's registry is global per app instance, so a
// single registration covers every <MarkdownRender> consumer.
registerMarkdownComponents();

export {};
