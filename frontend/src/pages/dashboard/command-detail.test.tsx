import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Command, CommandEvent } from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandStatus,
} from "@/types/proto-es/v1/command_pb";
import { CommandDetailPage } from "./command-detail";

// --- mock @/stores so the page's command RPCs are controllable ---
const mock = vi.hoisted(() => ({
  getCommand: vi.fn(),
  cancelCommand: vi.fn(),
  steerCommand: vi.fn(),
  watchCommand: vi.fn(),
  watchCommandEvents: vi.fn(),
  activeOutputs: {} as Record<string, unknown[]>,
  activeEvents: {} as Record<string, CommandEvent[]>,
}));

vi.mock("@/stores", () => {
  const state = {
    getCommand: mock.getCommand,
    cancelCommand: mock.cancelCommand,
    steerCommand: mock.steerCommand,
    watchCommand: mock.watchCommand,
    watchCommandEvents: mock.watchCommandEvents,
    get activeOutputs() {
      return mock.activeOutputs;
    },
    get activeEvents() {
      return mock.activeEvents;
    },
  };
  const useAppStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  useAppStore.getState = () => state;
  return { useAppStore };
});

const tFn = (key: string) => key;
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: tFn }),
}));

// markstream splits text across nested spans, which defeats getByText; stub
// the summary renderer so assertions read the content directly.
vi.mock("@/components/command-terminal", () => ({
  FinalSummary: ({ content }: { content: string }) => (
    <div data-testid="final-summary">{content}</div>
  ),
}));

const NAME = "agents/a/commands/c1";

function command(overrides: Partial<Command> = {}): Command {
  return {
    name: NAME,
    agent: "agents/a",
    principalId: "users/1",
    principalName: "Alice",
    command: "echo hi",
    status: CommandStatus.COMPLETED,
    exitCode: 0,
    durationMs: 1500n,
    createdAt: { seconds: 1700000000n },
    startedAt: undefined,
    completedAt: undefined,
    errorMessage: "",
    env: {},
    workingDir: "",
    instruction: "Do the thing",
    profile: "",
    finalSummary: "All done",
    result: undefined,
    allowDiff: false,
    conversationId: "",
    ...overrides,
  } as unknown as Command;
}

// The payload union requires $typeName on every message; keep the factory
// loose and cast once so tests can pass plain payload shapes.
type EventOverrides = {
  seqNo?: number;
  type: CommandEventType;
  summary?: string;
  timestamp?: { seconds?: bigint; nanos?: number };
  payload?: unknown;
};

function event(overrides: EventOverrides): CommandEvent {
  return {
    commandId: "c1",
    seqNo: 1,
    summary: "",
    timestamp: { seconds: 1700000000n },
    payload: { case: undefined, value: undefined },
    ...overrides,
  } as unknown as CommandEvent;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/members/agents/a/commands/c1"]}>
      <Routes>
        <Route
          path="/members/agents/:agentId/commands/:commandId"
          element={<CommandDetailPage />}
        />
        <Route
          path="/members/agents/:agentId/commands"
          element={<div data-testid="command-list" />}
        />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  mock.getCommand.mockReset();
  mock.cancelCommand.mockReset();
  mock.steerCommand.mockReset();
  mock.watchCommand.mockReset();
  mock.watchCommandEvents.mockReset();
  mock.activeOutputs = {};
  mock.activeEvents = {};
  mock.watchCommand.mockResolvedValue(false);
  mock.watchCommandEvents.mockResolvedValue(false);
});

describe("command-detail", () => {
  it("renders the header, meta and summary tab for a completed command", async () => {
    mock.getCommand.mockResolvedValue(
      command({
        status: CommandStatus.COMPLETED,
        finalSummary: "All done",
        principalName: "Alice",
        durationMs: 1500n,
      })
    );

    renderPage();

    expect(
      await screen.findByText("Do the thing", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(screen.getByText("command.status-completed")).toBeInTheDocument();
    expect(screen.getByText(/command\.duration/)).toHaveTextContent("1.5s");
    expect(screen.getByText(/command\.sent-by/)).toHaveTextContent("Alice");
    // Terminal commands default to the summary tab.
    expect(
      await screen.findByTestId("final-summary", {}, { timeout: 3000 })
    ).toHaveTextContent("All done");
  });

  it("shows the error message and exit code for a failed command", async () => {
    mock.getCommand.mockResolvedValue(
      command({
        status: CommandStatus.FAILED,
        errorMessage: "boom happened",
        exitCode: 1,
      })
    );

    renderPage();

    expect(
      await screen.findByText("boom happened", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(screen.getByText(/command\.exit-code/)).toHaveTextContent("1");
  });

  it("shows the no-final-summary placeholder when the summary is empty", async () => {
    mock.getCommand.mockResolvedValue(
      command({ status: CommandStatus.COMPLETED, finalSummary: "" })
    );

    renderPage();

    expect(
      await screen.findByText("command.no-final-summary", {}, { timeout: 3000 })
    ).toBeInTheDocument();
  });

  it("steers and cancels a running command", async () => {
    mock.getCommand.mockResolvedValue(
      command({ status: CommandStatus.RUNNING })
    );
    mock.steerCommand.mockResolvedValue(
      command({ status: CommandStatus.RUNNING })
    );
    mock.cancelCommand.mockResolvedValue(
      command({ status: CommandStatus.CANCELLED })
    );

    renderPage();

    const input = await screen.findByPlaceholderText(
      "command.steer-placeholder",
      {},
      { timeout: 3000 }
    );
    fireEvent.change(input, { target: { value: "keep going" } });
    fireEvent.click(screen.getByRole("button", { name: "command.steer" }));
    await waitFor(
      () => expect(mock.steerCommand).toHaveBeenCalledWith(NAME, "keep going"),
      { timeout: 3000 }
    );
    await waitFor(() => expect(input).toHaveValue(""), { timeout: 3000 });

    // Enter in the input also steers.
    fireEvent.change(input, { target: { value: "again" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(
      () => expect(mock.steerCommand).toHaveBeenCalledWith(NAME, "again"),
      { timeout: 3000 }
    );

    fireEvent.click(screen.getByRole("button", { name: "common.cancel" }));
    await waitFor(() => expect(mock.cancelCommand).toHaveBeenCalledWith(NAME), {
      timeout: 3000,
    });
  });

  it("renders event rows, expandable raw payloads and tool call cards", async () => {
    mock.getCommand.mockResolvedValue(
      command({ status: CommandStatus.RUNNING })
    );
    mock.activeEvents = {
      [NAME]: [
        event({
          seqNo: 1,
          type: CommandEventType.LIFECYCLE,
          summary: "started",
        }),
        event({
          seqNo: 2,
          type: CommandEventType.RAW_ACP,
          payload: { case: "rawAcp", value: { raw: "acp-json" } },
        }),
        event({
          seqNo: 3,
          type: CommandEventType.TOOL_CALL_STARTED,
          payload: {
            case: "toolCallStarted",
            value: { title: "read_file", rawInput: { path: "a.txt" } },
          },
        }),
        event({
          seqNo: 4,
          type: CommandEventType.TOOL_CALL_FINISHED,
          payload: {
            case: "toolCallFinished",
            value: { status: "completed", rawOutput: { ok: true } },
          },
        }),
        event({
          seqNo: 5,
          type: CommandEventType.CONTEXT_USAGE_UPDATE,
          payload: {
            case: "contextUsage",
            value: { size: 100n, used: 50n, usageRatio: 0.5 },
          },
        }),
        event({
          seqNo: 6,
          type: CommandEventType.TEXT_DELTA,
          payload: {
            case: "textDelta",
            value: { streamType: "stdout", content: "x" },
          },
        }),
      ],
    };

    renderPage();

    expect(
      await screen.findByText("command.event-lifecycle", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    // TEXT_DELTA events are filtered out of the panel.
    expect(screen.queryByText("command.event-text")).not.toBeInTheDocument();
    // Paired tool call renders as a card (timeline + events panel both show it).
    expect(screen.getAllByText("read_file").length).toBeGreaterThan(0);
    // Context usage renders as the live progress bar.
    expect(screen.getByText("command.event-context-usage")).toBeInTheDocument();

    // Expand the RAW_ACP payload.
    fireEvent.click(screen.getByRole("button", { name: "command.details" }));
    expect(
      await screen.findByText(/"acp-json"/, {}, { timeout: 3000 })
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "command.collapse" }));
    await waitFor(
      () => expect(screen.queryByText(/"acp-json"/)).not.toBeInTheDocument(),
      { timeout: 3000 }
    );
  });

  it("renders the token usage card on the summary tab", async () => {
    mock.getCommand.mockResolvedValue(
      command({ status: CommandStatus.COMPLETED, finalSummary: "Done" })
    );
    mock.activeEvents = {
      [NAME]: [
        event({
          seqNo: 1,
          type: CommandEventType.TOKEN_USAGE,
          payload: {
            case: "tokenUsage",
            value: {
              inputTokens: 10n,
              outputTokens: 5n,
              cacheReadTokens: 2n,
              cacheWriteTokens: 1n,
              totalTokens: 18n,
            },
          },
        }),
      ],
    };

    renderPage();

    expect(
      await screen.findByText("command.token-usage", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(screen.getByText(/command\.token-total/)).toHaveTextContent("18");
  });

  it("surfaces the final summary event optimistically", async () => {
    mock.getCommand.mockResolvedValue(
      command({ status: CommandStatus.RUNNING, finalSummary: "" })
    );
    mock.activeEvents = {
      [NAME]: [
        event({
          seqNo: 1,
          type: CommandEventType.FINAL_SUMMARY,
          summary: "Event summary",
        }),
      ],
    };

    renderPage();

    // The FINAL_SUMMARY event flips the running command to completed and
    // surfaces the summary text in the summary tab.
    expect(
      await screen.findByText("command.status-completed", {}, { timeout: 3000 })
    ).toBeInTheDocument();
    expect(
      await screen.findByTestId("final-summary", {}, { timeout: 3000 })
    ).toHaveTextContent("Event summary");
  });

  it("shows the completion hint when the command finishes and switches on confirm", async () => {
    mock.getCommand
      .mockResolvedValueOnce(command({ status: CommandStatus.RUNNING }))
      .mockImplementation(
        () =>
          new Promise((resolve) =>
            setTimeout(
              () =>
                resolve(
                  command({
                    status: CommandStatus.COMPLETED,
                    finalSummary: "Done!",
                  })
                ),
              50
            )
          )
      );
    // The watch stream closes after the initial render, triggering the reload
    // that transitions the command to completed.
    mock.watchCommand.mockImplementation(
      () => new Promise((resolve) => setTimeout(() => resolve(true), 20))
    );
    mock.watchCommandEvents.mockResolvedValue(false);

    renderPage();

    expect(
      await screen.findByPlaceholderText(
        "command.steer-placeholder",
        {},
        { timeout: 3000 }
      )
    ).toBeInTheDocument();
    // The watch stream close triggers a reload; the running -> completed
    // transition shows the one-shot hint instead of yanking the tab.
    expect(
      await screen.findByText("command.completed-hint", {}, { timeout: 3000 })
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", { name: "command.view-final-summary" })
    );
    expect(
      await screen.findByTestId("final-summary", {}, { timeout: 3000 })
    ).toHaveTextContent("Done!");
    expect(
      screen.queryByText("command.completed-hint")
    ).not.toBeInTheDocument();
  });

  it("navigates back to the command list", async () => {
    mock.getCommand.mockResolvedValue(
      command({ status: CommandStatus.COMPLETED })
    );

    renderPage();

    const back = await screen.findByRole(
      "button",
      { name: /command\.back/ },
      { timeout: 3000 }
    );
    fireEvent.click(back);
    expect(
      await screen.findByTestId("command-list", {}, { timeout: 3000 })
    ).toBeInTheDocument();
  });
});
