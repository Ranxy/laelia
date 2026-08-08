import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ChatToolCall } from "@/components/chat-events/tool-call";
import { CommandStatusBadge } from "@/components/command-status-badge";
import { FinalSummary } from "@/components/command-terminal";
import { CommandTimeline } from "@/components/command-timeline";
import { ContextUsageBar } from "@/components/context-usage-bar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsPanel, TabsTrigger } from "@/components/ui/tabs";
import {
  commandEventTypeToI18nKey,
  formatDuration,
  formatTimeOfDay,
  formatTimestamp,
} from "@/lib/command-status";
import { pairToolCallEvents, type ToolCallPair } from "@/lib/tool-call-events";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandStatus,
} from "@/types/proto-es/v1/command_pb";

const eventTypeColors: Record<number, string> = {
  [CommandEventType.LIFECYCLE]: "text-control-light",
  [CommandEventType.TOOL_CALL_STARTED]: "text-info",
  [CommandEventType.TOOL_CALL_FINISHED]: "text-success",
  [CommandEventType.DIFF_EMITTED]: "text-accent",
  [CommandEventType.WARNING]: "text-warning",
  [CommandEventType.RAW_ACP]: "text-control-light",
  [CommandEventType.FINAL_SUMMARY]: "text-success",
  [CommandEventType.CONTEXT_COMPACTION_STARTED]: "text-warning",
  [CommandEventType.CONTEXT_COMPACTION_FINISHED]: "text-success",
  [CommandEventType.CONTEXT_USAGE_UPDATE]: "text-info",
};

function isVisibleEvent(event: CommandEvent): boolean {
  return (
    event.type !== CommandEventType.TEXT_DELTA &&
    event.type !== CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED
  );
}

function EventRow({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const isRaw = event.type === CommandEventType.RAW_ACP;
  const showExpand = isRaw || event.type === CommandEventType.DIFF_EMITTED;

  const labelKey =
    commandEventTypeToI18nKey[event.type] ?? "command.event-unknown";

  return (
    <div
      className={cn(
        "flex flex-col gap-1 py-1.5 px-3 rounded border border-control-border",
        isRaw && "opacity-60"
      )}
    >
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "text-xs font-medium",
            eventTypeColors[event.type] ?? "text-control"
          )}
        >
          {t(labelKey)}
        </span>
        <span className="text-xs text-control-light">#{event.seqNo}</span>
        {event.summary && (
          <span className="text-xs text-control truncate flex-1">
            {event.summary.slice(0, 120)}
          </span>
        )}
        {showExpand && (
          <Button
            variant="ghost"
            size="sm"
            className="text-xs"
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? t("command.collapse") : t("command.details")}
          </Button>
        )}
        <span className="text-xs text-control-light ml-auto">
          {formatTimeOfDay(event.timestamp)}
        </span>
      </div>
      {expanded && event.payload.value && (
        <pre className="text-xs text-matrix-green font-mono bg-dark-bg rounded p-2 mt-1 overflow-auto max-h-64 whitespace-pre-wrap break-all min-w-0">
          {JSON.stringify(event.payload.value, null, 2)}
        </pre>
      )}
    </div>
  );
}

// Renders a tool-call pair as a structured card (title + input + output +
// status) inside an EventRow-style header. Used for TOOL_CALL_STARTED (with
// its paired finished) and orphan TOOL_CALL_FINISHED events. The card itself
// comes from the shared ChatToolCall component so command-detail and chat
// render tool calls identically.
function ToolEventRow({
  event,
  startedEvent,
  finishedEvent,
  selected,
  onSelect,
}: {
  event: CommandEvent;
  startedEvent?: CommandEvent;
  finishedEvent?: CommandEvent;
  selected?: boolean;
  onSelect?: (seqNo: number) => void;
}) {
  const { t } = useTranslation();
  const labelKey =
    commandEventTypeToI18nKey[event.type] ?? "command.event-unknown";
  const targetSeqNo = startedEvent?.seqNo ?? event.seqNo;

  return (
    <div
      role={onSelect ? "button" : undefined}
      tabIndex={onSelect ? 0 : undefined}
      aria-pressed={selected}
      onClick={() => onSelect?.(targetSeqNo)}
      onKeyDown={(e) => {
        if (!onSelect || (e.key !== "Enter" && e.key !== " ")) return;
        e.preventDefault();
        onSelect(targetSeqNo);
      }}
      className={cn(
        "flex flex-col gap-1 py-1.5 px-1 rounded",
        onSelect && "cursor-pointer",
        selected && "ring-1 ring-accent bg-accent/10"
      )}
    >
      <div className="flex items-center gap-2 px-2">
        <span
          className={cn(
            "text-xs font-medium",
            eventTypeColors[event.type] ?? "text-control"
          )}
        >
          {t(labelKey)}
        </span>
        <span className="text-xs text-control-light">#{event.seqNo}</span>
        <span className="text-xs text-control-light ml-auto">
          {formatTimeOfDay(event.timestamp)}
        </span>
      </div>
      <ChatToolCall startedEvent={startedEvent} finishedEvent={finishedEvent} />
    </div>
  );
}

export function CommandDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId, commandId } = useParams<{
    agentId: string;
    commandId: string;
  }>();
  const getCommand = useAppStore((s) => s.getCommand);
  const cancelCommand = useAppStore((s) => s.cancelCommand);
  const steerCommand = useAppStore((s) => s.steerCommand);
  const watchCommand = useAppStore((s) => s.watchCommand);
  const watchCommandEvents = useAppStore((s) => s.watchCommandEvents);
  const activeOutputs = useAppStore((s) => s.activeOutputs);
  const activeEvents = useAppStore((s) => s.activeEvents);
  const abortRef = useRef<AbortController | null>(null);

  const [cmd, setCmd] = useState<{
    name: string;
    command: string;
    instruction: string;
    profile: string;
    finalSummary: string;
    status: number;
    exitCode: number;
    durationMs: bigint;
    principalName: string;
    errorMessage: string;
    created: string;
  } | null>(null);
  const [cancelling, setCancelling] = useState(false);
  const [steerText, setSteerText] = useState("");
  const [steering, setSteering] = useState(false);
  const [tab, setTab] = useState<"run" | "summary" | null>(null);
  const [showCompletionHint, setShowCompletionHint] = useState(false);
  const [selectedToolSeq, setSelectedToolSeq] = useState<number | null>(null);
  const prevStatusRef = useRef<number | null>(null);
  const completionNotifiedRef = useRef(false);
  const refreshDoneRef = useRef(false);
  const loadedCmdNameRef = useRef<string | null>(null);

  const cmdName = `agents/${agentId}/commands/${commandId}`;

  const load = useCallback(async () => {
    if (!cmdName) return;
    const c = await getCommand(cmdName);
    if (!c) return;
    setCmd({
      name: c.name,
      command: c.command,
      instruction: c.instruction,
      profile: c.profile,
      finalSummary: c.finalSummary,
      status: c.status,
      exitCode: c.exitCode,
      durationMs: c.durationMs,
      principalName: c.principalName,
      errorMessage: c.errorMessage,
      created: formatTimestamp(c.createdAt),
    });
  }, [cmdName, getCommand]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!cmdName) return;

    abortRef.current?.abort();
    refreshDoneRef.current = false;
    loadedCmdNameRef.current = null;
    setTab(null);
    setShowCompletionHint(false);
    setSelectedToolSeq(null);

    const controller = new AbortController();
    abortRef.current = controller;
    const signal = controller.signal;

    const refreshOnFinish = async (finished: boolean) => {
      // The server closes both watch streams shortly after the command reaches
      // a terminal state. Re-fetch the command so the header status, duration,
      // exit code and final summary reflect the persisted result.
      if (!finished || refreshDoneRef.current) return;
      refreshDoneRef.current = true;
      await load();
    };

    watchCommand(cmdName, signal)
      .then(refreshOnFinish)
      .catch(() => {});
    watchCommandEvents(cmdName, signal)
      .then(refreshOnFinish)
      .catch(() => {});

    return () => {
      controller.abort();
    };
  }, [cmdName, watchCommand, watchCommandEvents, load]);

  const outputs = activeOutputs[cmdName] ?? [];
  const events = activeEvents[cmdName] ?? [];
  const visibleEvents = events.filter(isVisibleEvent);

  // The FINAL_SUMMARY event carries the summary text and is broadcast before
  // the manager persists it, so optimistically surface the terminal state and
  // summary from the event; the stream-close refetch reconciles with the
  // authoritative Command.
  const finalSummaryEvent = useMemo(() => {
    let latest: CommandEvent | undefined;
    for (const ev of visibleEvents) {
      if (ev.type === CommandEventType.FINAL_SUMMARY) latest = ev;
    }
    return latest;
  }, [visibleEvents]);

  const displayCmd = useMemo(() => {
    if (!cmd) return null;
    if (!finalSummaryEvent) return cmd;
    const running =
      cmd.status === CommandStatus.PENDING ||
      cmd.status === CommandStatus.RUNNING;
    return {
      ...cmd,
      status: running ? CommandStatus.COMPLETED : cmd.status,
      finalSummary: cmd.finalSummary || finalSummaryEvent.summary || "",
    };
  }, [cmd, finalSummaryEvent]);

  // Pick the default tab on first load (terminal -> summary, otherwise run).
  // When a command finishes while the page is open, never yank the user away
  // from the output: show a one-shot hint instead and only switch after they
  // confirm. Later manual tab switches never re-trigger the hint.
  useEffect(() => {
    const status = displayCmd?.status;
    const name = displayCmd?.name;
    if (status == null || !name) return;
    const isTerminal =
      status !== CommandStatus.PENDING && status !== CommandStatus.RUNNING;
    if (loadedCmdNameRef.current !== name) {
      loadedCmdNameRef.current = name;
      prevStatusRef.current = status;
      completionNotifiedRef.current = isTerminal;
      setTab(isTerminal ? "summary" : "run");
      return;
    }
    const wasRunning =
      prevStatusRef.current === CommandStatus.PENDING ||
      prevStatusRef.current === CommandStatus.RUNNING;
    prevStatusRef.current = status;
    if (!completionNotifiedRef.current && wasRunning && isTerminal) {
      completionNotifiedRef.current = true;
      if (tab === "run") setShowCompletionHint(true);
    }
  }, [displayCmd?.status, displayCmd?.name, tab]);

  const handleTabChange = (value: string) => {
    setTab(value === "summary" ? "summary" : "run");
    if (value === "summary") setShowCompletionHint(false);
  };

  const toggleToolSelect = (seqNo: number) => {
    setSelectedToolSeq((cur) => (cur === seqNo ? null : seqNo));
  };

  const latestUsage = useMemo(() => {
    const usage = visibleEvents.filter(
      (ev) => ev.type === CommandEventType.CONTEXT_USAGE_UPDATE
    );
    return usage.length > 0 ? usage[usage.length - 1] : null;
  }, [visibleEvents]);

  // Pair TOOL_CALL_STARTED with TOOL_CALL_FINISHED so the Events panel shows
  // one structured card per tool call (input + output) instead of two
  // disconnected rows whose payloads are never expanded today.
  const toolPairs = useMemo(
    () => pairToolCallEvents(visibleEvents),
    [visibleEvents]
  );
  const toolPairByStartedSeqNo = useMemo(() => {
    const m = new Map<number, ToolCallPair>();
    for (const p of toolPairs) m.set(p.started.seqNo, p);
    return m;
  }, [toolPairs]);
  const pairedFinishedSeqNos = useMemo(() => {
    const s = new Set<number>();
    for (const p of toolPairs) if (p.finished) s.add(p.finished.seqNo);
    return s;
  }, [toolPairs]);

  const renderEvent = (ev: CommandEvent) => {
    // Usage updates are rendered as one live progress bar (latestUsage), not
    // one row per rate-limited update.
    if (ev.type === CommandEventType.CONTEXT_USAGE_UPDATE) return null;
    if (ev.type === CommandEventType.TOOL_CALL_STARTED) {
      const pair = toolPairByStartedSeqNo.get(ev.seqNo);
      return (
        <ToolEventRow
          key={ev.seqNo}
          event={ev}
          startedEvent={ev}
          finishedEvent={pair?.finished}
          selected={selectedToolSeq === ev.seqNo}
          onSelect={toggleToolSelect}
        />
      );
    }
    if (ev.type === CommandEventType.TOOL_CALL_FINISHED) {
      // Paired finished events are already rendered inside the started card.
      if (pairedFinishedSeqNos.has(ev.seqNo)) return null;
      return (
        <ToolEventRow
          key={ev.seqNo}
          event={ev}
          startedEvent={undefined}
          finishedEvent={ev}
          selected={selectedToolSeq === ev.seqNo}
          onSelect={toggleToolSelect}
        />
      );
    }
    return <EventRow key={ev.seqNo} event={ev} />;
  };

  const isRunning =
    !!displayCmd &&
    (displayCmd.status === CommandStatus.PENDING ||
      displayCmd.status === CommandStatus.RUNNING);

  const handleCancel = async () => {
    if (!cmdName) return;
    setCancelling(true);
    try {
      await cancelCommand(cmdName);
      load();
    } finally {
      setCancelling(false);
    }
  };

  const handleSteer = async () => {
    if (!cmdName || !steerText.trim()) return;
    setSteering(true);
    try {
      await steerCommand(cmdName, steerText.trim());
      setSteerText("");
    } finally {
      setSteering(false);
    }
  };

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex-1 min-h-0 p-4 flex flex-col gap-4 w-full">
        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => navigate(`/members/agents/${agentId}/commands`)}
          >
            &larr; {t("command.back")}
          </Button>
        </div>

        {displayCmd && (
          <>
            <div className="flex items-center justify-between gap-3 shrink-0">
              <div className="flex items-center gap-3 min-w-0">
                <h1 className="text-lg font-mono font-semibold text-main truncate max-w-xl">
                  {displayCmd.instruction || displayCmd.command}
                </h1>
                <CommandStatusBadge status={displayCmd.status} />
              </div>
              {isRunning && (
                <div className="flex items-center gap-2">
                  <div className="flex items-center gap-1.5">
                    <Input
                      className="w-56"
                      placeholder={t("command.steer-placeholder")}
                      value={steerText}
                      onChange={(e) => setSteerText(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleSteer();
                      }}
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={handleSteer}
                      disabled={steering || !steerText.trim()}
                    >
                      {steering ? t("command.steering") : t("command.steer")}
                    </Button>
                  </div>
                  <Button
                    variant="outline"
                    onClick={handleCancel}
                    disabled={cancelling}
                  >
                    {cancelling ? t("command.cancelling") : t("common.cancel")}
                  </Button>
                </div>
              )}
            </div>

            <div className="flex gap-6 text-sm text-control-light flex-wrap shrink-0">
              <span>
                {t("command.duration")}: {formatDuration(displayCmd.durationMs)}
              </span>
              {displayCmd.exitCode !== undefined &&
                displayCmd.exitCode !== 0 && (
                  <span>
                    {t("command.exit-code")}: {displayCmd.exitCode}
                  </span>
                )}
              {displayCmd.principalName && (
                <span>
                  {t("command.sent-by")}: {displayCmd.principalName}
                </span>
              )}
              <span>{displayCmd.created}</span>
            </div>

            {displayCmd.errorMessage && (
              <div className="rounded bg-error/10 border border-control-border p-3 text-sm text-error shrink-0">
                {displayCmd.errorMessage}
              </div>
            )}

            {showCompletionHint && (
              <div className="shrink-0 flex items-center gap-3 rounded border border-accent/40 bg-accent/10 px-3 py-2">
                <span className="flex-1 text-sm text-control">
                  {t("command.completed-hint")}
                </span>
                <Button
                  size="sm"
                  onClick={() => {
                    setTab("summary");
                    setShowCompletionHint(false);
                  }}
                >
                  {t("command.view-final-summary")}
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowCompletionHint(false)}
                >
                  {t("common.close")}
                </Button>
              </div>
            )}
          </>
        )}

        {displayCmd && tab && (
          <Tabs
            value={tab}
            onValueChange={handleTabChange}
            className="flex-1 min-h-0 flex flex-col"
          >
            <TabsList className="shrink-0">
              <TabsTrigger value="run">{t("command.tab-run")}</TabsTrigger>
              <TabsTrigger value="summary">
                {t("command.tab-summary")}
              </TabsTrigger>
            </TabsList>

            <TabsPanel value="run" keepMounted className="flex-1 min-h-0 mt-3">
              <div className="h-full flex flex-col gap-4 lg:grid lg:grid-cols-[minmax(0,1fr)_380px] lg:grid-rows-[minmax(0,1fr)]">
                <div className="flex flex-col gap-2 min-w-0 min-h-0 flex-1 lg:h-full">
                  <h2 className="text-sm font-medium text-control shrink-0">
                    {t("command.output")}
                  </h2>
                  <CommandTimeline
                    outputs={outputs}
                    events={visibleEvents}
                    scrollToSeqNo={selectedToolSeq}
                    activeSeqNo={selectedToolSeq}
                    onToolCardClick={toggleToolSelect}
                    active={tab === "run"}
                    className="flex-1 min-h-0"
                  />
                </div>

                {visibleEvents.length > 0 && (
                  <div className="flex flex-col gap-2 min-w-0 min-h-0 flex-1 lg:h-full">
                    <h2 className="text-sm font-medium text-control shrink-0">
                      {t("command.events")}
                    </h2>
                    <div className="rounded border border-control-border p-2 flex flex-col gap-1 flex-1 min-h-0 overflow-auto">
                      {latestUsage && <ContextUsageBar event={latestUsage} />}
                      {visibleEvents.map((ev) => renderEvent(ev))}
                    </div>
                  </div>
                )}

                {visibleEvents.length === 0 &&
                  displayCmd.status === CommandStatus.RUNNING && (
                    <div className="flex flex-col gap-2 min-w-0 min-h-0 flex-1 lg:h-full">
                      <h2 className="text-sm font-medium text-control shrink-0">
                        {t("command.events")}
                      </h2>
                      <div className="rounded border border-control-border p-4 text-xs text-control-light">
                        {t("command.waiting-events")}
                      </div>
                    </div>
                  )}
              </div>
            </TabsPanel>

            <TabsPanel
              value="summary"
              keepMounted
              className="flex-1 min-h-0 mt-3"
            >
              <div className="h-full overflow-y-auto rounded border border-control-border p-3">
                {displayCmd.finalSummary ? (
                  <FinalSummary content={displayCmd.finalSummary} />
                ) : (
                  <p className="text-sm text-control-light">
                    {t("command.no-final-summary")}
                  </p>
                )}
              </div>
            </TabsPanel>
          </Tabs>
        )}
      </div>
    </div>
  );
}
