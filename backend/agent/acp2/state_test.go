package acp2

import "testing"

func TestTurnGateLifecycle(t *testing.T) {
	g := NewTurnGate()
	if g.State() != TurnIdle {
		t.Fatalf("expected idle, got %v", g.State())
	}

	g.NoteTurnAccepted("turn-1")
	if g.State() != TurnIdle {
		t.Fatalf("accepted turn must stay idle until started, got %v", g.State())
	}
	if g.CanSteerBusy() {
		t.Fatal("must not steer before turn started")
	}

	g.MarkTurnStarted("turn-1")
	if g.State() != TurnStarted || g.ActiveTurnID() != "turn-1" {
		t.Fatalf("unexpected started state: %v %q", g.State(), g.ActiveTurnID())
	}
	if !g.CanSteerBusy() {
		t.Fatal("must be steerable after turn started")
	}

	g.MarkTurnCompleted()
	if g.State() != TurnIdle || g.ActiveTurnID() != "" {
		t.Fatalf("unexpected completed state: %v %q", g.State(), g.ActiveTurnID())
	}
	if g.CanSteerBusy() {
		t.Fatal("must not steer after completion")
	}
}

func TestTurnGateSteeringGate(t *testing.T) {
	g := NewTurnGate()
	g.NoteTurnAccepted("turn-1")
	g.MarkTurnStarted("turn-1")

	g.MarkToolBoundary()
	if g.CanSteerBusy() {
		t.Fatal("steering must be gated after a tool boundary")
	}

	g.MarkProgress()
	if !g.CanSteerBusy() {
		t.Fatal("progress must reopen the steering gate")
	}

	// A pending steer/start request also gates steering.
	g.NoteTurnAccepted("turn-2")
	if g.CanSteerBusy() {
		t.Fatal("must not steer while a turn request is pending")
	}
}

func TestTurnGateCompletedWithoutActivity(t *testing.T) {
	g := NewTurnGate()
	g.MarkTurnStarted("turn-1")
	if !g.CompletedWithoutActivity() {
		t.Fatal("freshly started turn must count as activity-free")
	}

	g.MarkProgress()
	if g.CompletedWithoutActivity() {
		t.Fatal("progress must count as activity")
	}

	g.MarkTurnStarted("turn-2")
	g.MarkTokenUsage()
	if g.CompletedWithoutActivity() {
		t.Fatal("token usage must count as activity")
	}

	g.MarkTurnCompleted()
	if g.CompletedWithoutActivity() {
		t.Fatal("completed turn must not report activity-free")
	}
}

func TestTurnGateMarkNonEmptyInput(t *testing.T) {
	g := NewTurnGate()
	g.NoteTurnAccepted("turn-1")
	g.MarkTurnStarted("turn-1")
	if g.HasNonEmptyInput() {
		t.Fatal("must start without input evidence")
	}
	g.MarkNonEmptyInput("turn-1")
	if !g.HasNonEmptyInput() {
		t.Fatal("mark must be visible during the turn")
	}

	g.MarkTurnCompleted()
	// A late mark for a completed turn must be ignored on the next turn.
	g.MarkNonEmptyInput("turn-1")
	g.MarkTurnStarted("turn-2")
	if g.HasNonEmptyInput() {
		t.Fatal("late mark must not leak into the next turn")
	}
	g.MarkNonEmptyInput("turn-2")
	if !g.HasNonEmptyInput() {
		t.Fatal("current turn mark must be visible")
	}
}

func TestTurnGateReset(t *testing.T) {
	g := NewTurnGate()
	g.NoteTurnAccepted("turn-1")
	g.MarkTurnStarted("turn-1")
	g.MarkToolBoundary()
	g.Reset()
	if g.State() != TurnIdle || g.CanSteerBusy() || g.CompletedWithoutActivity() {
		t.Fatal("reset must restore idle state")
	}
}
