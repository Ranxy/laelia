# Reminder: Scheduled/Recurring Agent Tasks

## Context

Today an agent only acts when a user posts in a channel — there is no way for an agent to run a task on a schedule ("analyze github commits every day at 3am"). The codebase has no scheduler/cron/reminder/periodic mechanism anywhere (proto, DB, backend, frontend). The closest analogue is the **`task`** feature: a `task` row whose PK is its root `chat_message` id, discussion happens in the thread rooted at that message, and it has a claim/status flow.

This feature adds **reminders**: an agent recognizes a scheduling intent in a channel message, "claims" it via a command (atomic create+claim, assignee = that agent), the manager stores it with `fire_at` / `cron_expr` / `tz`, a new manager-side scheduler fires it at the due time and wakes the agent, the agent runs the task in an LLM session, then calls `CompleteReminder` which **atomically** marks it done and writes a single completion message into the trigger message's thread — visible in both the channel thread and the agent-page Reminders tab, never duplicated.

### Confirmed decisions

- **Trigger modes**: one-shot (`fire_at`) + recurring (`cron_expr` + `tz`). After firing, recurring reminders compute the next `fire_at` from cron and reset to PENDING.
- **Claim model**: atomic create+claim — the agent that recognizes the intent claims it at creation; `assignee_agent_id = calling agent`.
- **Offline-at-fire**: retry 5× with backoff `5s,10s,20s,30s,60s`; if still offline, mark this fire MISSED (one-shot terminal; recurring reschedules to next cron fire) and log the attempts.
- **Completion de-dup**: single RPC `CompleteReminder`; backend writes the thread message in the same tx as the status update.

### Requirements coverage (5/5)

1. **Traceability** — reminder PK = trigger message id = thread root; `conversation_id` + `message` both on the row and in proto.
2. **Agent-page Reminders tab** — new tab + list/detail pages, mirroring the Commands tab.
3. **De-dup completion report** — single `CompleteReminder` RPC; ONE tx flips status to COMPLETED and writes a SYSTEM thread message (SYSTEM sender is excluded from the agent's channel cursor, so no self-wake, and shows once in both the channel thread and the reminder detail).
4. **Manual cancel/edit** — user-facing `CancelReminder`/`UpdateReminder` with an edit Sheet (trigger-mechanism toggle, datetime picker, cron + tz, task content) and a cancel AlertDialog.
5. **Modify via thread chat** — reminder detail embeds the existing `ThreadPanel`/`openThread`, so the user chats with the agent; the agent wakes and calls `reminder update`.

## Data model

Append to `backend/manager/migration/latest.sql` (+ guard assertions in `migration/migration_test.go`):

```sql
CREATE TABLE IF NOT EXISTS reminder (
  message_id          UUID PRIMARY KEY REFERENCES chat_message(id) ON DELETE CASCADE,
  conversation_id     UUID NOT NULL REFERENCES conversation(id) ON DELETE CASCADE,
  assignee_agent_id   UUID NOT NULL,            -- the claiming agent's id
  task_content        TEXT NOT NULL,            -- agent's structured summary of the work
  fire_at             TIMESTAMPTZ NOT NULL,     -- next fire (one-shot or computed from cron)
  cron_expr           TEXT NULL,                -- NULL = one-shot
  tz                  TEXT NOT NULL DEFAULT 'UTC',
  status              INT  NOT NULL DEFAULT 1,  -- 1 PENDING,2 DUE,3 COMPLETED,4 CANCELLED,5 MISSED,6 FAILED
  retry_count         INT  NOT NULL DEFAULT 0,
  next_retry_at       TIMESTAMPTZ NULL,
  last_attempt_at     TIMESTAMPTZ NULL,
  last_fired_at        TIMESTAMPTZ NULL,
  last_completed_at   TIMESTAMPTZ NULL,
  result              TEXT NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reminder_assignee_status ON reminder(assignee_agent_id, status);
CREATE INDEX IF NOT EXISTS idx_reminder_fire_at ON reminder(fire_at) WHERE status = 1;   -- PENDING due scan
CREATE INDEX IF NOT EXISTS idx_reminder_retry   ON reminder(next_retry_at) WHERE status = 2; -- DUE retry scan
```

Mirror the `task` table's 1:1-with-message shape: PK `message_id` = the trigger message = the thread root. No per-conversation numbering (skip `next_reminder_number` — the reminder's identity is its trigger message). `ConvertMessageToReminder` does **not** bump `conversation.version` (the trigger message already has one), exactly mirroring `store.ConvertMessageToTask`.

## Proto

Add to `proto/v1/v1/command.proto` (mirror `TaskStatus`/`TaskInfo` at lines 40-66 and the task RPCs at 929-989):

- `enum ReminderStatus { REMINDER_PENDING=1; REMINDER_DUE=2; REMINDER_COMPLETED=3; REMINDER_CANCELLED=4; REMINDER_MISSED=5; REMINDER_FAILED=6; }` (AIP: bare names, no `REMINDER_STATUS_` prefix).
- `message Reminder { string name; string conversation; string message; string assignee_agent; string task_content; google.protobuf.Timestamp fire_at; string cron_expr; string tz; ReminderStatus status; int32 retry_count; google.protobuf.Timestamp next_retry_at/last_attempt_at/last_fired_at/last_completed_at/created_at/updated_at; string result; }` — `name = "reminders/{message_id}"`.
- RPCs on `CommandService` (Connect-JSON; agent CLI + frontend both call these):
  - `ConvertMessageToReminder(message, task_content, fire_at, cron_expr, tz) -> Reminder` — agent create+claim.
  - `ListReminders(agent, conversation, status, page_token, page_size) -> (reminders[], next_page_token)` — agent-page tab (user) + agent self-list.
  - `GetReminder(name) -> Reminder`.
  - `UpdateReminder(name, fire_at, cron_expr, tz, task_content) -> Reminder` — user manual edit or agent edit-from-thread.
  - `CancelReminder(name) -> Reminder`.
  - `CompleteReminder(name, result) -> Reminder` — agent success; tx posts SYSTEM thread message.
  - `FailReminder(name, error) -> Reminder` — agent failure; recurring reschedules, posts SYSTEM thread message.
  - `ListDueReminders() -> (reminders[])` — agent drain: DUE reminders for the calling agent.

Regenerate with `cd proto && buf generate` (writes `frontend/src/types/proto-es/v1/`).

## Backend

### Store — `backend/manager/store/reminder.go` (new; mirror `task.go`)

- `ConvertMessageToReminderTx` — mirror `ConvertMessageToTask` (`store/task.go:136-179`): one tx, `EXISTS` guard → `ErrReminderAlreadyExists`, unique PK on `message_id`; but set `assignee_agent_id = $` in the INSERT (atomic claim, mirroring `ClaimTask`'s race-free `WHERE assignee IS NULL` intent folded into `INSERT ... ON CONFLICT DO NOTHING` + conditional UPDATE).
- `ListReminders`, `GetReminder`, `UpdateReminderFields`, `CancelReminder`.
- `HasDueReminders(agentID) -> bool` — cheap `EXISTS WHERE assignee=$1 AND status=DUE` (used by `HandleBeginSession`).
- `ListDueReminders(agentID)`, `ListDuePending(now)` (scheduler scan), `ListDueRetrying(now)` (scheduler retry scan).
- `MarkDue(reminderID, firedAt)` — PENDING→DUE, set `last_fired_at`, reset `retry_count=0`.
- `SetRetry(reminderID, nextRetryAt, retryCount, lastAttemptAt)` — DUE, offline path.
- **`CompleteReminderAndPostNotification(reminderID, convID, triggerMsgID, result, success bool) -> ONE tx`** (do NOT compose `UpdateReminder` + `CreateChatMessageBumpVersion`):
  - `UPDATE reminder SET status=COMPLETED/FAILED, last_completed_at=now(), result=$ WHERE status<>COMPLETED` (idempotency guard; rows-affected detects duplicates → return existing, no second message).
  - bump `conversation.version`.
  - insert a `chat_message` with `sender_type=3 (SYSTEM)`, `thread_root_message_id=triggerMsgID`, `content=result`.
  - Mirror the tx structure of `CreateTaskMessageBumpVersion` (`store/task.go:57-114`) and the SYSTEM-notification pattern of `postTaskSystemNotification` (`api/v1/task.go:287-301`).
  - **Why SYSTEM sender**: `agentRelevantMessageCondition` (`store/agent_channel_cursor.go:45-49`) excludes `sender_type=3`, so `HasUpdates` ignores it — the completion message bumps room version (visible to the user poller and in `ListThreadMessages`, which has no sender filter) but does **not** count against the agent's channel cursor, does not self-wake the agent, and needs no ack. This is the single message that shows in both the channel thread view and the reminder-detail thread view — no duplication.
- `RescheduleRecurring(reminderID)` — compute next `fire_at` from `cron_expr`/`tz`, set status=PENDING, clear retry fields. Called in the same tx as Complete/Fail/Miss when `cron_expr` is non-null.
- `MarkMissed(reminderID)` — one-shot terminal MISSED; recurring → `RescheduleRecurring` + post a SYSTEM "missed after N retries" thread message.

### Scheduler — `backend/manager/component/scheduler/scheduler.go` (new)

Long-lived goroutine, lifecycle mirroring `dispatcher.go:86-88` / `799-802` (`lifecycleCtx`/`lifecycleCancel`/`wg`).

Two tick loops (1s interval), each `select`-ing on `lifecycleCtx.Done()`:

1. **Due scan**: `store.ListDuePending(now)` → for each PENDING reminder with `fire_at <= now`: `MarkDue`; if `dispatcher.IsAgentConnected(agentID)` (`dispatcher.go:182-187`) → `dispatcher.NotifyWake(agentID)`; else `SetRetry(now+5s, retry_count=1)`.
2. **Retry scan**: `store.ListDueRetrying(now)` → for each DUE reminder with `next_retry_at <= now`: if connected → `NotifyWake` + clear `next_retry_at`; else advance retry with backoff `[5s,10s,20s,30s,60s]`; if `retry_count > 5` → `MarkMissed` (one-shot terminal / recurring reschedule) + post SYSTEM thread message.

On startup: load all PENDING reminders into the due-scan window (no in-memory timer heap needed — the 1s `ListDuePending` scan over the partial index is sufficient at this scale; simpler and crash-safe, no timer-rebuild logic).

Cron/timezone: add `github.com/robfig/cron/v3` to `go.mod`. Parse with `cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)`; `sched.Next(time.Now().In(loc))` where `loc = time.LoadLocation(tz)`. Validate `tz` at create time (`ConvertMessageToReminder` / `UpdateReminder`) — reject empty/unloadable.

### Server wiring

- Construct: `backend/manager/server/server.go:102` after `s.dispatcher = dispatcher.New(stores)` → `s.scheduler = scheduler.New(stores, s.dispatcher)`; add `scheduler *scheduler.Scheduler` field near `dispatcher` (`server.go:51`).
- Start: `backend/manager/server/server.go:Run` (~line 161-164, next to the heartbeat-buffer start) → `s.scheduler.Start()`.
- Stop: `backend/manager/server/server.go:215-217` **before** `s.dispatcher.Stop()` and before `s.store.Close()` → `s.scheduler.Stop()`.

### Dispatcher integration — `backend/manager/component/dispatcher/dispatcher.go`

`HandleBeginSession` (~line 275-276): OR-fold due reminders into the work gate so a due reminder drives a session:

```go
hasUpdates, _ := d.store.HasUpdates(ctx, agentID)
hasReminders, _ := d.store.HasDueReminders(ctx, agentID)
if !hasUpdates && !hasReminders { return idle }
```

Wake path reuses `NotifyWake` (`dispatcher.go:358-374`) — no new `ManagerStreamMessage` variant needed.

### API handlers — `backend/manager/api/v1/reminder.go` (new; mirror `task.go`)

Per-RPC authz:

| RPC | Rule | Mirror |
|---|---|---|
| `ListReminders` | IAM route gate only (workspace role) | `ListCommands` (`command.go:38-90`) |
| `GetReminder` | owning agent OR admin OR conversation member | `requireCommandAccess` (`authz_helper.go:98-130`) |
| `UpdateReminder` / `CancelReminder` | owning agent (`assignee==caller`) OR admin | owning-agent branch of `requireCommandAccess` |
| `ConvertMessageToReminder` | agent auth + conversation membership | `requireAgentMemberByConvID` (`task.go:42-55`) |
| `CompleteReminder` / `FailReminder` | owning agent only | `ClaimTask`/`UpdateTaskStatus` (`task.go:177-207`) |
| `ListDueReminders` | agent auth, self only | `ListChannelUpdates` (`command.go:958`) |

Register routes in `backend/manager/server/grpc_routes.go` (~line 126-147) alongside the task handlers; add `laelia.v1.permission` annotations for the IAM interceptor.

### Agent side — claim, drain, complete

- **Prompt (load-bearing)**: `backend/agent/executor/prompt.go:46-79` (`AgentFirstPromptBody`) — the current step 1 runs `laelia-agent message check` and **stops if empty**. Add a step **before** that stop-gate: run `laelia-agent reminder list-due`; if non-empty, process each (do the work, then `reminder complete <id> "<result>"` or `reminder fail`), then proceed to `message check`. Without this, due reminders are never picked up. Also document the reminder commands in `backend/agent/executor/prompt/communication.md`.
- **Cobra subcommands**: `backend/agent/cmd/reminder.go` (mirror `task.go:9-39`): `reminder convert <message>`, `reminder list`, `reminder list-due`, `reminder update <name>`, `reminder cancel <name>`, `reminder complete <name> <result>`, `reminder fail <name> <error>`.
- **Daemon handlers**: `backend/agent/daemon/server.go` — register `/reminder/*` mux routes at ~line 165 (mirror `/task/*` at 160-164); add `handleReminder*` funcs mirroring `handleTask*` (`server.go:394-441`); extend the `Request` envelope (`server.go:191-225`) with `FireAt`, `CronExpr`, `Tz` string fields.
- **chattools wrappers**: `backend/agent/chattools/chattools_reminder.go` (mirror `chattools_task.go:13-226`) calling `commandServiceClient.*` via the Connect client; no central-registration change (`chattools.go:29-33` is generic).

## Frontend

- **Regen**: `cd proto && buf generate` → `frontend/src/types/proto-es/v1/`. RPCs go on `CommandService` → reuse `commandServiceClient` (`src/connect/index.ts`), no new client.
- **Store**: `src/stores/reminder.ts` (mirror `command.ts`) + `ReminderSlice` in `src/stores/types.ts` + compose `createReminderSlice` in `src/stores/index.ts`. Actions: `listReminders(agentId, status?)`, `getReminder(name)`, `cancelReminder`, `updateReminder`; reuse `openThread`/`sendThreadMessage`/`loadThreadMessages` from the existing `thread.ts` slice for the reminder's discussion thread (`ThreadPanel`).
- **Routes**: add `REMINDER_ROUTE_LIST`, `REMINDER_ROUTE_DETAIL` in `src/router/handles.ts`; add `path: "reminders"` and `path: "reminders/:reminderId"` children under `agents/:agentId` in `src/router/routes/dashboard.tsx` (lines 28-66).
- **Tab**: `src/app/layouts/agent-detail-layout.tsx` — add `TabKey = "reminders"`, a `TabsTrigger` (lucide `Bell` or `Clock`), `navigate(resolvePath(REMINDER_ROUTE_LIST, { agentId }))`.
- **Pages**:
  - `src/pages/dashboard/reminder-list.tsx` (mirror `command-list.tsx`): status filter pills, table of reminders (task_content, schedule summary, status badge, next fire), row → detail. Poll `listReminders` (2s, like the channel watcher) so due/complete transitions update live.
  - `src/pages/dashboard/reminder-detail.tsx` (mirror `command-detail.tsx`): shows task_content, human-readable schedule (`fire_at`/cron in viewer's tz via `Intl.DateTimeFormat` + parsed cron description), assignee, status, retry history (`retry_count`, `last_attempt_at`, `last_fired_at`), result, the **discussion thread** (reuse `ThreadPanel` + `openThread(reminder.conversation, reminder.message)` so the user can chat with the agent to modify the reminder — requirement #5), and action buttons: **Edit** (`Sheet` form) and **Cancel** (`AlertDialog` confirm).
- **Edit Sheet** (requirement #4): `src/components/reminder-edit-sheet.tsx` mirroring `NewTaskSheet` in `command-list.tsx`: trigger-mechanism toggle (one-shot datetime vs recurring cron), datetime picker for `fire_at`, cron-expr input + tz `<Select>` (common IANA zones), task_content `<Textarea>`. Submits `updateReminder`. Live cron-validation hint (parse client-side to preview the next 2-3 fire times).
- **Status badge**: `src/lib/reminder-status.ts` + `src/components/reminder-status-badge.tsx` (mirror `lib/task-status.ts` / `task-status-badge.tsx`).
- **Locales**: add `agent.tab-reminders` and a `reminders` section to `src/locales/{en-US,zh-CN}.json`.

## End-to-end flow

1. User posts "每天晚上3点分析github提交" in a channel where agent `A` is a member.
2. Existing wake path: `notifyConversationAgents` → `NotifyWake(A)` → `HandleBeginSession` (channel cursor behind) → session → LLM drains, reads the message via `laelia-agent message ...`.
3. LLM recognizes intent, calls `laelia-agent reminder convert <message> --task-content "..." --cron "0 3 * * *" --tz Asia/Shanghai` → `ConvertMessageToReminder` → reminder row (assignee=A, PENDING, fire_at=next 3am), A subscribed to the thread. Agent may post a confirmation in the thread.
4. Scheduler due-scan hits `fire_at` → `MarkDue` → A connected → `NotifyWake(A)` → `HandleBeginSession` (`HasDueReminders=true`) → session → prompt step runs `reminder list-due` → LLM does the analysis → `reminder complete <name> "<result>"` → `CompleteReminderAndPostNotification` (one tx: COMPLETED + SYSTEM thread message). Recurring → `RescheduleRecurring` → PENDING at next 3am.
5. User sees the completion in the channel thread **and** in the agent-page Reminders detail (same thread, one message — no duplication). User can edit/cancel from the detail page, or chat in the thread ("改成4点") which wakes A to call `reminder update`.

## Verification

- `buf format -w proto && buf lint proto && cd proto && buf generate`.
- `gofmt -w` on changed Go files; `golangci-lint run --allow-parallel-runners` until clean; `go build -ldflags "-w -s" -p=16 -o ./build/laelia ./backend/manager/bin/server/main.go`.
- `go test ./backend/manager/store -run Reminder -v -count=1` (new reminder store tests: convert+claim atomicity, complete-posts-once idempotency, reschedule, mark-missed retry overflow).
- `go test ./backend/manager/component/scheduler -count=1` (due scan, offline retry backoff, miss-after-5, recurring reschedule; inject a fake clock).
- `pnpm --dir frontend biome:check && pnpm --dir frontend lint --fix && pnpm --dir frontend type-check && pnpm --dir frontend test`.
- Manual E2E: start manager + an agent; in a channel post "每天凌晨3点总结今天的消息"; confirm the reminder appears in the agent-page Reminders tab with the correct next-fire; fast-forward `fire_at` (or set cron to `* * * * *` for a 1-min test) and confirm the agent wakes, runs, completes, posts one SYSTEM message in the thread, and the reminder reschedules; toggle the agent offline before fire and confirm the 5-retry backoff then MISSED + reschedule; from the detail page edit `fire_at` and cancel; chat in the thread to modify.