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
    threadRoot: msg.threadRoot || undefined,
    threadReplyCount: msg.threadReplyCount || undefined,
  };
}

// appendNewMessages appends the messages from `delta` whose id is not already
// present in `prev`, preserving delta order. It is the incremental companion to
// the cursor-based watcher: each poll fetches only messages newer than the last
// seen room_version (a small delta), so reconciliation is a dedup-and-append
// rather than a full-list re-merge. Returns the exact same reference as `prev`
// when nothing was added, so the store setter (and its subscribers) can bail out
// entirely and polling does not churn the array identity.
export function appendNewMessages(
  prev: ChatMessageUI[],
  delta: ChatMessageUI[]
): ChatMessageUI[] {
  if (delta.length === 0) return prev;
  const seen = new Set(prev.map((m) => m.id));
  const fresh = delta.filter((m) => !seen.has(m.id));
  if (fresh.length === 0) return prev;
  return [...prev, ...fresh];
}
