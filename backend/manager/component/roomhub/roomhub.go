// Package roomhub is an in-process pub/sub that wakes long-polling readers
// when a conversation gains new messages. It is the user-facing counterpart
// of the agent wake path (dispatcher.NotifyNewMessages): the frontend
// long-polls ListConversationMessages with wait_ms, and the hub lets the
// handler return as soon as a new message lands instead of sleeping the full
// timeout. Single-process only; a multi-instance deployment needs a shared
// notifier (e.g. Postgres LISTEN/NOTIFY) behind the same interface.
package roomhub

import (
	"sync"

	"github.com/google/uuid"
)

// Hub wakes waiters when a conversation's room version changes. Waiters are
// per-conversation channels; Notify broadcasts to all of them. A waiter that
// is not currently selecting on its channel receives the signal in its buffer
// and wakes on the next select, so a Notify that races with a waiter's
// re-check is never lost.
type Hub struct {
	mu      sync.Mutex
	waiters map[uuid.UUID]map[chan struct{}]struct{}
}

// New returns an empty Hub.
func New() *Hub {
	return &Hub{waiters: make(map[uuid.UUID]map[chan struct{}]struct{})}
}

// Subscribe registers a waiter channel for a conversation and returns it. The
// channel is buffered (size 1) so a Notify that lands between the caller's
// version check and its select is observed. Callers must re-check the room
// version after subscribing (see CommandService.ListConversationMessages) and
// must Unsubscribe when done.
func (h *Hub) Subscribe(conversationID uuid.UUID) chan struct{} {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	defer h.mu.Unlock()
	m := h.waiters[conversationID]
	if m == nil {
		m = make(map[chan struct{}]struct{})
		h.waiters[conversationID] = m
	}
	m[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a waiter channel. Safe to call after Notify; a signal
// already buffered in the channel is simply dropped with it.
func (h *Hub) Unsubscribe(conversationID uuid.UUID, ch chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.waiters[conversationID]; m != nil {
		delete(m, ch)
		if len(m) == 0 {
			delete(h.waiters, conversationID)
		}
	}
}

// NotifyConversation wakes every waiter of a conversation. Non-blocking: a
// waiter whose buffer is already full (a previous unprocessed wake) is skipped
// — one wake is enough, the waiter re-queries the room on wake. Called after a
// message insert bumps the conversation version. It implements
// store.RoomNotifier.
func (h *Hub) NotifyConversation(conversationID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.waiters[conversationID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
