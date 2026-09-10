package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestPendingReplies_Cancel(t *testing.T) {
	p := newPendingReplies[*v1pb.WorkspaceReadResponse]()
	reqID := "req-cancel"

	ch := p.register(reqID)
	p.cancel(reqID)

	// A late completion after cancel must be a no-op and must not deliver.
	p.complete(reqID, &v1pb.WorkspaceReadResponse{RequestId: reqID})

	select {
	case got := <-ch:
		t.Fatalf("expected no delivery after cancel, got %v", got)
	default:
	}
}

func TestPendingReplies_CompleteTwice(t *testing.T) {
	p := newPendingReplies[*v1pb.WorkspaceListResponse]()
	reqID := "req-twice"

	ch := p.register(reqID)
	p.complete(reqID, &v1pb.WorkspaceListResponse{RequestId: reqID})
	// Second completion must be a no-op; the channel still has only one value.
	p.complete(reqID, &v1pb.WorkspaceListResponse{RequestId: reqID})

	select {
	case <-ch:
	default:
		t.Fatal("expected exactly one delivered reply")
	}
	select {
	case got := <-ch:
		t.Fatalf("expected no second delivery, got %v", got)
	default:
	}
}

func TestDispatcher_PendingDiscoverUsesGenericReplies(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	reqID := "discover-1"
	ch := d.RegisterPendingDiscover(reqID)
	require.NotNil(t, ch)

	msg := &v1pb.ProvidersDiscovered{RequestId: reqID}
	d.CompletePendingDiscover(msg)

	select {
	case got := <-ch:
		require.Same(t, msg, got)
	case <-time.After(time.Second):
		t.Fatal("pending discover was not delivered")
	}

	d.CancelPendingDiscover(reqID) // must be safe after completion
}

func TestDispatcher_PendingModelsRoundTrip(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	reqID := "models-1"
	ch := d.RegisterPendingModels(reqID)
	require.NotNil(t, ch)

	msg := &v1pb.ModelsDiscovered{RequestId: reqID, Provider: "codex", Models: []*v1pb.AgentModelOption{{Value: "gpt-5.2-codex"}}}
	d.CompletePendingModels(msg)

	select {
	case got := <-ch:
		require.Same(t, msg, got)
	case <-time.After(time.Second):
		t.Fatal("pending models was not delivered")
	}

	d.CancelPendingModels(reqID) // must be safe after completion
}

func TestDispatcher_SendMethodsReturnErrorWhenOffline(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	require.Error(t, d.SendAgentAssignment(1, &v1pb.AgentAssignment{}))
	require.Error(t, d.SendDiscoverProviders(2, "req"))
	require.Error(t, d.SendWorkspaceListRequest(2, "req", "/", false))
	require.Error(t, d.SendMachineWorkspaceScan(1, "req"))
	require.Error(t, d.SendRestartAgent(1, "a1"))
}

func TestDispatcher_MachineSendMethods(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	received := make([]*v1pb.ManagerMachineStreamMessage, 0)
	d.RegisterMachine(1, "machines/m1", func(msg *v1pb.ManagerMachineStreamMessage) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	})

	require.NoError(t, d.SendAgentAssignment(1, &v1pb.AgentAssignment{AgentName: "a1"}))
	require.NoError(t, d.SendAgentConfigUpdate(1, "a1", nil))
	require.NoError(t, d.SendRemoveAgent(1, "a1"))
	require.NoError(t, d.SendReloadAgentAssignment(1, &v1pb.ReloadAgentAssignment{}))
	require.NoError(t, d.SendRestartAgent(1, "a1"))
	require.NoError(t, d.SendDiscoverProvidersToMachine(1, "req-m"))
	require.NoError(t, d.SendPongToMachine(1))
	require.NoError(t, d.SendUpgradeRequest(1, &v1pb.UpgradeRequest{}))
	require.NoError(t, d.SendDeleteAgentWorkspace(1, "a1"))
	require.NoError(t, d.SendDiscoverModelsToMachine(1, "codex", map[string]string{"CODEX_HOME": "/tmp/cx"}, "req-models"))

	mu.Lock()
	require.Len(t, received, 10)
	mu.Unlock()

	// The DiscoverModels message must carry the provider + env overlay.
	dm := received[9].GetDiscoverModels()
	require.NotNil(t, dm)
	require.Equal(t, "req-models", dm.GetRequestId())
	require.Equal(t, "codex", dm.GetProvider())
	require.Equal(t, "/tmp/cx", dm.GetEnv()["CODEX_HOME"])
}

func TestDispatcher_AgentSendMethods(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	received := make([]*v1pb.ManagerMachineStreamMessage, 0)
	d.RegisterMachine(1, "machines/m1", func(msg *v1pb.ManagerMachineStreamMessage) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	})
	// The per-agent session registry is retired: the agent→machine binding is
	// resolved from the store, so the routing half is exercised directly with
	// a loaded agent.
	agent := &store.AgentMessage{ID: 2, MachineID: 1, ResourceID: "a2"}

	require.NoError(t, d.sendWorkspaceListRequestTo(agent, "req-list", "/tmp", true))
	require.NoError(t, d.sendWorkspaceReadRequestTo(agent, "req-read", "/tmp/a.txt"))

	mu.Lock()
	require.Len(t, received, 2)
	mu.Unlock()

	lr, ok := received[0].Message.(*v1pb.ManagerMachineStreamMessage_WorkspaceListRequest)
	require.True(t, ok)
	require.Equal(t, "req-list", lr.WorkspaceListRequest.GetRequestId())
	require.Equal(t, "/tmp", lr.WorkspaceListRequest.GetDirPath())
	require.Equal(t, true, lr.WorkspaceListRequest.GetIncludeHidden())
	require.Equal(t, "agents/a2", lr.WorkspaceListRequest.GetAgentName(),
		"the machine stream serves every hosted agent, so requests carry agent_name")

	rr, ok := received[1].Message.(*v1pb.ManagerMachineStreamMessage_WorkspaceReadRequest)
	require.True(t, ok)
	require.Equal(t, "req-read", rr.WorkspaceReadRequest.GetRequestId())
	require.Equal(t, "agents/a2", rr.WorkspaceReadRequest.GetAgentName())
}

func TestDispatcher_WatcherBroadcastAndUnsubscribe(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	cmdID := "cmd-1"
	ch, err := d.Subscribe(context.Background(), cmdID)
	require.NoError(t, err)
	defer d.Unsubscribe(cmdID, ch)

	output := &v1pb.CommandOutput{
		CommandId: cmdID,
		SeqNo:     1,
		Content:   "hello",
		Timestamp: timestamppb.Now(),
	}
	d.broadcast(cmdID, output)

	select {
	case got := <-ch:
		require.Same(t, output, got)
	case <-time.After(time.Second):
		t.Fatal("broadcast output was not received")
	}

	d.Unsubscribe(cmdID, ch)
	// After unsubscribe the channel is closed.
	_, ok := <-ch
	require.False(t, ok, "channel should be closed after Unsubscribe")
}

func TestDispatcher_WatcherEventBroadcast(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	cmdID := "cmd-evt"
	ch, err := d.SubscribeEvents(context.Background(), cmdID)
	require.NoError(t, err)
	defer d.UnsubscribeEvents(cmdID, ch)

	event := &v1pb.CommandEvent{
		CommandId: cmdID,
		SeqNo:     1,
		Type:      v1pb.CommandEventType_TEXT_DELTA,
	}
	d.broadcastEvent(cmdID, event)

	select {
	case got := <-ch:
		require.Same(t, event, got)
	case <-time.After(time.Second):
		t.Fatal("broadcast event was not received")
	}
}

func TestDispatcher_SessionLifecycle(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	require.False(t, d.IsMachineConnected(1))

	d.RegisterMachine(1, "machines/m1", func(*v1pb.ManagerMachineStreamMessage) error { return nil })

	require.True(t, d.IsMachineConnected(1))

	d.UnregisterMachine(1)
	require.False(t, d.IsMachineConnected(1))
}

func TestDispatcher_UnregisterMachineIfDoesNotDeleteReplacement(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	old := d.RegisterMachine(1, "machines/m1", func(*v1pb.ManagerMachineStreamMessage) error { return nil })
	replacement := d.RegisterMachine(1, "machines/m1", func(*v1pb.ManagerMachineStreamMessage) error { return nil })

	d.UnregisterMachineIf(1, old)

	require.True(t, d.IsMachineConnected(1), "old machine session teardown must not remove the replacement")
	require.Same(t, replacement, d.registry.machines[1])
}

// agentControlRouting exercises the per-agent control push shape against a
// registered machine session (the routing half; the agent→machine binding is
// resolved from the store in production). Returns the delivered control
// request with the agent_name already filled.
func agentControlRouting(t *testing.T, push func(d *Dispatcher, agent *store.AgentMessage) error) *v1pb.AgentControlRequest {
	t.Helper()
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	var got *v1pb.ManagerMachineStreamMessage
	d.RegisterMachine(1, "machines/m1", func(msg *v1pb.ManagerMachineStreamMessage) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		return nil
	})
	agent := &store.AgentMessage{ID: 1, MachineID: 1, ResourceID: "a1"}
	require.NoError(t, push(d, agent))
	mu.Lock()
	defer mu.Unlock()
	ac, ok := got.Message.(*v1pb.ManagerMachineStreamMessage_AgentControl)
	require.True(t, ok, "agent control interactions travel on the machine control stream")
	require.Equal(t, "agents/a1", ac.AgentControl.GetAgentName())
	return ac.AgentControl
}

func TestDispatcher_NotifyShapes(t *testing.T) {
	// NewMessagesAvailable wake with payload.
	ac := agentControlRouting(t, func(d *Dispatcher, agent *store.AgentMessage) error {
		return d.sendAgentControl(agent, &v1pb.AgentControlRequest{
			Control: &v1pb.AgentControlRequest_Wake{
				Wake: &v1pb.NewMessagesAvailable{
					ConversationIds: []string{"conversations/c1"},
					Versions:        []int64{7},
				},
			},
		})
	})
	require.Equal(t, []string{"conversations/c1"}, ac.GetWake().GetConversationIds())
	require.Equal(t, []int64{7}, ac.GetWake().GetVersions())

	// Empty wake (the NotifyWake "check for work" tick).
	ac = agentControlRouting(t, func(d *Dispatcher, agent *store.AgentMessage) error {
		return d.sendAgentControl(agent, &v1pb.AgentControlRequest{
			Control: &v1pb.AgentControlRequest_Wake{Wake: &v1pb.NewMessagesAvailable{}},
		})
	})
	require.Empty(t, ac.GetWake().GetConversationIds())

	// Thread mention carries the thread root id.
	ac = agentControlRouting(t, func(d *Dispatcher, agent *store.AgentMessage) error {
		return d.sendAgentControl(agent, &v1pb.AgentControlRequest{
			Control: &v1pb.AgentControlRequest_Wake{
				Wake: &v1pb.NewMessagesAvailable{ThreadRootMessageId: "thread-1"},
			},
		})
	})
	require.Equal(t, "thread-1", ac.GetWake().GetThreadRootMessageId())
}

func TestDispatcher_CancelCommandShape(t *testing.T) {
	cmdID := uuid.NewString()
	ac := agentControlRouting(t, func(d *Dispatcher, agent *store.AgentMessage) error {
		return d.sendAgentControl(agent, agentControlRequest(store.ControlKindCancel, mustParseUUID(t, cmdID), ""))
	})
	require.Equal(t, cmdID, ac.GetCancel().GetCommandId())
}

func TestDispatcher_SteerCommandShape(t *testing.T) {
	cmdID := uuid.NewString()
	ac := agentControlRouting(t, func(d *Dispatcher, agent *store.AgentMessage) error {
		return d.sendAgentControl(agent, agentControlRequest(store.ControlKindSteer, mustParseUUID(t, cmdID), "continue"))
	})
	require.Equal(t, cmdID, ac.GetSteer().GetCommandId())
	require.Equal(t, "continue", ac.GetSteer().GetText())
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}

func TestDispatcher_MachineUpgradeStatus(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	require.Nil(t, d.MachineUpgradeStatus(1))
	progress := &v1pb.UpgradeProgress{Version: "v1.2.3", Stage: "downloading"}
	d.RecordMachineUpgrade(1, progress)
	require.Same(t, progress, d.MachineUpgradeStatus(1))
}

func TestWatcherDrop(t *testing.T) {
	var w watcher[int]
	n, log := w.drop()
	require.Equal(t, int64(1), n)
	require.True(t, log, "first drop should log")

	n, log = w.drop()
	require.Equal(t, int64(2), n)
	require.True(t, log, "second drop should log (power of two)")

	n, log = w.drop()
	require.Equal(t, int64(3), n)
	require.False(t, log, "third drop should not log")
}

func TestHandleMachinePing(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	sess := d.RegisterMachine(1, "machines/m1", func(*v1pb.ManagerMachineStreamMessage) error { return nil })
	before := sess.lastPingAt
	time.Sleep(time.Millisecond)
	d.HandleMachinePing(1, &v1pb.Ping{})
	require.True(t, sess.lastPingAt.After(before))
}
