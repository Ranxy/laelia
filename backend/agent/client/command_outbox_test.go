package client

import (
	"context"
	"sync"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// memorySink is a turnSink that captures records in memory for assertions.
// onAppend can script per-record failures (e.g. only the result) to drive the
// append-failure paths without a real WAL.
type memorySink struct {
	mu       sync.Mutex
	entries  []*outbox.Entry
	onAppend func(entry *outbox.Entry) error
}

func (s *memorySink) appendProgress(commandID string, chunk executor.OutputChunk) error {
	return s.append(progressEnvelope(commandID, chunk))
}

func (s *memorySink) appendEvent(commandID string, event *executor.Event) error {
	return s.append(eventEnvelope(commandID, event))
}

func (s *memorySink) appendResult(_ string, result *v1pb.CommandResult) error {
	return s.append(resultEnvelope(result))
}

func (s *memorySink) append(entry *outbox.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onAppend != nil {
		if err := s.onAppend(entry); err != nil {
			return err
		}
	}
	s.entries = append(s.entries, entry)
	return nil
}

// Entries returns a snapshot of the captured records.
func (s *memorySink) Entries() []*outbox.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*outbox.Entry(nil), s.entries...)
}

// newMemoryTurnSink returns a capture sink for turn-loop tests.
func newMemoryTurnSink() *memorySink {
	return &memorySink{}
}

// fakeUploadTransport is a scripted UploadFunc recording every call. By
// default it acks everything (per-kind watermarks equal the batch's maxima and
// every result acked), so uploader-driven tests can drain a group.
type fakeUploadTransport struct {
	mu    sync.Mutex
	calls [][]*outbox.Entry
	err   error
}

func (f *fakeUploadTransport) upload(_ context.Context, entries []*outbox.Entry) (*v1pb.UploadCommandDataResponse, error) {
	f.mu.Lock()
	f.calls = append(f.calls, entries)
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return fullAck(entries), nil
}

// fullAck accepts everything: per-kind watermarks equal the batch's maxima and
// every result acked.
func fullAck(entries []*outbox.Entry) *v1pb.UploadCommandDataResponse {
	resp := &v1pb.UploadCommandDataResponse{}
	type ack struct {
		progress, event int32
		result          bool
	}
	byCmd := map[string]*ack{}
	for _, e := range entries {
		a := byCmd[e.GetCommandId()]
		if a == nil {
			a = &ack{}
			byCmd[e.GetCommandId()] = a
		}
		switch e.GetKind() {
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS:
			a.progress = e.GetSeqNo()
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_EVENT:
			a.event = e.GetSeqNo()
		case v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_RESULT:
			a.result = true
		default:
		}
	}
	for id, a := range byCmd {
		resp.Acks = append(resp.Acks, &v1pb.UploadCommandDataAck{
			CommandId:       id,
			LastProgressSeq: a.progress,
			LastEventSeq:    a.event,
			ResultAcked:     a.result,
		})
	}
	return resp
}

func (f *fakeUploadTransport) allEntries() []*outbox.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*outbox.Entry
	for _, c := range f.calls {
		out = append(out, c...)
	}
	return out
}
