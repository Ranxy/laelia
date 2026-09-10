package dispatcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestMachineLostDecision locks in the reaper's dual-signal judgment: a
// machine's RUNNING commands are only reapable when BOTH signals agree the
// machine is gone — its MachineChannel is unregistered in this process AND its
// persisted heartbeat (machine.status.last_heartbeat_at, refreshed every 30s
// by MachineHeartbeat) expired more than the grace period ago. Either signal
// alone is spurious: the stream drops for minutes while a live machine keeps
// heartbeating, and the in-memory registry lags a manager restart.
func TestMachineLostDecision(t *testing.T) {
	now := time.Now()
	graceSecs := int64(reaperGrace.Seconds())
	online := func(heartbeatAgeSecs int64) *store.MachineMessage {
		return &store.MachineMessage{Status: &storepb.MachineStatus{
			State:           storepb.MachineStatus_ONLINE,
			LastHeartbeatAt: now.Unix() - heartbeatAgeSecs,
		}}
	}

	require.True(t, machineLost(now, nil), "an unknown machine row is lost")
	require.True(t, machineLost(now, &store.MachineMessage{Deleted: true, Status: &storepb.MachineStatus{
		State: storepb.MachineStatus_ONLINE, LastHeartbeatAt: now.Unix(),
	}}), "a deleted machine is lost")

	require.False(t, machineLost(now, online(0)), "a fresh heartbeat keeps the commands")
	require.False(t, machineLost(now, online(graceSecs-30)), "inside the grace window the commands stay")
	require.True(t, machineLost(now, online(graceSecs)), "an expired heartbeat past the grace reaps")
	require.True(t, machineLost(now, &store.MachineMessage{Status: &storepb.MachineStatus{
		State: storepb.MachineStatus_OFFLINE, LastHeartbeatAt: now.Unix(),
	}}), "a machine that is not ONLINE is lost however fresh its heartbeat")
	require.True(t, machineLost(now, &store.MachineMessage{Status: nil}), "no persisted status is loss")
}

// TestReapGraceHardConstraint pins the design's hard constraint: the grace
// must be at least twice the machine's reconnect backoff ceiling (the agent
// client's defaultRetryMaxWait, 1 minute) so a flapping machine — a proxy
// killing long streams on a short period — is never reaped mid-reconnect.
func TestReapGraceHardConstraint(t *testing.T) {
	require.GreaterOrEqual(t, reaperGrace, 2*time.Minute)
}
