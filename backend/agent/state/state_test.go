package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s := &State{
		ManagerURL:   "https://laelia.example.com",
		MachineID:    "machines/abc123",
		RefreshToken: "eyJ...",
		Hostname:     "my-laptop",
		CreatedAt:    time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC),
	}
	require.NoError(t, Save(s))

	got, err := Load()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, s.ManagerURL, got.ManagerURL)
	assert.Equal(t, s.MachineID, got.MachineID)
	assert.Equal(t, s.RefreshToken, got.RefreshToken)
	assert.Equal(t, s.Hostname, got.Hostname)
	assert.True(t, got.CreatedAt.Equal(s.CreatedAt))

	// The file must be 0600: it holds the refresh token.
	info, err := os.Stat(filepath.Join(home, ".laelia", "machine.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestLoadMissingReturnsNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := Load()
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, Save(&State{ManagerURL: "https://x", RefreshToken: "t"}))
	require.NoError(t, Clear())
	got, err := Load()
	require.NoError(t, err)
	assert.Nil(t, got)
	// Clearing an already-missing file is a no-op.
	require.NoError(t, Clear())
}
