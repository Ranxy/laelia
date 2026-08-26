package dispatcher

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
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
	received := make([]*v1pb.ManagerStreamMessage, 0)
	d.RegisterAgent(context.Background(), 2, 0, "agents/a2", func(msg *v1pb.ManagerStreamMessage) error {
		mu.Lock()
		received = append(received, msg)
		mu.Unlock()
		return nil
	})

	require.NoError(t, d.SendDiscoverProviders(2, "req-discover"))
	require.NoError(t, d.SendWorkspaceListRequest(2, "req-list", "/tmp", true))
	require.NoError(t, d.SendWorkspaceReadRequest(2, "req-read", "/tmp/a.txt"))

	mu.Lock()
	require.Len(t, received, 3)
	mu.Unlock()
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

	require.False(t, d.IsAgentConnected(1))
	require.False(t, d.IsMachineConnected(1))

	d.RegisterAgent(context.Background(), 1, 10, "agents/a1", noopSend)
	d.RegisterMachine(1, "machines/m1", func(*v1pb.ManagerMachineStreamMessage) error { return nil })

	require.True(t, d.IsAgentConnected(1))
	require.True(t, d.IsMachineConnected(1))

	d.UnregisterAgent(1)
	d.UnregisterMachine(1)
	require.False(t, d.IsAgentConnected(1))
	require.False(t, d.IsMachineConnected(1))
}

func TestDispatcher_UnregisterMachineDetachesAgents(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	d.RegisterAgent(context.Background(), 1, 10, "agents/a1", noopSend)
	d.RegisterAgent(context.Background(), 2, 10, "agents/a2", noopSend)
	d.RegisterMachine(10, "machines/m10", func(*v1pb.ManagerMachineStreamMessage) error { return nil })

	require.True(t, d.IsAgentConnected(1))
	require.True(t, d.IsAgentConnected(2))

	d.UnregisterMachine(10)

	require.False(t, d.IsAgentConnected(1))
	require.False(t, d.IsAgentConnected(2))
	require.False(t, d.IsMachineConnected(10))
}

func TestDispatcher_NotifyNewMessages(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	var got *v1pb.ManagerStreamMessage
	d.RegisterAgent(context.Background(), 1, 0, "agents/a1", func(msg *v1pb.ManagerStreamMessage) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		return nil
	})

	d.NotifyNewMessages(context.Background(), 1, "conversations/c1", 7)

	mu.Lock()
	require.NotNil(t, got)
	nm, ok := got.Message.(*v1pb.ManagerStreamMessage_NewMessages)
	require.True(t, ok)
	require.Equal(t, []string{"conversations/c1"}, nm.NewMessages.ConversationIds)
	require.Equal(t, []int64{7}, nm.NewMessages.Versions)
	mu.Unlock()
}

func TestDispatcher_NotifyWake(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	var got *v1pb.ManagerStreamMessage
	d.RegisterAgent(context.Background(), 1, 0, "agents/a1", func(msg *v1pb.ManagerStreamMessage) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		return nil
	})

	d.NotifyWake(context.Background(), 1)

	mu.Lock()
	require.NotNil(t, got)
	nm, ok := got.Message.(*v1pb.ManagerStreamMessage_NewMessages)
	require.True(t, ok)
	require.Empty(t, nm.NewMessages.ConversationIds)
	mu.Unlock()
}

func TestDispatcher_NotifyThreadMention(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	var got *v1pb.ManagerStreamMessage
	d.RegisterAgent(context.Background(), 1, 0, "agents/a1", func(msg *v1pb.ManagerStreamMessage) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		return nil
	})

	d.NotifyThreadMention(context.Background(), 1, "conversations/c1", 9, "thread-1")

	mu.Lock()
	require.NotNil(t, got)
	nm, ok := got.Message.(*v1pb.ManagerStreamMessage_NewMessages)
	require.True(t, ok)
	require.Equal(t, "thread-1", nm.NewMessages.ThreadRootMessageId)
	mu.Unlock()
}

func TestDispatcher_CancelCommand(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	var got *v1pb.ManagerStreamMessage
	d.RegisterAgent(context.Background(), 1, 0, "agents/a1", func(msg *v1pb.ManagerStreamMessage) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		return nil
	})

	require.NoError(t, d.CancelCommand(context.Background(), 1, "cmd-1"))
	mu.Lock()
	require.NotNil(t, got)
	c, ok := got.Message.(*v1pb.ManagerStreamMessage_Cancel)
	require.True(t, ok)
	require.Equal(t, "cmd-1", c.Cancel.CommandId)
	mu.Unlock()
}

func TestDispatcher_SteerCommand(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	var mu sync.Mutex
	var got *v1pb.ManagerStreamMessage
	d.RegisterAgent(context.Background(), 1, 0, "agents/a1", func(msg *v1pb.ManagerStreamMessage) error {
		mu.Lock()
		got = msg
		mu.Unlock()
		return nil
	})

	require.NoError(t, d.SteerCommand(context.Background(), 1, "cmd-1", "continue"))
	mu.Lock()
	require.NotNil(t, got)
	s, ok := got.Message.(*v1pb.ManagerStreamMessage_Steer)
	require.True(t, ok)
	require.Equal(t, "cmd-1", s.Steer.CommandId)
	require.Equal(t, "continue", s.Steer.Text)
	mu.Unlock()
}

func TestDispatcher_UnregisterAgentIfDoesNotDeleteReplacement(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	old := d.RegisterAgent(context.Background(), 1, 0, "agents/a1", noopSend)
	replacement := d.RegisterAgent(context.Background(), 1, 0, "agents/a1", noopSend)

	d.UnregisterAgentIf(1, old)

	require.True(t, d.IsAgentConnected(1), "old session teardown must not remove the replacement")
	require.Same(t, replacement, d.registry.sessions[1])
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

func TestDispatcher_SendAfterUnregisterReturnsError(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	d.RegisterAgent(context.Background(), 1, 0, "agents/a1", noopSend)
	d.UnregisterAgent(1)

	require.Error(t, d.SendDiscoverProviders(1, "req"))

	d.RegisterMachine(1, "machines/m1", func(*v1pb.ManagerMachineStreamMessage) error { return nil })
	d.UnregisterMachine(1)

	require.Error(t, d.SendAgentAssignment(1, &v1pb.AgentAssignment{}))
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

func TestAgentSessionClearCurrentCommand(t *testing.T) {
	sess := &AgentSession{agentID: 1, currentCmdID: "cmd-1"}
	sess.ClearCurrentCommand("other")
	require.Equal(t, "cmd-1", sess.currentCmdID)
	sess.ClearCurrentCommand("cmd-1")
	require.Empty(t, sess.currentCmdID)
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
