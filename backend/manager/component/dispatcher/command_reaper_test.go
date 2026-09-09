package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestShouldReapCommand locks in the zombie-RUNNING-row decision: the drain
// loop is strictly serial per agent, so a command is only alive while its
// session tracks it as the current in-flight command and it has not outlived
// the reap threshold. Everything else can never receive a result.
func TestShouldReapCommand(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	old := time.Now().Add(-staleCommandReapAfter - time.Minute)

	t.Run("fresh command is kept", func(t *testing.T) {
		cmd := &store.CommandMessage{ID: uuid.New(), AgentID: 1, CreatedAt: old}
		require.False(t, d.shouldReapCommand(cmd, old.Add(staleCommandReapAfter-time.Second)))
	})

	t.Run("old command with no live session is reaped", func(t *testing.T) {
		cmd := &store.CommandMessage{ID: uuid.New(), AgentID: 1, CreatedAt: old}
		require.True(t, d.shouldReapCommand(cmd, time.Now()))
	})

	t.Run("old command still tracked as current is kept", func(t *testing.T) {
		// A legitimately long turn stays RUNNING no matter how silent: the
		// session's currentCmdID exempts it from the age threshold.
		d.RegisterAgent(context.Background(), 2, 1, "agents/a2", func(*v1pb.ManagerStreamMessage) error { return nil })
		defer d.UnregisterAgent(2)

		sess, ok := d.registry.getAgent(2)
		require.True(t, ok)
		cmd := &store.CommandMessage{ID: uuid.New(), AgentID: 2, CreatedAt: old}
		sess.mu.Lock()
		sess.currentCmdID = cmd.ID.String()
		sess.mu.Unlock()

		require.False(t, d.shouldReapCommand(cmd, time.Now()))
	})

	t.Run("old command superseded in a live session is reaped", func(t *testing.T) {
		// The reconnect-then-idle zombie: the session came back and tracks a
		// different (or no) command, so the orphaned RUNNING row can never be
		// resolved by the client and must be reaped.
		d.RegisterAgent(context.Background(), 3, 1, "agents/a3", func(*v1pb.ManagerStreamMessage) error { return nil })
		defer d.UnregisterAgent(3)

		cmd := &store.CommandMessage{ID: uuid.New(), AgentID: 3, CreatedAt: old}
		require.True(t, d.shouldReapCommand(cmd, time.Now()), "empty currentCmdID must reap the orphan")

		sess, ok := d.registry.getAgent(3)
		require.True(t, ok)
		other := uuid.New()
		sess.mu.Lock()
		sess.currentCmdID = other.String()
		sess.mu.Unlock()
		require.True(t, d.shouldReapCommand(cmd, time.Now()), "a session tracking another command must reap the orphan")
	})
}

// TestPickResumeCommand locks in the BeginSession resume decision: only an
// enabled, drain-capable agent continues an interrupted turn, and the resume
// target is the newest RUNNING row.
func TestPickResumeCommand(t *testing.T) {
	capable := func() *store.AgentMessage {
		return &store.AgentMessage{Enabled: true, Info: &models.AgentInfo{
			Capability: &models.AgentCapability{SupportsPi: true},
		}}
	}
	disabled := &store.AgentMessage{Enabled: false, Info: &models.AgentInfo{
		Capability: &models.AgentCapability{SupportsPi: true},
	}}
	incapable := &store.AgentMessage{Enabled: true, Info: &models.AgentInfo{}}
	running := []*store.CommandMessage{{ID: uuid.New()}, {ID: uuid.New()}}

	require.Equal(t, running[0], pickResumeCommand(capable(), running), "the newest RUNNING row is the resume target")
	require.Nil(t, pickResumeCommand(disabled, running), "a stopped agent cannot resume an interrupted turn")
	require.Nil(t, pickResumeCommand(incapable, running), "an agent with no drain runtime cannot resume")
	require.Nil(t, pickResumeCommand(nil, running))
}
