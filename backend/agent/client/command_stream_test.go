package client

import (
	"context"
	"errors"
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
			Type:    v1pb.CommandEventType_WARNING,
			Summary: "tool warning",
			Warning: &v1pb.WarningPayload{Message: "warn-1"},
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

	req := executor.Request{
		CommandID: "cmd-1",
		Profile:   "opencode",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		(&commandStream{}).runCommand(ctx, runtime, stream, req, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runCommand")
	}

	require.NoError(t, stream.CloseRequest())

	state, stateErr := executor.LoadLocalState("", "")
	require.NoError(t, stateErr)
	assert.Nil(t, state)

	msgs := recorder.Messages()
	require.Len(t, msgs, 5)

	lifecycle := msgs[0].GetEvent()
	require.NotNil(t, lifecycle)
	assert.Equal(t, int32(1), lifecycle.SeqNo)
	assert.Equal(t, v1pb.CommandEventType_LIFECYCLE, lifecycle.Type)
	assert.Equal(t, "command started", lifecycle.Summary)
	assert.Equal(t, "ACP", lifecycle.GetLifecycle().GetExecutorKind())
	assert.Equal(t, "opencode", lifecycle.GetLifecycle().GetProfile())

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
	assert.Equal(t, "warn-1", warning.GetWarning().GetMessage())

	textDelta := msgs[3].GetEvent()
	require.NotNil(t, textDelta)
	assert.Equal(t, v1pb.CommandEventType_TEXT_DELTA, textDelta.Type)
	assert.Equal(t, "hello from runtime", textDelta.Summary)
	assert.Equal(t, "STDOUT", textDelta.GetTextDelta().GetStreamType())
	assert.Equal(t, "hello from runtime", textDelta.GetTextDelta().GetContent())

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
	close(runtime.eventCh)

	state := &executor.LocalState{LastSeqSent: 4, LastEventSeqSent: 6}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	drainOutput(ctx, runtime, stream, "cmd-2", state, &mergedText{}, nil)

	assert.Equal(t, int32(9), state.LastSeqSent, "LastSeqSent must advance to the drained chunk")
	// The synthesized TEXT_DELTA flush increments the event seq from 6 -> 7
	// and state must reflect it (the pre-fix code wrote against a throwaway
	// LocalState, leaving LastEventSeqSent stale at 6).
	assert.Equal(t, int32(7), state.LastEventSeqSent, "LastEventSeqSent must advance past the flushed TEXT_DELTA")

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
	assert.Equal(t, int32(7), textDelta.SeqNo, "TEXT_DELTA seq must come from the live state counter")
	assert.Equal(t, "STDERR", textDelta.GetTextDelta().GetStreamType())
	assert.Equal(t, "remaining output", textDelta.GetTextDelta().GetContent())
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

	// cancelCh is closed the first time Cancel is called, so a script can block
	// on Canceled() until the runtime is cancelled (mimicking a real runtime's
	// Cancel-unblocks-the-turn path).
	cancelCh   chan struct{}
	cancelOnce sync.Once
}

func newScriptedRuntime(script func(*scriptedRuntime)) *scriptedRuntime {
	return &scriptedRuntime{
		outputCh: make(chan executor.OutputChunk),
		eventCh:  make(chan executor.Event),
		resultCh: make(chan executor.Result, 1),
		doneCh:   make(chan struct{}),
		cancelCh: make(chan struct{}),
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
	r.cancelOnce.Do(func() { close(r.cancelCh) })
}

// Canceled returns a channel closed the first time Cancel is called.
func (r *scriptedRuntime) Canceled() <-chan struct{} {
	return r.cancelCh
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
	messages []*v1pb.AgentStreamMessage
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
	typed, ok := msg.(*v1pb.AgentStreamMessage)
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

func (s *recordingStreamingClientConn) Messages() []*v1pb.AgentStreamMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs := make([]*v1pb.AgentStreamMessage, len(s.messages))
	copy(msgs, s.messages)
	return msgs
}

func newTestCommandChannel(t *testing.T) (*connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage], *recordingStreamingClientConn, func()) {
	t.Helper()

	recorder := newRecordingStreamingClientConn()
	stream := &connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage]{}
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

// TestDrainLoopIdleResponseEndsPass verifies that when BeginSession replies
// idle=true, the drain loop sends a BeginSession message, builds no runtime,
// and ends the drain pass without running a session. The drain loop goroutine
// itself stays alive to wait for the next wake (it only exits on ctx/doneCh),
// so the test cancels ctx to stop it rather than expecting it to return on idle.
func TestDrainLoopIdleResponseEndsPass(t *testing.T) {
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	cs := &commandStream{
		wakeCh:      make(chan struct{}, 1),
		beginRespCh: make(chan *v1pb.BeginSessionResponse, 1),
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			t.Fatal("runtime must not be built for an idle session")
			return nil, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	doneCh := make(chan struct{})

	go func() {
		cs.drainLoop(ctx, stream, doneCh)
		close(doneCh)
	}()

	cs.wake()
	cs.beginRespCh <- &v1pb.BeginSessionResponse{Idle: true}

	// The drain pass ends on idle: exactly one BeginSession is sent and no
	// runtime is built (the newSessionRuntime closure would t.Fatal otherwise).
	require.Eventually(t, func() bool {
		return len(recorder.Messages()) >= 1
	}, time.Second, 10*time.Millisecond, "drain loop did not send BeginSession on idle response")

	// The loop keeps waiting for the next wake; cancel ctx to let it exit.
	cancel()
	select {
	case <-doneCh:
	case <-time.After(time.Second):
		t.Fatal("drain loop did not exit on ctx cancel")
	}

	require.NoError(t, stream.CloseRequest())
	msgs := recorder.Messages()
	require.Len(t, msgs, 1, "drain loop should send exactly one BeginSession message")
	require.NotNil(t, msgs[0].GetBeginSession())
}

// TestRunSessionExecutesRuntime verifies that a non-idle session builds the
// runtime and pumps lifecycle + result over the stream via runCommand.
func TestRunSessionExecutesRuntime(t *testing.T) {
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "drain done"}
		close(r.resultCh)
		close(r.doneCh)
	})

	cs := &commandStream{
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cs.runSession(ctx, stream, "drain-1", "TestAgent")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		runtime.Cancel()
		t.Fatal("runSession did not complete")
	}

	require.NoError(t, stream.CloseRequest())
	msgs := recorder.Messages()
	require.NotEmpty(t, msgs)

	lifecycle := msgs[0].GetEvent()
	require.NotNil(t, lifecycle)
	assert.Equal(t, v1pb.CommandEventType_LIFECYCLE, lifecycle.Type)
	assert.Equal(t, "ACP", lifecycle.GetLifecycle().GetExecutorKind())

	result := msgs[len(msgs)-1].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, "drain-1", result.CommandId)
	assert.Equal(t, int32(0), result.ExitCode)
	assert.Equal(t, "drain done", result.FinalSummary)

	assert.Equal(t, int32(1), runtime.startInvoked.Load())
	assert.False(t, cs.isExecuting.Load(), "isExecuting must be cleared after the session")
}

// TestRunnerCoordinatesInFlightTurnOnReload is the Phase 4 / T6 acceptance
// test: a config hot-reload mid-turn must cancel the in-flight turn, wait for
// it to end, and surface an explicit "config reloaded mid-turn" failure to the
// manager (not a generic "context canceled" and not a 30-min hang). It
// exercises the commandStream InFlight/CancelInFlight mechanism end-to-end
// through the runner's coordinateInFlightTurn glue.
func TestRunnerCoordinatesInFlightTurnOnReload(t *testing.T) {
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	// A runtime that blocks until cancelled, then finishes with a generic
	// "context canceled" result — exactly what a pi/ACP executor reports when
	// its turn ctx is cancelled mid-flight.
	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		<-r.Canceled() // block until CancelInFlight cancels the turn
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: -1, ErrorMessage: "context canceled"}
		close(r.resultCh)
		close(r.doneCh)
	})

	cs := &commandStream{
		newSessionRuntime: func(_ executor.Request) (executor.Runtime, error) {
			return runtime, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go cs.runSession(ctx, stream, "drain-reload", "TestAgent")

	// Wait until the turn is in flight before reloading.
	require.Eventually(t, cs.InFlight, 2*time.Second, 5*time.Millisecond, "turn must become in flight")

	r := &agentRunner{agentName: "agents/x", cs: cs}
	start := time.Now()
	// Hot-reload: cancel the in-flight turn and wait for it to end. This must
	// return quickly (bounded by inFlightTurnTimeout, here near-instant once
	// the runtime unblocks on cancel), not hang to the turn timeout.
	r.coordinateInFlightTurn("config reloaded mid-turn")
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, runtime.cancelCount.Load(), int32(1), "in-flight turn must be cancelled")
	assert.Less(t, elapsed, 2*time.Second, "coordination must end fast after cancel, not hang")
	assert.False(t, cs.InFlight(), "turn must no longer be in flight after coordination")

	require.NoError(t, stream.CloseRequest())
	msgs := recorder.Messages()
	require.NotEmpty(t, msgs)
	result := msgs[len(msgs)-1].GetResult()
	require.NotNil(t, result, "expected a CommandResult carrying the reload reason")
	assert.Equal(t, "drain-reload", result.CommandId)
	assert.Equal(t, "config reloaded mid-turn", result.ErrorMessage, "manager must see the explicit reload reason, not a generic cancel")
}

// TestDrainOutput_SequenceMonotonic guards the T15 drainOutput rewrite: with
// both buffered output and buffered events pending after Done(), the drain
// must forward all of them and leave state.LastEventSeqSent monotonically
// ahead of its input (previously events were dropped and the seq counter was
// written against a throwaway LocalState, rolling it back).
func TestDrainOutput_SequenceMonotonic(t *testing.T) {
	stream, recorder, cleanup := newTestCommandChannel(t)
	defer cleanup()

	runtime := &scriptedRuntime{
		outputCh: make(chan executor.OutputChunk, 4),
		eventCh:  make(chan executor.Event, 4),
		resultCh: make(chan executor.Result, 1),
		doneCh:   make(chan struct{}),
	}
	runtime.outputCh <- executor.OutputChunk{StreamType: v1pb.CommandOutput_STDOUT, Content: "aaa", SeqNo: 10}
	runtime.outputCh <- executor.OutputChunk{StreamType: v1pb.CommandOutput_STDOUT, Content: "bbb", SeqNo: 11}
	runtime.eventCh <- executor.Event{Type: v1pb.CommandEventType_WARNING, Summary: "w1"}
	runtime.eventCh <- executor.Event{Type: v1pb.CommandEventType_WARNING, Summary: "w2"}
	close(runtime.outputCh)
	close(runtime.eventCh)

	state := &executor.LocalState{LastSeqSent: 4, LastEventSeqSent: 6}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	drainOutput(ctx, runtime, stream, "cmd-m", state, &mergedText{}, nil)

	assert.Equal(t, int32(11), state.LastSeqSent, "LastSeqSent must be the max drained chunk seq")
	// 2 forwarded events (7, 8) + 1 synthesized TEXT_DELTA flush (9) = 9, which
	// is strictly greater than the input 6 (monotonic, no rollback).
	assert.Equal(t, int32(9), state.LastEventSeqSent)

	require.NoError(t, stream.CloseRequest())
	// 2 progress + 2 warning events + 1 TEXT_DELTA = 5 messages.
	assert.Len(t, recorder.Messages(), 5)
}

// failingConn is a connect streaming conn whose Send always errors, simulating a
// dead bidi stream.
type failingConn struct{}

func (failingConn) Spec() connect.Spec           { return connect.Spec{} }
func (failingConn) Peer() connect.Peer           { return connect.Peer{} }
func (failingConn) Send(any) error               { return errors.New("stream dead") }
func (failingConn) RequestHeader() http.Header   { return http.Header{} }
func (failingConn) CloseRequest() error          { return nil }
func (failingConn) Receive(any) error            { return io.EOF }
func (failingConn) ResponseHeader() http.Header  { return http.Header{} }
func (failingConn) ResponseTrailer() http.Header { return http.Header{} }
func (failingConn) CloseResponse() error         { return nil }

type fakeStreamClient struct {
	stream *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage]
}

func (f *fakeStreamClient) AgentChannel(_ context.Context) *connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage] {
	return f.stream
}

// TestCommandStreamStart_SurfacesTerminalError guards the T15 death fuse: Start
// must return the stream's terminal error directly instead of swallowing it in
// an internal retry loop. Run's heartbeat loop watches this to tear down and
// reconnect the whole agent connection when the bidi stream dies. The old code
// logged + backed off + looped forever, so the agent went deaf while heartbeat
// stayed healthy. This test fails if Start retries (it would return a ctx
// deadline error after the backoff, not the "stream dead" error, and only
// after the full backoff wait).
func TestCommandStreamStart_SurfacesTerminalError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	stream := &connect.BidiStreamForClient[v1pb.AgentStreamMessage, v1pb.ManagerStreamMessage]{}
	setUnexportedField(t, stream, "conn", &failingConn{})

	cs := &commandStream{
		client:    &fakeStreamClient{stream: stream},
		getToken:  func() string { return "tok" },
		getSessID: func() string { return "sess" },
		backoff:   NewExponentialBackoff(2*time.Second, 1*time.Minute),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	err := cs.Start(ctx)
	elapsed := time.Since(start)

	require.Error(t, err, "Start must surface the stream's terminal error")
	assert.Contains(t, err.Error(), "stream dead", "Start must return the stream error, not a backoff/ctx error")
	assert.Less(t, elapsed, 1*time.Second, "Start must return promptly, not retry-then-backoff")
}
