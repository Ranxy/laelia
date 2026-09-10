package dispatcher

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// §8.2 reconciliation metrics, exposed at /metrics via the default registry
// (same registration surface as watcherDroppedTotal).
var (
	// commandRegradeTotal counts §3.6 rule-2 outcomes: "regraded_completed"
	// (a late success over a machine_unreachable reap) and
	// "late_failure_reattributed" (a late real failure re-attributed to the
	// agent's own verdict).
	commandRegradeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_command_regrade_total",
		Help: "Command re-grades from late terminals arriving over uploads.",
	}, []string{"direction"})

	// commandReapedTotal counts RUNNING commands closed by the stale-command
	// reaper, by failure_kind (machine_unreachable).
	commandReapedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_command_reaped_total",
		Help: "Running commands reaped after machine loss, by failure kind.",
	}, []string{"failure_kind"})

	// pendingControlDispatchTotal counts queued control interactions delivered
	// to a machine at (re)connect, by kind (cancel/steer).
	pendingControlDispatchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_pending_control_dispatch_total",
		Help: "Queued agent control interactions delivered at machine (re)connect.",
	}, []string{"kind"})

	// pendingControlDroppedTotal counts queued control interactions dropped
	// because they can no longer matter, by kind.
	pendingControlDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "laelia_pending_control_dropped_total",
		Help: "Queued agent control interactions dropped without delivery.",
	}, []string{"kind"})
)
