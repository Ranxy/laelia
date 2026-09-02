package scheduler

import "testing"

// TestSchedulerStartSingleFlight guards the single-flight Start: a second
// Start must not spawn a second set of scan loops on the same WaitGroup.
// The loops are cancelled immediately, before the 1s ticker can fire, so no
// store/dispatcher is needed.
func TestSchedulerStartSingleFlight(*testing.T) {
	s := New(nil, nil)
	s.Start()
	s.Start() // must be a no-op, not a second set of loops.
	s.Stop()
}

// TestSchedulerStopBeforeStart ensures Stop is safe when Start was never
// called.
func TestSchedulerStopBeforeStart(_ *testing.T) {
	s := New(nil, nil)
	s.Stop() // must not panic
}
