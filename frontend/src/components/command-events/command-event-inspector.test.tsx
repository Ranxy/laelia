import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import { CommandEventInspector } from "./command-event-inspector";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (k: string) => k }),
}));

vi.mock("markstream-react", () => ({
  default: ({ content }: { content: string }) => <div>{content}</div>,
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

describe("CommandEventInspector", () => {
  it("shows the raw tab by default for RAW_ACP events", () => {
    render(
      <CommandEventInspector
        event={event({
          seqNo: 3,
          type: CommandEventType.RAW_ACP,
          payload: { case: "rawAcp", value: { raw: "acp-json" } },
        })}
      />
    );

    expect(screen.getByText(/"acp-json"/)).toBeInTheDocument();
  });

  it("shows the diff tab by default for DIFF_EMITTED events", () => {
    render(
      <CommandEventInspector
        event={event({
          seqNo: 4,
          type: CommandEventType.DIFF_EMITTED,
          payload: {
            case: "diffEmitted",
            value: { path: "a.ts", oldText: "old", newText: "new" },
          },
        })}
      />
    );

    expect(screen.getByText("a.ts")).toBeInTheDocument();
  });

  it("switches tabs and calls onClose", () => {
    const onClose = vi.fn();
    render(
      <CommandEventInspector
        event={event({
          seqNo: 1,
          type: CommandEventType.LIFECYCLE,
          summary: "started",
        })}
        onClose={onClose}
      />
    );

    fireEvent.click(
      screen.getAllByRole("tab").find((el) => el.textContent === "command.inspector-raw")!
    );
    expect(screen.getByText("command.event-no-payload")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "common.close" }));
    expect(onClose).toHaveBeenCalled();
  });

  it("shows summary/preview/raw tabs for an ASSISTANT output", () => {
    render(
      <CommandEventInspector
        event={
          {
            commandId: "c1",
            seqNo: 1,
            type: CommandEventType.TOOL_CALL_STARTED,
            summary: "",
            payload: { case: undefined, value: undefined },
          } as unknown as CommandEvent
        }
        output={{
          content: "**hello** world",
          startTs: 1700000001000,
          endTs: 1700000003000,
          type: 4,
        }}
      />
    );

    // Default tab is summary.
    expect(screen.getByText("command.inspector-length")).toBeInTheDocument();
    expect(screen.getByText(/chars/)).toBeInTheDocument();

    // Preview tab renders the content (markdown stubbed).
    fireEvent.click(
      screen.getAllByRole("tab").find((el) => el.textContent === "command.inspector-preview")!
    );
    expect(screen.getByText("**hello** world")).toBeInTheDocument();

    // Raw tab shows the raw content too.
    fireEvent.click(
      screen.getAllByRole("tab").find((el) => el.textContent === "command.inspector-raw")!
    );
    expect(screen.getByText("**hello** world")).toBeInTheDocument();
  });
});
