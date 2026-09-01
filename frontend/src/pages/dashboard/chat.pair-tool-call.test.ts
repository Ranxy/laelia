import { describe, expect, it } from "vitest";
import { pairToolCallEvents } from "@/lib/tool-call-events";
import {
  type CommandEvent,
  CommandEventType,
} from "@/types/proto-es/v1/command_pb";

// Minimal event stubs: pairing only reads `.type` and the payload's
// toolCallId, so we build typed shells instead of fully-populated proto
// messages. A built shell without a payload mirrors proto-es's unset oneof
// ({ case: undefined }).
function started(seqNo: number): CommandEvent {
  return {
    type: CommandEventType.TOOL_CALL_STARTED,
    seqNo,
    payload: { case: undefined, value: undefined },
  } as unknown as CommandEvent;
}
function finished(seqNo: number): CommandEvent {
  return {
    type: CommandEventType.TOOL_CALL_FINISHED,
    seqNo,
    payload: { case: undefined, value: undefined },
  } as unknown as CommandEvent;
}
function startedWithId(seqNo: number, id: string): CommandEvent {
  return {
    type: CommandEventType.TOOL_CALL_STARTED,
    seqNo,
    payload: { case: "toolCallStarted", value: { toolCallId: id } },
  } as unknown as CommandEvent;
}
function finishedWithId(seqNo: number, id: string): CommandEvent {
  return {
    type: CommandEventType.TOOL_CALL_FINISHED,
    seqNo,
    payload: { case: "toolCallFinished", value: { toolCallId: id } },
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

  it("pairs concurrent tool calls by tool_call_id", () => {
    // The regression case: two concurrent tool calls interleave, and the
    // finished events arrive in the opposite order to their starts. Matching
    // by id keeps A→A; the old FIFO pairing swapped their results.
    const interleaved = pairToolCallEvents([
      startedWithId(1, "A"),
      startedWithId(2, "B"),
      finishedWithId(3, "B"),
      finishedWithId(4, "A"),
    ]);
    expect(interleaved).toEqual([
      { started: startedWithId(1, "A"), finished: finishedWithId(4, "A") },
      { started: startedWithId(2, "B"), finished: finishedWithId(3, "B") },
    ]);

    // A finished whose id matches no open started (its start was lost in a
    // disconnect gap) falls back to closing the oldest open call instead of
    // being dropped, and the call that owns its id still gets its own close.
    const lostStart = pairToolCallEvents([
      startedWithId(1, "B"),
      finishedWithId(2, "A"),
      finishedWithId(3, "B"),
    ]);
    expect(lostStart).toEqual([
      { started: startedWithId(1, "B"), finished: finishedWithId(2, "A") },
    ]);

    // Id-less events (legacy) interleave with id-bearing ones: the id-bearing
    // close finds its own start; the id-less finished closes the oldest open
    // call. Pair order still follows the started-event order.
    const mixed = pairToolCallEvents([
      started(1),
      startedWithId(2, "B"),
      finishedWithId(3, "B"),
      finished(4),
    ]);
    expect(mixed).toEqual([
      { started: started(1), finished: finished(4) },
      { started: startedWithId(2, "B"), finished: finishedWithId(3, "B") },
    ]);
  });
});
