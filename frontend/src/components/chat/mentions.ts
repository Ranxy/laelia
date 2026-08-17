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

// isMentionNameRune mirrors the backend's mention-name character class
// (letters, digits, '_', '-', '.'): the runes allowed inside a @handle. It is
// used as the trailing/leading boundary so a short handle is not matched inside
// a longer one (e.g. "@ran-user-1" inside "@ran-user-10"), and so an email
// local-part like "ran-user-1@x" does not trigger a mention on the "@".
function isMentionNameRune(code: number): boolean {
  return (
    (code >= 0x30 && code <= 0x39) || // 0-9
    (code >= 0x41 && code <= 0x5a) || // A-Z
    (code >= 0x61 && code <= 0x7a) || // a-z
    code === 0x5f || // _
    code === 0x2d || // -
    code === 0x2e || // .
    // Non-ASCII letters/digits: handles may be slugified from CJK or other
    // scripts, and a trailing CJK letter continues the handle the same way an
    // ASCII letter does.
    code > 0x7f
  );
}

// continuesHandle reports whether the rune at `idx` would extend a matched
// handle (so the match is incomplete and a longer handle should be tried
// instead). It mirrors the backend's trailing-dot stripping: a '.' is a valid
// INTERNAL handle separator (e.g. "team.lead-user-1") but a TRAILING '.' is
// sentence-ending punctuation, so it only continues the handle when another
// name rune follows. Without this, "@para-agent-1." at the end of a sentence
// would be rejected (the '.' after the handle is a name rune), and the badge
// would never render even though the backend resolved the mention.
function continuesHandle(content: string, idx: number): boolean {
  if (idx >= content.length) return false;
  const code = content.charCodeAt(idx);
  if (!isMentionNameRune(code)) return false;
  if (code === 0x2e /* '.' */) {
    return (
      idx + 1 < content.length && isMentionNameRune(content.charCodeAt(idx + 1))
    );
  }
  return true;
}

// isLeadingBoundary reports whether the rune at `idx` (immediately before an
// "@") is a valid mention boundary — i.e. the "@" starts a fresh token rather
// than continuing a word. This mirrors the backend's isMentionBoundary: space
// or punctuation (except '_' and '-') is a boundary. A '.' IS a boundary here
// (unlike in continuesHandle, where an internal '.' is part of the handle), so
// "end.@para-agent-1" still resolves — matching the backend, which treats '.'
// as a boundary before "@".
function isLeadingBoundary(code: number): boolean {
  // '.' is punctuation and a boundary before "@" (matches backend
  // isMentionBoundary).
  if (code === 0x2e /* '.' */) return true;
  // '_' and '-' are name runes, NOT boundaries (they continue a word).
  if (code === 0x5f || code === 0x2d) return false;
  // Other name runes (letters, digits) are not boundaries.
  if (isMentionNameRune(code)) return false;
  // Everything else (space, punctuation, non-ASCII punct) is a boundary.
  return true;
}

export function splitByMentions(
  content: string,
  mentions: MentionRef[]
): ContentSegment[] {
  if (mentions.length === 0) return [{ text: content, mention: null }];

  // Map each handle to its mention ref. The backend dedups mentions by
  // type:id, so a member @mentioned several times arrives as a single Mention
  // — but EVERY occurrence in the content must still render as a badge, so we
  // scan the whole content rather than matching each mention once.
  const byHandle = new Map<string, MentionRef>();
  for (const m of mentions) {
    if (!byHandle.has(m.name)) byHandle.set(m.name, m);
  }

  // Longest handle first so a longer handle is tried before a shorter prefix
  // (e.g. "ran-user-10" before "ran-user-1"); the trailing-boundary check
  // below then rejects the shorter match if a longer handle continues there,
  // so "@ran-user-1" can never match inside "@ran-user-10".
  const handles = [...byHandle.keys()].sort((a, b) => b.length - a.length);

  const segments: ContentSegment[] = [];
  let lastIndex = 0;

  // Scan content left to right for the next "@" that begins a known handle,
  // checking both boundaries ourselves (rather than relying on a lookbehind,
  // which is not supported by every target engine) so a preceding mention-name
  // rune — e.g. the "1" in "ran-user-1@x.com" — never starts a mention.
  const atRe = /@/g;
  let at: RegExpExecArray | null;
  while ((at = atRe.exec(content)) !== null) {
    const atIdx = at.index;
    // Leading boundary: the char before "@" must be a boundary (mirrors the
    // backend's isMentionBoundary). At index 0 there is no preceding char,
    // which is a valid boundary.
    if (atIdx > 0 && !isLeadingBoundary(content.charCodeAt(atIdx - 1))) {
      continue;
    }
    // Try each handle (longest first) at this position.
    let matched: MentionRef | null = null;
    let matchedLen = 0;
    for (const h of handles) {
      if (content.startsWith(h, atIdx + 1)) {
        // Trailing boundary: the char after the handle must not continue the
        // handle, otherwise a longer handle continues here (or a trailing
        // '.' is part of the handle). A trailing '.' is punctuation, not a
        // continuation — see continuesHandle.
        const afterIdx = atIdx + 1 + h.length;
        if (afterIdx < content.length && continuesHandle(content, afterIdx)) {
          continue;
        }
        matched = byHandle.get(h) ?? null;
        matchedLen = h.length;
        break;
      }
    }
    if (!matched) continue;

    if (atIdx > lastIndex) {
      segments.push({ text: content.slice(lastIndex, atIdx), mention: null });
    }
    segments.push({ text: "", mention: matched });
    lastIndex = atIdx + 1 + matchedLen;
    // Skip past the matched handle so we don't re-scan its runes for "@".
    atRe.lastIndex = lastIndex;
  }

  if (lastIndex < content.length) {
    segments.push({ text: content.slice(lastIndex), mention: null });
  }

  return segments;
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

// label is the display text shown inside the badge (the member's display
// name, or "name(handle)" when the channel has same-named members); it is
// purely cosmetic — matching and click dispatch still use the handle in
// `name`. When omitted the badge falls back to the handle itself.
export function mentionTagMarkdown(m: MentionRef, label?: string): string {
  const text = escapeHtml(`@${label ?? m.name}`);
  const labelAttr = label ? ` label="${escapeHtml(label)}"` : "";
  return `<mention type="${escapeHtml(m.type)}" id="${escapeHtml(m.id)}" name="${escapeHtml(m.name)}"${labelAttr}>${text}</mention>`;
}

// Rewrites a message body so every @mention matched by splitByMentions becomes
// an inline <mention> node, while leaving the surrounding text (and any other
// markdown) intact. Reuses splitByMentions so the matching/dedup behavior is
// identical to the plain-text segment path used for user messages. labelFor
// maps a mention handle to its display label (see mentionTagMarkdown).
export function contentWithMentionTags(
  content: string,
  mentions: MentionRef[],
  labelFor?: (handle: string) => string | undefined
): string {
  if (mentions.length === 0) return content;
  return splitByMentions(content, mentions)
    .map((seg) =>
      seg.mention
        ? mentionTagMarkdown(seg.mention, labelFor?.(seg.mention.name))
        : seg.text
    )
    .join("");
}
