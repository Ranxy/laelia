import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { CommandEventInspector } from "@/components/command-events/command-event-inspector";
import { isVisibleEvent } from "@/components/command-events/command-event-kind";
import {
  type CommandEventFilter,
  CommandEventLedger,
} from "@/components/command-events/command-event-ledger";
import { CommandEventTimelineOverview } from "@/components/command-events/command-event-timeline-overview";
import { CommandEventToolbar } from "@/components/command-events/command-event-toolbar";
import { CommandStatusBadge } from "@/components/command-status-badge";
import { FinalSummary } from "@/components/command-terminal";
import { TokenUsageCard } from "@/components/token-usage-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsList, TabsPanel, TabsTrigger } from "@/components/ui/tabs";
import { formatDuration, formatTimestamp } from "@/lib/command-status";
import { pairToolCallEvents, type ToolCallPair } from "@/lib/tool-call-events";
import { useAppStore } from "@/stores";
import type {
  CommandEvent,
  CommandOutput,
} from "@/types/proto-es/v1/command_pb";
import {
  CommandEventType,
  CommandStatus,
} from "@/types/proto-es/v1/command_pb";

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
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [rangeKeys, setRangeKeys] = useState<string[] | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [filter, setFilter] = useState<CommandEventFilter>("all");
  const [inspectorOpen, setInspectorOpen] = useState(false);
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
    setSelectedKey(null);
    setRangeKeys(null);
    setInspectorOpen(false);

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

  const toggleToolSelect = (key: string) => {
    setSelectedKey((cur) => (cur === key ? null : key));
    setInspectorOpen(true);
  };

  // The per-command token consumption recorded at turn end; shown on the
  // summary tab once the command completes.
  const tokenUsage = useMemo(() => {
    let latest: CommandEvent | undefined;
    for (const ev of visibleEvents) {
      if (ev.type === CommandEventType.TOKEN_USAGE) latest = ev;
    }
    return latest?.payload.case === "tokenUsage" ? latest.payload.value : null;
  }, [visibleEvents]);

  // Pair TOOL_CALL_STARTED with TOOL_CALL_FINISHED so the inspector can show
  // one structured card per tool call (input + output).
  const toolPairs = useMemo(
    () => pairToolCallEvents(visibleEvents),
    [visibleEvents]
  );
  const toolPairByStartedSeqNo = useMemo(() => {
    const m = new Map<number, ToolCallPair>();
    for (const p of toolPairs) m.set(p.started.seqNo, p);
    return m;
  }, [toolPairs]);

  // Merge consecutive same-type output chunks (mirrors the ledger) so the
  // inspector can show a full ASSISTANT message rather than one tiny chunk.
  // A tool/event between two chunks breaks the merge so each separate message
  // keeps its own row key and stays clickable.
  const mergedOutputs = useMemo(() => {
    const tsToMs = (ts: { seconds?: bigint; nanos?: number } | undefined) =>
      ts?.seconds ? Number(ts.seconds) * 1000 + (ts.nanos ?? 0) / 1_000_000 : 0;
    type Item =
      | { kind: "output"; ts: number; output: CommandOutput }
      | { kind: "break"; ts: number };
    const items: Item[] = [];
    for (const o of outputs) {
      items.push({ kind: "output", ts: tsToMs(o.timestamp), output: o });
    }
    for (const ev of visibleEvents) {
      if (
        ev.type === CommandEventType.TEXT_DELTA ||
        ev.type === CommandEventType.CONTEXT_USAGE_UPDATE ||
        ev.type === CommandEventType.RAW_ACP ||
        ev.type === CommandEventType.COMMAND_EVENT_TYPE_UNSPECIFIED
      ) {
        continue;
      }
      items.push({ kind: "break", ts: tsToMs(ev.timestamp) });
    }
    items.sort((a, b) => a.ts - b.ts);

    const out: Array<{
      key: string;
      content: string;
      startTs: number;
      endTs: number;
      type: number;
    }> = [];
    let current: (typeof out)[number] | null = null;
    for (const item of items) {
      if (item.kind === "break") {
        current = null;
        continue;
      }
      const ts = item.ts;
      if (current && current.type === item.output.type && current.endTs <= ts) {
        current.content += item.output.content;
        current.endTs = ts;
        continue;
      }
      current = {
        key: `out-${item.output.seqNo}`,
        content: item.output.content,
        startTs: ts,
        endTs: ts,
        type: item.output.type,
      };
      out.push(current);
    }
    return out;
  }, [outputs, visibleEvents]);

  // The event currently shown in the inspector, resolved from the unique row
  // key so outputs (out-*) and events (ev-*/tool-*) never get mixed up even
  // though they use independent seq_no spaces.
  const selectedInspector = useMemo(() => {
    if (selectedKey == null) return null;
    if (selectedKey.startsWith("tool-")) {
      const seqNo = Number(selectedKey.slice("tool-".length));
      const pair = toolPairByStartedSeqNo.get(seqNo);
      if (!pair) return null;
      return {
        event: pair.started,
        startedEvent: pair.started,
        finishedEvent: pair.finished,
      };
    }
    if (selectedKey.startsWith("ev-")) {
      const seqNo = Number(selectedKey.slice("ev-".length));
      const ev = visibleEvents.find((e) => e.seqNo === seqNo);
      return ev
        ? { event: ev, startedEvent: undefined, finishedEvent: undefined }
        : null;
    }
    if (selectedKey.startsWith("out-")) {
      const merged = mergedOutputs.find((m) => m.key === selectedKey);
      if (!merged) return null;
      return {
        event: undefined as never,
        startedEvent: undefined,
        finishedEvent: undefined,
        output: merged,
      };
    }
    return null;
  }, [selectedKey, toolPairByStartedSeqNo, visibleEvents, mergedOutputs]);

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
        <div className="hidden items-center gap-2 shrink-0 lg:flex">
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
                <h1 className="hidden text-lg font-mono font-semibold text-main truncate max-w-xl lg:block">
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
              <div className="flex h-full min-h-0 flex-col gap-2">
                <CommandEventToolbar
                  searchQuery={searchQuery}
                  onSearchQueryChange={setSearchQuery}
                  filter={filter}
                  onFilterChange={setFilter}
                />
                <CommandEventTimelineOverview
                  outputs={outputs}
                  events={visibleEvents}
                  selectedKey={selectedKey}
                  onSelect={toggleToolSelect}
                  onRangeSelect={setRangeKeys}
                />
                <div className="relative min-h-0 flex-1">
                  <CommandEventLedger
                    outputs={outputs}
                    events={visibleEvents}
                    selectedKey={selectedKey}
                    onSelect={toggleToolSelect}
                    scrollToKey={selectedKey}
                    rangeKeys={rangeKeys}
                    searchQuery={searchQuery}
                    filter={filter}
                    className="h-full min-w-0"
                  />
                  {inspectorOpen && selectedInspector && (
                    <CommandEventInspector
                      event={selectedInspector.event}
                      startedEvent={selectedInspector.startedEvent}
                      finishedEvent={selectedInspector.finishedEvent}
                      output={selectedInspector.output}
                      onClose={() => {
                        setInspectorOpen(false);
                        setSelectedKey(null);
                      }}
                      className="absolute inset-y-0 right-0 z-10 w-80"
                    />
                  )}
                </div>
              </div>
            </TabsPanel>

            <TabsPanel
              value="summary"
              keepMounted
              className="flex-1 min-h-0 mt-3"
            >
              <div className="h-full overflow-y-auto rounded border border-control-border p-3 flex flex-col gap-3">
                {tokenUsage && <TokenUsageCard usage={tokenUsage} />}
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
