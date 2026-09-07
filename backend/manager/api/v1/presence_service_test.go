package v1

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// fakePresenceStore records Touch calls and serves a fixed ListPresence
// result, standing in for the DB-backed store hermetically.
type fakePresenceStore struct {
	touched    []string
	touchErr   error
	listResult []store.PresenceRow
	listErr    error
}

func (f *fakePresenceStore) TouchPresence(_ context.Context, handle string, _ time.Time) error {
	if f.touchErr != nil {
		return f.touchErr
	}
	f.touched = append(f.touched, handle)
	return nil
}

func (f *fakePresenceStore) ListPresence(_ context.Context) ([]store.PresenceRow, error) {
	return f.listResult, f.listErr
}

func TestSendHeartbeatRecordsCallerHandle(t *testing.T) {
	s := NewPresenceService(&fakePresenceStore{})
	user := &store.UserMessage{Handle: "ran-user-1"}
	ctx := withUser(context.Background(), user)

	_, err := s.SendHeartbeat(ctx, connect.NewRequest(&v1pb.SendHeartbeatRequest{}))
	require.NoError(t, err)
	assert.Equal(t, []string{"ran-user-1"}, s.store.(*fakePresenceStore).touched)
}

func TestSendHeartbeatAgentCallerNoOp(t *testing.T) {
	s := NewPresenceService(&fakePresenceStore{})
	agent := &store.AgentMessage{}
	ctx := withAgent(context.Background(), agent)

	_, err := s.SendHeartbeat(ctx, connect.NewRequest(&v1pb.SendHeartbeatRequest{}))
	require.NoError(t, err)
	// Agents are not tracked here: their connection state lives in
	// AgentService, so an agent heartbeat must not touch the store.
	assert.Empty(t, s.store.(*fakePresenceStore).touched)
}

func TestSendHeartbeatWithoutCaller(t *testing.T) {
	s := NewPresenceService(&fakePresenceStore{})

	_, err := s.SendHeartbeat(context.Background(), connect.NewRequest(&v1pb.SendHeartbeatRequest{}))
	require.NoError(t, err)
	assert.Empty(t, s.store.(*fakePresenceStore).touched)
}

func TestSendHeartbeatStoreError(t *testing.T) {
	s := NewPresenceService(&fakePresenceStore{touchErr: errors.New("db down")})
	ctx := withUser(context.Background(), &store.UserMessage{Handle: "ran-user-1"})

	_, err := s.SendHeartbeat(ctx, connect.NewRequest(&v1pb.SendHeartbeatRequest{}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestListPresenceMapsRows(t *testing.T) {
	now := time.Now()
	s := NewPresenceService(&fakePresenceStore{listResult: []store.PresenceRow{
		{Handle: "zoe", LastSeenAt: now.Add(-time.Second)},
		{Handle: "alice", LastSeenAt: now.Add(-2 * store.PresenceTTL)}, // expired → offline
		{Handle: "bob", LastSeenAt: now.Add(-time.Second)},
	}})

	res, err := s.ListPresence(context.Background(), connect.NewRequest(&v1pb.ListPresenceRequest{}))
	require.NoError(t, err)

	require.Len(t, res.Msg.Presences, 3)
	// Rows come back sorted by handle for deterministic responses.
	assert.Equal(t, "users/alice", res.Msg.Presences[0].Name)
	assert.False(t, res.Msg.Presences[0].Online)
	assert.True(t, res.Msg.Presences[1].LastSeenAt != nil)
	assert.Equal(t, "users/bob", res.Msg.Presences[1].Name)
	assert.True(t, res.Msg.Presences[1].Online)
	assert.Equal(t, "users/zoe", res.Msg.Presences[2].Name)
	assert.True(t, res.Msg.Presences[2].Online)
}

func TestListPresenceStoreError(t *testing.T) {
	s := NewPresenceService(&fakePresenceStore{listErr: errors.New("db down")})

	_, err := s.ListPresence(context.Background(), connect.NewRequest(&v1pb.ListPresenceRequest{}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestConvertToV1PresencesCap(t *testing.T) {
	now := time.Now()
	rows := make([]store.PresenceRow, 0, maxPresenceResults+1)
	for i := 0; i <= maxPresenceResults; i++ {
		rows = append(rows, store.PresenceRow{Handle: string(rune('a'+i%26)) + time.Duration(i).String(), LastSeenAt: now})
	}

	got := convertToV1Presences(rows, now)
	assert.Len(t, got, maxPresenceResults)
}
