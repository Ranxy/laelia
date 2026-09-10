package dispatcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestDispatcher_PendingMetricsRoundTrip locks the GetMachineMetrics round
// trip: a registered pending entry is delivered by CompletePendingMetrics and
// cancel-after-completion is a no-op.
func TestDispatcher_PendingMetricsRoundTrip(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	reqID := "metrics-1"
	ch := d.RegisterPendingMetrics(reqID)
	require.NotNil(t, ch)

	msg := &v1pb.MachineMetricsResponse{RequestId: reqID, Payload: "# HELP laelia_outbox_lag_records test\n"}
	d.CompletePendingMetrics(msg)

	select {
	case got := <-ch:
		require.Same(t, msg, got)
	case <-time.After(time.Second):
		t.Fatal("pending metrics was not delivered")
	}

	d.CancelPendingMetrics(reqID) // must be safe after completion
}

// TestDispatcher_SendMachineMetricsRequest locks the wire shape of the
// metrics scrape request sent on the machine control stream, and that an
// offline machine errors instead of queueing.
func TestDispatcher_SendMachineMetricsRequest(t *testing.T) {
	d := New(nil)
	defer d.Stop()

	require.Error(t, d.SendMachineMetricsRequest(1, "req"), "offline machine must error")

	var sent *v1pb.ManagerMachineStreamMessage
	d.RegisterMachine(1, "machines/m1", func(msg *v1pb.ManagerMachineStreamMessage) error {
		sent = msg
		return nil
	})
	require.NoError(t, d.SendMachineMetricsRequest(1, "req-42"))
	require.NotNil(t, sent)
	mr := sent.GetMachineMetricsRequest()
	require.NotNil(t, mr, "the control message must carry machine_metrics_request")
	require.Equal(t, "req-42", mr.GetRequestId())
}
