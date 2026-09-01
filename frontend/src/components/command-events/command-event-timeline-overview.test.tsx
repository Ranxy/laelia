import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CommandEventTimelineOverview } from "./command-event-timeline-overview";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

type OutputStub = Record<string, unknown>;
type EventStub = Record<string, unknown>;

function output(
  seqNo: number,
  seconds: number,
  type = 4,
  content = "a"
): OutputStub {
  return {
    commandId: "c1",
    type,
    content,
    seqNo,
    timestamp: { seconds: BigInt(seconds) },
  };
}

function event(
  seqNo: number,
  seconds: number,
  type = 2,
  extra: Record<string, unknown> = {}
): EventStub {
  return {
    commandId: "c1",
    seqNo,
    type,
    summary: "tick",
    timestamp: { seconds: BigInt(seconds) },
    ...extra,
  };
}

function outputSpans() {
  return screen
    .getAllByRole("button")
    .filter((b) => b.getAttribute("aria-label")?.startsWith("output"));
}

function trackOf(container: HTMLElement) {
  return container.querySelector('[aria-label="command.timeline-drag-hint"]')!;
}

function mockTrackRect(track: Element, width = 200) {
  vi.spyOn(track, "getBoundingClientRect").mockReturnValue({
    left: 0,
    width,
  } as DOMRect);
}

describe("CommandEventTimelineOverview", () => {
  it("merges consecutive same-type output chunks into one bar", () => {
    render(
      <CommandEventTimelineOverview
        outputs={[output(1, 1700000001), output(2, 1700000003)] as never[]}
        events={[]}
      />
    );

    // The two consecutive ASSISTANT chunks merge into a single output span.
    const spans = outputSpans();
    expect(spans).toHaveLength(1);
    // The span covers the whole run and should render wider than a point.
    expect(parseFloat(spans[0].style.width)).toBeGreaterThan(40);
  });
});

describe("CommandEventTimelineOverview filled layout", () => {
  it("fills the track with consecutive ordinal event slots", () => {
    render(
      <CommandEventTimelineOverview
        outputs={
          [
            // Different stream types stay separate runs and still receive
            // adjacent slots even when their timestamps are far apart.
            output(1, 1700000001, 1),
            output(2, 1700000002, 2),
          ] as never[]
        }
        events={[]}
      />
    );

    const spans = outputSpans();
    expect(spans).toHaveLength(2);
    expect(spans[0].style.left).toBe("0%");
    expect(parseFloat(spans[0].style.width)).toBeGreaterThan(40);
    expect(parseFloat(spans[1].style.left)).toBeGreaterThan(45);
    expect(parseFloat(spans[1].style.left)).toBeLessThan(55);
  });

  it("caps rendered spans and marks the truncated prefix", () => {
    // 600 standalone (non-merging) events → 600 spans → capped at 500.
    const events = Array.from({ length: 600 }, (_, i) =>
      event(i + 1, 1700000001 + i)
    );
    render(
      <CommandEventTimelineOverview outputs={[]} events={events as never[]} />
    );

    const spans = outputSpans();
    expect(spans).toHaveLength(500);
    // The oldest 100 spans are dropped and the loss is announced by the chip.
    expect(screen.queryByLabelText("output #1")).toBeNull();
    expect(screen.getByLabelText("output #600")).toBeInTheDocument();
    expect(screen.getByText("+100")).toBeInTheDocument();
  });
});

describe("CommandEventTimelineOverview merge respects events", () => {
  it("does not merge output runs separated by a tool event", () => {
    render(
      <CommandEventTimelineOverview
        outputs={[output(1, 1700000001), output(3, 1700000003)] as never[]}
        events={
          [
            event(2, 1700000002, 3, {
              payload: {
                case: "toolCallStarted",
                value: { title: "read_file", rawInput: {} },
              },
            }),
          ] as never[]
        }
      />
    );

    // Two ASSISTANT runs separated by a tool event must stay as two output
    // spans (plus one tool span = 3 total).
    expect(outputSpans()).toHaveLength(2);
  });
});

describe("CommandEventTimelineOverview drag selection", () => {
  const threeSpans = [
    output(1, 1700000001, 1),
    output(2, 1700000011, 2),
    output(3, 1700000021, 1),
  ] as never[];

  it("selects the spans overlapping the dragged time window", () => {
    const onSelect = vi.fn();
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        outputs={threeSpans}
        events={[]}
        onSelect={onSelect}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = trackOf(container);
    mockTrackRect(track);

    // Drag over the middle of the track: only the middle span (at ~50%)
    // overlaps the selected time window.
    fireEvent.pointerDown(track, { clientX: 20, pointerId: 1 });
    fireEvent.pointerMove(track, { clientX: 120, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 120, pointerId: 1 });

    expect(onRangeSelect).toHaveBeenCalledWith(["out-1", "out-2"]);
    expect(onSelect).toHaveBeenCalledWith("out-1");
  });

  it("keeps partially covered spans selected and undimmed", () => {
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        // One long merged run covering the whole window.
        outputs={[output(1, 1700000001), output(2, 1700000061)] as never[]}
        events={[]}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = trackOf(container);
    mockTrackRect(track);

    fireEvent.pointerDown(track, { clientX: 20, pointerId: 1 });
    fireEvent.pointerMove(track, { clientX: 120, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 120, pointerId: 1 });

    // The 10%-60% range only partially covers the single full-width span;
    // overlap (not containment) selects it and the dimming matches.
    expect(onRangeSelect).toHaveBeenCalledWith(["out-1"]);
    const span = outputSpans()[0];
    expect(span.className).not.toContain("opacity-15");
  });

  it("treats a short background press as a click that clears the selection", () => {
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        outputs={threeSpans}
        events={[]}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = trackOf(container);
    mockTrackRect(track);

    // Select the middle span first.
    fireEvent.pointerDown(track, { clientX: 100, pointerId: 1 });
    fireEvent.pointerMove(track, { clientX: 120, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 120, pointerId: 1 });
    expect(onRangeSelect).toHaveBeenLastCalledWith(["out-2"]);

    // A press+release without drag travel clears instead of re-selecting
    // whatever sits under the point.
    fireEvent.pointerDown(track, { clientX: 60, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 62, pointerId: 1 });
    expect(onRangeSelect).toHaveBeenLastCalledWith(null);
  });

  it("does not start a drag when the press lands on a span button", () => {
    const onSelect = vi.fn();
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        outputs={threeSpans}
        events={[]}
        onSelect={onSelect}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = trackOf(container);
    mockTrackRect(track);
    const middle = screen.getByLabelText("output #2");

    // Press the span itself, drag across the track: the pointer sequence is
    // ignored (the button owns the interaction), and only the click selects.
    fireEvent.pointerDown(middle, { clientX: 100, pointerId: 1 });
    fireEvent.pointerMove(track, { clientX: 150, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 150, pointerId: 1 });
    expect(onRangeSelect).not.toHaveBeenCalled();

    fireEvent.click(middle);
    expect(onSelect).toHaveBeenCalledWith("out-2");
    expect(onRangeSelect).toHaveBeenCalledWith(null);
  });
});

describe("CommandEventTimelineOverview right-click clears", () => {
  it("clears the range selection on right-click", () => {
    const onRangeSelect = vi.fn();
    const { container } = render(
      <CommandEventTimelineOverview
        outputs={[output(1, 1700000001, 1)] as never[]}
        events={[]}
        onRangeSelect={onRangeSelect}
      />
    );

    const track = trackOf(container);
    mockTrackRect(track);

    // Drag a selection first.
    fireEvent.pointerDown(track, { clientX: 20, pointerId: 1 });
    fireEvent.pointerUp(track, { clientX: 120, pointerId: 1 });
    expect(onRangeSelect).toHaveBeenCalledWith(["out-1"]);

    // Right-click clears the selection.
    fireEvent.contextMenu(track);
    expect(onRangeSelect).toHaveBeenLastCalledWith(null);
  });
});
