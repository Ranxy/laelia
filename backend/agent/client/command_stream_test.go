package client

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestCommandStreamRunCommandSendsProgressEventAndResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	resultPayload, err := structpb.NewStruct(map[string]any{"status": "ok"})
	require.NoError(t, err)

	runtime := newScriptedRuntime(func(runtime *scriptedRuntime) {
		runtime.outputCh <- executor.OutputChunk{
			StreamType: v1pb.CommandOutput_STDOUT,
			Content:    "hello from runtime",
			SeqNo:      7,
		}
		runtime.eventCh <- executor.Event{
			Type:      v1pb.CommandEventType_WARNING,
			Summary:   "tool warning",
			Timestamp: time.Unix(1710000000, 0),
			Payload: map[string]any{
				"code": "warn-1",
			},
		}
		close(runtime.outputCh)
		close(runtime.eventCh)
		runtime.resultCh <- executor.Result{
			ExitCode:     0,
			DurationMs:   321,
			FinalSummary: "command completed",
			Result:       resultPayload,
		}
		close(runtime.resultCh)
		close(runtime.doneCh)
	})

	req := &v1pb.CommandRequest{
		CommandId:    "cmd-1",
		ExecutorKind: v1pb.ExecutorKind_ACP,
		Profile:      "opencode",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		(&commandStream{}).runCommand(ctx, runtime, stream, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runCommand")
	}

	require.NoError(t, stream.CloseRequest())

	state, stateErr := executor.LoadLocalState()
	require.NoError(t, stateErr)
	assert.Nil(t, state)

	msgs := recorder.Messages()
	require.Len(t, msgs, 5)

	lifecycle := msgs[0].GetEvent()
	require.NotNil(t, lifecycle)
	assert.Equal(t, int32(1), lifecycle.SeqNo)
	assert.Equal(t, v1pb.CommandEventType_LIFECYCLE, lifecycle.Type)
	assert.Equal(t, "command started", lifecycle.Summary)
	assert.Equal(t, "ACP", lifecycle.Payload.AsMap()["executor_kind"])
	assert.Equal(t, "opencode", lifecycle.Payload.AsMap()["profile"])

	progress := msgs[1].GetProgress()
	require.NotNil(t, progress)
	assert.Equal(t, "cmd-1", progress.CommandId)
	assert.Equal(t, v1pb.CommandOutput_STDOUT, progress.Type)
	assert.Equal(t, "hello from runtime", progress.Content)
	assert.Equal(t, int32(7), progress.SeqNo)

	warning := msgs[2].GetEvent()
	require.NotNil(t, warning)
	assert.Equal(t, v1pb.CommandEventType_WARNING, warning.Type)
	assert.Equal(t, "tool warning", warning.Summary)
	assert.Equal(t, "warn-1", warning.Payload.AsMap()["code"])

	textDelta := msgs[3].GetEvent()
	require.NotNil(t, textDelta)
	assert.Equal(t, v1pb.CommandEventType_TEXT_DELTA, textDelta.Type)
	assert.Equal(t, "hello from runtime", textDelta.Summary)
	assert.Equal(t, "STDOUT", textDelta.Payload.AsMap()["stream_type"])
	assert.Equal(t, "hello from runtime", textDelta.Payload.AsMap()["content"])

	result := msgs[4].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, "cmd-1", result.CommandId)
	assert.Equal(t, int32(0), result.ExitCode)
	assert.Equal(t, int64(321), result.DurationMs)
	assert.Equal(t, int32(7), result.LastSeqNo)
	assert.Equal(t, "command completed", result.FinalSummary)
	assert.Equal(t, map[string]any{"status": "ok"}, result.Result.AsMap())

	assert.Equal(t, int32(0), runtime.cancelCount.Load())
	assert.Equal(t, int32(1), runtime.startInvoked.Load())
	assert.True(t, recorder.closed.Load())
}

func TestDrainOutputSendsProgressAndSynthesizedEvent(t *testing.T) {
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	runtime := &scriptedRuntime{
		outputCh: make(chan executor.OutputChunk, 1),
		eventCh:  make(chan executor.Event),
		resultCh: make(chan executor.Result, 1),
		doneCh:   make(chan struct{}),
	}
	runtime.outputCh <- executor.OutputChunk{
		StreamType: v1pb.CommandOutput_STDERR,
		Content:    "remaining output",
		SeqNo:      9,
	}
	close(runtime.outputCh)

	lastSeqNo, lastEventSeqNo := drainOutput(runtime, stream, "cmd-2", 4, 6, &mergedText{})
	assert.Equal(t, int32(9), lastSeqNo)
	assert.Equal(t, int32(6), lastEventSeqNo)

	require.NoError(t, stream.CloseRequest())

	msgs := recorder.Messages()
	require.Len(t, msgs, 2)

	progress := msgs[0].GetProgress()
	require.NotNil(t, progress)
	assert.Equal(t, "cmd-2", progress.CommandId)
	assert.Equal(t, v1pb.CommandOutput_STDERR, progress.Type)
	assert.Equal(t, "remaining output", progress.Content)
	assert.Equal(t, int32(9), progress.SeqNo)

	textDelta := msgs[1].GetEvent()
	require.NotNil(t, textDelta)
	assert.Equal(t, v1pb.CommandEventType_TEXT_DELTA, textDelta.Type)
	assert.Equal(t, "remaining output", textDelta.Summary)
	assert.Equal(t, "STDERR", textDelta.Payload.AsMap()["stream_type"])
	assert.Equal(t, "remaining output", textDelta.Payload.AsMap()["content"])
	assert.True(t, recorder.closed.Load())
}

type scriptedRuntime struct {
	outputCh     chan executor.OutputChunk
	eventCh      chan executor.Event
	resultCh     chan executor.Result
	doneCh       chan struct{}
	script       func(*scriptedRuntime)
	cancelCount  atomic.Int32
	startInvoked atomic.Int32
}

func newScriptedRuntime(script func(*scriptedRuntime)) *scriptedRuntime {
	return &scriptedRuntime{
		outputCh: make(chan executor.OutputChunk),
		eventCh:  make(chan executor.Event),
		resultCh: make(chan executor.Result, 1),
		doneCh:   make(chan struct{}),
		script:   script,
	}
}

func (r *scriptedRuntime) Start() {
	r.startInvoked.Add(1)
	if r.script != nil {
		go r.script(r)
	}
}

func (r *scriptedRuntime) Cancel() {
	r.cancelCount.Add(1)
}

func (r *scriptedRuntime) OutputChannel() <-chan executor.OutputChunk {
	return r.outputCh
}

func (r *scriptedRuntime) EventChannel() <-chan executor.Event {
	return r.eventCh
}

func (r *scriptedRuntime) ResultChannel() <-chan executor.Result {
	return r.resultCh
}

func (r *scriptedRuntime) Done() <-chan struct{} {
	return r.doneCh
}

type recordingStreamingClientConn struct {
	mu       sync.Mutex
	messages []*v1pb.AgentCommandMessage
	closed   atomic.Bool
}

func newRecordingStreamingClientConn() *recordingStreamingClientConn {
	return &recordingStreamingClientConn{}
}

func (*recordingStreamingClientConn) Spec() connect.Spec {
	return connect.Spec{}
}

func (*recordingStreamingClientConn) Peer() connect.Peer {
	return connect.Peer{}
}

func (s *recordingStreamingClientConn) Send(msg any) error {
	typed, ok := msg.(*v1pb.AgentCommandMessage)
	if !ok {
		return io.ErrUnexpectedEOF
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, typed)
	return nil
}

func (*recordingStreamingClientConn) RequestHeader() http.Header {
	return http.Header{}
}

func (s *recordingStreamingClientConn) CloseRequest() error {
	s.closed.Store(true)
	return nil
}

func (*recordingStreamingClientConn) Receive(any) error {
	return io.EOF
}

func (*recordingStreamingClientConn) ResponseHeader() http.Header {
	return http.Header{}
}

func (*recordingStreamingClientConn) ResponseTrailer() http.Header {
	return http.Header{}
}

func (*recordingStreamingClientConn) CloseResponse() error {
	return nil
}

func (s *recordingStreamingClientConn) Messages() []*v1pb.AgentCommandMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := make([]*v1pb.AgentCommandMessage, len(s.messages))
	copy(msgs, s.messages)
	return msgs
}

func newTestCommandChannel(t *testing.T) (*connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage], *recordingStreamingClientConn, func()) {
	t.Helper()

	recorder := newRecordingStreamingClientConn()
	stream := &connect.BidiStreamForClient[v1pb.AgentCommandMessage, v1pb.ManagerCommandMessage]{}
	setUnexportedField(t, stream, "conn", recorder)

	cleanup := func() {
		_ = stream.CloseResponse()
	}
	return stream, recorder, cleanup
}

func setUnexportedField(t *testing.T, target any, fieldName string, value any) {
	t.Helper()
	field := reflect.ValueOf(target).Elem().FieldByName(fieldName)
	require.True(t, field.IsValid(), "field %s must exist", fieldName)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}
