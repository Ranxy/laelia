import { describe, expect, it } from "vitest";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandOutput_StreamType,
} from "@/types/proto-es/v1/command_pb";
import {
  formatRunTimeRange,
  isToolCallError,
  mergeOutputRuns,
  tsToMs,
} from "./command-events-model";

function output(
  seqNo: number,
  type: number,
  content: string,
  seconds: number
): CommandOutput {
  return {
    commandId: "c1",
    seqNo,
    type,
    content,
    timestamp: { seconds: BigInt(seconds), nanos: 0 },
  } as unknown as CommandOutput;
}

function event(
  seqNo: number,
  type: CommandEventType,
  seconds: number
): CommandEvent {
  return {
    commandId: "c1",
    seqNo,
    type,
    summary: "",
    timestamp: { seconds: BigInt(seconds), nanos: 0 },
    payload: { case: undefined, value: undefined },
  } as unknown as CommandEvent;
}

const S = CommandOutput_StreamType;

describe("tsToMs", () => {
  it("converts seconds + nanos to epoch ms", () => {
    expect(tsToMs({ seconds: 2n, nanos: 500_000_000 })).toBe(2500);
    expect(tsToMs({ seconds: 1n })).toBe(1000);
  });

  it("collapses unset timestamps to 0", () => {
    expect(tsToMs(undefined)).toBe(0);
    expect(tsToMs({ seconds: undefined, nanos: 5 })).toBe(0);
  });
});

describe("formatRunTimeRange", () => {
  it("renders a single chunk as one clock time", () => {
    // Locale-dependent (12h/24h), so assert the shape loosely.
    expect(formatRunTimeRange(1000, 1000)).toMatch(
      /^\d{1,2}:\d{2}:\d{2}( AM| PM)?$/
    );
  });

  it("renders a merged run as start → end", () => {
    expect(formatRunTimeRange(1000, 2000)).toMatch(/→/);
  });

  it("renders nothing without a start", () => {
    expect(formatRunTimeRange(0, 1000)).toBe("");
  });
});

describe("mergeOutputRuns", () => {
  it("merges consecutive same-type chunks and keys the run by the first chunk", () => {
    const runs = mergeOutputRuns(
      [
        output(1, S.STDOUT, "hello ", 1),
        output(2, S.STDOUT, "world", 2),
        output(3, S.STDERR, "boom", 3),
      ],
      []
    );
    expect(runs).toHaveLength(2);
    expect(runs[0]).toMatchObject({
      key: "out-1",
      type: S.STDOUT,
      content: "hello world",
      startTs: 1000,
      endTs: 2000,
    });
    expect(runs[1]).toMatchObject({ key: "out-3", type: S.STDERR });
  });

  it("splits runs on a stream-type change", () => {
    const runs = mergeOutputRuns(
      [output(1, S.STDOUT, "a", 1), output(2, S.STDERR, "b", 2)],
      []
    );
    expect(runs.map((r) => r.key)).toEqual(["out-1", "out-2"]);
  });

  it("breaks a run on any row-visible event", () => {
    const runs = mergeOutputRuns(
      [output(1, S.ASSISTANT, "one ", 1), output(2, S.ASSISTANT, "two", 3)],
      [event(2, CommandEventType.TOOL_CALL_STARTED, 2)]
    );
    expect(runs.map((r) => r.key)).toEqual(["out-1", "out-2"]);
  });

  it("does not break runs on internal CONTEXT_USAGE_UPDATE / RAW_ACP frames", () => {
    const runs = mergeOutputRuns(
      [output(1, S.STDOUT, "a", 1), output(2, S.STDOUT, "b", 3)],
      [
        event(2, CommandEventType.CONTEXT_USAGE_UPDATE, 2),
        event(3, CommandEventType.RAW_ACP, 2),
      ]
    );
    expect(runs).toHaveLength(1);
    expect(runs[0]?.content).toBe("ab");
  });

  it("produces identical keys under timestamp jitter (F-B4 regression)", () => {
    // Same-type chunks with a non-monotonic timestamp: whatever the clock
    // says, all surfaces share one deterministic row set from this function.
    const outputs = [
      output(1, S.STDOUT, "a", 10),
      output(2, S.STDOUT, "b", 10),
      output(3, S.STDOUT, "c", 5),
    ];
    const runs = mergeOutputRuns(outputs, []);
    expect(runs).toHaveLength(1);
    // The ts-ordered earliest chunk anchors the run; the stable sort keeps
    // the input order as the tie-break at equal timestamps.
    expect(runs[0]?.key).toBe("out-3");
    expect(runs[0]?.content).toBe("cab");
    expect(runs[0]?.startTs).toBe(5000);
    expect(runs[0]?.endTs).toBe(10000);
  });
});

describe("isToolCallError", () => {
  it("classifies the backend spellings and accepts legacy ones", () => {
    expect(isToolCallError("error")).toBe(true);
    expect(isToolCallError("failed")).toBe(true);
    expect(isToolCallError("success")).toBe(false);
    expect(isToolCallError("completed")).toBe(false);
    expect(isToolCallError(undefined)).toBe(false);
  });
});
