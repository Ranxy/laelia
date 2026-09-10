package dispatcher

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// ApplyCommandUpload applies one UploadCommandData batch for an authenticated
// machine: it validates and converts the wire entries (malformed entries are
// rejected explicitly as poison messages, not dropped silently), applies the
// rest in one idempotent store transaction, broadcasts the newly inserted rows
// to the live command watchers, and reports per-(command, kind) ack watermarks
// plus the rejection list so the machine's uploader can evict.
func (d *Dispatcher) ApplyCommandUpload(
	ctx context.Context,
	machineID int,
	entries []*v1pb.UploadCommandDataEntry,
) (*v1pb.UploadCommandDataResponse, error) {
	converted := make([]*store.CommandUploadEntry, 0, len(entries))
	accepted := make([]*v1pb.UploadCommandDataEntry, 0, len(entries))
	// poison collects rejections that could not be represented as a store
	// entry (unparseable command id, missing payload, unknown type).
	rejected := make([]*v1pb.UploadCommandDataRejection, 0)
	for _, we := range entries {
		se, reason := convertUploadEntry(we)
		if reason != "" {
			rejected = append(rejected, &v1pb.UploadCommandDataRejection{
				CommandId: we.GetCommandId(),
				Kind:      we.GetKind(),
				SeqNo:     we.GetSeqNo(),
				Reason:    reason,
			})
			continue
		}
		converted = append(converted, se)
		accepted = append(accepted, we)
	}

	res, err := d.store.ApplyCommandUploadBatch(ctx, machineID, converted)
	if err != nil {
		return nil, err
	}

	// Broadcast the newly persisted rows to live watchers, in batch order,
	// using the wire forms (they carry the timestamp and typed payload the
	// watchers render). Dedup-skipped retransmissions are not in the inserted
	// lists, so a watcher never sees a record twice.
	wireByKey := make(map[string]*v1pb.UploadCommandDataEntry, len(accepted))
	for _, we := range accepted {
		wireByKey[we.GetCommandId()+"|"+strconv.Itoa(int(we.GetSeqNo()))] = we
	}
	for _, o := range res.InsertedOutputs {
		we := wireByKey[o.CommandID.String()+"|"+strconv.Itoa(int(o.SeqNo))]
		if we == nil {
			continue
		}
		p := we.GetProgress()
		d.broadcast(o.CommandID.String(), &v1pb.CommandOutput{
			CommandId: p.GetCommandId(),
			Type:      p.GetType(),
			Content:   p.GetContent(),
			SeqNo:     p.GetSeqNo(),
			Timestamp: p.GetTimestamp(),
		})
	}
	for _, ev := range res.InsertedEvents {
		we := wireByKey[ev.CommandID.String()+"|"+strconv.Itoa(int(ev.SeqNo))]
		if we == nil {
			continue
		}
		d.broadcastEvent(we.GetEvent().GetCommandId(), we.GetEvent())
	}

	// Terminal cleanup, mirroring the stream-era HandleResult: clear the
	// agent's in-flight command mark and close the command's watchers (after a
	// short delay so the final broadcast drains).
	for _, t := range res.Terminals {
		d.clearCurrentCommand(t.AgentID, t.CommandID.String())
		d.wgMu.Lock()
		d.wg.Add(1)
		d.wgMu.Unlock()
		go func(commandID string) {
			defer d.wg.Done()
			select {
			case <-d.lifecycleCtx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				d.closeWatchers(commandID)
				d.closeEventWatchers(commandID)
			}
		}(t.CommandID.String())
		slog.Info("command terminal applied from upload batch", "commandID", t.CommandID, "status", t.Status)
	}

	resp := &v1pb.UploadCommandDataResponse{}
	for _, a := range res.Acks {
		resp.Acks = append(resp.Acks, &v1pb.UploadCommandDataAck{
			CommandId:       a.CommandID.String(),
			LastProgressSeq: a.LastProgressSeq,
			LastEventSeq:    a.LastEventSeq,
			ResultAcked:     a.ResultAcked,
		})
	}
	resp.Rejected = append(resp.Rejected, rejected...)
	for _, r := range res.Rejected {
		resp.Rejected = append(resp.Rejected, &v1pb.UploadCommandDataRejection{
			CommandId: r.CommandID.String(),
			Kind:      v1pb.UploadEntryKind(r.Kind),
			SeqNo:     r.SeqNo,
			Reason:    r.Reason,
		})
	}
	return resp, nil
}

// clearCurrentCommand clears the agent's in-flight command mark if it still
// points at the command that just finished.
func (d *Dispatcher) clearCurrentCommand(agentID int, commandID string) {
	sess, ok := d.registry.getAgent(agentID)
	if !ok {
		return
	}
	sess.mu.Lock()
	if sess.currentCmdID == commandID {
		sess.currentCmdID = ""
	}
	sess.mu.Unlock()
}

// convertUploadEntry validates one wire entry into its neutral store form and
// returns it with an empty reason, or nil with the poison reason. Validation
// failures are explicit rejections, never silent drops: the uploader must see
// the refused seq or it would retransmit the entry forever.
func convertUploadEntry(we *v1pb.UploadCommandDataEntry) (*store.CommandUploadEntry, string) {
	if we == nil {
		return nil, "nil entry"
	}
	commandID, err := uuid.Parse(we.GetCommandId())
	if err != nil {
		return nil, "invalid command id"
	}

	se := &store.CommandUploadEntry{
		CommandID: commandID,
		Kind:      int32(we.GetKind()),
		SeqNo:     we.GetSeqNo(),
	}
	if ts := we.GetAgentSideTimestamp(); ts.IsValid() {
		se.Timestamp = ts.AsTime()
	}

	switch we.GetKind() {
	case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS:
		p := we.GetProgress()
		if p == nil {
			return nil, "progress payload missing"
		}
		if p.GetType() < v1pb.CommandOutput_STDOUT || p.GetType() > v1pb.CommandOutput_SYSTEM {
			return nil, "unknown progress stream type"
		}
		se.StreamType = int32(p.GetType())
		se.Content = p.GetContent()

	case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT:
		ev := we.GetEvent()
		if ev == nil || ev.GetPayload() == nil {
			return nil, "unknown event payload"
		}
		payloadJSON, err := marshalEventPayload(ev)
		if err != nil {
			return nil, "event payload marshal failed"
		}
		if payloadJSON == nil {
			payloadJSON = []byte("{}")
		}
		se.EventType = int32(ev.GetType())
		se.Summary = ev.GetSummary()
		se.PayloadJSON = string(payloadJSON)
		if usage := ev.GetTokenUsage(); usage != nil {
			se.HasTokenUsage = true
			se.InputTokens = usage.InputTokens
			se.OutputTokens = usage.OutputTokens
			se.CacheReadTokens = usage.CacheReadTokens
			se.CacheWriteTokens = usage.CacheWriteTokens
			se.TotalTokens = usage.TotalTokens
		}

	case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT:
		r := we.GetResult()
		if r == nil {
			return nil, "result payload missing"
		}
		se.ExitCode = r.GetExitCode()
		se.DurationMs = r.GetDurationMs()
		se.ErrorMessage = r.GetErrorMessage()
		se.FinalSummary = r.GetFinalSummary()
		se.LastSeqNo = r.GetLastSeqNo()
		if r.GetResult() != nil {
			data, err := protojson.Marshal(r.GetResult())
			if err != nil {
				return nil, "result struct marshal failed"
			}
			se.ResultJSON = string(data)
		}

	default:
		return nil, "unknown upload entry kind"
	}
	return se, ""
}
