// Splits message content into plain-text segments interleaved with mention
// markers so the channel message row can render @mentions as badges while
// leaving the surrounding text intact. Used by channel-chat (and available
// for any other mention-aware message renderer).
export interface MentionRef {
  type: string;
  id: string;
  name: string;
}

export interface ContentSegment {
  text: string;
  mention: MentionRef | null;
}

export function splitByMentions(
  content: string,
  mentions: MentionRef[]
): ContentSegment[] {
  if (mentions.length === 0) return [{ text: content, mention: null }];

  const segments: ContentSegment[] = [];
  const sorted = [...mentions].sort((a, b) => {
    const ai = content.indexOf(`@${a.name}`);
    const bi = content.indexOf(`@${b.name}`);
    return ai - bi;
  });

  let lastIndex = 0;
  const used = new Set<string>();

  for (const m of sorted) {
    const pattern = `@${escapeRegex(m.name)}`;
    const re = new RegExp(pattern, "g");
    re.lastIndex = lastIndex;
    const match = re.exec(content);
    if (!match) continue;

    const idx = match.index;
    const key = `${idx}-${m.name}`;
    if (used.has(key)) continue;
    used.add(key);

    if (idx > lastIndex) {
      segments.push({ text: content.slice(lastIndex, idx), mention: null });
    }
    segments.push({ text: "", mention: m });
    lastIndex = idx + match[0].length;
  }

  if (lastIndex < content.length) {
    segments.push({ text: content.slice(lastIndex), mention: null });
  }

  return segments;
}

function escapeRegex(str: string): string {
  return str.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Mentions inside agent markdown are rendered as a custom inline <mention>
// node (registered in lib/markdown) so the badge flows inline with the
// surrounding prose instead of being forced onto its own line by per-segment
// block <p> wrappers. The node carries {type, id, name} as attributes; a
// delegated click handler on the bubble reads them back to dispatch
// onMentionClick. Using a custom tag (rather than a markdown link) avoids
// exposing the mention ref in an <a href>, which the browser surfaces as a
// hover tooltip / status-bar preview.
function escapeHtml(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

export function mentionTagMarkdown(m: MentionRef): string {
  const text = escapeHtml(`@${m.name}`);
  return `<mention type="${escapeHtml(m.type)}" id="${escapeHtml(m.id)}" name="${escapeHtml(m.name)}">${text}</mention>`;
}

// Rewrites a message body so every @mention matched by splitByMentions becomes
// an inline <mention> node, while leaving the surrounding text (and any other
// markdown) intact. Reuses splitByMentions so the matching/dedup behavior is
// identical to the plain-text segment path used for user messages.
export function contentWithMentionTags(
  content: string,
  mentions: MentionRef[]
): string {
  if (mentions.length === 0) return content;
  return splitByMentions(content, mentions)
    .map((seg) => (seg.mention ? mentionTagMarkdown(seg.mention) : seg.text))
    .join("");
}
