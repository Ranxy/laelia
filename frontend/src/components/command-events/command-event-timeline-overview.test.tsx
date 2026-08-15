import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CommandEventTimelineOverview } from "./command-event-timeline-overview";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

describe("CommandEventTimelineOverview", () => {
  it("merges consecutive same-type output chunks into one bar", () => {
    render(
      <CommandEventTimelineOverview
        outputs={[
          {
            commandId: "c1",
            type: 4,
            content: "a",
            seqNo: 1,
            timestamp: { seconds: 1700000001n },
          } as never,
          {
            commandId: "c1",
            type: 4,
            content: "b",
            seqNo: 2,
            timestamp: { seconds: 1700000003n },
          } as never,
        ]}
        events={[]}
      />
    );

    // The two consecutive ASSISTANT chunks merge into a single output span.
    const outputSpans = screen
      .getAllByRole("button")
      .filter((b) => b.getAttribute("aria-label")?.startsWith("output"));
    expect(outputSpans).toHaveLength(1);
    // The span covers 2 seconds and should render wider than a 1ms point.
    expect(outputSpans[0].style.width).not.toBe("0.5px");
  });
});

describe("CommandEventTimelineOverview equal width", () => {
  it("distributes spans evenly instead of stretching by real time", () => {
    render(
      <CommandEventTimelineOverview
        outputs={[
          {
            commandId: "c1",
            type: 1,
            content: "a",
            seqNo: 1,
            timestamp: { seconds: 1700000001n },
          } as never,
          {
            commandId: "c1",
            type: 2,
            content: "b",
            seqNo: 2,
            timestamp: { seconds: 1700000002n },
          } as never,
        ]}
        events={[]}
      />
    );

    const spans = screen
      .getAllByRole("button")
      .filter(
        (b) => b.getAttribute("aria-label") !== "command.timeline-drag-hint"
      );
    expect(spans).toHaveLength(2);
    // Each span should be roughly half the track (equal width), not a tiny
    // point stretched by real time.
    const widths = spans.map((s) => parseFloat(s.style.width));
    expect(widths[0]).toBeGreaterThan(40);
    expect(widths[1]).toBeGreaterThan(40);
  });
});

describe("CommandEventTimelineOverview merge respects events", () => {
  it("does not merge output runs separated by a tool event", () => {
    render(
      <CommandEventTimelineOverview
        outputs={[
          {
            commandId: "c1",
            type: 4,
            content: "first",
            seqNo: 1,
            timestamp: { seconds: 1700000001n },
          } as never,
          {
            commandId: "c1",
            type: 4,
            content: "second",
            seqNo: 3,
            timestamp: { seconds: 1700000003n },
          } as never,
        ]}
        events={[
          {
            commandId: "c1",
            seqNo: 2,
            type: 3,
            summary: "read_file",
            timestamp: { seconds: 1700000002n },
            payload: {
              case: "toolCallStarted",
              value: { title: "read_file", rawInput: {} },
            },
          } as never,
        ]}
      />
    );

    // Two ASSISTANT runs separated by a tool event must stay as two output
    // spans (plus one tool span = 3 total).
    const outputSpans = screen
      .getAllByRole("button")
      .filter((b) => b.getAttribute("aria-label")?.startsWith("output"));
    expect(outputSpans).toHaveLength(2);
  });
});

describe("CommandEventTimelineOverview drag selection", () => {
  it("selects a range and scrolls to the first span in it", () => {
    const onSelect = vi.fn();
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        outputs={[
          {
            commandId: "c1",
            type: 1,
            content: "a",
            seqNo: 1,
            timestamp: { seconds: 1700000001n },
          } as never,
          {
            commandId: "c1",
            type: 2,
            content: "b",
            seqNo: 2,
            timestamp: { seconds: 1700000002n },
          } as never,
        ]}
        events={[]}
        onSelect={onSelect}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = container.querySelector(
      '[aria-label="command.timeline-drag-hint"]'
    )!;
    const rect = { left: 0, width: 200 } as DOMRect;
    vi.spyOn(track, "getBoundingClientRect").mockReturnValue(rect);

    // Drag from 10% to 60% of the track.
    fireEvent.pointerDown(track, { clientX: 20, pointerId: 1 });
    fireEvent.pointerMove(track, { clientX: 120, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 120, pointerId: 1 });

    // The first span overlapping the range start is the first output ("out-1").
    expect(onSelect).toHaveBeenCalledWith("out-1");
    // Both spans are inside the 10%–60% range (both cover the selection).
    expect(onRangeSelect).toHaveBeenCalledWith(["out-1", "out-2"]);
  });
});

describe("CommandEventTimelineOverview right-click clears", () => {
  it("clears the range selection on right-click", () => {
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        outputs={[
          {
            commandId: "c1",
            type: 1,
            content: "a",
            seqNo: 1,
            timestamp: { seconds: 1700000001n },
          } as never,
        ]}
        events={[]}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = container.querySelector(
      '[aria-label="command.timeline-drag-hint"]'
    )!;
    const rect = { left: 0, width: 200 } as DOMRect;
    vi.spyOn(track, "getBoundingClientRect").mockReturnValue(rect);

    // Drag a selection first.
    fireEvent.pointerDown(track, { clientX: 20, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 120, pointerId: 1 });
    expect(onRangeSelect).toHaveBeenCalledWith(["out-1"]);

    // Right-click clears the selection.
    fireEvent.contextMenu(track);
    expect(onRangeSelect).toHaveBeenLastCalledWith(null);
  });
});
