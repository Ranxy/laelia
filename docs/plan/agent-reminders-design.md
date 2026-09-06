# Reminder: Scheduled/Recurring Agent Tasks

> Status: verified and updated against the current code on 2026-09-06. Main changes: feature is fully implemented with deviations — completion posts TWO thread messages (SYSTEM pill + agent result reply), due reminders are delivered via the turn-opening batch ("Due reminders:" section) instead of a prompt stop-gate poll, the frontend uses TanStack Query hooks instead of a store slice, and the scheduler runs a third unverified-user cleanup loop.

## Context

Today an agent only acts when a user posts in a channel — there is no way for an agent to run a task on a schedule ("analyze github commits every day at 3am"). The codebase had no scheduler/cron/reminder/periodic mechanism anywhere (proto, DB, backend, frontend). The closest analogue is the **`task`** feature: a `task` row whose PK is its root `chat_message` id, discussion happens in the thread rooted at that message, and it has a claim/status flow.

This feature adds **reminders**: an agent recognizes a scheduling intent in a channel message, "claims" it via a command (atomic create+claim, assignee = that agent), the manager stores it with `fire_at` / `cron_expr` / `tz`, a new manager-side scheduler fires it at the due time and wakes the agent, the agent runs the task in an LLM session, then calls `CompleteReminder` which **atomically** marks it done and writes the completion into the trigger message's thread — visible in both the channel thread and the agent-page Reminders tab, never duplicated. (As built, the completion tx posts TWO messages: a short SYSTEM lifecycle pill plus the result/error as a normal AGENT thread reply.)

### Confirmed decisions

- **Trigger modes**: one-shot (`fire_at`) + recurring (`cron_expr` + `tz`). After firing, recurring reminders compute the next `fire_at` from cron and reset to PENDING.
- **Claim model**: atomic create+claim — the agent that recognizes the intent claims it at creation; `assignee_agent_id = calling agent`.
- **Offline-at-fire**: retry 5× with backoff `5s,10s,20s,30s,60s`; if still offline, mark this fire MISSED (one-shot terminal; recurring reschedules to next cron fire) and log the attempts.
- **Completion de-dup**: single RPC `CompleteReminder`; backend writes the thread message in the same tx as the status update (as built: a SYSTEM pill + an AGENT result reply, both idempotent — a duplicate call on a non-DUE reminder posts nothing).

### Requirements coverage (5/5)

1. **Traceability** — reminder PK = trigger message id = thread root; `conversation_id` + `message` both on the row and in proto.
2. **Agent-page Reminders tab** — new tab + list/detail pages, mirroring the Commands tab.
3. **De-dup completion report** — single `CompleteReminder` RPC; ONE tx flips status to COMPLETED and posts a SYSTEM lifecycle pill plus an AGENT result reply (SYSTEM sender is excluded from the agent's channel cursor, so no self-wake, and both show once in the channel thread and the reminder detail).
4. **Manual cancel/edit** — user-facing `CancelReminder`/`UpdateReminder` with an edit Sheet (trigger-mechanism toggle, datetime picker, cron + tz, task content) and a cancel AlertDialog.
5. **Modify via thread chat** — reminder detail embeds the existing `ThreadPanel`/`openThread`, so the user chats with the agent; the agent wakes and calls `reminder update`.

## Data model

Implemented in `backend/manager/migration/migration/LATEST.sql` (reminder block; guard assertions in `migration/migration_test.go`, `TestReminderTablePresent`):

```sql
CREATE TABLE IF NOT EXISTS reminder (
  message_id          UUID PRIMARY KEY REFERENCES chat_message(id) ON DELETE CASCADE,
  conversation_id     UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
  assignee_agent_id   INTEGER NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  task_content        TEXT NOT NULL,            -- agent's structured summary of the work
  fire_at             TIMESTAMPTZ NOT NULL,     -- next fire (one-shot or computed from cron)
  cron_expr           TEXT NULL,                -- NULL = one-shot
  tz                  TEXT NOT NULL DEFAULT 'UTC',
  status              SMALLINT NOT NULL DEFAULT 1,  -- 1 PENDING,2 DUE,3 COMPLETED,4 CANCELLED,5 MISSED,6 FAILED
  retry_count         INTEGER NOT NULL DEFAULT 0,
  next_retry_at       TIMESTAMPTZ NULL,
  last_attempt_at     TIMESTAMPTZ NULL,
  last_fired_at       TIMESTAMPTZ NULL,
  last_completed_at   TIMESTAMPTZ NULL,
  result              TEXT NOT NULL DEFAULT '',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT reminder_status_check CHECK (status IN (1,2,3,4,5,6))
);
CREATE INDEX IF NOT EXISTS idx_reminder_assignee_status ON reminder(assignee_agent_id, status);
CREATE INDEX IF NOT EXISTS idx_reminder_fire_at ON reminder(fire_at) WHERE status = 1;   -- PENDING due scan
CREATE INDEX IF NOT EXISTS idx_reminder_retry   ON reminder(next_retry_at) WHERE status = 2; -- DUE retry scan
```

The DDL above is the as-built schema (the original sketch had `assignee_agent_id UUID` with no FK and a nullable `result`; the implemented table uses an INTEGER FK to `agent(id)` and `result TEXT NOT NULL DEFAULT ''`).

Mirror the `task` table's 1:1-with-message shape: PK `message_id` = the trigger message = the thread root. No per-conversation numbering (skip `next_reminder_number` — the reminder's identity is its trigger message). `ConvertMessageToReminder` does **not** bump `conversation.version` (the trigger message already has one), exactly mirroring `store.ConvertMessageToTask`.

## Proto

Implemented in `proto/v1/v1/command.proto` (mirroring `TaskStatus`/`TaskInfo` and the task RPCs):

- `enum ReminderStatus { REMINDER_STATUS_UNSPECIFIED=0; REMINDER_STATUS_PENDING=1; REMINDER_STATUS_DUE=2; REMINDER_STATUS_COMPLETED=3; REMINDER_STATUS_CANCELLED=4; REMINDER_STATUS_MISSED=5; REMINDER_STATUS_FAILED=6; }` (buf lint requires the `REMINDER_STATUS_` prefix — the "bare names" note in the original plan did not survive lint).
- `message Reminder { string name; string conversation; string message; string assignee_agent; string assignee_name; string task_content; google.protobuf.Timestamp fire_at; string cron_expr; string tz; ReminderStatus status; int32 retry_count; google.protobuf.Timestamp next_retry_at/last_attempt_at/last_fired_at/last_completed_at/created_at/updated_at; string result; }` — `name = "reminders/{message_id}"`.
- RPCs on `CommandService` (ConnectRPC default routes; agent CLI + frontend both call these):
  - `ConvertMessageToReminder(message, task_content, fire_at, cron_expr, tz) -> Reminder` — agent create+claim (agent-only, no auth_method).
  - `ListReminders(agent, conversation, status_filter, page_size, page_token) -> (reminders[], next_page_token)` — IAM + `laelia.reminders.list`.
  - `GetReminder(name) -> Reminder` — IAM + `laelia.reminders.get`.
  - `UpdateReminder(name, fire_at, cron_expr, tz, task_content) -> Reminder` — IAM + `laelia.reminders.update`, audit.
  - `CancelReminder(name) -> Reminder` — IAM + `laelia.reminders.cancel`, audit.
  - `CompleteReminder(name, result) -> Reminder` — IAM + `laelia.reminders.update`; owning agent only (handler `requireReminderOwner`).
  - `FailReminder(name, error) -> Reminder` — IAM + `laelia.reminders.update`; owning agent only.
  - `ListDueReminders() -> (reminders[])` — agent drain: DUE reminders for the calling agent (no auth_method).

Regenerate with `cd proto && buf generate` (writes `frontend/src/types/proto-es/v1/`).

## Backend

### Store — `backend/manager/store/reminder.go` (implemented; mirrors `task.go`)

- `ConvertMessageToReminder(ctx, msgID, convID, assigneeAgentID, taskContent, fireAt, cronExpr, tz)` — one tx, `EXISTS` guard → `ErrReminderAlreadyExists`, unique PK on `message_id`; the assignee is set in the INSERT (atomic create+claim). No version bump.
- `GetReminder(msgID)`, `ListReminders(agentID, convID, statusFilter, viewer, pageSize, pageToken)` (offset pagination, PENDING-first ordering; `viewer *ConversationMemberFilter` restricts non-admin users to reminders in conversations they belong to), `UpdateReminderFields(msgID, fireAt, cronExpr, tz, taskContent)` (DUE/MISSED reset to PENDING; terminal → `ErrReminderInvalidTransition`), `CancelReminder(msgID)` (PENDING/DUE/MISSED → CANCELLED; terminal is a no-op returning current state).
- `HasDueReminders(agentID) -> bool` — cheap `EXISTS WHERE assignee=$1 AND status=DUE` (OR-folded into `HandleBeginSession`'s work gate).
- `ListDueReminders(agentID)` (DUE reminders for the drain loop), `ListDuePending(now)` (scheduler due scan), `ListDueRetrying(now)` (scheduler retry scan).
- `MarkDue(msgID, firedAt)` — PENDING→DUE, set `last_fired_at`, reset retry fields (idempotent).
- `SetRetry(msgID, retryCount, nextRetryAt, attemptAt)` — DUE, offline path; `ClearRetry(msgID)` clears the timer when the agent is connected and woken.
- **`CompleteReminderAndPostNotification(ctx, msgID, result, label string, nextFireAt *time.Time) (posted []*ChatMessage, r *Reminder, err error)`** and `FailReminderAndPostNotification(ctx, msgID, errMsg, label, nextFireAt)` — shared tx body `completeReminderTx`:
  - `UPDATE reminder SET status=COMPLETED/FAILED (or back to PENDING with `fire_at = nextFireAt` for recurring), result=$, retry fields cleared WHERE status=DUE` — the rows-affected guard makes duplicates a no-op (current state returned, nothing posted).
  - bumps `conversation.version` twice and inserts TWO thread replies in the same tx: a short **SYSTEM lifecycle pill** (`label`, e.g. "✅ Jane completed the reminder") and the result/error as a **normal AGENT message** (markdown, avatar, owner-anchored principal) so it renders like any other agent reply instead of being jammed into a system notification line.
  - The next cron fire is computed by the CALLER (`api/v1/reminder.go` `nextFireOrNil`, scheduler `miss`) and passed in — there is no standalone `RescheduleRecurring` store function. The tx posts directly via `createChatMessageInTx` and does NOT call NotifyWake, so posting never wakes any agent (the owner consumes its own message in the same drain session via message check / thread check, IsOwn → ignored → ack).
- `MarkMissedAndPostNotification(ctx, msgID, nextFireAt)` — one-shot terminal MISSED; recurring → PENDING at `nextFireAt` (nil → terminal MISSED). A single SYSTEM thread message "⏰ Reminder missed after N delivery retries (agent offline)".

### Scheduler — `backend/manager/component/scheduler/scheduler.go` (implemented)

Long-lived goroutine set, lifecycle via `lifecycleCtx`/`lifecycleCancel`/`wg`, single-flight `Start`, 1s tick loops each with a 5s per-scan context timeout. **Three** loops (the third was added later for account hygiene):

1. **Due scan** (`scanDue`): `store.ListDuePending(now)` → for each PENDING reminder with `fire_at <= now`: `MarkDue`; then `deliver` — if `dispatcher.IsAgentConnected(agentID)` → `ClearRetry` + `dispatcher.NotifyWake(agentID)`; else `SetRetry(now+backoff[0], retryCount=1)`.
2. **Retry scan** (`scanRetry`): `store.ListDueRetrying(now)` → `deliver` with the stored retryCount: connected → wake + clear retry; else advance backoff `[5s,10s,20s,30s,60s]`; when `retryCount >= len(retryBackoff)` → `miss` (recurring reschedules via `schedule.NextFire`, one-shot terminal MISSED) + SYSTEM thread message + REMINDER activity.
3. **Unverified-user cleanup** (`scanUnverifiedUsers`, ~daily): soft-deletes END_USER accounts unverified for 72h — unrelated to reminders, lives in this component for its ticker.

On startup no timer heap is needed: the 1s `ListDuePending` scan over the partial index is crash-safe.

Cron/timezone: the cron library dependency is isolated in `backend/manager/component/schedule/schedule.go` (`github.com/robfig/cron/v3`, 5-field parser `cron.Minute|Hour|Dom|Month|Dow`, `Validate`/`NextFire`). `tz` is validated at create/update time (`ConvertMessageToReminder` / `UpdateReminder` → `schedule.Validate`); recurring reminders may omit `fire_at` entirely — the manager computes the first fire from cron.

### Server wiring

- Construct: `backend/manager/server/server.go` after `s.dispatcher = dispatcher.New(stores)` → `s.scheduler = scheduler.New(stores, s.dispatcher)`; `scheduler *scheduler.Scheduler` field next to `dispatcher`.
- Start: `Server.Run`, after the dispatcher is ready → `s.scheduler.Start()`.
- Stop: `s.scheduler.Stop()` **before** `s.dispatcher.Stop()` and before `s.store.Close()`.

### Dispatcher integration — `backend/manager/component/dispatcher/dispatcher.go`

`HandleBeginSession` OR-folds due reminders into the work gate so a due reminder drives a session:

```go
hasUpdates, _ := d.store.HasUpdates(ctx, agentID)
hasReminders, err := d.store.HasDueReminders(ctx, agentID)
if !hasUpdates && !hasReminders { return idle }
```

Wake path reuses `NotifyWake` — no new `ManagerStreamMessage` variant needed.

### API handlers — `backend/manager/api/v1/reminder.go` (implemented; mirrors `task.go`)

Per-RPC authz (as built):

| RPC | Rule |
|---|---|
| `ListReminders` | IAM `laelia.reminders.list`; non-admin users further restricted to reminders in conversations they are members of (unless `conversations.reviewAll`) |
| `GetReminder` | IAM `laelia.reminders.get` |
| `UpdateReminder` | IAM `laelia.reminders.update` + audit; full-replacement schedule edit (fire_at wins, else computed from cron) |
| `CancelReminder` | IAM `laelia.reminders.cancel` + audit; terminal statuses are a no-op |
| `ConvertMessageToReminder` | agent auth + conversation membership (`requireAgentMemberByConvID`); validates cron/tz via `schedule.Validate`, computes first fire via `resolveFireAt` |
| `CompleteReminder` / `FailReminder` | IAM `laelia.reminders.update` + `requireReminderOwner` (owning agent only — even an admin may not complete on the agent's behalf) |
| `ListDueReminders` | agent auth, self only |

All reminder handlers live on `CommandService` in `api/v1/reminder.go`; registered with the other command handlers in `backend/manager/server/grpc_routes.go` (no separate service). System lifecycle messages ("⏰ … scheduled", "📝 … updated", "🚫 … cancelled") are posted via `postReminderSystemMessage` and feed REMINDER activity via `GenerateActivityForMessage`.

### Agent side — claim, drain, complete

- **Prompt (load-bearing)**: `backend/agent/executor/prompt.go` (`AgentFirstPromptBody`) — as built, the drain turn is **batch-driven** (step 1 no longer begins with a stop-gating `message check`; the batch header carries each channel's address + processed_version). Due reminders are delivered as **step 0**: the turn-opening batch (built by `backend/agent/chattools/turn_batch.go` — `BuildTurnBatch`/`reminderSection`, which calls `ListDueReminders`) lists the agent's DUE reminders under a "Due reminders:" section before the message batch, and step 0 instructs the agent to do each one's work and report with `reminder complete <name> --result "..."` / `reminder fail <name> --error "..."` — and explicitly **NOT** to poll `reminder list-due` (the manager lists them only when it has fired them). The reminder commands are documented in `backend/agent/executor/prompt/communication.md` (command table + "Reminders" section: create+claim semantics, "do NOT compute `--fire-at` for a recurring reminder", full-replacement update, offline retry behavior, notification lines are never replied to).
- **Cobra subcommands**: `backend/agent/cmd/reminder.go`: `reminder convert <message-handle>`, `reminder list [<address>]`, `reminder list-due`, `reminder update <name>`, `reminder cancel <name>`, `reminder complete <name> --result`, `reminder fail <name> --error`.
- **Daemon handlers**: `backend/agent/daemon/server.go` — `/reminder/{convert,list,list-due,update,cancel,complete,fail}` routes with `handleReminder*` funcs; the shared `Request` envelope carries `FireAt`, `CronExpr`, `Tz` fields.
- **chattools wrappers**: `backend/agent/chattools/chattools_reminder.go` (Convert/List/ListDue/Complete/Fail/Update/Cancel + `formatReminderLine`, single `parseFireAtTime`) calling `commandServiceClient` via the Connect client; no central-registration change (chattools registration is generic).

## Frontend

- **Regen**: `cd proto && buf generate` → `frontend/src/types/proto-es/v1/`. RPCs live on `CommandService` → `commandServiceClient` (`src/connect/index.ts`), no new client.
- **Store**: as built there is **no** `src/stores/reminder.ts` slice — reminder reads/writes use TanStack Query hooks: `src/hooks/use-reminder-list.ts` (`useReminderPage`, 5s silent poll of the current page, `keepPreviousData`) and `src/hooks/use-reminder-detail.ts` (`useUpdateReminder`, `useCancelReminder` mutations). The reminder's discussion thread reuses the existing `thread.ts` slice (`openThread`/`sendThreadMessage`/`loadThreadMessages`) with `ThreadPanel`.
- **Routes**: `REMINDER_ROUTE_LIST`, `REMINDER_ROUTE_DETAIL` in `src/router/handles.ts`; `path: "reminders"` and `path: "reminders/:reminderId"` children under `agents/:agentId` in `src/router/routes/dashboard.tsx`.
- **Tab**: `src/app/layouts/agent-detail-layout.tsx` — reminders tab with the lucide `Bell` icon and `agent.tab-reminders` label.
- **Pages**:
  - `src/pages/dashboard/reminder-list.tsx` (mirrors `command-list.tsx`): status filter pills, table of reminders (task_content, schedule summary, status badge, next fire), row → detail, polled via `useReminderPage` (5s, silent).
  - `src/pages/dashboard/reminder-detail.tsx` (mirrors `command-detail.tsx`): task_content, human-readable schedule, assignee, status, retry history (`retry_count`, `last_attempt_at`, `last_fired_at`), result, the **discussion thread** (`ThreadPanel` + `openThread(reminder.conversation, reminder.message)` so the user can chat with the agent to modify the reminder — requirement #5), and inline action UI: an **Edit** `Sheet` and a **Cancel** `AlertDialog` (both defined in the page; there is no separate `src/components/reminder-edit-sheet.tsx`).
- **Edit Sheet** (requirement #4): trigger-mechanism toggle (one-shot datetime vs recurring cron), datetime picker for `fire_at`, cron-expr input + tz input, task_content `Textarea`; submits `useUpdateReminder`. Full-replacement semantics match `UpdateReminder`.
- **Status badge**: `src/lib/reminder-status.ts` + `src/components/reminder-status-badge.tsx` (mirror `lib/task-status.ts` / `task-status-badge.tsx`).
- **Locales**: `agent.tab-reminders` and a `reminders` section in `src/locales/{en-US,zh-CN}.json`.

## End-to-end flow

1. User posts "每天晚上3点分析github提交" in a channel where agent `A` is a member.
2. Existing wake path: `notifyConversationAgents` → `NotifyWake(A)` → `HandleBeginSession` (channel cursor behind) → session → LLM drains, reads the message via `laelia-machine message ...`.
3. LLM recognizes intent, calls `laelia-machine reminder convert <message-handle> --content "..." --cron "0 3 * * *" --tz Asia/Shanghai` (no `--fire-at` — the manager computes the first fire) → `ConvertMessageToReminder` → reminder row (assignee=A, PENDING), A subscribed to the thread; a "⏰ … scheduled a reminder" system message lands in the thread.
4. Scheduler due-scan hits `fire_at` → `MarkDue` → A connected → `ClearRetry` + `NotifyWake(A)` → `HandleBeginSession` (`HasDueReminders=true`) → turn batch opens with the "Due reminders:" section → prompt step 0 → LLM does the analysis → `reminder complete <name> --result "<report>"` → `CompleteReminderAndPostNotification` (one tx: COMPLETED + SYSTEM pill + agent result reply). Recurring → reset to PENDING at the next cron fire.
5. User sees the pill and the result in the channel thread **and** in the agent-page Reminders detail (same thread, no duplication). User can edit/cancel from the detail page, or chat in the thread ("改成4点") which wakes A to call `reminder update`.

## Verification

- `buf format -w proto && buf lint proto && cd proto && buf generate`.
- `gofmt -w` on changed Go files; `golangci-lint run --allow-parallel-runners` until clean; `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`.
- `go test ./backend/manager/store -run Reminder -v -count=1` (hermetic reminder store tests: convert+claim atomicity, complete-posts-once idempotency, reschedule, mark-missed retry overflow).
- `go test ./backend/manager/component/scheduler -count=1` (due scan, offline retry backoff, miss-after-5, recurring reschedule; injectable fake clock).
- `pnpm --dir frontend biome:check && pnpm --dir frontend lint --fix && pnpm --dir frontend type-check && pnpm --dir frontend test`.
- Manual E2E: start manager + an agent; in a channel post "每天凌晨3点总结今天的消息"; confirm the reminder appears in the agent-page Reminders tab with the correct next-fire; fast-forward `fire_at` (or set cron to `* * * * *` for a 1-min test) and confirm the agent wakes, runs, completes, posts a SYSTEM pill + one result message in the thread, and the reminder reschedules; toggle the agent offline before fire and confirm the 5-retry backoff then MISSED + reschedule; from the detail page edit `fire_at` and cancel; chat in the thread to modify.