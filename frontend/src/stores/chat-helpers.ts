import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { ChatMessage } from "@/types/proto-es/v1/command_pb";
import type { ChatMessageUI } from "./types";

// toUiMessage is the single mapper from a backend ChatMessage to the UI shape.
// It always populates mentions/attachments (previously omitted by three of the
// four call sites, which silently dropped mentions during channel polling). Any
// extra fields that the streaming path grafts on (commandName/events/status)
// are layered by the caller via spread.
export function toUiMessage(msg: ChatMessage): ChatMessageUI {
  return {
    id: msg.name,
    role: msg.role === 1 ? "user" : "assistant",
    content: msg.content,
    timestamp: msg.createdAt ? timestampDate(msg.createdAt) : new Date(),
    commandId: msg.commandId || undefined,
    senderName: msg.senderName || undefined,
    senderType: msg.senderType || undefined,
    mentions: msg.mentions,
    attachments: msg.attachments,
  };
}

// sameArray compares two arrays by value (length + serialized elements).
// proto-es repeated fields are always arrays, but UI messages created locally
// (e.g. optimistic user messages) may leave the field undefined; treat missing
// as empty so a backend round-trip that adds nothing is still "unchanged".
function sameArray(
  a: unknown[] | undefined,
  b: unknown[] | undefined
): boolean {
  const aa = a ?? [];
  const bb = b ?? [];
  if (aa.length !== bb.length) return false;
  return aa.every((v, i) => JSON.stringify(v) === JSON.stringify(bb[i]));
}

// messagesEqual is a value comparison across every field the UI renders. It is
// deliberately field-by-field so that a backend round-trip that only adds
// attachments or mentions (leaving content/role untouched) is detected as a
// change and the new object is propagated to subscribers.
function messagesEqual(a: ChatMessageUI, b: ChatMessageUI): boolean {
  return (
    a.content === b.content &&
    a.role === b.role &&
    a.senderName === b.senderName &&
    a.senderType === b.senderType &&
    (a.commandId ?? "") === (b.commandId ?? "") &&
    a.timestamp.getTime() === b.timestamp.getTime() &&
    sameArray(a.mentions, b.mentions) &&
    sameArray(a.attachments, b.attachments)
  );
}

// mergeMessages reconciles a freshly polled message list with the cached one.
// Unchanged messages keep their previous object reference so React.memo can skip
// re-rendering them, and an unchanged list returns the exact same reference so
// the store setter (and its subscribers) can bail out entirely.
export function mergeMessages(
  prev: ChatMessageUI[],
  next: ChatMessageUI[]
): ChatMessageUI[] {
  if (prev.length === 0 || next.length === 0) return next;
  if (prev[0].id !== next[0].id) return next;

  const prevById = new Map(prev.map((m) => [m.id, m]));
  let changed = false;
  const out: ChatMessageUI[] = [];
  for (const n of next) {
    const p = prevById.get(n.id);
    if (p && messagesEqual(p, n)) {
      out.push(p);
    } else {
      out.push(n);
      changed = true;
    }
  }
  return changed || out.length !== prev.length ? out : prev;
}
