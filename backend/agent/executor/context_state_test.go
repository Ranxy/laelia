package executor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSaveContextState_RoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	got, err := LoadContextState("m", "a")
	require.NoError(t, err)
	assert.Nil(t, got, "missing file should yield nil, nil")

	want := &ContextState{
		Usage: ContextUsage{Size: 200000, Used: 120000, UpdatedAt: time.Now()},
		Compaction: CompactionInfo{
			Count:  2,
			LastAt: time.Now(),
			Active: true,
		},
		Session:       SessionHealth{Turns: 4, ColdStarts: 1},
		NeedsReanchor: true,
		Fingerprint:   "fp-1",
	}
	require.NoError(t, SaveContextState("m", "a", want))

	got, err = LoadContextState("m", "a")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.Usage.Size, got.Usage.Size)
	assert.Equal(t, want.Usage.Used, got.Usage.Used)
	assert.True(t, want.Usage.UpdatedAt.Equal(got.Usage.UpdatedAt))
	assert.Equal(t, want.Compaction.Count, got.Compaction.Count)
	assert.True(t, want.Compaction.LastAt.Equal(got.Compaction.LastAt))
	assert.True(t, want.Compaction.LastStartAt.Equal(got.Compaction.LastStartAt))
	assert.Equal(t, want.Compaction.Active, got.Compaction.Active)
	assert.Equal(t, want.Session, got.Session)
	assert.True(t, got.NeedsReanchor)
	assert.Equal(t, "fp-1", got.Fingerprint)
}

func TestSaveContextState_AtomicNoTempLeftBehind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	require.NoError(t, SaveContextState("m", "a", &ContextState{Fingerprint: "fp"}))

	entries, err := os.ReadDir(filepath.Join(home, ".laelia", "m", "a"))
	require.NoError(t, err)
	require.Len(t, entries, 1, "only context-state.json, no temp files")
	assert.Equal(t, "context-state.json", entries[0].Name())

	info, err := entries[0].Info()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestContextState_ResetForFingerprint(t *testing.T) {
	s := &ContextState{
		Usage:         ContextUsage{Size: 100, Used: 90},
		Compaction:    CompactionInfo{Count: 5, Active: true},
		Session:       SessionHealth{Turns: 8, ColdStarts: 2},
		NeedsReanchor: true,
		Fingerprint:   "old",
	}
	s.ResetForFingerprint("new")

	assert.Zero(t, s.Usage)
	assert.Zero(t, s.Compaction)
	assert.Zero(t, s.Session)
	assert.False(t, s.NeedsReanchor)
	assert.Equal(t, "new", s.Fingerprint)
}

func TestContextState_UsageRatio(t *testing.T) {
	assert.Zero(t, (&ContextState{}).UsageRatio())
	s := &ContextState{Usage: ContextUsage{Size: 200000, Used: 180000}}
	assert.InDelta(t, 0.9, s.UsageRatio(), 1e-9)
}
