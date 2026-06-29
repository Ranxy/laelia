import { describe, expect, it } from "vitest";
import {
  type CommandEvent,
  CommandEventType,
} from "@/types/proto-es/v1/command_pb";
import { pairToolCallEvents } from "./chat";

// Minimal event stubs: pairing only reads `.type`, so we build typed shells
// instead of fully-populated proto messages.
function started(seqNo: number): CommandEvent {
  return {
    type: CommandEventType.TOOL_CALL_STARTED,
    seqNo,
  } as unknown as CommandEvent;
}
function finished(seqNo: number): CommandEvent {
  return {
    type: CommandEventType.TOOL_CALL_FINISHED,
    seqNo,
  } as unknown as CommandEvent;
}

describe("pairToolCallEvents", () => {
  it("TestPairToolCallEvents_MatchesByCorrelationId: pairs by event order, not array index", () => {
    // Sequential tool calls pair start-to-finish in order.
    const sequential = pairToolCallEvents([
      started(1),
      finished(2),
      started(3),
      finished(4),
    ]);
    expect(sequential).toEqual([
      { started: started(1), finished: finished(2) },
      { started: started(3), finished: finished(4) },
    ]);

    // An orphan finished event (no matching started) is dropped instead of
    // stealing the next call's finished event — the old index-based pairing
    // would have mis-paired s1 with the orphan.
    const orphanFinished = pairToolCallEvents([
      finished(0),
      started(1),
      finished(2),
    ]);
    expect(orphanFinished).toEqual([
      { started: started(1), finished: finished(2) },
    ]);

    // A started event with no finished yet (tool call still in flight) renders
    // as an open tool call instead of consuming the following call's finished.
    const inFlight = pairToolCallEvents([started(1), finished(2), started(3)]);
    expect(inFlight).toEqual([
      { started: started(1), finished: finished(2) },
      { started: started(3), finished: undefined },
    ]);
  });
});
