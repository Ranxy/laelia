package cmd

// The shipped example config is the copy-template users start from
// (docs/deploy.md §8.3): these guards keep both documented variants loadable,
// so a config-surface change that drifts the example fails CI.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goyaml "go.yaml.in/yaml/v3"

	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

func TestExampleConfigParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "provisioner.example.yaml"))
	require.NoError(t, err)

	var cfg Config
	require.NoError(t, goyaml.Unmarshal(data, &cfg))
	assert.Equal(t, "kubernetes", cfg.Backend)
	assert.Equal(t, "laelia-machines", cfg.Namespace)
	assert.Equal(t, "10Gi", cfg.Storage.Size)
	assert.True(t, cfg.Resources.Limits["cpu"] == "2" && cfg.Resources.Limits["memory"] == "4Gi")
	assert.Contains(t, cfg.ParamBounds, "cpu")
	assert.Equal(t, "true", cfg.ExtraEnv["LAELIA_INSECURE"])

	// The backend-neutral slice carries what the docker variant needs too.
	bc := cfg.backendConfig()
	assert.Equal(t, "10Gi", bc.Storage.Size)
	assert.Equal(t, backend.ParamBounds{Min: "250m", Max: "8"}, bc.ParamBounds["cpu"])
}

func TestExampleConfigDockerVariantParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "provisioner.example.yaml"))
	require.NoError(t, err)

	lines := strings.Split(string(data), "\n")
	var variant []string
	inBlock := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "# docker-variant-begin":
			inBlock = true
		case "# docker-variant-end":
			inBlock = false
		default:
			if inBlock {
				require.True(t, strings.HasPrefix(line, "#   "),
					"the docker variant block must keep its '#   ' comment prefix: %q", line)
				variant = append(variant, strings.TrimPrefix(line, "#   "))
			}
		}
	}
	require.NotEmpty(t, variant, "the docker variant block must be present")

	var cfg Config
	require.NoError(t, goyaml.Unmarshal([]byte(strings.Join(variant, "\n")), &cfg))
	assert.Equal(t, "docker", cfg.Backend)
	assert.Equal(t, "2", cfg.Resources.Limits["cpu"])
	assert.Empty(t, cfg.Namespace, "docker ignores namespace")
	assert.Empty(t, cfg.Storage.Size, "docker ignores storage sizing")
}
