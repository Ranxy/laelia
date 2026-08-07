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
	})
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
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, readDelta)
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
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, readDelta)
	require.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, int64(2), version)
}

func TestLongPollDeltaTimesOutWithEmptyDelta(t *testing.T) {
	svc := &CommandService{roomhub: roomhub.New()}
	convID := uuid.New()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 50, func() ([]*store.ChatMessage, int64, error) {
		return nil, 7, nil
	})
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, int64(7), version)
}

func TestLongPollDeltaNilHubReturnsImmediately(t *testing.T) {
	svc := &CommandService{}
	convID := uuid.New()
	msgs, version, err := svc.longPollDelta(context.Background(), convID, 5000, func() ([]*store.ChatMessage, int64, error) {
		return nil, 3, nil
	})
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, int64(3), version)
}
