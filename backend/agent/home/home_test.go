package home

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirDefaultsToDotLaeliaUnderHome(t *testing.T) {
	t.Setenv(EnvDir, "")
	t.Setenv("HOME", "/home/test-user")

	got := Dir()
	assert.Equal(t, filepath.Join("/home/test-user", ".laelia"), got)
}

func TestDirUsesEnvOverride(t *testing.T) {
	t.Setenv(EnvDir, "/var/lib/laelia")
	t.Setenv("HOME", "/home/test-user")

	assert.Equal(t, "/var/lib/laelia", Dir())
}

func TestDirConvertsRelativeEnvToAbsolute(t *testing.T) {
	t.Setenv(EnvDir, "relative/laelia")
	t.Setenv("HOME", "/home/test-user")

	abs, err := filepath.Abs("relative/laelia")
	require.NoError(t, err)
	assert.Equal(t, abs, Dir())
}

func TestJoinUsesEnvRoot(t *testing.T) {
	t.Setenv(EnvDir, "/data/laelia")

	assert.Equal(t, filepath.Join("/data/laelia", "machine.json"), Join("machine.json"))
	assert.Equal(t, filepath.Join("/data/laelia", "m", "a", "state.json"), Join("m", "a", "state.json"))
}
