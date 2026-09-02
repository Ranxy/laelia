package dispatcher

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// commandBus owns the live command output/event watchers and their broadcast
// fan-out. It is extracted from Dispatcher so the pub/sub concern can be
// tested and evolved independently of session/connection management.
type commandBus struct {
	mu            sync.RWMutex
	watchers      map[string]map[*watcher[*v1pb.CommandOutput]]struct{}
	eventWatchers map[string]map[*watcher[*v1pb.CommandEvent]]struct{}
}

func newCommandBus() *commandBus {
	return &commandBus{
		watchers:      make(map[string]map[*watcher[*v1pb.CommandOutput]]struct{}),
		eventWatchers: make(map[string]map[*watcher[*v1pb.CommandEvent]]struct{}),
	}
}

func (b *commandBus) subscribeOutput(commandID string) chan *v1pb.CommandOutput {
	ch := make(chan *v1pb.CommandOutput, watcherBufSize)
	b.mu.Lock()
	if b.watchers[commandID] == nil {
		b.watchers[commandID] = make(map[*watcher[*v1pb.CommandOutput]]struct{})
	}
	b.watchers[commandID][&watcher[*v1pb.CommandOutput]{ch: ch}] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *commandBus) unsubscribeOutput(commandID string, ch chan *v1pb.CommandOutput) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if watchers, ok := b.watchers[commandID]; ok {
		for w := range watchers {
			if w.ch == ch {
				delete(watchers, w)
				close(ch)
				break
			}
		}
		if len(watchers) == 0 {
			delete(b.watchers, commandID)
		}
	}
}

func (b *commandBus) subscribeEvent(commandID string) chan *v1pb.CommandEvent {
	ch := make(chan *v1pb.CommandEvent, watcherBufSize)
	b.mu.Lock()
	if b.eventWatchers[commandID] == nil {
		b.eventWatchers[commandID] = make(map[*watcher[*v1pb.CommandEvent]]struct{})
	}
	b.eventWatchers[commandID][&watcher[*v1pb.CommandEvent]{ch: ch}] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *commandBus) unsubscribeEvent(commandID string, ch chan *v1pb.CommandEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if watchers, ok := b.eventWatchers[commandID]; ok {
		for w := range watchers {
			if w.ch == ch {
				delete(watchers, w)
				close(ch)
				break
			}
		}
		if len(watchers) == 0 {
			delete(b.eventWatchers, commandID)
		}
	}
}

func (b *commandBus) broadcast(commandID string, output *v1pb.CommandOutput) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for w := range b.watchers[commandID] {
		select {
		case w.ch <- output:
		default:
			total, log := w.drop()
			watcherDroppedTotal.WithLabelValues("output").Inc()
			if log {
				slog.Warn("command watcher too slow; dropping live output (DB replay is the fallback)", "commandID", commandID, "dropped", total)
			}
		}
	}
}

func (b *commandBus) broadcastEvent(commandID string, event *v1pb.CommandEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for w := range b.eventWatchers[commandID] {
		select {
		case w.ch <- event:
		default:
			total, log := w.drop()
			watcherDroppedTotal.WithLabelValues("event").Inc()
			if log {
				slog.Warn("command event watcher too slow; dropping live events (DB replay is the fallback)", "commandID", commandID, "dropped", total)
			}
		}
	}
}

func (b *commandBus) closeOutput(commandID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for w := range b.watchers[commandID] {
		close(w.ch)
	}
	delete(b.watchers, commandID)
}

func (b *commandBus) closeEvents(commandID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for w := range b.eventWatchers[commandID] {
		close(w.ch)
	}
	delete(b.eventWatchers, commandID)
}

// watcher is one subscribed consumer of a command's live stream. dropped
// counts messages discarded because the consumer was slower than the producer
// (buffer full); it is only mutated via atomics, so broadcast can update it
// while holding the dispatcher's read lock.
type watcher[T any] struct {
	ch      chan T
	dropped atomic.Int64
}

// drop records one dropped message and reports whether this drop should be
// logged: the first drop and every doubling after it, so a flood of drops
// costs a logarithmic number of log lines.
func (w *watcher[T]) drop() (total int64, log bool) {
	n := w.dropped.Add(1)
	return n, n&(n-1) == 0
}

// watcherDroppedTotal counts live-stream messages dropped because a watcher's
// buffer was full. Exposed at /metrics via the default registry (folded in
// echo_routes). kind: "output" | "event".
var watcherDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "laelia_watcher_dropped_total",
	Help: "Live command stream messages dropped because a watcher's buffer was full.",
}, []string{"kind"})
