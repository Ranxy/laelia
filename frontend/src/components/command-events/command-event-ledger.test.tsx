import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import { CommandEventLedger } from "./command-event-ledger";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

function event(
  overrides: Partial<Omit<CommandEvent, "payload">> & {
    type: CommandEventType;
    payload?: unknown;
  }
): CommandEvent {
  return {
    commandId: "c1",
    seqNo: 1,
    summary: "",
    timestamp: { seconds: 1700000000n },
    payload: { case: undefined, value: undefined },
    ...(overrides as object),
  } as unknown as CommandEvent;
}

describe("CommandEventLedger", () => {
  it("renders kind tags, content and time for visible events", () => {
    render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 1,
            type: CommandEventType.LIFECYCLE,
            summary: "started",
          }),
          event({
            seqNo: 2,
            type: CommandEventType.WARNING,
            payload: { case: "warning", value: { message: "careful" } },
          }),
        ]}
      />
    );

    expect(screen.getByText("command.event-lifecycle")).toBeInTheDocument();
    expect(screen.getByText("started")).toBeInTheDocument();
    expect(screen.getByText("command.event-warning")).toBeInTheDocument();
    expect(screen.getByText("careful")).toBeInTheDocument();
  });

  it("merges paired tool calls into a single row", () => {
    render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 1,
            type: CommandEventType.TOOL_CALL_STARTED,
            payload: {
              case: "toolCallStarted",
              value: { title: "read_file", rawInput: { path: "a.txt" } },
            },
          }),
          event({
            seqNo: 2,
            type: CommandEventType.TOOL_CALL_FINISHED,
            payload: {
              case: "toolCallFinished",
              value: { status: "completed", rawOutput: { ok: true } },
            },
          }),
        ]}
      />
    );

    // The paired finished event is not rendered as its own row.
    expect(screen.getAllByText("read_file").length).toBeGreaterThan(0);
    expect(screen.queryByText("command.event-tool-finished")).not.toBeInTheDocument();
  });

  it("filters by search query", () => {
    render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 1,
            type: CommandEventType.LIFECYCLE,
            summary: "alpha",
          }),
          event({
            seqNo: 2,
            type: CommandEventType.WARNING,
            payload: { case: "warning", value: { message: "beta" } },
          }),
        ]}
        searchQuery="beta"
      />
    );

    expect(screen.queryByText("alpha")).not.toBeInTheDocument();
    expect(screen.getByText("beta")).toBeInTheDocument();
  });

  it("filters by event type filter", () => {
    render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 1,
            type: CommandEventType.LIFECYCLE,
            summary: "started",
          }),
          event({
            seqNo: 2,
            type: CommandEventType.WARNING,
            payload: { case: "warning", value: { message: "careful" } },
          }),
        ]}
        filter="warnings"
      />
    );

    expect(screen.queryByText("started")).not.toBeInTheDocument();
    expect(screen.getByText("careful")).toBeInTheDocument();
  });

  it("calls onSelect when a row is clicked", () => {
    const onSelect = vi.fn();
    render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 7,
            type: CommandEventType.LIFECYCLE,
            summary: "started",
          }),
        ]}
        onSelect={onSelect}
      />
    );

    fireEvent.click(screen.getByText("started"));
    expect(onSelect).toHaveBeenCalledWith("ev-7");
  });

});

describe("CommandEventLedger output merging", () => {
  it("renders output chunks as rows with stream kind tags", () => {
    render(
      <CommandEventLedger
        outputs={[
          {
            commandId: "c1",
            type: 1,
            content: "hello world",
            seqNo: 1,
            timestamp: { seconds: 1700000000n },
          } as never,
        ]}
        events={[]}
      />
    );

    expect(screen.getByText("command.stream-stdout")).toBeInTheDocument();
    expect(screen.getByText("hello world")).toBeInTheDocument();
  });

  it("interleaves output and tool events by timestamp", () => {
    render(
      <CommandEventLedger
        outputs={[
          {
            commandId: "c1",
            type: 1,
            content: "first",
            seqNo: 1,
            timestamp: { seconds: 1700000001n },
          } as never,
        ]}
        events={[
          event({
            seqNo: 2,
            type: CommandEventType.TOOL_CALL_STARTED,
            payload: {
              case: "toolCallStarted",
              value: { title: "read_file", rawInput: { path: "a.txt" } },
            },
          }),
        ]}
      />
    );

    expect(screen.getByText("first")).toBeInTheDocument();
    expect(screen.getByText("read_file")).toBeInTheDocument();
  });
});

describe("CommandEventLedger output merging", () => {
  it("merges consecutive same-type output chunks into one row", () => {
    render(
      <CommandEventLedger
        outputs={[
          {
            commandId: "c1",
            type: 1,
            content: "hello ",
            seqNo: 1,
            timestamp: { seconds: 1700000000n },
          } as never,
          {
            commandId: "c1",
            type: 1,
            content: "world",
            seqNo: 2,
            timestamp: { seconds: 1700000001n },
          } as never,
          {
            commandId: "c1",
            type: 2,
            content: "boom",
            seqNo: 3,
            timestamp: { seconds: 1700000002n } as never,
          } as never,
        ]}
        events={[]}
      />
    );

    // The two STDOUT chunks merge into one "hello world" row; the STDERR
    // chunk stays separate.
    expect(screen.getAllByText("command.stream-stdout")).toHaveLength(1);
    expect(screen.getByText("hello world")).toBeInTheDocument();
    expect(screen.getByText("boom")).toBeInTheDocument();
    expect(screen.getAllByText("command.stream-stderr")).toHaveLength(1);
  });
});

describe("CommandEventLedger merge-before-filter", () => {
  it("does not merge separate output runs when filtering by output", () => {
    render(
      <CommandEventLedger
        outputs={[
          {
            commandId: "c1",
            type: 4,
            content: "first thinking ",
            seqNo: 1,
            timestamp: { seconds: 1700000000n },
          } as never,
          {
            commandId: "c1",
            type: 4,
            content: "continued",
            seqNo: 2,
            timestamp: { seconds: 1700000001n },
          } as never,
          {
            commandId: "c1",
            type: 4,
            content: "second thinking",
            seqNo: 4,
            timestamp: { seconds: 1700000003n },
          } as never,
        ]}
        events={[
          event({
            seqNo: 3,
            type: CommandEventType.TOOL_CALL_STARTED,
            timestamp: { seconds: 1700000002n } as never,
            payload: {
              case: "toolCallStarted",
              value: { title: "read_file", rawInput: { path: "a.txt" } },
            },
          }),
        ]}
        filter="output"
      />
    );

    // Two separate ASSISTANT runs (broken by the tool event) must stay as two
    // rows even though the tool event is filtered out.
    expect(screen.getByText(/first thinking\s*continued/)).toBeInTheDocument();
    expect(screen.getByText(/second thinking/)).toBeInTheDocument();
    expect(screen.getAllByText("command.stream-assistant")).toHaveLength(2);
  });
});

describe("CommandEventLedger output time range", () => {
  it("shows a start → end range for merged output rows", () => {
    const start = new Date("2024-01-01T12:00:01").getTime() / 1000;
    const end = new Date("2024-01-01T12:00:03").getTime() / 1000;
    render(
      <CommandEventLedger
        outputs={[
          {
            commandId: "c1",
            type: 4,
            content: "a",
            seqNo: 1,
            timestamp: { seconds: BigInt(Math.floor(start)) },
          } as never,
          {
            commandId: "c1",
            type: 4,
            content: "b",
            seqNo: 2,
            timestamp: { seconds: BigInt(Math.floor(end)) },
          } as never,
        ]}
        events={[]}
      />
    );

    // The merged row's time cell should contain an arrow between start and end.
    expect(screen.getByText(/→/)).toBeInTheDocument();
  });
});

describe("CommandEventLedger scrollToKey", () => {
  it("scrolls the matching row into view", () => {
    // jsdom does not implement Element.scrollTo; install a mock first.
    const scrollTo = vi.fn();
    Object.defineProperty(Element.prototype, "scrollTo", {
      configurable: true,
      writable: true,
      value: scrollTo,
    });
    render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 7,
            type: CommandEventType.LIFECYCLE,
            summary: "started",
          }),
        ]}
        scrollToKey="ev-7"
      />
    );

    expect(scrollTo).toHaveBeenCalled();
    delete (Element.prototype as { scrollTo?: unknown }).scrollTo;
  });
});

describe("CommandEventLedger range dimming", () => {
  it("dims rows outside the timeline range selection", () => {
    const { container } = render(
      <CommandEventLedger
        outputs={[]}
        events={[
          event({
            seqNo: 1,
            type: CommandEventType.LIFECYCLE,
            summary: "in range",
          }),
          event({
            seqNo: 2,
            type: CommandEventType.WARNING,
            payload: { case: "warning", value: { message: "out of range" } },
          }),
        ]}
        rangeKeys={["ev-1"]}
      />
    );

    const inRow = container.querySelector('[data-row-key="ev-1"]');
    const outRow = container.querySelector('[data-row-key="ev-2"]');
    expect(inRow?.className).not.toContain("opacity-30");
    expect(outRow?.className).toContain("opacity-30");
  });
});
