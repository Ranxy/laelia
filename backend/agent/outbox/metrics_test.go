package outbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// histogramSamples sums a labeled histogram's observed count from the default
// registry (testutil.ToFloat64 does not support histograms).
func histogramSamples(t *testing.T, metricName, label string) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	var samples uint64
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if len(m.GetLabel()) == 0 || m.GetLabel()[0].GetValue() != label {
				continue
			}
			if h := m.GetHistogram(); h != nil {
				samples += h.GetSampleCount()
			}
		}
	}
	return samples
}

// TestUploaderRecordsMetrics locks the §8.2 wiring: the barrier wait and
// upload batch/rtt histograms observe, flush-now counts, and the lag/bytes
// gauges track the settled frontier (lag 1 while the terminal is unacked,
// 0 after it drains).
func TestUploaderRecordsMetrics(t *testing.T) {
	fastUploaderTunables(t)
	o, err := Open(filepathJoinTemp(t))
	require.NoError(t, err)
	defer o.Close()
	label := agentLabel(o.Dir())

	require.NoError(t, o.Append(progressEntry("cmd-1", 1, "x")))
	require.NoError(t, o.Append(resultEntry("cmd-1", 0)))

	// The first cycle acks only the progress record, so the group stays queued
	// (lag 1); later cycles are unblocked by swapping the script below.
	fake := &fakeUploader{respond: func(_ int, entries []*Entry) (*v1pb.UploadCommandDataResponse, error) {
		resp := &v1pb.UploadCommandDataResponse{}
		for _, e := range entries {
			if e.GetKind() == v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS {
				resp.Acks = append(resp.Acks, &v1pb.UploadCommandDataAck{
					CommandId:       e.GetCommandId(),
					LastProgressSeq: e.GetSeqNo(),
				})
			}
		}
		return resp, nil
	}}
	u := NewUploader(o, fake.upload)
	runUploader(t, u)

	// One cycle must land before the lag assertion: the settled frontier is
	// the acked progress, the unacked terminal is the lag.
	require.NoError(t, waitFor(2*time.Second, func() bool { return fake.callCount() > 0 }))
	require.Equal(t, float64(1), testutil.ToFloat64(outboxLagRecords.WithLabelValues(label)),
		"lag must count records past the settled frontier while the terminal is unacked")
	require.Greater(t, histogramSamples(t, "laelia_upload_batch_size", label), uint64(0),
		"the batch-size histogram must observe the upload")
	require.Greater(t, histogramSamples(t, "laelia_upload_rtt", label), uint64(0),
		"the rtt histogram must observe the upload")

	// A bounded barrier wait observes its duration (and its entry flush-now
	// counts): the queue cannot drain under the progress-only ack, so ctx
	// bounds it.
	barrierCtx, barrierCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer barrierCancel()
	_ = u.WaitDrained(barrierCtx)
	require.Greater(t, histogramSamples(t, "laelia_barrier_wait_ms", label), uint64(0),
		"the barrier wait histogram must observe the wait")
	require.Equal(t, float64(1), testutil.ToFloat64(uploadFlushNowTotal.WithLabelValues(label)),
		"the barrier's entry flush-now must count")

	// After the terminal is acked and evicted, the lag gauge drops to 0.
	fake.mu.Lock()
	fake.respond = nil // default script: ack everything
	fake.mu.Unlock()
	u.FlushNow()
	require.Eventually(t, func() bool {
		empty, _ := o.Empty()
		return empty
	}, 2*time.Second, 5*time.Millisecond, "the log must drain once the terminal is acked")
	require.Eventually(t, func() bool {
		return testutil.ToFloat64(outboxLagRecords.WithLabelValues(label)) == 0
	}, 2*time.Second, 5*time.Millisecond, "lag must drop to 0 after the acked terminal evicts the group")
}

// TestQuarantineMetric locks the quarantine counter: a WAL whose open fails
// with corruption past the recovery retry is quarantined, a fresh log starts,
// and the incident is counted under the agent's label.
func TestQuarantineMetric(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "outbox")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	// A segment file whose bytes are not a valid WAL frame makes both open
	// attempts fail with ErrCorrupt, driving the quarantine path.
	require.NoError(t, os.WriteFile(filepath.Join(dir, walSegmentName(1)), []byte("garbage-not-a-wal-frame"), 0o600))

	o, err := Open(dir)
	require.NoError(t, err, "quarantine must recover with a fresh WAL")
	defer o.Close()
	require.Equal(t, float64(1), testutil.ToFloat64(outboxQuarantineTotal.WithLabelValues(agentLabel(dir))))
}

// walSegmentName mirrors tidwall/wal's %020d segment naming.
func walSegmentName(index uint64) string {
	return fmt.Sprintf("%020d", index)
}
