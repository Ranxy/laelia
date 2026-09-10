package client

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// scriptedContentSteerer is a runtime satisfying executor.SteerResolver (the
// ACP v2 thread shape: Steer without a delivery signal) with no-op plumbing —
// the router only needs the steer capability plus the in-flight cancel
// surface.
type scriptedContentSteerer struct {
	mu      sync.Mutex
	notices []string
}

func (s *scriptedContentSteerer) Steer(text string) {
	s.mu.Lock()
	s.notices = append(s.notices, text)
	s.mu.Unlock()
}

func (*scriptedContentSteerer) Start()                                     {}
func (*scriptedContentSteerer) Cancel()                                    {}
func (*scriptedContentSteerer) Done() <-chan struct{}                      { return nil }
func (*scriptedContentSteerer) OutputChannel() <-chan executor.OutputChunk { return nil }
func (*scriptedContentSteerer) EventChannel() <-chan executor.Event        { return nil }
func (*scriptedContentSteerer) ResultChannel() <-chan executor.Result      { return nil }

// scriptedNoticeSteerer is a runtime satisfying the machine router's steerer
// capability (the pi shape: Steer reports same-turn delivery success).
type scriptedNoticeSteerer struct {
	mu      sync.Mutex
	notices []string
}

func (s *scriptedNoticeSteerer) Steer(text string) error {
	s.mu.Lock()
	s.notices = append(s.notices, text)
	s.mu.Unlock()
	return nil
}

func (*scriptedNoticeSteerer) Start()                                     {}
func (*scriptedNoticeSteerer) Cancel()                                    {}
func (*scriptedNoticeSteerer) Done() <-chan struct{}                      { return nil }
func (*scriptedNoticeSteerer) OutputChannel() <-chan executor.OutputChunk { return nil }
func (*scriptedNoticeSteerer) EventChannel() <-chan executor.Event        { return nil }
func (*scriptedNoticeSteerer) ResultChannel() <-chan executor.Result      { return nil }

// newRoutedCommandStream wires a command stream under a machine client the way
// the runner does, so the machine-level control router can be exercised
// without a live process.
func newRoutedCommandStream(t *testing.T) (*MachineClient, *commandStream) {
	t.Helper()
	const agentName = "agents/a1"
	c := &MachineClient{machineID: "m1", runners: map[string]*agentRunner{}}
	r := &agentRunner{machine: c, agentName: agentName, agentID: bareAgentID(agentName)}
	cs := &commandStream{
		agentName: agentName,
		agentID:   bareAgentID(agentName),
		wakeCh:    make(chan struct{}, 1),
	}
	r.cs = cs
	c.runners[r.agentID] = r
	return c, cs
}

// TestAgentControlCancelScopesToInFlightCommand locks the command scoping of
// the machine-level cancel: a queued cancel names a command, and a turn that
// already moved on (or already ended) must not be killed by it.
func TestAgentControlCancelScopesToInFlightCommand(t *testing.T) {
	c, cs := newRoutedCommandStream(t)

	cancelled := newScriptedRuntime(func(_ *scriptedRuntime) {})
	cancelled.Start() // cancelCh closes on Cancel
	cs.setCurrentExecutor(cancelled)
	cs.setCurrentCommand("cmd-1")

	// A cancel for an older command must not touch the in-flight turn.
	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control:   &v1pb.AgentControlRequest_Cancel{Cancel: &v1pb.CancelMessage{CommandId: "cmd-old"}},
	})
	assert.Equal(t, int32(0), cancelled.cancelCount.Load(), "a cancel for another command must not kill the in-flight turn")

	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control:   &v1pb.AgentControlRequest_Cancel{Cancel: &v1pb.CancelMessage{CommandId: "cmd-1"}},
	})
	assert.Equal(t, int32(1), cancelled.cancelCount.Load(), "the in-flight command is cancelled")
}

// TestAgentControlSteerScopedAndWakeKicks verifies the steer scoping and the
// wake: a steer into a command that is no longer the in-flight one is ignored;
// a wake always kicks the drain loop.
func TestAgentControlSteerScopedAndWakeKicks(t *testing.T) {
	c, cs := newRoutedCommandStream(t)

	steerer := &scriptedContentSteerer{}
	cs.setCurrentExecutor(steerer)
	cs.setCurrentCommand("cmd-1")

	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control:   &v1pb.AgentControlRequest_Steer{Steer: &v1pb.SteerMessage{CommandId: "cmd-stale", Text: "late"}},
	})
	assert.Empty(t, steerer.notices, "a steer for another command must not reach the in-flight turn")

	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control:   &v1pb.AgentControlRequest_Steer{Steer: &v1pb.SteerMessage{CommandId: "cmd-1", Text: "follow-up"}},
	})
	require.Len(t, steerer.notices, 1)
	assert.Equal(t, "follow-up", steerer.notices[0])
}

// TestAgentControlWakeKicksAndSteersNotice verifies the wake control: it
// always kicks the drain loop, and a steerable in-flight runtime also gets the
// content-free inbox notice injected same-turn.
func TestAgentControlWakeKicksAndSteersNotice(t *testing.T) {
	c, cs := newRoutedCommandStream(t)

	steerer := &scriptedNoticeSteerer{}
	cs.setCurrentExecutor(steerer)

	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control:   &v1pb.AgentControlRequest_Wake{Wake: &v1pb.NewMessagesAvailable{ConversationIds: []string{"conv-1"}}},
	})
	select {
	case <-cs.wakeCh:
	default:
		t.Fatal("a wake control must kick the drain loop")
	}
	require.Len(t, steerer.notices, 1)
	assert.Contains(t, steerer.notices[0], "new messages arrived")
}

// TestAgentControlPromptNoticeQueuedWhenNotSteerable verifies the fallback
// path: a notice that cannot be steered into the in-flight turn is queued for
// the next drain turn (and the loop is woken), with no immediate ack.
func TestAgentControlPromptNoticeQueuedWhenNotSteerable(t *testing.T) {
	c, cs := newRoutedCommandStream(t)

	sent := make([]*v1pb.MachineStreamMessage, 0, 1)
	cs.sendMachine = func(m *v1pb.MachineStreamMessage) error {
		sent = append(sent, m)
		return nil
	}

	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control: &v1pb.AgentControlRequest_PromptNotice{PromptNotice: &v1pb.PromptReleaseNotice{
			NoticeKey:     "k1",
			Message:       "Your system prompt has been updated.",
			PromptVersion: "static1.dyn1",
		}},
	})

	notice := cs.takePendingPromptNotice()
	require.NotNil(t, notice, "non-steerable runtime must queue the notice for the next turn")
	assert.Equal(t, "k1", notice.GetNoticeKey())
	assert.Equal(t, "static1.dyn1", notice.GetPromptVersion())
	// The queued path does not ack immediately; runSession acks after injecting.
	assert.Empty(t, sent)
}

// TestAgentControlPromptNoticeAckCarriesAgentName verifies the same-turn steer
// path: a steerable runtime gets the notice injected and the ack travels on
// the machine control stream naming the agent.
func TestAgentControlPromptNoticeAckCarriesAgentName(t *testing.T) {
	c, cs := newRoutedCommandStream(t)

	var mu sync.Mutex
	var sent []*v1pb.MachineStreamMessage
	cs.sendMachine = func(m *v1pb.MachineStreamMessage) error {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
		return nil
	}
	steerer := &scriptedNoticeSteerer{}
	cs.setCurrentExecutor(steerer)

	c.handleAgentControl(&v1pb.AgentControlRequest{
		AgentName: "agents/a1",
		Control: &v1pb.AgentControlRequest_PromptNotice{PromptNotice: &v1pb.PromptReleaseNotice{
			NoticeKey:     "k1",
			Message:       "Your system prompt has been updated.",
			PromptVersion: "static1.dyn1",
		}},
	})

	require.Len(t, steerer.notices, 1)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	ack := sent[0].GetPromptReleaseNoticeAck()
	require.NotNil(t, ack)
	assert.Equal(t, "agents/a1", ack.GetAgentName(), "the ack must name the agent (one machine stream serves every hosted agent)")
	assert.Equal(t, "k1", ack.GetNoticeKey())
	assert.Equal(t, "static1.dyn1", ack.GetPromptVersion())
}

// TestAckPromptNoticeCarriesAgentName pins the runSession-side ack shape
// (notice injected at turn start, acked with the agent's resource name).
func TestAckPromptNoticeCarriesAgentName(t *testing.T) {
	cs := &commandStream{agentName: "agents/a1"}
	var mu sync.Mutex
	var sent []*v1pb.MachineStreamMessage
	cs.sendMachine = func(m *v1pb.MachineStreamMessage) error {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
		return nil
	}

	cs.ackPromptNotice(&v1pb.PromptReleaseNotice{NoticeKey: "k1", PromptVersion: "v1"})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	ack := sent[0].GetPromptReleaseNoticeAck()
	require.NotNil(t, ack)
	assert.Equal(t, "agents/a1", ack.GetAgentName())
	assert.Equal(t, "k1", ack.GetNoticeKey())
}

// TestAgentWorkspaceListRepliesOnMachineStream verifies the workspace listing
// round trip: the request is answered on the machine control stream, correlated
// by request id, from the named agent's workspace dir.
func TestAgentWorkspaceListRepliesOnMachineStream(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _ := newRoutedCommandStream(t)

	var mu sync.Mutex
	var sent []*v1pb.MachineStreamMessage
	c.setControlSend(func(m *v1pb.MachineStreamMessage) error {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
		return nil
	})

	c.handleAgentWorkspaceList(&v1pb.WorkspaceListRequest{
		RequestId: "req-1",
		AgentName: "agents/a1",
	})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	resp := sent[0].GetWorkspaceListResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "req-1", resp.GetRequestId())
}

// TestAgentWorkspaceReadRepliesOnMachineStream verifies the read round trip
// and the error-field refusals (a missing file reads as an error reply, not a
// failed stream send).
func TestAgentWorkspaceReadRepliesOnMachineStream(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _ := newRoutedCommandStream(t)

	var mu sync.Mutex
	var sent []*v1pb.MachineStreamMessage
	c.setControlSend(func(m *v1pb.MachineStreamMessage) error {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
		return nil
	})

	c.handleAgentWorkspaceRead(&v1pb.WorkspaceReadRequest{
		RequestId: "req-2",
		AgentName: "agents/a1",
		Path:      "missing.txt",
	})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	resp := sent[0].GetWorkspaceReadResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "req-2", resp.GetRequestId())
	assert.NotEmpty(t, resp.GetError(), "a missing file is refused in the error field")
}

// TestAgentControlForUnknownAgentIsIgnored verifies the router drops
// interactions for agents this machine is not hosting.
func TestAgentControlForUnknownAgentIsIgnored(t *testing.T) {
	c, _ := newRoutedCommandStream(t)

	require.NotPanics(t, func() {
		c.handleAgentControl(&v1pb.AgentControlRequest{
			AgentName: "agents/stranger",
			Control:   &v1pb.AgentControlRequest_Wake{Wake: &v1pb.NewMessagesAvailable{}},
		})
	})
}

// TestSendOnControlStreamWithoutConnectionErrors verifies the runner-side
// reply path degrades gracefully (an error, not a panic) when the control
// stream is down.
func TestSendOnControlStreamWithoutConnectionErrors(t *testing.T) {
	c := &MachineClient{}
	err := c.sendOnControlStream(&v1pb.MachineStreamMessage{})
	require.Error(t, err)
}

// TestSendOnControlStreamRoutesThroughInstalledSender verifies the
// runControlStream-installed send function is used (and cleared with it).
func TestSendOnControlStreamRoutesThroughInstalledSender(t *testing.T) {
	c := &MachineClient{}

	var mu sync.Mutex
	var sent int
	c.setControlSend(func(*v1pb.MachineStreamMessage) error {
		mu.Lock()
		sent++
		mu.Unlock()
		return nil
	})
	require.NoError(t, c.sendOnControlStream(&v1pb.MachineStreamMessage{}))
	mu.Lock()
	require.Equal(t, 1, sent)
	mu.Unlock()

	c.clearControlSend()
	require.Error(t, c.sendOnControlStream(&v1pb.MachineStreamMessage{}))
}

// TestTurnLocalEventSequences guards the per-turn seq spaces at the turn
// boundary: runSession scopes the current-command mark so the control router
// can scope interactions, and a completed turn's records all carry
// turn-local sequence numbers starting at 1.
func TestTurnLocalEventSequences(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	runtime := newScriptedRuntime(func(r *scriptedRuntime) {
		r.eventCh <- executor.Event{Type: v1pb.CommandEventType_WARNING, Summary: "w1", Warning: &v1pb.WarningPayload{Message: "one"}}
		close(r.outputCh)
		close(r.eventCh)
		r.resultCh <- executor.Result{ExitCode: 0, FinalSummary: "done"}
		close(r.resultCh)
		close(r.doneCh)
	})
	sink := newMemoryTurnSink()

	cs := &commandStream{machineID: "seq-m", agentID: "seq-a", sink: sink}
	cs.setCurrentCommand("cmd-t")
	result := cs.runCommand(context.Background(), runtime, sink, executor.Request{CommandID: "cmd-t"}, &executor.ContextState{})
	require.NotNil(t, result)

	// lifecycle=1, forwarded warning=2, result envelope seq=1 (per-kind space).
	require.Equal(t, int32(1), sink.Entries()[0].GetEvent().GetSeqNo())
	require.Equal(t, int32(2), sink.Entries()[1].GetEvent().GetSeqNo())
	require.Equal(t, int32(1), sink.Entries()[2].GetSeqNo(), "the result envelope keeps its own seq")

	// The command mark is runSession-owned: runCommand leaves it alone, so the
	// control router's scoping window covers the whole turn.
	assert.Equal(t, "cmd-t", cs.currentCommand())
}

// TestMachineMetricsRequestRepliesWithPayload verifies the §8.2 scrape round
// trip: a MachineMetricsRequest is answered on the machine control stream with
// a Prometheus text payload rendered from the machine's local registry.
func TestMachineMetricsRequestRepliesWithPayload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _ := newRoutedCommandStream(t)

	// Touch one machine-local metric deterministically (an agent outbox's
	// flush-now counter) so the rendered payload provably carries the laelia
	// series and not just the Go runtime's.
	ob, err := outbox.Open(outbox.AgentOutboxDir(home.Dir(), c.machineID, "a1"))
	require.NoError(t, err)
	defer ob.Close()
	outbox.NewUploader(ob, func(_ context.Context, _ []*outbox.Entry) (*v1pb.UploadCommandDataResponse, error) {
		return &v1pb.UploadCommandDataResponse{}, nil
	}).FlushNow()

	var mu sync.Mutex
	var sent []*v1pb.MachineStreamMessage
	c.setControlSend(func(m *v1pb.MachineStreamMessage) error {
		mu.Lock()
		sent = append(sent, m)
		mu.Unlock()
		return nil
	})

	c.handleMachineMetrics(func(m *v1pb.MachineStreamMessage) error {
		return c.sendOnControlStream(m)
	}, &v1pb.MachineMetricsRequest{RequestId: "req-metrics"})

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, sent, 1)
	resp := sent[0].GetMachineMetricsResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "req-metrics", resp.GetRequestId())
	assert.Empty(t, resp.GetError())
	assert.Contains(t, resp.GetPayload(), "laelia_upload_flush_now_total",
		"the payload must render the machine's local metrics in text exposition format")
}
