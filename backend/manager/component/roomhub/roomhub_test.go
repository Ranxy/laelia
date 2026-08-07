package roomhub

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNotifyUnsubscribe(t *testing.T) {
	h := New()
	convID := uuid.New()

	ch := h.Subscribe(convID)
	require.NotNil(t, ch)

	select {
	case <-ch:
		t.Fatal("no notify yet, channel must be empty")
	default:
	}

	h.NotifyConversation(convID)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected wake after notify")
	}

	h.Unsubscribe(convID, ch)
	// After unsubscribe the hub must not hold the waiter: a notify must not
	// deliver to the (now unregistered) channel.
	h.NotifyConversation(convID)
	select {
	case <-ch:
		t.Fatal("waiter must not receive after unsubscribe")
	default:
	}
}

func TestNotifyWakesAllSubscribers(t *testing.T) {
	h := New()
	convID := uuid.New()
	other := uuid.New()

	ch1 := h.Subscribe(convID)
	ch2 := h.Subscribe(convID)
	ch3 := h.Subscribe(other)

	h.NotifyConversation(convID)
	for i, ch := range []chan struct{}{ch1, ch2} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d not woken", i)
		}
	}
	select {
	case <-ch3:
		t.Fatal("subscriber of another conversation must not be woken")
	default:
	}
}

func TestNotifyNonBlockingWhenBufferFull(t *testing.T) {
	h := New()
	convID := uuid.New()
	ch := h.Subscribe(convID)

	// Fill the buffer with an unprocessed wake, then notify again: the second
	// notify must be dropped, not block the caller.
	ch <- struct{}{}
	done := make(chan struct{})
	go func() {
		h.NotifyConversation(convID)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("notify must not block on a full buffer")
	}
}

func TestUnsubscribeRemovesEmptyConversationEntry(t *testing.T) {
	h := New()
	convID := uuid.New()
	ch := h.Subscribe(convID)
	h.Unsubscribe(convID, ch)

	h.mu.Lock()
	defer h.mu.Unlock()
	assert.Empty(t, h.waiters, "conversation entry must be removed when the last waiter leaves")
}

func TestNotifyWithoutWaitersIsNoOp(_ *testing.T) {
	h := New()
	h.NotifyConversation(uuid.New())
}
