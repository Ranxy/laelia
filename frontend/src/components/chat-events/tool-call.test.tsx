import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import { ChatToolCall } from "./tool-call";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

function toolEvent(
  type: CommandEventType,
  payload: Record<string, unknown>
): CommandEvent {
  return {
    commandId: "c1",
    seqNo: 1,
    summary: "",
    timestamp: { seconds: 1700000000n },
    payload: { case: undefined, value: undefined },
    ...(type === CommandEventType.TOOL_CALL_STARTED
      ? { type, payload: { case: "toolCallStarted", value: payload } }
      : { type, payload: { case: "toolCallFinished", value: payload } }),
  } as unknown as CommandEvent;
}

function renderToolCall(started?: CommandEvent, finished?: CommandEvent): void {
  render(<ChatToolCall startedEvent={started} finishedEvent={finished} />);
}

describe("ChatToolCall status badge (08 F-R3)", () => {
  it("renders the error label for a failed tool call", () => {
    const started = toolEvent(CommandEventType.TOOL_CALL_STARTED, {
      title: "run_tests",
    });
    const finished = toolEvent(CommandEventType.TOOL_CALL_FINISHED, {
      status: "error",
      rawOutput: { ok: false },
    });
    renderToolCall(started, finished);
    expect(screen.getByText("chat.tool-error")).toBeInTheDocument();
    expect(screen.queryByText("chat.tool-finished")).not.toBeInTheDocument();
  });

  it("accepts the legacy 'failed' spelling as an error too", () => {
    const finished = toolEvent(CommandEventType.TOOL_CALL_FINISHED, {
      status: "failed",
    });
    renderToolCall(undefined, finished);
    expect(screen.getByText("chat.tool-error")).toBeInTheDocument();
  });

  it("renders the finished label for a successful tool call", () => {
    const finished = toolEvent(CommandEventType.TOOL_CALL_FINISHED, {
      status: "success",
    });
    renderToolCall(undefined, finished);
    expect(screen.getByText("chat.tool-finished")).toBeInTheDocument();
  });

  it("renders the started badge while the tool call is in flight", () => {
    const started = toolEvent(CommandEventType.TOOL_CALL_STARTED, {
      title: "run_tests",
    });
    renderToolCall(started, undefined);
    expect(screen.getByText("chat.tool-started")).toBeInTheDocument();
  });
});
