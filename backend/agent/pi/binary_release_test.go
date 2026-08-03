//go:build release

package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveBinaryMaterializesDistribution guards the release packaging:
// pi resolves theme/ and package.json relative to its own executable, so the
// extracted cache dir must contain the whole distribution, not just the
// binary.
func TestResolveBinaryMaterializesDistribution(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	binPath, err := ResolveBinary()
	require.NoError(t, err)
	info, err := os.Stat(binPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	distDir := filepath.Dir(binPath)
	require.FileExists(t, filepath.Join(distDir, "package.json"))
	require.FileExists(t, filepath.Join(distDir, "theme", "dark.json"))
	require.FileExists(t, filepath.Join(distDir, "theme", "light.json"))

	// A second call must reuse the materialized cache dir.
	cachedPath, err := ResolveBinary()
	require.NoError(t, err)
	require.Equal(t, binPath, cachedPath)
}
