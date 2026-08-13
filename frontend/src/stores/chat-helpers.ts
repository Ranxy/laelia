import { timestampDate } from "@bufbuild/protobuf/wkt";
import { type ChatMessage, SenderType } from "@/types/proto-es/v1/command_pb";
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
    agentId: msg.agentId || undefined,
    senderName: msg.senderName || undefined,
    senderType: msg.senderType || undefined,
    principalId: msg.principalId || undefined,
    mentions: msg.mentions,
    attachments: msg.attachments,
    threadRoot: msg.threadRoot || undefined,
    threadReplyCount: msg.threadReplyCount || undefined,
    roomVersion: msg.roomVersion || undefined,
    task: msg.task
      ? {
          taskNumber: msg.task.taskNumber,
          status: msg.task.status,
          assigneeName: msg.task.assigneeName || undefined,
          assigneeResourceId: msg.task.assigneeResourceId || undefined,
        }
      : undefined,
  };
}

// isOwnUserMessage reports whether a user-direction message was sent by the
// current user, so the UI can render it as "You" and distinguish it from other
// users' messages in shared channels. The current user's principal id is their
// mention handle (currentUser.handle), which is also what the backend surfaces
// as ChatMessage.principal_id.
//
// Either id can be unknown in transient states: the optimistic placeholder a
// row is built from at send time has no principalId (the committed echo carries
// it), and rows predating the field lack it. In those cases we fall back to
// "own" so a just-sent message keeps its "You" label instead of flipping to a
// stranger's name mid-stream. A committed message from another user always
// carries a distinct principalId, so the fallback never mislabels real others.
export function isOwnUserMessage(
  msg: ChatMessageUI,
  currentPrincipalId?: string
): boolean {
  if (msg.role !== "user") return false;
  if (!msg.principalId || !currentPrincipalId) return true;
  return msg.principalId === currentPrincipalId;
}

// senderKeyForMessage returns a stable per-sender key for grouping consecutive
// messages: the UI hides the avatar/header on continuation rows, so the key
// must distinguish every sender that gets its own identity block. It keys
// users by principal_id (the sender handle) and agents by agent_id so that
// distinct senders who share a display name are not merged into one block. The
// optimistic placeholder created at send time carries no principalId, so it
// falls back to senderName (and then role) — fine, since it is always the
// current user's and dedups against the committed echo under the same id.
export function senderKeyForMessage(msg: ChatMessageUI): string {
  if (msg.senderType === SenderType.SYSTEM) return "system";
  if (msg.role === "user")
    return `u:${msg.principalId ?? msg.senderName ?? ""}`;
  return `a:${msg.agentId ?? msg.senderName ?? ""}`;
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
