import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate, useParams } from "react-router-dom";
import { ThreadPanel } from "@/components/chat/thread-panel";
import { ReminderStatusBadge } from "@/components/reminder-status-badge";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Textarea } from "@/components/ui/textarea";
import { formatTimestamp } from "@/lib/command-status";
import { useAppStore } from "@/stores";
import type { Reminder } from "@/types/proto-es/v1/command_pb";
import { ReminderStatus } from "@/types/proto-es/v1/command_pb";

// Common IANA timezones offered in the edit sheet. The agent/user may also type
// a custom zone; the manager validates it.
const COMMON_TZ = [
  "UTC",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Asia/Singapore",
  "Asia/Kolkata",
  "Asia/Dubai",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "America/New_York",
  "America/Chicago",
  "America/Los_Angeles",
  "America/Sao_Paulo",
];

// toDatetimeLocal converts a reminder fire_at timestamp to the value a
// <input type="datetime-local"> expects (local time, "YYYY-MM-DDTHH:MM").
function toDatetimeLocal(ts: Reminder["fireAt"]): string {
  if (!ts?.seconds) return "";
  const d = new Date(Number(ts.seconds) * 1000);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(
    d.getHours()
  )}:${pad(d.getMinutes())}`;
}

// fromDatetimeLocal parses a datetime-local string into a Date for the store.
function fromDatetimeLocal(s: string): Date | undefined {
  if (!s) return undefined;
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? undefined : d;
}

// conversationId extracts the bare conversation id from "conversations/{id}".
function conversationId(r: Reminder): string {
  return r.conversation?.split("/").pop() ?? "";
}

// messageId extracts the bare message id from "conversations/{c}/messages/{m}".
function messageId(r: Reminder): string {
  return r.message?.split("/").pop() ?? "";
}

export function ReminderDetailPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { agentId, reminderId } = useParams<{
    agentId: string;
    reminderId: string;
  }>();
  const getReminder = useAppStore((s) => s.getReminder);
  const updateReminder = useAppStore((s) => s.updateReminder);
  const cancelReminder = useAppStore((s) => s.cancelReminder);
  const channels = useAppStore((s) => s.channels);
  const openThread = useAppStore((s) => s.openThread);
  const closeThread = useAppStore((s) => s.closeThread);

  const [reminder, setReminder] = useState<Reminder | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [editOpen, setEditOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [actionError, setActionError] = useState("");
  const [saving, setSaving] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  // Edit-form state. Populated from the reminder when the sheet opens.
  const [taskContent, setTaskContent] = useState("");
  const [fireAtLocal, setFireAtLocal] = useState("");
  const [cronExpr, setCronExpr] = useState("");
  const [tz, setTz] = useState("UTC");

  const name = `reminders/${reminderId}`;

  const load = useCallback(async () => {
    if (!reminderId) return;
    const r = await getReminder(name);
    if (r) setReminder(r);
    setLoading(false);
  }, [reminderId, name, getReminder]);

  useEffect(() => {
    load();
    const handle = setInterval(load, 2000);
    return () => clearInterval(handle);
  }, [load]);

  // Open the reminder's discussion thread so ThreadPanel has messages to
  // render. ThreadPanel reads threadByRoot[rootId], which is populated by
  // openThread (it also starts a polling watcher). Depends only on the root
  // id so the 2s reminder re-fetch doesn't close/reopen the thread. Closed on
  // unmount or when a different reminder's thread is opened.
  const threadConvId = reminder ? conversationId(reminder) : "";
  const threadRootId = reminder ? messageId(reminder) : "";
  useEffect(() => {
    if (!threadConvId || !threadRootId) return;
    openThread(`conversations/${threadConvId}`, threadRootId);
    return () => closeThread();
  }, [threadConvId, threadRootId, openThread, closeThread]);

  // Terminal reminders cannot be edited or cancelled.
  const isTerminal =
    reminder?.status === ReminderStatus.COMPLETED ||
    reminder?.status === ReminderStatus.CANCELLED ||
    reminder?.status === ReminderStatus.FAILED;

  const openEdit = () => {
    if (!reminder) return;
    setTaskContent(reminder.taskContent ?? "");
    setFireAtLocal(toDatetimeLocal(reminder.fireAt));
    setCronExpr(reminder.cronExpr ?? "");
    setTz(reminder.tz || "UTC");
    setActionError("");
    setEditOpen(true);
  };

  const handleSave = async () => {
    if (!reminder) return;
    setSaving(true);
    setActionError("");
    try {
      // For a one-shot reminder fire_at is required; for a recurring reminder
      // it may be omitted and the manager computes the next cron fire.
      const fireAt = fromDatetimeLocal(fireAtLocal);
      if (!cronExpr && !fireAt) {
        setActionError(t("reminders.edit-fire-required"));
        setSaving(false);
        return;
      }
      const updated = await updateReminder(reminder.name!, {
        fireAt,
        cronExpr,
        tz,
        taskContent,
      });
      if (updated) {
        setReminder(updated);
        setEditOpen(false);
      } else {
        setActionError(t("reminders.edit-failed"));
      }
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = async () => {
    if (!reminder?.name) return;
    setCancelling(true);
    setActionError("");
    try {
      const updated = await cancelReminder(reminder.name);
      if (updated) {
        setReminder(updated);
        setCancelOpen(false);
      } else {
        setActionError(t("reminders.cancel-failed"));
      }
    } finally {
      setCancelling(false);
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-control-light">
        {t("common.loading")}
      </div>
    );
  }

  if (!reminder) {
    return (
      <div className="flex h-full flex-col">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate(`/agents/${agentId}/reminders`)}
        >
          &larr; {t("reminders.back")}
        </Button>
        <div className="flex-1 flex items-center justify-center text-sm text-control-light">
          {t("reminders.not-found")}
        </div>
      </div>
    );
  }

  const convId = conversationId(reminder);
  const rootId = messageId(reminder);
  const channel = channels.find((c) => c.name.endsWith(`/${convId}`));
  const channelTitle = channel?.title ?? convId;

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <div className="shrink-0 px-4 py-3 border-b border-control-border">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate(`/agents/${agentId}/reminders`)}
        >
          &larr; {t("reminders.back")}
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-5xl px-4 py-4 flex flex-col gap-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-3 min-w-0">
              <h1 className="text-base font-semibold text-main truncate max-w-xl">
                {t("reminders.title")}
              </h1>
              <ReminderStatusBadge status={reminder.status} />
            </div>
            {!isTerminal && (
              <div className="flex items-center gap-2 shrink-0">
                <Button variant="outline" size="sm" onClick={openEdit}>
                  {t("reminders.edit")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setActionError("");
                    setCancelOpen(true);
                  }}
                >
                  {t("reminders.cancel")}
                </Button>
              </div>
            )}
          </div>

          <div className="rounded border border-control-border p-4 flex flex-col gap-3">
            <div className="text-xs font-medium text-control-light">
              {t("reminders.header-task")}
            </div>
            <pre className="text-sm text-main whitespace-pre-wrap font-sans">
              {reminder.taskContent || "-"}
            </pre>
          </div>

          <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
            <Field
              label={t("reminders.header-schedule")}
              value={
                reminder.cronExpr
                  ? reminder.tz && reminder.tz !== "UTC"
                    ? `${reminder.cronExpr} (${reminder.tz})`
                    : reminder.cronExpr
                  : t("reminders.once")
              }
            />
            <Field
              label={t("reminders.header-fire-at")}
              value={formatTimestamp(reminder.fireAt)}
            />
            <Field
              label={t("reminders.header-assignee")}
              value={reminder.assigneeName || "-"}
            />
            <Field
              label={t("reminders.header-retry")}
              value={t("reminders.retry-count", {
                n: reminder.retryCount ?? 0,
              })}
            />
            <Field
              label={t("reminders.last-fired")}
              value={formatTimestamp(reminder.lastFiredAt)}
            />
            <Field
              label={t("reminders.last-attempt")}
              value={formatTimestamp(reminder.lastAttemptAt)}
            />
            <Field
              label={t("reminders.last-completed")}
              value={formatTimestamp(reminder.lastCompletedAt)}
            />
            <Field
              label={t("reminders.created")}
              value={formatTimestamp(reminder.createdAt)}
            />
          </div>

          {reminder.result && (
            <div className="rounded bg-accent/10 border border-control-border p-3">
              <div className="text-xs font-medium text-control mb-2">
                {t("reminders.result")}
              </div>
              <pre className="text-sm text-main whitespace-pre-wrap font-sans">
                {reminder.result}
              </pre>
            </div>
          )}
        </div>

        {/* The reminder's discussion thread: its root is the trigger message,
            so chatting here lets the user negotiate the schedule with the agent
            (requirement #5). ThreadPanel polls and renders replies + composer.
            `fluid` makes it fill this container instead of the 420px right-dock
            aside used in the channel page. */}
        {convId && rootId && (
          <div className="mx-auto max-w-5xl px-4 pb-4">
            <h2 className="text-sm font-medium text-control mb-2">
              {t("reminders.thread")}
            </h2>
            <div className="h-[480px] overflow-hidden rounded-lg border border-control-border">
              <ThreadPanel
                channelId={convId}
                channelTitle={channelTitle}
                rootMessageId={rootId}
                fluid
                onClose={() => navigate(`/agents/${agentId}/reminders`)}
                onViewInChannel={() => {
                  // Jump to the trigger message's channel and open its thread
                  // there. The chat page reads ?thread= on mount.
                  closeThread();
                  navigate(`/${convId}?thread=${rootId}`);
                }}
              />
            </div>
          </div>
        )}
      </div>

      {/* Edit sheet: full-replacement schedule + task content. */}
      <Sheet
        open={editOpen}
        onOpenChange={(next) => !next && setEditOpen(false)}
      >
        <SheetContent width="standard">
          <SheetHeader>
            <SheetTitle>{t("reminders.edit")}</SheetTitle>
            <SheetDescription>
              {t("reminders.edit-description")}
            </SheetDescription>
          </SheetHeader>
          <SheetBody className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">
                {t("reminders.field-task")}
              </span>
              <Textarea
                className="font-mono text-sm min-h-[120px]"
                value={taskContent}
                onChange={(e) => setTaskContent(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">
                {t("reminders.field-cron")}
              </span>
              <Input
                className="font-mono text-sm"
                placeholder={t("reminders.field-cron-placeholder")}
                value={cronExpr}
                onChange={(e) => setCronExpr(e.target.value)}
              />
              <span className="text-xs text-control-light">
                {t("reminders.field-cron-hint")}
              </span>
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">
                {t("reminders.field-tz")}
              </span>
              <select
                className="h-9 rounded-md border border-control-border bg-transparent px-2 text-sm"
                value={tz}
                onChange={(e) => setTz(e.target.value)}
              >
                {COMMON_TZ.map((z) => (
                  <option key={z} value={z}>
                    {z}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1.5">
              <span className="text-sm text-control-light">
                {t("reminders.field-fire-at")}
              </span>
              <Input
                type="datetime-local"
                className="text-sm"
                value={fireAtLocal}
                onChange={(e) => setFireAtLocal(e.target.value)}
              />
              <span className="text-xs text-control-light">
                {t("reminders.field-fire-at-hint")}
              </span>
            </div>
            {actionError && <p className="text-sm text-error">{actionError}</p>}
          </SheetBody>
          <SheetFooter>
            <Button
              variant="outline"
              onClick={() => setEditOpen(false)}
              disabled={saving}
            >
              {t("common.cancel")}
            </Button>
            <Button
              disabled={saving || !taskContent.trim()}
              onClick={handleSave}
            >
              {saving ? t("common.loading") : t("common.save")}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <AlertDialog
        open={cancelOpen}
        onOpenChange={(next) => !next && setCancelOpen(false)}
      >
        <AlertDialogContent>
          <AlertDialogTitle>
            {t("reminders.cancel-confirm-title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("reminders.cancel-confirm-description")}
          </AlertDialogDescription>
          {actionError && (
            <p className="text-sm text-error mt-2">{actionError}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogClose>
              <Button variant="outline" disabled={cancelling}>
                {t("common.cancel")}
              </Button>
            </AlertDialogClose>
            <Button disabled={cancelling} onClick={handleCancel}>
              {cancelling ? t("common.loading") : t("reminders.cancel")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs text-control-light">{label}</span>
      <span className="text-sm text-main">{value}</span>
    </div>
  );
}
