package client

import (
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/version"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	// reanchorEveryTurns is the warm-turn cadence for periodic re-anchoring:
	// after this many consecutive warm turns without a compaction, the next
	// warm turn carries the identity anchor.
	reanchorEveryTurns = 10

	// contextWarningThreshold is the used/size ratio at or above which the turn
	// batch carries a context-window warning.
	contextWarningThreshold = 0.9

	// usageDropInferenceRatio is the used-token drop (vs the last observation)
	// that infers a context compaction finished when no direct event arrived.
	usageDropInferenceRatio = 0.3

	// contextQuietWindow is the no-agent-message quiet period required before a
	// usage drop is treated as a compaction rather than active generation.
	contextQuietWindow = 10 * time.Second
)

// compactionStaleTimeout is how long a CONTEXT_COMPACTION_STARTED may run
// without a matching FINISHED before the drain loop surfaces a WARNING
// ("Context compaction still running; no finish event observed"). It mirrors
// A var (not const) so tests can shrink the window.
var compactionStaleTimeout = 5 * time.Minute

// contextObserver applies the context state machine (design doc L1/L2) to the
// events flowing through one command on the runCommand goroutine: it records
// usage observations, infers compaction from usage drops, runs the compaction
// watchdog, and folds compaction events into the per-agent ContextState. All
// mutations happen on the pump goroutine, so no locking is needed. Extra events
// it emits (inferred compaction finish, stale warning) use the same LocalState
// sequence counter as the pump.
type contextObserver struct {
	state      *executor.ContextState
	sink       turnSink
	commandID  string
	localState *executor.LocalState

	// watchdogCh receives one signal when a compaction has been active for
	// compactionStaleTimeout without a finish event.
	watchdogCh chan struct{}
	timer      *time.Timer
	// lastAgentChunkAt is the last time an agent_message_chunk raw event was
	// observed; a usage drop while the agent is actively generating is not a
	// compaction.
	lastAgentChunkAt time.Time
}

func newContextObserver(state *executor.ContextState, sink turnSink, commandID string, localState *executor.LocalState) *contextObserver {
	return &contextObserver{
		state:      state,
		sink:       sink,
		commandID:  commandID,
		localState: localState,
		watchdogCh: make(chan struct{}, 1),
	}
}

func (o *contextObserver) startWatchdog() {
	o.stopWatchdog()
	o.timer = time.AfterFunc(compactionStaleTimeout, func() {
		select {
		case o.watchdogCh <- struct{}{}:
		default:
		}
	})
}

func (o *contextObserver) stopWatchdog() {
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
}

// observe applies context-state updates for one forwarded event and emits any
// derived events (inferred compaction finish). It is a no-op when context
// tracking is disabled (state nil).
func (o *contextObserver) observe(event *executor.Event) error {
	if o.state == nil {
		return nil
	}
	switch event.Type {
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED:
		o.state.Compaction.Active = true
		o.state.Compaction.LastStartAt = time.Now()
		o.startWatchdog()
	case v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		o.onCompactionFinished()
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		if event.ContextUsage == nil {
			return nil
		}
		prevUsed := o.state.Usage.Used
		o.state.Usage = executor.ContextUsage{
			Size:      event.ContextUsage.Size,
			Used:      event.ContextUsage.Used,
			UpdatedAt: time.Now(),
		}
		if !o.state.Compaction.Active && o.inferCompaction(prevUsed, event.ContextUsage.Used) {
			return o.emitInferredCompaction()
		}
	case v1pb.CommandEventType_RAW_ACP:
		if event.Summary == "agent_message_chunk" {
			o.lastAgentChunkAt = time.Now()
		}
	default:
	}
	return nil
}

// inferCompaction reports whether the observed used-token drop looks like a
// context compaction: > usageDropInferenceRatio below the previous observation
// and not while the agent is actively streaming message chunks.
func (o *contextObserver) inferCompaction(prevUsed, used int64) bool {
	if prevUsed <= 0 || used < 0 {
		return false
	}
	drop := prevUsed - used
	if drop <= 0 || float64(drop)/float64(prevUsed) <= usageDropInferenceRatio {
		return false
	}
	if !o.lastAgentChunkAt.IsZero() && time.Since(o.lastAgentChunkAt) < contextQuietWindow {
		return false
	}
	return true
}

func (o *contextObserver) onCompactionFinished() {
	o.finishCompaction()
	// After a direct compaction event the old usage snapshot is stale (pi
	// reports null tokens/percent until a fresh assistant response). Reset it
	// so the next CONTEXT_USAGE_UPDATE repopulates with post-compaction values
	// instead of showing the pre-compaction usage.
	o.state.Usage = executor.ContextUsage{}
}

// finishCompaction applies the state transitions shared by direct and inferred
// compaction finish events. It does not touch Usage: an inferred compaction is
// triggered by a CONTEXT_USAGE_UPDATE that already carries the post-compaction
// usage, so that observation must be preserved.
func (o *contextObserver) finishCompaction() {
	o.stopWatchdog()
	o.state.Compaction.Active = false
	o.state.Compaction.Count++
	o.state.Compaction.LastAt = time.Now()
	o.state.NeedsReanchor = true
	o.state.Session.Turns = 0
}

// emitInferredCompaction reports a compaction that was detected from a usage
// drop (no direct agent event) and applies the same finish state as a direct
// event, except that the triggering usage observation is kept.
func (o *contextObserver) emitInferredCompaction() error {
	o.finishCompaction()
	event := executor.Event{
		SeqNo:   nextEventSeq(o.localState),
		Type:    v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED,
		Summary: "Context compaction finished (inferred from usage drop)",
		ContextCompaction: &v1pb.ContextCompactionPayload{
			Inferred: true,
		},
	}
	return o.sink.appendEvent(o.commandID, &event)
}

// onWatchdog surfaces the stale-compaction warning.
func (o *contextObserver) onWatchdog() error {
	msg := "Context compaction still running; no finish event observed"
	event := executor.Event{
		SeqNo:   nextEventSeq(o.localState),
		Type:    v1pb.CommandEventType_WARNING,
		Summary: msg,
		Warning: &v1pb.WarningPayload{Message: msg},
	}
	return o.sink.appendEvent(o.commandID, &event)
}

// reanchorPrompt decides whether this turn carries the identity anchor and
// consumes the decision state: NeedsReanchor (set after a compaction) or the
// periodic warm-turn threshold. The anchor is only actually prepended on warm
// turns by the executor; a cold turn re-sends the full init prompt, so
// consuming the decision either way is correct.
func reanchorPrompt(ctxState *executor.ContextState, name, ownerDisplayName, teamPrompt string) string {
	if ctxState == nil {
		return ""
	}
	if !ctxState.NeedsReanchor && ctxState.Session.Turns < reanchorEveryTurns {
		return ""
	}
	ctxState.NeedsReanchor = false
	ctxState.Session.Turns = 0
	return executor.BuildReanchorPrompt(name, ownerDisplayName, teamPrompt)
}

// appendContextWarning appends the context-window warning to the turn batch
// when the last observed usage is at or above contextWarningThreshold.
// applyPromptVersion compares the manager's composite prompt version against
// the locally confirmed one and, when it changed, injects a prompt-update
// notice into the turn and/or forces a re-anchor. The composite is
// "<static_expected>.<dynamic_hash>": the static part is the manager's expected
// machine-binary prompt bundle version, the dynamic part is the hash of
// persona/team/owner. It updates ctxState.PromptVersion to the newly confirmed
// value (the caller persists it via persistContextState).
func (*commandStream) applyPromptVersion(ctxState *executor.ContextState, promptVersion, turnPrompt string) string {
	if ctxState == nil || promptVersion == "" || ctxState.PromptVersion == promptVersion {
		return turnPrompt
	}
	expectedStatic, dynamicHash, _ := strings.Cut(promptVersion, ".")
	prevStatic, prevDynamic, _ := strings.Cut(ctxState.PromptVersion, ".")

	var notices []string
	dynamicChanged := prevDynamic != "" && prevDynamic != dynamicHash
	if dynamicChanged {
		ctxState.NeedsReanchor = true
		notices = append(notices, "Your system prompt has been updated (persona, team, or owner). Re-read the relevant sections before continuing.")
	}
	// Static detection: either the machine binary's bundled prompt version
	// changed since the last confirmed turn (binary upgraded), or the manager
	// expects a different version than the local binary provides (stale
	// machine). A binary upgrade already forces a cold start via the session /
	// launch fingerprint; the notice here makes the change explicit to the
	// agent.
	staticChanged := prevStatic != "" && prevStatic != version.PromptBundleVersion
	stale := expectedStatic != "" && version.PromptBundleVersion != expectedStatic
	if stale {
		notices = append(notices, "The system prompt has been updated on the server, but this machine's bundled system prompt is out of date. Upgrade laelia-machine to receive the latest instructions.")
	} else if staticChanged {
		ctxState.NeedsReanchor = true
		notices = append(notices, "Your machine's bundled system prompt has been updated. Re-read the latest instructions.")
	}

	ctxState.PromptVersion = promptVersion
	if len(notices) == 0 {
		return turnPrompt
	}
	notice := "System prompt update notice.\n\n" + strings.Join(notices, "\n\n")
	if strings.TrimSpace(turnPrompt) == "" {
		return notice
	}
	return notice + "\n\n" + turnPrompt
}

func appendContextWarning(prompt string, ctxState *executor.ContextState) string {
	if prompt == "" || ctxState == nil || ctxState.Usage.Size <= 0 {
		return prompt
	}
	ratio := ctxState.UsageRatio()
	if ratio < contextWarningThreshold {
		return prompt
	}
	pct := int(math.Round(ratio * 100))
	warning := fmt.Sprintf(
		"Context warning: your context window is ~%d%% full (%d/%d tokens). Prefer concise replies; write durable knowledge to MEMORY.md.",
		pct, ctxState.Usage.Used, ctxState.Usage.Size,
	)
	return strings.TrimRight(prompt, "\n") + "\n\n" + warning
}

// persistContextState folds the completed turn into the context state (warm
// turn count, fingerprint-change reset) and saves it. In-turn observations
// (usage/compaction) are persisted even when result is nil (failed turn).
func (c *commandStream) persistContextState(ctxState *executor.ContextState, result *executor.Result) {
	if ctxState == nil {
		return
	}
	if result != nil {
		if result.Fingerprint != "" {
			if ctxState.Fingerprint != "" && ctxState.Fingerprint != result.Fingerprint {
				ctxState.ResetForFingerprint(result.Fingerprint)
			} else {
				ctxState.Fingerprint = result.Fingerprint
			}
		}
		if result.Resumed {
			ctxState.Session.Turns++
		} else {
			ctxState.Session.Turns = 0
			ctxState.Session.ColdStarts++
		}
		// The ACP executor owns the resume-failure counter (it increments on
		// each failed ResumeSession and resets after the warning); mirror its
		// final value so this save is the single writer for the file.
		ctxState.Session.ResumeFailures = result.ResumeFailures
	}
	if err := executor.SaveContextState(c.machineID, c.agentID, ctxState); err != nil {
		slog.Warn("failed to persist context state", "agent", c.agentID, "error", err)
	}
}
