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
	Status    int32
}

// CommandUploadBatchResult is ApplyCommandUploadBatch's outcome: watermarks,
// explicit rejections, and the terminal transitions that were applied.
type CommandUploadBatchResult struct {
	Acks      []*CommandUploadAck
	Rejected  []*CommandUploadRejection
	Terminals []*CommandUploadTerminal
}

// uploadOwnershipSQL loads the ownership + status state of every command in a
// batch in one query. The machine_id check is applied in Go (not SQL) so a
// foreign command produces an explicit per-entry rejection instead of
// silently vanishing.
const uploadOwnershipSQL = `
	SELECT c.id, COALESCE(c.machine_id, 0), c.status
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
// the user-visible terminal state is the irreversible anchor.
const uploadTerminalSQL = `
	UPDATE command
	SET status = $1, completed_at = $2, exit_code = $3, duration_ms = $4, error_message = $5
	WHERE id = $6 AND status IN ($7, $8)
	RETURNING id
`

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
		machineID int
		status    int32
	}
	states := make(map[uuid.UUID]cmdState, len(ids))
	for rows.Next() {
		var id uuid.UUID
		var st cmdState
		if err := rows.Scan(&id, &st.machineID, &st.status); err != nil {
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
			if _, err := tx.ExecContext(ctx, uploadAppendOutputSQL,
				e.CommandID, e.SeqNo, e.StreamType, e.Content, ts); err != nil {
				return nil, errors.Wrapf(err, "failed to append command output in upload batch (command %s seq %d)", e.CommandID, e.SeqNo)
			}
			w := wm(e.CommandID)
			if e.SeqNo > w.progress {
				w.progress = e.SeqNo
			}

		case UploadKindEvent:
			payload := e.PayloadJSON
			if payload == "" {
				payload = "{}"
			}
			if _, err := tx.ExecContext(ctx, uploadAppendEventSQL,
				e.CommandID, e.SeqNo, e.EventType, e.Summary, payload); err != nil {
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

		case UploadKindResult:
			status := CommandStatusCompleted
			if e.ExitCode != 0 {
				status = CommandStatusFailed
			}
			if _, err := tx.ExecContext(ctx, uploadTerminalSQL,
				status, ts, e.ExitCode, e.DurationMs, e.ErrorMessage,
				e.CommandID, CommandStatusPending, CommandStatusRunning); err != nil {
				return nil, errors.Wrapf(err, "failed to apply terminal result in upload batch (command %s)", e.CommandID)
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
			// Terminal reporting: only a command that was still
			// pending/running when this batch loaded transitions here. A
			// retransmitted result finds the row terminal and stays a no-op.
			if st.status == CommandStatusPending || st.status == CommandStatusRunning {
				res.Terminals = append(res.Terminals, &CommandUploadTerminal{CommandID: e.CommandID, Status: status})
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

// eventTypeTokenUsage mirrors v1.CommandEventType_TOKEN_USAGE (15) without
// importing the generated package into the store layer.
const eventTypeTokenUsage int32 = 15

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
