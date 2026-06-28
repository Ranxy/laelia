import { timestampDate } from "@bufbuild/protobuf/wkt";
import type { ChatMessage, CommandEvent } from "@/types/proto-es/v1/command_pb";
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

// lastEventSeqNo is the high-water mark of events already received for a
// command; watchCommandEvents replays from after this seq so a reconnected
// stream does not duplicate events the store already has.
export function lastEventSeqNo(events: CommandEvent[] | undefined): number {
  return events && events.length > 0 ? events[events.length - 1].seqNo : -1;
}

// omitKey returns a copy of rec without key, without mutating rec.
export function omitKey<T>(
  rec: Record<string, T>,
  key: string
): Record<string, T> {
  const rest: Record<string, T> = {};
  for (const k of Object.keys(rec)) {
    if (k !== key) rest[k] = rec[k];
  }
  return rest;
}

// finalizeAssistant merges a patch into the assistant message identified by
// commandName within the cached list, returning the same array reference when
// no message matched (so subscribers bail out). Used by streamChatCommand's
// cleanup to mark streaming done while preserving every other message —
// including ones a concurrent watcher poll or a new send may have appended
// between the stream closing and the cleanup set.
export function finalizeAssistant(
  msgs: ChatMessageUI[],
  commandName: string,
  patch: Partial<ChatMessageUI>
): ChatMessageUI[] {
  let changed = false;
  const out = msgs.map((m) => {
    if (m.commandName === commandName) {
      changed = true;
      return { ...m, ...patch };
    }
    return m;
  });
  return changed ? out : msgs;
}

// abortableSleep resolves after ms, or immediately if signal aborts. Used so
// the reconnect backoff unblocks on abort instead of waiting the full delay.
export function abortableSleep(
  ms: number,
  signal: AbortSignal | undefined
): Promise<void> {
  if (signal?.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true }
    );
  });
}
