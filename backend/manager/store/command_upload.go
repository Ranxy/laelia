package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// Upload entry kinds mirror v1.UploadEntryKind. The store layer stays
// proto-free, so the dispatcher converts the wire entries into these values.
const (
	UploadKindProgress int32 = 1
	UploadKindEvent    int32 = 2
	UploadKindResult   int32 = 3
)

// CommandUploadEntry is one neutral command data record from an
// UploadCommandData batch. Exactly the fields for Kind are populated.
type CommandUploadEntry struct {
	CommandID uuid.UUID
	Kind      int32
	SeqNo     int32
	Timestamp time.Time // agent-side; zero means "use arrival time"

	// Progress fields.
	StreamType int32
	Content    string

	// Event fields.
	EventType   int32
	Summary     string
	PayloadJSON string

	// TokenUsage fields (kind == event, EventType TOKEN_USAGE).
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	TotalTokens      int64
	HasTokenUsage    bool

	// Result fields.
	ExitCode     int32
	DurationMs   int64
	ErrorMessage string
	FinalSummary string
	ResultJSON   string
	LastSeqNo    int32
}

// CommandUploadAck is the per-command persisted watermark of one applied batch.
type CommandUploadAck struct {
	CommandID       uuid.UUID
	LastProgressSeq int32
	LastEventSeq    int32
	ResultAcked     bool
}

// CommandUploadRejection describes one entry the manager refused. The caller
// treats a rejected entry as settled (never retransmitted).
type CommandUploadRejection struct {
	CommandID uuid.UUID
	Kind      int32
	SeqNo     int32
	Reason    string
}

// CommandUploadTerminal describes one terminal result applied to a
// previously-pending/running command, so the dispatcher can clean up watchers
// and the session registry exactly once.
type CommandUploadTerminal struct {
	CommandID uuid.UUID
	AgentID   int
	Status    int32
}

// CommandUploadBatchResult is ApplyCommandUploadBatch's outcome: watermarks,
// explicit rejections, terminal transitions, late-result re-grades, and the
// rows that were actually inserted (dedup-skipped retransmissions are omitted
// so the caller does not re-broadcast records its watchers already saw).
type CommandUploadBatchResult struct {
	Acks            []*CommandUploadAck
	Rejected        []*CommandUploadRejection
	Terminals       []*CommandUploadTerminal
	Regrades        []*CommandUploadRegrade
	InsertedOutputs []*CommandOutputMessage
	InsertedEvents  []*CommandEventMessage
	// LateFailures counts §3.6 rule-2's reverse direction: FAILED
	// (machine_unreachable) rows that a late real failure re-attributed to
	// the agent's own verdict (status unchanged; audit trail updated).
	LateFailures int
}

// uploadOwnershipSQL loads the ownership + status state of every command in a
// batch in one query. The machine_id check is applied in Go (not SQL) so a
// foreign command produces an explicit per-entry rejection instead of
// silently vanishing. failure_kind rides along because a FAILED row's
// re-grade eligibility (design §3.6 rule 2) depends on it.
const uploadOwnershipSQL = `
	SELECT c.id, COALESCE(c.machine_id, 0), c.status, c.agent_id, COALESCE(c.failure_kind, '')
	FROM command c
	WHERE c.id = ANY($1::uuid[])
`

// uploadAppendOutputSQL is the progress insert; the (command_id, seq_no)
// unique index plus ON CONFLICT DO NOTHING is the retransmission dedup key.
const uploadAppendOutputSQL = `
	INSERT INTO command_output (command_id, seq_no, stream_type, content, created_at)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (command_id, seq_no) DO NOTHING
`

// uploadAppendEventSQL persists one command event; dedup on (command_id, seq_no).
const uploadAppendEventSQL = `
	INSERT INTO command_event (command_id, seq_no, event_type, summary, payload_json)
	VALUES ($1, $2, $3, $4, $5::jsonb)
	ON CONFLICT (command_id, seq_no) DO NOTHING
`

// uploadTerminalSQL applies a terminal result with a status guard: only a
// PENDING/RUNNING command can transition. A late result for a command the user
// already cancelled (or the reaper failed) is acked without touching state —
// the user-visible terminal state is the irreversible anchor. failure_kind
// records the provenance of a FAILED row: the machine reported this failure,
// so it is agent_failed.
const uploadTerminalSQL = `
	UPDATE command
	SET status = $1, completed_at = $2, exit_code = $3, duration_ms = $4, error_message = $5, failure_kind = $6
	WHERE id = $7 AND status IN ($8, $9)
	RETURNING id
`

// uploadRegradeSQL re-grades a FAILED(machine_unreachable) command to
// COMPLETED when a late successful terminal arrives (design §3.6 rule 2): the
// reap was the manager's timeout guess and the machine's real result is
// authoritative. The double predicate (status AND failure_kind) is the
// compare-and-set guard — a retransmitted or already-re-graded row matches
// nothing and stays untouched.
const uploadRegradeSQL = `
	UPDATE command
	SET status = $1, completed_at = $2, exit_code = $3, duration_ms = $4, error_message = '', failure_kind = NULL
	WHERE id = $5 AND status = $6 AND failure_kind = $7
	RETURNING id
`

// uploadLateFailureSQL records the machine's real failure over a
// machine_unreachable reap: the status stays FAILED but the audit trail moves
// from "manager guessed" to "the agent failed for real" (design §3.6 rule 2,
// reverse direction). An empty late error message keeps the existing one.
const uploadLateFailureSQL = `
	UPDATE command
	SET failure_kind = $1, error_message = CASE WHEN $2 = '' THEN error_message ELSE $2 END
	WHERE id = $3 AND status = $4 AND failure_kind = $5
`

// regradeEventSeqSQL picks the manager-side seq for the re-grade explanation
// event: one past the largest event seq already recorded for the command. The
// barrier guarantees the machine has uploaded every record it will ever write
// for the command before its late terminal, so the max is stable.
const regradeEventSeqSQL = `
	SELECT COALESCE(MAX(seq_no), 0) FROM command_event WHERE command_id = $1
`

// insertRegradeEventSQL appends the manager-generated SYSTEM explanation event.
const insertRegradeEventSQL = `
	INSERT INTO command_event (command_id, seq_no, event_type, summary, payload_json)
	VALUES ($1, $2, $3, $4, '{}'::jsonb)
`

// CommandEventTypeSystem mirrors v1.CommandEventType_SYSTEM: the
// manager-generated explanation event kind. The store layer stays proto-free,
// so the enum value is pinned here (proto enums are append-only).
const CommandEventTypeSystem int32 = 16

// regradeEventSummary is the human-readable explanation recorded with the
// re-grade event.
const regradeEventSummary = "Marked failed while the machine was unreachable; re-graded to completed from the late result."

// CommandUploadRegrade describes one FAILED(machine_unreachable) command that
// a late successful terminal re-graded to COMPLETED, together with the SYSTEM
// explanation event the manager appended for the audit trail.
type CommandUploadRegrade struct {
	CommandID uuid.UUID
	AgentID   int
	Event     *CommandEventMessage
}

// uploadAckSeqSQL advances the event-side persisted cursor. GREATEST keeps a
// reordered/partial retransmission from regressing the watermark.
const uploadAckSeqSQL = `
	UPDATE command SET last_ack_seq = GREATEST(last_ack_seq, $1) WHERE id = $2
`

// ApplyCommandUploadBatch applies one UploadCommandData batch inside a single
// transaction: ownership validation (command.machine_id == machineID), ordered
// application of progress/events, the terminal result last, and per-(command,
// kind) ack watermarks plus an explicit rejection list. A failed entry never
// aborts the batch: it is rejected explicitly (poison message) and the rest of
// the batch proceeds.
func (s *Store) ApplyCommandUploadBatch(
	ctx context.Context,
	machineID int,
	entries []*CommandUploadEntry,
) (*CommandUploadBatchResult, error) {
	if len(entries) == 0 {
		return &CommandUploadBatchResult{}, nil
	}

	// 1. Load ownership/status for the distinct command ids in the batch.
	ids := make([]uuid.UUID, 0, len(entries))
	seen := make(map[uuid.UUID]struct{}, len(entries))
	for _, e := range entries {
		if _, ok := seen[e.CommandID]; !ok {
			seen[e.CommandID] = struct{}{}
			ids = append(ids, e.CommandID)
		}
	}
	rows, err := s.GetDB().QueryContext(ctx, uploadOwnershipSQL, ids)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load command ownership for upload batch")
	}
	defer rows.Close()

	type cmdState struct {
		machineID   int
		status      int32
		agentID     int
		failureKind string
	}
	states := make(map[uuid.UUID]cmdState, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var st cmdState
		if err := rows.Scan(&id, &st.machineID, &st.status, &st.agentID, &st.failureKind); err != nil {
			return nil, errors.Wrap(err, "failed to scan command ownership row")
		}
		states[id] = st
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to iterate command ownership rows")
	}

	// 2. Apply the whole batch in one transaction.
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res := &CommandUploadBatchResult{}
	type watermark struct {
		progress, event, ack int32
		result               bool
	}
	marks := make(map[uuid.UUID]*watermark)
	wm := func(id uuid.UUID) *watermark {
		w, ok := marks[id]
		if !ok {
			w = &watermark{}
			marks[id] = w
		}
		return w
	}
	reject := func(e *CommandUploadEntry, reason string) {
		res.Rejected = append(res.Rejected, &CommandUploadRejection{
			CommandID: e.CommandID, Kind: e.Kind, SeqNo: e.SeqNo, Reason: reason,
		})
	}
	now := time.Now()

	for _, e := range entries {
		st, ok := states[e.CommandID]
		if !ok {
			reject(e, "command not found")
			continue
		}
		if st.machineID != machineID {
			reject(e, "command is not bound to the authenticated machine")
			continue
		}
		if e.SeqNo < 1 {
			reject(e, "sequence number must be positive")
			continue
		}
		ts := e.Timestamp
		if ts.IsZero() {
			ts = now
		}

		switch e.Kind {
		case UploadKindProgress:
			out, err := tx.ExecContext(ctx, uploadAppendOutputSQL,
				e.CommandID, e.SeqNo, e.StreamType, e.Content, ts)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to append command output in upload batch (command %s seq %d)", e.CommandID, e.SeqNo)
			}
			w := wm(e.CommandID)
			if e.SeqNo > w.progress {
				w.progress = e.SeqNo
			}
			if n, _ := out.RowsAffected(); n > 0 {
				res.InsertedOutputs = append(res.InsertedOutputs, &CommandOutputMessage{
					CommandID: e.CommandID, SeqNo: e.SeqNo, StreamType: e.StreamType, Content: e.Content, CreatedAt: ts,
				})
			}

		case UploadKindEvent:
			payload := e.PayloadJSON
			if payload == "" {
				payload = "{}"
			}
			ev, err := tx.ExecContext(ctx, uploadAppendEventSQL,
				e.CommandID, e.SeqNo, e.EventType, e.Summary, payload)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to append command event in upload batch (command %s seq %d)", e.CommandID, e.SeqNo)
			}
			if e.HasTokenUsage {
				// TOKEN_USAGE is additionally denormalized into
				// command_token_usage. Failure must not break the batch: the
				// event row above is the source of truth.
				if err := recordCommandTokenUsageTx(ctx, tx, e); err != nil {
					_ = err
				}
			}
			w := wm(e.CommandID)
			if e.SeqNo > w.event {
				w.event = e.SeqNo
			}
			if e.SeqNo > w.ack {
				w.ack = e.SeqNo
			}
			if n, _ := ev.RowsAffected(); n > 0 {
				res.InsertedEvents = append(res.InsertedEvents, &CommandEventMessage{
					CommandID: e.CommandID, SeqNo: e.SeqNo, EventType: e.EventType,
					Summary: e.Summary, PayloadJSON: payload, CreatedAt: ts,
				})
			}

		case UploadKindResult:
			status := CommandStatusCompleted
			if e.ExitCode != 0 {
				status = CommandStatusFailed
			}
			failureKind := ""
			if status == CommandStatusFailed {
				failureKind = CommandFailureKindAgentFailed
			}

			switch {
			case st.status == CommandStatusPending || st.status == CommandStatusRunning:
				// Normal first terminal: guarded transition with the
				// machine-reported provenance.
				if _, err := tx.ExecContext(ctx, uploadTerminalSQL,
					status, ts, e.ExitCode, e.DurationMs, e.ErrorMessage, failureKind,
					e.CommandID, CommandStatusPending, CommandStatusRunning); err != nil {
					return nil, errors.Wrapf(err, "failed to apply terminal result in upload batch (command %s)", e.CommandID)
				}
				// Terminal reporting: only a command that was still
				// pending/running when this batch loaded transitions here. A
				// retransmitted result finds the row terminal and stays a
				// no-op.
				res.Terminals = append(res.Terminals, &CommandUploadTerminal{
					CommandID: e.CommandID, AgentID: st.agentID, Status: status,
				})

			case st.status == CommandStatusFailed && st.failureKind == CommandFailureKindMachineUnreachable && status == CommandStatusCompleted:
				// Late success over a machine-loss reap: re-grade to
				// COMPLETED (design §3.6 rule 2). The SQL guard re-checks the
				// state read above, so a row that changed concurrently (or an
				// already-re-graded retransmission) is a no-op.
				var id uuid.UUID
				err := tx.QueryRowContext(ctx, uploadRegradeSQL,
					CommandStatusCompleted, ts, e.ExitCode, e.DurationMs,
					e.CommandID, CommandStatusFailed, CommandFailureKindMachineUnreachable).Scan(&id)
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					return nil, errors.Wrapf(err, "failed to re-grade unreachable command (command %s)", e.CommandID)
				}
				event, err := appendRegradeEventTx(ctx, tx, e.CommandID)
				if err != nil {
					return nil, errors.Wrapf(err, "failed to record re-grade explanation (command %s)", e.CommandID)
				}
				res.Regrades = append(res.Regrades, &CommandUploadRegrade{
					CommandID: e.CommandID, AgentID: st.agentID, Event: event,
				})

			case st.status == CommandStatusFailed && st.failureKind == CommandFailureKindMachineUnreachable && status == CommandStatusFailed:
				// Late real failure over a machine-loss reap: keep FAILED but
				// re-attribute it to the agent's own verdict.
				if _, err := tx.ExecContext(ctx, uploadLateFailureSQL,
					CommandFailureKindAgentFailed, e.ErrorMessage,
					e.CommandID, CommandStatusFailed, CommandFailureKindMachineUnreachable); err != nil {
					return nil, errors.Wrapf(err, "failed to re-attribute late failure (command %s)", e.CommandID)
				}
				res.LateFailures++

			default:
				// COMPLETED / CANCELED / FAILED(agent_failed): the irreversible
				// anchor stands; the late terminal is acked without touching
				// state.
			}

			if err := updateCommandResultSummaryTx(ctx, tx, e.CommandID, e.FinalSummary, e.ResultJSON); err != nil {
				return nil, errors.Wrapf(err, "failed to update command result summary in upload batch (command %s)", e.CommandID)
			}
			w := wm(e.CommandID)
			w.result = true
			// The result's LastSeqNo is a progress-side cursor; keep feeding
			// last_ack_seq the way the stream handler did (GREATEST guards
			// against a regressed late result).
			if e.LastSeqNo > w.ack {
				w.ack = e.LastSeqNo
			}

		default:
			reject(e, "unknown upload entry kind")
		}
	}

	// 3. Fold per-command watermarks into last_ack_seq and the response acks.
	for id, w := range marks {
		if w.ack > 0 {
			if _, err := tx.ExecContext(ctx, uploadAckSeqSQL, w.ack, id); err != nil {
				return nil, errors.Wrapf(err, "failed to advance command ack seq in upload batch (command %s)", id)
			}
		}
		res.Acks = append(res.Acks, &CommandUploadAck{
			CommandID:       id,
			LastProgressSeq: w.progress,
			LastEventSeq:    w.event,
			ResultAcked:     w.result,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit upload batch")
	}
	return res, nil
}

// appendRegradeEventTx appends the manager-generated SYSTEM explanation event
// for a re-grade, seq-assigned one past the command's largest event seq so it
// cannot collide with any machine-recorded event.
func appendRegradeEventTx(ctx context.Context, tx *sql.Tx, commandID uuid.UUID) (*CommandEventMessage, error) {
	var lastSeq int32
	if err := tx.QueryRowContext(ctx, regradeEventSeqSQL, commandID).Scan(&lastSeq); err != nil {
		return nil, errors.Wrapf(err, "failed to read command event watermark for re-grade")
	}
	seq := lastSeq + 1
	createdAt := time.Now()
	if _, err := tx.ExecContext(ctx, insertRegradeEventSQL, commandID, seq, CommandEventTypeSystem, regradeEventSummary); err != nil {
		return nil, errors.Wrapf(err, "failed to insert re-grade explanation event")
	}
	return &CommandEventMessage{
		CommandID:   commandID,
		SeqNo:       seq,
		EventType:   CommandEventTypeSystem,
		Summary:     regradeEventSummary,
		PayloadJSON: "{}",
		CreatedAt:   createdAt,
	}, nil
}

// recordCommandTokenUsageTx is RecordCommandTokenUsage bound to an open
// transaction so the denormalized aggregate lands atomically with the batch.
func recordCommandTokenUsageTx(ctx context.Context, tx *sql.Tx, e *CommandUploadEntry) error {
	var agentID, principalID int
	var machineID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT agent_id, principal_id, machine_id
		FROM command
		WHERE id = $1
	`, e.CommandID).Scan(&agentID, &principalID, &machineID); err != nil {
		return errors.Wrapf(err, "failed to load command dimensions for token usage")
	}

	var machineArg any
	if machineID.Valid {
		machineArg = machineID.Int64
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO command_token_usage
			(command_id, agent_id, principal_id, machine_id,
			 input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (command_id) DO NOTHING
	`, e.CommandID, agentID, principalID, machineArg,
		e.InputTokens, e.OutputTokens, e.CacheReadTokens, e.CacheWriteTokens, e.TotalTokens)
	if err != nil {
		return errors.Wrapf(err, "failed to record command token usage")
	}
	return nil
}

// updateCommandResultSummaryTx is UpdateCommandResultSummary bound to an open
// transaction so the terminal result's summary lands with the batch.
func updateCommandResultSummaryTx(ctx context.Context, tx *sql.Tx, id uuid.UUID, finalSummary, resultJSON string) error {
	sets := make([]string, 0, 2)
	args := make([]any, 0, 3)
	if finalSummary != "" {
		sets = append(sets, "final_summary = $1")
		args = append(args, finalSummary)
	}
	if resultJSON != "" {
		sets = append(sets, "result_json = $2::jsonb")
		args = append(args, resultJSON)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := tx.ExecContext(ctx, `
		UPDATE command SET `+strings.Join(sets, ", ")+` WHERE id = $3
	`, args...)
	if err != nil {
		return errors.Wrapf(err, "failed to update command result summary")
	}
	return nil
}
