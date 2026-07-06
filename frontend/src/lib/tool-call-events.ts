import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";

export interface ToolCallPair {
  started: CommandEvent;
  finished?: CommandEvent;
}

// Pairs each TOOL_CALL_STARTED event with its matching TOOL_CALL_FINISHED
// event. Tool-call payloads carry no correlation id, so we pair by event
// order: each finished event closes the oldest still-open tool call (FIFO).
// Unlike index-based pairing, this stays correct when a started event has no
// finished yet (the tool call is still in flight).
export function pairToolCallEvents(events: CommandEvent[]): ToolCallPair[] {
  const pairs: ToolCallPair[] = [];
  const pendingIndices: number[] = [];
  for (const event of events) {
    if (event.type === CommandEventType.TOOL_CALL_STARTED) {
      pendingIndices.push(pairs.length);
      pairs.push({ started: event });
    } else if (event.type === CommandEventType.TOOL_CALL_FINISHED) {
      const idx = pendingIndices.shift();
      if (idx !== undefined) pairs[idx].finished = event;
    }
  }
  return pairs;
}
