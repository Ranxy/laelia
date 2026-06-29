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
