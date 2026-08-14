# Command Detail "Output & Events" Redesign — Trajectory-Style Ledger

## 1. Background & Goal

We want to bring the visual/UX quality of deepseek-harness's **Trajectory** view
into our command detail **Output & Events** tab. We are **not** building a full
turn-aware trajectory; we are borrowing its *front-end design language*:

- dense, table-like event ledger with colored **kind tags**;
- sticky headers, group headers, and left **rails** for selection/turn context;
- a **local inspector** (right-side details panel) instead of inline expansion;
- a compact **toolbar** (search / filter / fold);
- an optional Chrome-Network-style **overview timeline**;
- consistent hover/selection/error states and monospace-friendly typography.

While doing this we also audit the event model and propose supplements for
events that are currently missing or under-represented.

---

## 2. Current Implementation (as-is)

### 2.1 Page structure

`frontend/src/pages/dashboard/command-detail.tsx`

- Two tabs: `run` (**Output & Events**) and `summary` (**Final Summary**).
- The `run` tab is a two-column grid:
  - **Left**: `CommandTimeline` — terminal stdout/stderr/system output
    interleaved with `ChatToolCall` cards, ordered by timestamp.
  - **Right**: an events panel — a vertical list of:
    - `EventRow` (label + `#seqNo` + summary + expand button + time),
    - `ToolEventRow` (paired tool-call card),
    - `ContextUsageBar` (live progress bar),
    - `TokenUsageCard` (only on summary tab).
- `TEXT_DELTA` events are filtered out of the events panel (they feed the
  terminal output instead).
- `CONTEXT_USAGE_UPDATE` and `TOKEN_USAGE` are collapsed into one live bar /
  one summary card rather than one row per update.

### 2.2 Event model

`proto/v1/v1/command.proto` defines `CommandEventType`:

| Value | Meaning | Rendered today? |
|---|---|---|
| `LIFECYCLE` | command start / executor info | yes (row) |
| `TEXT_DELTA` | streaming text | no (feeds output) |
| `TOOL_CALL_STARTED` | tool invocation | yes (card) |
| `TOOL_CALL_FINISHED` | tool result | yes (card) |
| `DIFF_EMITTED` | file diff | yes (row, expandable) |
| `WARNING` | warning | yes (row) |
| `RAW_ACP` | raw ACP frame | yes (row, expandable) |
| `FINAL_SUMMARY` | terminal summary | yes (row + summary tab) |
| `PERMISSION_REQUESTED` | deprecated | no |
| `PERMISSION_TIMED_OUT` | deprecated | no |
| `PERMISSION_DECIDED` | deprecated | no |
| `CONTEXT_COMPACTION_STARTED` | compaction begin | yes (row) |
| `CONTEXT_COMPACTION_FINISHED` | compaction end | yes (row) |
| `CONTEXT_USAGE_UPDATE` | context window snapshot | yes (bar) |
| `TOKEN_USAGE` | per-command token delta | yes (summary card) |

### 2.3 Pain points of the current UI

1. **Events panel is a plain list of boxes** — no visual hierarchy, no grouping,
   no density; long commands become a wall of identical bordered rows.
2. **Inline expansion** — raw payloads/diffs expand inside the row, pushing
   content around; there is no persistent "details" surface.
3. **No search / filter / fold** — a long command with hundreds of tool calls
   is hard to navigate.
4. **No timing overview** — you cannot see at a glance where the time went
   (tool calls vs. thinking vs. output).
5. **Kind labels are plain text** — no color/icon coding, so scanning is slow.
6. **Tool call cards are duplicated** between the left timeline and the right
   events panel, but the two panels are not visually unified.

---

## 3. deepseek-harness Trajectory Design Language (reference)

Source: `packages/client/ui-trajectory` in `/home/ran/project/deepseek-harness`.

### 3.1 Ledger table

- A fixed two-column table: **event** (kind tag + rails) and **content**
  (text + result preview).
- 30px rows, subtle `border-l1` row separators, sticky header.
- Hover background, selected background, focus ring.
- Left **turn rail** (2px accent) and **selection rail** (3px brand) mark
  context and selection.
- **Kind tags**: small colored pills with icon + label
  (`SYSTEM`, `USER`, `CONTEXT`, `COMPACTED`, `ASSISTANT`, `TOOL`, `SUBTOOL`),
  each with a distinct semantic color (neutral / success / warn / brand).
- **Content cell**: request text + `→` result preview; tool rows use a
  monospace font; error rows tint red.

### 3.2 Headers & grouping

- **Sticky turn header** (`Turn N`) with metric column labels
  (`Input / Output / Think / Time`).
- **Group headers** (`Message`, `Step N`) with optional description.
- **Collapsible turns / assistants** with summary ellipsis rows.

### 3.3 Toolbar

- Sticky toolbar with:
  - duration toggle (actual vs equal-width),
  - fold-all turns / fold-all calls,
  - live search box.

### 3.4 Overview timeline

- Chrome-Network-style horizontal lanes: `Input / Model / Tools`.
- Spans represent real start/duration; assistant spans split TTFT vs decoding.
- Drag to select a time range, wheel to zoom, click to focus a record.

### 3.5 Local inspector

- Selecting a row opens a **right-side details panel** (resizable).
- Header shows kind tag + location.
- Tabs: `Summary`, `Options`, `Usage`, `Timing`, `Input`, `Output`, `Diff`,
  `Raw`, `Source`, `Schema`, `System Prompt`, `Tools`.
- Each tab renders a focused payload (JSON tree, markdown, diff, usage rows,
  timing rows).

### 3.6 What we should borrow (and what we should not)

**Borrow:**
- dense ledger + kind tags + rails,
- sticky/group headers,
- toolbar (search/filter/fold),
- local inspector with tabs,
- overview timeline (simplified),
- consistent hover/selection/error styling.

**Do not borrow (out of scope):**
- full turn/assistant/request model,
- virtualization (our command event volume is much smaller; can add later),
- deep hierarchy navigation (subtool nesting),
- resizable split with persisted widths (nice-to-have, not required).

---

## 4. Proposed Design for "Output & Events"

### 4.1 Overall layout

```
┌──────────────────────────────────────────────────────────────────┐
│ CommandEventToolbar                                              │
│  [search] [filter: All/Tools/Diffs/Warnings] [fold all]           │
├──────────────────────────────────────────────────────────────────┤
│ CommandEventTimelineOverview (optional, collapsible)             │
│  lanes: Output / Tools / System                                  │
├──────────────────────────────────────────────────────────────────┤
│ CommandEventLedger (table)                    │ CommandEvent    │
│  ┌──────┬──────────┬────────────────────────┐ │ Inspector       │
│  │ #    │ Kind     │ Content      Time      │ │ (right side)    │
│  ├──────┼──────────┼────────────────────────┤ │                 │
│  │ 1    │ LIFECYCLE│ started      12:00:01  │ │ tabs:           │
│  │ 2    │ TOOL     │ read_file → ok 12:00:02│ │  Summary        │
│  │ 3    │ DIFF     │ src/a.ts     12:00:03  │ │  Input/Output   │
│  │ 4    │ WARNING  │ ...          12:00:04  │ │  Diff/Raw       │
│  └──────┴──────────┴────────────────────────┘ │  Usage/Timing   │
└──────────────────────────────────────────────────────────────────┘
```

The left `CommandTimeline` (terminal output) stays as the primary "raw output"
view; the new ledger replaces the current right-side events panel. The two are
kept in sync via the existing `selectedToolSeq` mechanism (click a tool card in
the ledger → scroll the timeline to it, and vice versa).

### 4.2 New components

#### 4.2.1 `CommandEventLedger` (new)

`frontend/src/components/command-events/command-event-ledger.tsx`

- Props: `events`, `outputs?`, `selectedSeqNo`, `onSelect`, `searchQuery`,
  `filter`, `collapsedGroups`.
- Builds rows from `visibleEvents`:
  - pair `TOOL_CALL_STARTED`/`FINISHED` (reuse `pairToolCallEvents`),
  - group consecutive events into **phases** (e.g. `Lifecycle`, `Tool Calls`,
    `Diffs`, `Warnings`, `Compaction`, `Summary`) — a lightweight analogue of
    Trajectory's turn/group headers,
  - render each row as:
    - `#seqNo` (tertiary, tabular-nums),
    - **kind tag** (colored pill + icon),
    - content (summary + `→` result preview for tool calls),
    - time (right-aligned),
    - optional metric columns (e.g. tool duration, diff line counts).
- Selection: clicking a row sets `selectedSeqNo`; a 3px **selection rail** on
  the left; keyboard accessible (`role="button"`, Enter/Space).
- Collapsible groups: clicking a group header folds its rows into a single
  `… N events` summary row.
- Empty state: reuse the existing "Waiting for structured events..." message.

#### 4.2.2 `CommandEventInspector` (new)

`frontend/src/components/command-events/command-event-inspector.tsx`

- Props: `event`, `startedEvent?`, `finishedEvent?`, `onClose`.
- Right-side panel with a header (kind tag + `#seqNo` + time) and tabs.
- Tab set depends on event type:
  - **Tool call**: `Summary`, `Input`, `Output`, `Raw`.
  - **Diff**: `Diff` (reuse `ChatDiff`), `Raw`.
  - **Warning**: `Summary`, `Raw`.
  - **Raw ACP**: `Raw` (JSON tree).
  - **Compaction**: `Summary`, `Raw`.
  - **Context usage**: `Usage` (reuse `ContextUsageBar`).
  - **Token usage**: `Usage` (reuse `TokenUsageCard`).
  - **Lifecycle / Final summary**: `Summary`, `Raw`.
- Reuses existing `ChatToolCall`, `ChatDiff`, `ChatWarning`,
  `ContextUsageBar`, `TokenUsageCard` so chat and command detail stay
  consistent.

#### 4.2.3 `CommandEventToolbar` (new)

`frontend/src/components/command-events/command-event-toolbar.tsx`

- Search input (filters by summary / tool title / path / message).
- Filter dropdown: `All`, `Tools`, `Diffs`, `Warnings`, `Compaction`,
  `System`.
- Fold-all toggle.
- (Optional) "Actual duration" toggle for the overview timeline.

#### 4.2.4 `CommandEventTimelineOverview` (new, optional)

`frontend/src/components/command-events/command-event-timeline-overview.tsx`

- Simplified Chrome-Network-style overview:
  - lanes: `Output`, `Tools`, `System`,
  - spans from event timestamps (tool call duration = started→finished,
    output = chunk timestamps, compaction = started→finished),
  - click a span to focus the ledger row,
  - no drag/zoom in v1 (can add later).

#### 4.2.5 Modify `command-detail.tsx`

- Replace the right-side events panel with:
  ```
  <CommandEventToolbar ... />
  <CommandEventTimelineOverview ... />   (optional)
  <div class="flex-1 flex min-h-0">
    <CommandEventLedger ... />
    {selected && <CommandEventInspector ... />}
  </div>
  ```
- Keep `CommandTimeline` on the left; wire `onSelect` to
  `toggleToolSelect` so ledger ↔ timeline stay in sync.
- Keep `ContextUsageBar` as a pinned summary row above the ledger (or inside
  the inspector's Usage tab).

### 4.3 Styling approach

- Use existing Tailwind tokens (`control`, `control-light`, `control-border`,
  `info`, `success`, `warning`, `error`, `accent`, `dark-bg`, `matrix-green`).
- Add a small CSS module or Tailwind utilities for:
  - kind-tag colors per event type,
  - selection/turn rails,
  - dense table rows (h-7 / h-8),
  - sticky headers,
  - inspector panel.
- Keep the terminal's `dark-bg` + `matrix-green` identity on the left; the
  ledger can use the light `background`/`control-bg` palette to match the rest
  of the dashboard, or stay dark to match the terminal — recommend matching
  the existing dashboard light theme for the ledger and keeping the terminal
  dark as a deliberate contrast.

### 4.4 Kind-tag mapping (proposal)

| Event type | Tag label | Color (Tailwind) | Icon |
|---|---|---|---|
| `LIFECYCLE` | Lifecycle | `control` / neutral | `Play` |
| `TOOL_CALL_STARTED` | Tool | `warning` | `Wrench` |
| `TOOL_CALL_FINISHED` | Tool | `success` (or `error` on failure) | `Wrench` |
| `DIFF_EMITTED` | Diff | `accent` | `FileDiff` |
| `WARNING` | Warning | `warning` | `AlertTriangle` |
| `RAW_ACP` | Raw ACP | `control-light` | `Braces` |
| `FINAL_SUMMARY` | Summary | `success` | `CheckCircle` |
| `CONTEXT_COMPACTION_*` | Compaction | `warning` | `Compress` |
| `CONTEXT_USAGE_UPDATE` | Context | `info` | `Gauge` |
| `TOKEN_USAGE` | Tokens | `info` | `Coins` |
| `PERMISSION_*` (if re-added) | Permission | `info` | `Shield` |
| `STEER` (new) | Steer | `accent` | `Send` |
| `RETRY_*` (new) | Retry | `warning` | `RotateCcw` |

---

## 5. Missing / Under-represented Events & Supplements

### 5.1 Currently defined but not rendered

- `PERMISSION_REQUESTED` / `PERMISSION_TIMED_OUT` / `PERMISSION_DECIDED`
  (enum values 9–11) exist in the proto but have **no payloads** (reserved
  18–20) and are not emitted by the current runtime (permissions are
  auto-granted). The frontend `commandEventTypeToI18nKey` also lacks keys for
  them.
  - **Recommendation**: keep them reserved; if permission flows are re-enabled
    later, add payloads + i18n + kind tags. Do not render them now.

### 5.2 Missing events worth adding

1. **`STEER` / `USER_INTERVENTION`**
   - Today `steerCommand` injects a message but emits **no event**, so the
     Output & Events view gives no trace that the user steered mid-run.
   - **Proposal**: add `CommandEventType_STEER = 16` with a
     `SteerPayload { string text }`; emit from the executor when `Steer()` is
     accepted. Render as a `Steer` kind tag in the ledger + a marker in the
     timeline.
   - Backend touch points: `backend/agent/pi/executor.go` (emit in the
     `steerCh` drain case), `backend/manager/component/dispatcher/dispatcher.go`
     (pass-through), `proto/v1/v1/command.proto`, regenerate Go/TS.

2. **`RETRY_STARTED` / `RETRY_FINISHED`**
   - Auto-retry currently only produces a `WARNING` string
     (`pi agent will retry: ...`). A structured pair would let the ledger show
     a retry group and the timeline show a retry span.
   - **Proposal**: add `RETRY_STARTED = 17` / `RETRY_FINISHED = 18` with
     `RetryPayload { string reason; int32 attempt; }`. Emit from
     `handleEvent` on `eventAutoRetryStart` / `eventAutoRetryEnd`.

3. **`AGENT_START` / `AGENT_END`**
   - The executor currently ignores `eventAgentStart` and only uses
     `eventAgentSettled` to end the turn. A structured pair would make the
     ledger show "agent thinking" phases and let the overview timeline show
     model time.
   - **Proposal**: add `AGENT_START = 19` / `AGENT_END = 20` with
     `AgentPayload { string reason; }` (or reuse `LifecyclePayload`).

4. **Tool-call error detail**
   - `TOOL_CALL_FINISHED` already carries `status: "error"`; the UI should
     render an error state (red tag, red result preview) even without a new
     event type. This is a front-end-only improvement.

5. **Diff line counts / stats**
   - `DIFF_EMITTED` has `old_text`/`new_text`; the ledger can show
     `+N/-M` in the content cell and the inspector can show a unified diff.
     Front-end only.

### 5.3 Priority

- **P0 (front-end only)**: redesign the ledger/inspector/toolbar; render
  tool-call error state; diff stats.
- **P1 (backend + front-end)**: add `STEER` event so user interventions are
  visible.
- **P2 (backend + front-end)**: add `RETRY_*` and `AGENT_*` structured events
  for richer timeline/grouping.

---

## 6. Implementation Plan

### Phase 1 — Data & types (front-end only)

1. Add `commandEventTypeToKind` mapping + i18n keys for any new labels.
2. Add helper `commandEventKindLabel`, `commandEventKindColor`,
   `commandEventKindIcon` in `frontend/src/lib/command-status.ts` (or a new
   `command-events.ts`).
3. Add `formatEventDuration` (started→finished) helper.

### Phase 2 — Ledger component

1. Create `frontend/src/components/command-events/command-event-ledger.tsx`.
2. Build row model from `visibleEvents` + `pairToolCallEvents`.
3. Implement kind tags, rails, group headers, collapse, selection.
4. Add CSS (Tailwind utilities or a module).

### Phase 3 — Inspector component

1. Create `frontend/src/components/command-events/command-event-inspector.tsx`.
2. Implement tabs per event type, reusing `ChatToolCall`, `ChatDiff`,
   `ChatWarning`, `ContextUsageBar`, `TokenUsageCard`.
3. Add close button + keyboard support.

### Phase 4 — Toolbar & overview

1. Create `frontend/src/components/command-events/command-event-toolbar.tsx`.
2. Create `frontend/src/components/command-events/command-event-timeline-overview.tsx`
   (optional; can be a follow-up).

### Phase 5 — Integrate into command-detail

1. Replace the right-side events panel in `command-detail.tsx`.
2. Wire selection ↔ `CommandTimeline` scroll/highlight.
3. Keep `ContextUsageBar` pinned above the ledger (or in inspector).

### Phase 6 — Backend supplements (P1/P2)

1. Update `proto/v1/v1/command.proto` with new event types + payloads.
2. Regenerate Go/TS (`buf generate`).
3. Emit new events in `backend/agent/pi/executor.go`.
4. Pass through in `backend/manager/component/dispatcher/dispatcher.go`.
5. Add i18n keys + kind tags in frontend.

### Phase 7 — Tests

1. Update `frontend/src/pages/dashboard/command-detail.test.tsx` for the new
   ledger/inspector.
2. Add unit tests for `CommandEventLedger` (grouping, pairing, selection,
   collapse, search/filter).
3. Add unit tests for `CommandEventInspector` (tab selection per event type).
4. Add backend tests for new event emission (if P1/P2 implemented).

---

## 7. Risks & Considerations

- **Scope creep**: resist building full virtualization / drag-zoom timeline in
  v1; keep the overview simple.
- **Two panels duplication**: the left terminal + right ledger both show tool
  calls. Keep them visually distinct (terminal = raw stream, ledger =
  structured summary) and rely on selection sync.
- **Event volume**: `CONTEXT_USAGE_UPDATE` can be high-frequency; keep it as a
  pinned bar / inspector tab, not one row per update (current behavior).
- **Backward compatibility**: adding proto enum values is additive; old servers
  won't send them, new frontend must handle unknown types gracefully (fallback
  to `event-unknown`).
- **i18n**: add all new keys to both `en-US.json` and `zh-CN.json`.

---

## 8. Summary

The redesign replaces the current flat events panel with a Trajectory-inspired
**dense ledger + local inspector + toolbar (+ optional overview timeline)**,
while keeping the existing terminal output and tool-call components. It also
identifies concrete event-model gaps — most notably the absence of a `STEER`
event — and proposes additive supplements with clear priorities. The work is
front-end-first (P0) with optional backend additions (P1/P2).

---

## 9. Implementation Status (2026-08-14)

### Done (P0 — front-end redesign)

- Added `frontend/src/components/command-events/`:
  - `command-event-kind.ts` — kind-tag mapping (label/color/icon/phase) for all
    current event types plus placeholders for future `STEER`/`RETRY` types.
  - `command-event-ledger.tsx` — dense table ledger with kind tags, phase
    group headers, collapse, search/filter, selection rail, tool-pair merging.
  - `command-event-inspector.tsx` — right-side details panel with per-type
    tabs (Summary/Input/Output/Diff/Raw/Usage), reusing existing
    `ChatToolCall`/`ChatDiff`/`ChatWarning`/`ContextUsageBar`/`TokenUsageCard`.
  - `command-event-toolbar.tsx` — search, filter, collapse-all.
  - `command-event-timeline-overview.tsx` — simplified Chrome-Network-style
    overview (Output/Tools/System lanes).
- Rewrote `frontend/src/pages/dashboard/command-detail.tsx` to integrate the
  new ledger + inspector + toolbar + overview into the **Output & Events** tab,
  keeping the left terminal `CommandTimeline` and the existing selection sync.
- Added i18n keys (en-US + zh-CN) and updated the i18n checker's
  `DYNAMIC_PREFIXES` for `command.filter-*` / `command.phase-*`.
- Added unit tests:
  - `command-event-ledger.test.tsx` (6 tests)
  - `command-event-inspector.test.tsx` (3 tests)
  - updated `command-detail.test.tsx` for the new inspector flow.
- `tsc --noEmit`, `npm run check`, and the full Vitest suite (483 tests) pass.

### Deferred (P1/P2 — backend event supplements)

- **STEER event** (`CommandEventType_STEER = 16` + `SteerPayload`): the
  frontend already has a placeholder kind tag; the backend change requires
  editing `proto/v1/v1/command.proto`, regenerating Go/TS (needs the buf
  toolchain / network), emitting in `backend/agent/pi/executor.go`, and
  passing through `backend/manager/component/dispatcher/dispatcher.go`.
- **RETRY_STARTED/FINISHED** and **AGENT_START/END**: same proto/regeneration
  dependency; frontend placeholders are already in `command-event-kind.ts`.

### Revision (2026-08-14, after review)

Per review feedback, the **Output & Events** tab was changed from a two-column
layout (left terminal + right events panel) to a **single unified Trajectory-style
ledger**:

- The terminal output (`CommandOutput` stdout/stderr/system) is now **merged
  into the ledger** as rows with stream kind tags (`OUTPUT` / `ERROR` / `SYSTEM`),
  interleaved with structured event rows by timestamp.
- The run tab is now a single column:
  `Toolbar → TimelineOverview → ContextUsageBar → Ledger (+ Inspector on selection)`.
- The separate `CommandTimeline` component is no longer used on the run tab;
  the ledger itself is the output + events view.
- The ledger table matches the reference structure: two columns
  (`event` kind tag + `content`), compact rows, phase group headers, selection
  rail, and a subtle right-aligned timestamp in the content cell.
- Added `output` filter and stream kind tags; updated tests (485 total pass).
