import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";

export interface ToolCallPair {
  started: CommandEvent;
  finished?: CommandEvent;
}

// Pairs each TOOL_CALL_STARTED event with its matching TOOL_CALL_FINISHED
// event. Tool calls that report a runtime id are paired by that id, which
// stays correct when concurrent tool calls interleave their finished events —
// the id-less FIFO guess below would swap their results. Events without an id
// (runtimes that never reported one, or events persisted before the field
// existed) fall back to pairing by event order: each finished event closes the
// oldest still-open tool call (FIFO) — the same fallback applies when a
// finished id matches no open start (e.g. its start was lost in a disconnect
// gap). Unlike index-based pairing, this stays correct when a started event
// has no finished yet (the tool call is still in flight).
export function pairToolCallEvents(events: CommandEvent[]): ToolCallPair[] {
  const pairs: ToolCallPair[] = [];
  const pendingIndices: number[] = [];
  const pendingIndexById = new Map<string, number>();
  for (const event of events) {
    if (event.type === CommandEventType.TOOL_CALL_STARTED) {
      pairs.push({ started: event });
      const index = pairs.length - 1;
      pendingIndices.push(index);
      const id =
        event.payload.case === "toolCallStarted"
          ? event.payload.value.toolCallId
          : undefined;
      if (id) pendingIndexById.set(id, index);
    } else if (event.type === CommandEventType.TOOL_CALL_FINISHED) {
      const id =
        event.payload.case === "toolCallFinished"
          ? event.payload.value.toolCallId
          : undefined;
      let index: number | undefined;
      if (id !== undefined) {
        const matched = pendingIndexById.get(id);
        if (matched !== undefined) {
          pendingIndexById.delete(id);
          pendingIndices.splice(pendingIndices.indexOf(matched), 1);
          index = matched;
        }
      }
      if (index === undefined) {
        index = pendingIndices.shift();
        if (index !== undefined) {
          // The fallback still closes a call whose start registered an id;
          // retire that entry so its own close cannot re-pair it later.
          const closedStart = pairs[index].started;
          const closedId =
            closedStart.payload.case === "toolCallStarted"
              ? closedStart.payload.value.toolCallId
              : undefined;
          if (closedId) pendingIndexById.delete(closedId);
        }
      }
      if (index !== undefined) pairs[index].finished = event;
    }
  }
  return pairs;
}
