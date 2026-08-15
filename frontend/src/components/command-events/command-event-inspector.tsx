import { ChevronDown, ChevronRight } from "lucide-react";
import MarkdownRender from "markstream-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChatDiff } from "@/components/chat-events/diff-view";
import { ChatWarning } from "@/components/chat-events/warning";
import { ContextUsageBar } from "@/components/context-usage-bar";
import { TokenUsageCard } from "@/components/token-usage-card";
import { cn } from "@/lib/utils";
import type { CommandEvent } from "@/types/proto-es/v1/command_pb";
import { CommandEventType } from "@/types/proto-es/v1/command_pb";
import { getCommandEventKind, getOutputStreamKind } from "./command-event-kind";

export interface InspectorOutput {
  /** Merged content of the output run. */
  content: string;
  /** Epoch ms when the run started. */
  startTs: number;
  /** Epoch ms when the run ended. */
  endTs: number;
  type: number;
}

export interface CommandEventInspectorProps {
  event: CommandEvent;
  startedEvent?: CommandEvent;
  finishedEvent?: CommandEvent;
  /** When set, the inspector shows an ASSISTANT/output view instead of an event. */
  output?: InspectorOutput;
  onClose?: () => void;
  className?: string;
}

type TabId =
  | "summary"
  | "preview"
  | "payload"
  | "result"
  | "diff"
  | "raw"
  | "usage"
  | "timing";

function availableTabs(event: CommandEvent): TabId[] {
  const tabs: TabId[] = ["summary"];
  switch (event.type) {
    case CommandEventType.TOOL_CALL_STARTED:
    case CommandEventType.TOOL_CALL_FINISHED:
      tabs.push("payload", "result", "timing", "raw");
      break;
    case CommandEventType.DIFF_EMITTED:
      tabs.unshift("diff");
      tabs.push("raw");
      break;
    case CommandEventType.WARNING:
      tabs.push("raw");
      break;
    case CommandEventType.RAW_ACP:
      tabs.unshift("raw");
      break;
    case CommandEventType.CONTEXT_USAGE_UPDATE:
      tabs.unshift("usage");
      tabs.push("raw");
      break;
    case CommandEventType.TOKEN_USAGE:
      tabs.unshift("usage");
      tabs.push("raw");
      break;
    case CommandEventType.CONTEXT_COMPACTION_STARTED:
    case CommandEventType.CONTEXT_COMPACTION_FINISHED:
      tabs.push("payload", "raw");
      break;
    default:
      tabs.push("raw");
      break;
  }
  return tabs;
}

function formatDateTime(ts: { seconds?: bigint } | undefined): string {
  if (!ts?.seconds) return "";
  return new Date(Number(ts.seconds) * 1000).toLocaleString();
}

function formatTimeMs(ms: number): string {
  return new Date(ms).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatDurationMs(ms: number | undefined): string {
  if (ms === undefined || !Number.isFinite(ms)) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`;
  return `${(ms / 60000).toFixed(1)} min`;
}

// JSON.stringify throws on BigInt (protobuf int64 fields are BigInt). Convert
// them to strings so payloads containing e.g. token/context counts can render.
function safeStringify(value: unknown): string {
  return JSON.stringify(
    value,
    (_key, v) => (typeof v === "bigint" ? v.toString() : v),
    2
  );
}

function RawPayload({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  if (!event.payload.value) {
    return (
      <p className="text-xs italic text-control-light">
        {t("command.event-no-payload")}
      </p>
    );
  }
  return (
    <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded border border-control-border bg-control-bg/50 p-2 font-mono text-[11px] text-control">
      {safeStringify(event.payload.value)}
    </pre>
  );
}

function OverviewRow({ dt, dd }: { dt: string; dd: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 py-1">
      <dt className="shrink-0 text-[11px] text-control-light">{dt}</dt>
      <dd className="min-w-0 flex-1 truncate text-right text-[11px] text-control">
        {dd}
      </dd>
    </div>
  );
}

function OverviewSection({
  title,
  children,
  defaultOpen = true,
}: {
  title: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className="border-t border-control-border">
      <h3 className="m-0">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center gap-1.5 px-3 py-2 text-left text-[11px] font-medium text-control hover:bg-control-bg/60"
        >
          {open ? (
            <ChevronDown className="size-3 shrink-0 text-control-light" />
          ) : (
            <ChevronRight className="size-3 shrink-0 text-control-light" />
          )}
          <span>{title}</span>
        </button>
      </h3>
      {open && <div className="px-3 pb-3">{children}</div>}
    </section>
  );
}

function ToolOverview({
  event,
  startedEvent,
  finishedEvent,
}: {
  event: CommandEvent;
  startedEvent?: CommandEvent;
  finishedEvent?: CommandEvent;
}) {
  const { t } = useTranslation();
  const title =
    startedEvent?.payload.case === "toolCallStarted"
      ? startedEvent.payload.value.title
      : t("chat.tool-call");
  const status =
    finishedEvent?.payload.case === "toolCallFinished"
      ? finishedEvent.payload.value.status
      : undefined;
  const startedAt = startedEvent?.timestamp?.seconds
    ? Number(startedEvent.timestamp.seconds) * 1000
    : undefined;
  const finishedAt = finishedEvent?.timestamp?.seconds
    ? Number(finishedEvent.timestamp.seconds) * 1000
    : undefined;
  const duration = startedAt && finishedAt ? finishedAt - startedAt : undefined;

  return (
    <div>
      <dl className="px-3 py-1">
        <OverviewRow dt={t("command.inspector-name")} dd={title} />
        <OverviewRow
          dt={t("command.inspector-status")}
          dd={
            finishedEvent ? (
              <span
                className={
                  status === "error" || status === "failed"
                    ? "text-error"
                    : "text-success"
                }
              >
                {status === "error" || status === "failed"
                  ? t("chat.tool-error")
                  : t("chat.tool-finished")}
              </span>
            ) : (
              <span className="text-warning">{t("chat.tool-started")}</span>
            )
          }
        />
        <OverviewRow
          dt={t("command.inspector-started")}
          dd={formatDateTime(startedEvent?.timestamp)}
        />
        <OverviewRow
          dt={t("command.inspector-duration")}
          dd={formatDurationMs(duration)}
        />
      </dl>

      {startedEvent?.payload.case === "toolCallStarted" && (
        <OverviewSection title={t("command.inspector-payload")}>
          <RawPayload event={startedEvent} />
        </OverviewSection>
      )}

      {finishedEvent?.payload.case === "toolCallFinished" && (
        <OverviewSection title={t("command.inspector-result")}>
          <RawPayload event={finishedEvent} />
        </OverviewSection>
      )}

      <OverviewSection title={t("command.inspector-raw")}>
        <RawPayload event={event} />
      </OverviewSection>
    </div>
  );
}

function DiffOverview({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  return (
    <div>
      <dl className="px-3 py-1">
        <OverviewRow
          dt={t("command.inspector-name")}
          dd={
            event.payload.case === "diffEmitted"
              ? event.payload.value.path
              : "—"
          }
        />
      </dl>
      <OverviewSection title={t("command.inspector-diff")}>
        <ChatDiff event={event} />
      </OverviewSection>
      <OverviewSection title={t("command.inspector-raw")}>
        <RawPayload event={event} />
      </OverviewSection>
    </div>
  );
}

function SummaryOverview({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  const kind = getCommandEventKind(event.type);
  return (
    <div>
      <dl className="px-3 py-1">
        <OverviewRow dt={t("command.inspector-type")} dd={t(kind.labelKey)} />
        <OverviewRow dt={t("command.inspector-seq")} dd={`#${event.seqNo}`} />
        <OverviewRow
          dt={t("command.inspector-time")}
          dd={formatDateTime(event.timestamp)}
        />
      </dl>
      <OverviewSection title={t("command.inspector-summary")}>
        <p className="text-xs text-control">
          {event.summary || t("command.event-no-summary")}
        </p>
      </OverviewSection>
      <OverviewSection title={t("command.inspector-raw")}>
        <RawPayload event={event} />
      </OverviewSection>
    </div>
  );
}

function UsageOverview({ event }: { event: CommandEvent }) {
  const { t } = useTranslation();
  return (
    <div>
      <dl className="px-3 py-1">
        <OverviewRow
          dt={t("command.inspector-type")}
          dd={t(getCommandEventKind(event.type).labelKey)}
        />
        <OverviewRow
          dt={t("command.inspector-time")}
          dd={formatDateTime(event.timestamp)}
        />
      </dl>
      {event.type === CommandEventType.CONTEXT_USAGE_UPDATE && (
        <OverviewSection title={t("command.inspector-usage")}>
          <ContextUsageBar event={event} />
        </OverviewSection>
      )}
      {event.type === CommandEventType.TOKEN_USAGE &&
        event.payload.case === "tokenUsage" && (
          <OverviewSection title={t("command.inspector-usage")}>
            <TokenUsageCard usage={event.payload.value} />
          </OverviewSection>
        )}
      <OverviewSection title={t("command.inspector-raw")}>
        <RawPayload event={event} />
      </OverviewSection>
    </div>
  );
}

function OutputOverview({ output }: { output: InspectorOutput }) {
  const { t } = useTranslation();
  const chars = output.content.length;
  const lines = output.content.split("\n").length;
  const duration = Math.max(0, output.endTs - output.startTs);
  return (
    <div className="flex flex-col gap-3 p-3">
      <dl>
        <OverviewRow
          dt={t("command.inspector-length")}
          dd={`${chars.toLocaleString()} chars · ${lines.toLocaleString()} lines`}
        />
        <OverviewRow
          dt={t("command.inspector-started")}
          dd={formatTimeMs(output.startTs)}
        />
        <OverviewRow
          dt={t("command.inspector-finished")}
          dd={formatTimeMs(output.endTs)}
        />
        <OverviewRow
          dt={t("command.inspector-duration")}
          dd={formatDurationMs(duration)}
        />
      </dl>
    </div>
  );
}

function OutputPreview({ output }: { output: InspectorOutput }) {
  return (
    <div className="markstream-chat p-3">
      <MarkdownRender
        customId="command-output-preview"
        content={output.content}
        final
        smoothStreaming={false}
        fade
      />
    </div>
  );
}

function OutputRaw({ output }: { output: InspectorOutput }) {
  return (
    <div className="p-3">
      <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all rounded border border-control-border bg-control-bg/50 p-2 font-mono text-[11px] text-control">
        {output.content}
      </pre>
    </div>
  );
}

export function CommandEventInspector({
  event,
  startedEvent,
  finishedEvent,
  output,
  onClose,
  className,
}: CommandEventInspectorProps) {
  const { t } = useTranslation();
  const tabs = useMemo(
    () =>
      output
        ? (["summary", "preview", "raw"] as TabId[])
        : availableTabs(event),
    [output, event]
  );
  const [activeTab, setActiveTab] = useState<TabId>(tabs[0] ?? "summary");
  const kind = output
    ? getOutputStreamKind(output.type)
    : getCommandEventKind(event.type);

  const tabLabels: Record<TabId, string> = {
    summary: t("command.inspector-summary"),
    preview: t("command.inspector-preview"),
    payload: t("command.inspector-payload"),
    result: t("command.inspector-result"),
    diff: t("command.inspector-diff"),
    raw: t("command.inspector-raw"),
    usage: t("command.inspector-usage"),
    timing: t("command.inspector-timing"),
  };

  const isTool =
    !output &&
    (event.type === CommandEventType.TOOL_CALL_STARTED ||
      event.type === CommandEventType.TOOL_CALL_FINISHED);
  const isDiff = !output && event.type === CommandEventType.DIFF_EMITTED;
  const isUsage =
    !output &&
    (event.type === CommandEventType.CONTEXT_USAGE_UPDATE ||
      event.type === CommandEventType.TOKEN_USAGE);

  return (
    <aside
      aria-label={t("command.inspector-title")}
      className={cn(
        "flex min-h-0 flex-col overflow-hidden rounded-lg border border-control-border bg-background shadow-xl",
        className
      )}
    >
      {/* Header */}
      <div className="flex shrink-0 items-center gap-2 border-b border-control-border bg-control-bg/50 px-3 py-2">
        <span
          className={cn(
            "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium",
            kind.tagClass
          )}
        >
          <kind.icon className="size-3 shrink-0" />
          {t(kind.labelKey)}
        </span>
        {!output && (
          <span className="text-[10px] text-control-light">#{event.seqNo}</span>
        )}
        <span className="ml-auto min-w-0 flex-1 truncate text-right text-[10px] text-control-light">
          {output
            ? formatTimeMs(output.startTs)
            : formatDateTime(event.timestamp)}
        </span>
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-control-light hover:bg-control-bg hover:text-control"
          >
            <span aria-hidden="true">×</span>
          </button>
        )}
      </div>

      {/* Tabs */}
      <div
        className="flex shrink-0 gap-1 border-b border-control-border px-2"
        role="tablist"
        aria-label={t("command.inspector-title")}
      >
        {tabs.map((tab) => (
          <button
            key={tab}
            type="button"
            role="tab"
            aria-selected={activeTab === tab}
            onClick={() => setActiveTab(tab)}
            className={cn(
              "relative rounded-t px-2 py-1.5 text-[11px] font-medium text-control-light transition-colors hover:text-control",
              activeTab === tab &&
                "text-accent after:absolute after:inset-x-1 after:bottom-0 after:h-0.5 after:bg-accent"
            )}
          >
            {tabLabels[tab]}
          </button>
        ))}
      </div>

      {/* Body */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {output ? (
          <>
            {activeTab === "summary" && <OutputOverview output={output} />}
            {activeTab === "preview" && <OutputPreview output={output} />}
            {activeTab === "raw" && <OutputRaw output={output} />}
          </>
        ) : (
          <>
            {activeTab === "summary" &&
              (isTool ? (
                <ToolOverview
                  event={event}
                  startedEvent={startedEvent}
                  finishedEvent={finishedEvent}
                />
              ) : isDiff ? (
                <DiffOverview event={event} />
              ) : isUsage ? (
                <UsageOverview event={event} />
              ) : (
                <SummaryOverview event={event} />
              ))}

            {activeTab === "payload" && startedEvent && (
              <RawPayload event={startedEvent} />
            )}
            {activeTab === "result" && finishedEvent && (
              <RawPayload event={finishedEvent} />
            )}
            {activeTab === "diff" && <ChatDiff event={event} />}
            {activeTab === "raw" && <RawPayload event={event} />}
            {activeTab === "usage" &&
              event.type === CommandEventType.CONTEXT_USAGE_UPDATE && (
                <div className="p-3">
                  <ContextUsageBar event={event} />
                </div>
              )}
            {activeTab === "usage" &&
              event.type === CommandEventType.TOKEN_USAGE &&
              event.payload.case === "tokenUsage" && (
                <div className="p-3">
                  <TokenUsageCard usage={event.payload.value} />
                </div>
              )}
            {activeTab === "timing" && (
              <div className="p-3">
                {isTool ? (
                  <dl>
                    <OverviewRow
                      dt={t("command.inspector-started")}
                      dd={formatDateTime(startedEvent?.timestamp)}
                    />
                    <OverviewRow
                      dt={t("command.inspector-finished")}
                      dd={formatDateTime(finishedEvent?.timestamp)}
                    />
                  </dl>
                ) : (
                  <p className="text-xs italic text-control-light">
                    {t("command.event-no-timing")}
                  </p>
                )}
              </div>
            )}

            {event.type === CommandEventType.WARNING && (
              <div className="p-3">
                <ChatWarning event={event} />
              </div>
            )}
          </>
        )}
      </div>
    </aside>
  );
}
