package v1

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/manager/store"
)

// fakeAuditWriter captures the batches handed to CreateAuditLogs.
type fakeAuditWriter struct {
	count   atomic.Int64
	batches chan []*store.AuditLogMessage
}

func (f *fakeAuditWriter) CreateAuditLogs(_ context.Context, logs []*store.AuditLogMessage) error {
	f.count.Add(1)
	if f.batches != nil {
		f.batches <- logs
	}
	return nil
}

func TestAuditBuffer_StartIdempotentAndFlushes(t *testing.T) {
	fake := &fakeAuditWriter{batches: make(chan []*store.AuditLogMessage, 4)}
	b := NewAuditBuffer(nil, 30*time.Millisecond)
	b.store = fake
	defer b.Stop()

	b.Start(context.Background())
	b.Start(context.Background()) // must be a no-op, not a second loop.

	b.Record(&store.AuditLogMessage{Method: "m1"})
	b.Record(&store.AuditLogMessage{Method: "m2"})

	select {
	case batch := <-fake.batches:
		require.Len(t, batch, 2, "both records should flush in one batch")
		assert.Equal(t, "m1", batch[0].Method)
		assert.Equal(t, "m2", batch[1].Method)
	case <-time.After(2 * time.Second):
		t.Fatal("buffer did not flush within the tick interval")
	}

	// Stop cancels the loop (final flush writes nothing here). If Start had
	// leaked a second loop, its uncancelled ctx would keep flushing; the count
	// must stay frozen after Stop.
	b.Stop()
	before := fake.count.Load()
	time.Sleep(100 * time.Millisecond)
	if got := fake.count.Load(); got != before {
		t.Errorf("flush loop kept writing after Stop: before=%d after=%d", before, got)
	}
}

func TestAuditBuffer_StopBeforeStart(_ *testing.T) {
	b := NewAuditBuffer(nil, 30*time.Millisecond)
	b.Stop() // must not panic on nil cancel
}

func TestAuditBuffer_FinalFlushOnStop(t *testing.T) {
	fake := &fakeAuditWriter{batches: make(chan []*store.AuditLogMessage, 4)}
	b := NewAuditBuffer(nil, time.Hour) // no ticker flush during the test
	b.store = fake
	b.Start(context.Background())

	b.Record(&store.AuditLogMessage{Method: "m1"})
	b.Stop() // final flush happens in the loop's ctx.Done branch

	select {
	case batch := <-fake.batches:
		assert.Len(t, batch, 1)
	case <-time.After(2 * time.Second):
		t.Fatal("final flush on Stop did not persist pending records")
	}
}
