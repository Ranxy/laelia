package v1

import (
	"context"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/presence"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func newPresenceService() *CommandService {
	return &CommandService{presence: presence.New()}
}

func TestSyncPresenceRecordsCallerHeartbeat(t *testing.T) {
	s := newPresenceService()
	user := &store.UserMessage{Handle: "ran-user-1"}
	ctx := withUser(context.Background(), user)

	_, err := s.SyncPresence(ctx, connect.NewRequest(&v1pb.SyncPresenceRequest{}))
	require.NoError(t, err)

	// The caller's own heartbeat must land in the registry under its resource
	// name ("users/<handle>"), so a follow-up query reports it online.
	res, err := s.SyncPresence(ctx, connect.NewRequest(&v1pb.SyncPresenceRequest{
		Names: []string{"users/ran-user-1"},
	}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Presences, 1)
	assert.Equal(t, "users/ran-user-1", res.Msg.Presences[0].Name)
	assert.True(t, res.Msg.Presences[0].Online)
}

func TestSyncPresenceAgentCallerHeartbeat(t *testing.T) {
	s := newPresenceService()
	agent := &store.AgentMessage{ResourceID: "agents/rei-agent-1"}
	ctx := withAgent(context.Background(), agent)

	_, err := s.SyncPresence(ctx, connect.NewRequest(&v1pb.SyncPresenceRequest{}))
	require.NoError(t, err)

	// The agent's heartbeat is recorded, but agents are never answered online:
	// agent presence comes from ListAgents' connection state, not this registry.
	res, err := s.SyncPresence(ctx, connect.NewRequest(&v1pb.SyncPresenceRequest{
		Names: []string{"agents/rei-agent-1"},
	}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Presences, 1)
	assert.False(t, res.Msg.Presences[0].Online)
}

func TestSyncPresenceTTLExpiry(t *testing.T) {
	// A heartbeat far outside the window answers offline; an untouched name
	// answers offline too. The handler reads time.Now, so the stale beat is
	// primed directly on the registry.
	s := newPresenceService()
	s.presence.Touch("users/stale-beat", time.Now().Add(-2*presence.DefaultTTL))

	res, err := s.SyncPresence(context.Background(), connect.NewRequest(&v1pb.SyncPresenceRequest{
		Names: []string{"users/stale-beat", "users/ghost"},
	}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Presences, 2)
	byName := make(map[string]bool, 2)
	for _, p := range res.Msg.Presences {
		byName[p.Name] = p.Online
	}
	assert.False(t, byName["users/stale-beat"])
	assert.False(t, byName["users/ghost"])
}

func TestSyncPresenceDedupesAndSkipsEmpty(t *testing.T) {
	s := newPresenceService()
	s.presence.Touch("users/alice", time.Now())

	res, err := s.SyncPresence(context.Background(), connect.NewRequest(&v1pb.SyncPresenceRequest{
		Names: []string{"", "users/alice", "users/alice"},
	}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Presences, 1)
	assert.Equal(t, "users/alice", res.Msg.Presences[0].Name)
	assert.True(t, res.Msg.Presences[0].Online)
}

func TestSyncPresenceRejectsTooManyNames(t *testing.T) {
	s := newPresenceService()
	names := make([]string, 0, 201)
	for i := 0; i <= 200; i++ {
		names = append(names, "users/user-"+strconv.Itoa(i))
	}
	_, err := s.SyncPresence(context.Background(), connect.NewRequest(&v1pb.SyncPresenceRequest{Names: names}))
	assert.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestSyncPresenceWithoutCaller(t *testing.T) {
	// No user/agent in context: the RPC still answers queries, it just records
	// no heartbeat.
	s := newPresenceService()
	res, err := s.SyncPresence(context.Background(), connect.NewRequest(&v1pb.SyncPresenceRequest{
		Names: []string{"users/anyone"},
	}))
	require.NoError(t, err)
	require.Len(t, res.Msg.Presences, 1)
	assert.False(t, res.Msg.Presences[0].Online)
}
