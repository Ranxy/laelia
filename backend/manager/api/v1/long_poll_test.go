package v1

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/manager/component/roomhub"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestLongPollDeltaReturnsImmediatelyWhenMessagesExist(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 25000, func() ([]*store.ChatMessage, int64, error) {
		return []*store.ChatMessage{{ID: uuid.New()}}, 5, nil
	}, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, int64(5), version)
}

func TestLongPollDeltaWakesOnNotify(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	readStarted := make(chan struct{}, 1)
	var calls int
	readDelta := func() ([]*store.ChatMessage, int64, error) {
		calls++
		select {
		case readStarted <- struct{}{}:
		default:
		}
		if calls < 2 {
			return nil, 1, nil
		}
		return []*store.ChatMessage{{ID: uuid.New()}}, 2, nil
	}
	go func() {
		<-readStarted // waiter is subscribed and selecting
		svc.roomhub.NotifyConversation(convID)
	}()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, readDelta, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, int64(2), version)
}

func TestLongPollDeltaKeepsWaitingOnSpuriousWake(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	readStarted := make(chan struct{}, 1)
	var calls int
	readDelta := func() ([]*store.ChatMessage, int64, error) {
		calls++
		select {
		case readStarted <- struct{}{}:
		default:
		}
		if calls < 4 {
			return nil, 1, nil
		}
		return []*store.ChatMessage{{ID: uuid.New()}}, 2, nil
	}
	go func() {
		<-readStarted
		// Burst of wakes: the first two re-reads still find nothing (a bump
		// this read cannot see, e.g. a thread reply), the third returns data.
		for i := 0; i < 10; i++ {
			svc.roomhub.NotifyConversation(convID)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, readDelta, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, int64(2), version)
}

func TestLongPollDeltaTimesOutWithEmptyDelta(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 50, func() ([]*store.ChatMessage, int64, error) {
		return nil, 7, nil
	}, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, int64(7), version)
}

func TestLongPollDeltaNilHubReturnsImmediately(t *testing.T) {
	svc := &CommandService{}
	convID := uuid.New()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, func() ([]*store.ChatMessage, int64, error) {
		return nil, 3, nil
	}, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, int64(3), version)
}

func TestLongPollDeltaCancelReturnsEmptyDelta(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled -> long poll exits immediately
	msgs, version, err := svc.longPollDelta(ctx, convID, 25000, func() ([]*store.ChatMessage, int64, error) {
		return nil, 7, nil
	}, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, int64(7), version)
}

// The thread long poll must consider a delta that holds only the thread root
// (which ListThreadMessages always includes, even on an after_version read) as
// "no new replies", so it keeps holding until a real reply arrives rather than
// returning immediately and letting the watcher spin into a tight request loop.
func TestThreadLongPollKeepsWaitingWhenDeltaIsRootOnly(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	readStarted := make(chan struct{}, 1)
	var calls int
	rootID := uuid.New()
	hasNew := func(msgs []*store.ChatMessage) bool { return len(msgs) > 1 }
	readDelta := func() ([]*store.ChatMessage, int64, error) {
		calls++
		select {
		case readStarted <- struct{}{}:
		default:
		}
		if calls < 2 {
			// Root only: no new replies after the cursor.
			return []*store.ChatMessage{{ID: rootID}}, 5, nil
		}
		// A reply arrived -> root + one reply.
		return []*store.ChatMessage{{ID: rootID}, {ID: uuid.New()}}, 6, nil
	}
	go func() {
		<-readStarted
		svc.roomhub.NotifyConversation(convID)
	}()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, readDelta, hasNew)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, int64(6), version)
}

func TestThreadLongPollDeltaTimesOutWithRootOnly(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 50, func() ([]*store.ChatMessage, int64, error) {
		return []*store.ChatMessage{{ID: uuid.New()}}, 7, nil
	}, func(msgs []*store.ChatMessage) bool { return len(msgs) > 1 })
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, int64(7), version)
}
