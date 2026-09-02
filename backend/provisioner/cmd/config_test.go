package cmd

// Config load/merge, validation, and digest tests.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

func resetFlags(t *testing.T) {
	t.Helper()
	old := flags
	t.Cleanup(func() { flags = old })
	flags.manager, flags.token, flags.backend, flags.config = "", "", "", ""
}

func TestLoadConfigMergesFlagsOverYaml(t *testing.T) {
	resetFlags(t)
	path := filepath.Join(t.TempDir(), "provisioner.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
manager_url: https://manager.test
token: llprov_from_file
backend: kubernetes
namespace: laelia-machines
manager_url_override: http://manager.svc:8181
retain_data: true
auto_upgrade: true
resources:
  requests:
    cpu: "1"
    memory: 2Gi
  limits:
    memory: 4Gi
extra_env:
  LAELIA_INSECURE: "true"
`), 0o600))

	flags.config = path
	flags.token = "llprov_flag" // flag wins over the file

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://manager.test", cfg.ManagerURL)
	assert.Equal(t, "llprov_flag", cfg.Token)
	assert.Equal(t, "kubernetes", cfg.Backend)
	assert.Equal(t, "laelia-machines", cfg.Namespace)
	assert.Equal(t, "http://manager.svc:8181", cfg.ManagerURLOverride)
	assert.True(t, cfg.RetainData)
	assert.True(t, cfg.AutoUpgrade)
	assert.Equal(t, map[string]string{"cpu": "1", "memory": "2Gi"}, cfg.Resources.Requests)
	assert.Equal(t, map[string]string{"memory": "4Gi"}, cfg.Resources.Limits)
	assert.Equal(t, map[string]string{"LAELIA_INSECURE": "true"}, cfg.ExtraEnv)

	bc := cfg.backendConfig()
	assert.True(t, bc.RetainData)
	assert.Equal(t, "laelia-machines", bc.Namespace)
}

func TestLoadConfigFlagOverrides(t *testing.T) {
	resetFlags(t)
	path := filepath.Join(t.TempDir(), "provisioner.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
manager_url: https://manager.test
token: from-file
backend: kubernetes
`), 0o600))
	flags.config = path
	flags.manager = "https://other.test"

	cfg, err := loadConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://other.test", cfg.ManagerURL)
	assert.Equal(t, "from-file", cfg.Token)
	assert.Equal(t, "kubernetes", cfg.Backend)
}

func TestLoadConfigRequiresEssentials(t *testing.T) {
	resetFlags(t)

	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager_url")

	flags.manager = "https://manager.test"
	_, err = loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")

	flags.token = "llprov_x"
	_, err = loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend")

	flags.backend = "kubernetes"
	_, err = loadConfig()
	require.NoError(t, err)
}

func TestLoadConfigMissingFile(t *testing.T) {
	resetFlags(t)
	flags.config = filepath.Join(t.TempDir(), "absent.yaml")
	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read provisioner config")
}

func TestLoadConfigInvalidYaml(t *testing.T) {
	resetFlags(t)
	path := filepath.Join(t.TempDir(), "provisioner.yaml")
	require.NoError(t, os.WriteFile(path, []byte("manager_url: [unclosed"), 0o600))
	flags.config = path
	_, err := loadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse provisioner config")
}

func TestConfigDigest(t *testing.T) {
	cfg := &Config{
		Backend:            "kubernetes",
		Namespace:          "laelia-machines",
		ManagerURLOverride: "http://manager.svc:8181",
		Token:              "llprov_secret",
		Resources: backend.Resources{
			Requests: map[string]string{"cpu": "1"},
		},
	}
	d1 := cfg.Digest()
	d2 := (&Config{
		Backend:            "kubernetes",
		Namespace:          "laelia-machines",
		ManagerURLOverride: "http://manager.svc:8181",
		Token:              "a-different-token",
		Resources:          cfg.Resources,
	}).Digest()
	assert.Equal(t, 12, len(d1))
	assert.Equal(t, d1, d2, "the token must not affect the digest")

	// Map iteration order must not matter.
	shuffled := &Config{Backend: "kubernetes", ExtraEnv: map[string]string{"A": "1", "B": "2"}}
	same := &Config{Backend: "kubernetes", ExtraEnv: map[string]string{"B": "2", "A": "1"}}
	assert.Equal(t, shuffled.Digest(), same.Digest())

	changed := &Config{Backend: "kubernetes", Namespace: "other"}
	assert.NotEqual(t, cfg.Digest(), changed.Digest())
}
