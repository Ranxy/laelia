package outbox

import (
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// §8.2 machine-local metrics. One uploader (and one WAL) per hosted agent, so
// every series carries an agent label derived from the WAL directory. The
// manager scrapes them through GetMachineMetrics (the metrics travel over the
// machine control stream — machines make outbound-only connections and expose
// no ports of their own).
var (
	outboxLagRecords = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "laelia_outbox_lag_records",
		Help: "Records uploaded but not yet acked (the settled boundary's lag).",
	}, []string{"agent"})

	outboxBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "laelia_outbox_bytes",
		Help: "The agent's outbox WAL segment footprint on disk.",
	}, []string{"agent"})

	outboxQuarantineTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_outbox_quarantine_total",
		Help: "Outbox WALs moved aside after unrecoverable corruption.",
	}, []string{"agent"})

	uploadBatchSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "laelia_upload_batch_size",
		Help:    "Entries per UploadCommandData request.",
		Buckets: prometheus.ExponentialBuckets(1, 2, 13), // 1 .. 4096
	}, []string{"agent"})

	uploadRTT = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "laelia_upload_rtt",
		Help:    "UploadCommandData round-trip seconds.",
		Buckets: prometheus.ExponentialBuckets(0.005, 2, 14), // 5ms .. ~41s
	}, []string{"agent"})

	uploadPoisonTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_upload_poison_total",
		Help: "Unparseable (torn) records read from the WAL and uploaded as poison.",
	}, []string{"agent"})

	uploadFlushNowTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_upload_flush_now_total",
		Help: "Flush-now requests that interrupted the uploader's wait.",
	}, []string{"agent"})

	barrierWaitSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "laelia_barrier_wait_ms",
		Help:    "Turn-start barrier wait milliseconds (flush-now effectiveness).",
		Buckets: prometheus.ExponentialBuckets(1, 2, 16), // 1ms .. ~65s
	}, []string{"agent"})
)

// agentLabel derives an outbox metric's agent label from the WAL directory.
// AgentOutboxDir is <data>/<machineID>/<agentID>/outbox, so the agent id is
// the directory one level up; any other shape (tests) falls back to the dir.
func agentLabel(dir string) string {
	if filepath.Base(dir) != "outbox" {
		return dir
	}
	return filepath.Base(filepath.Dir(dir))
}
