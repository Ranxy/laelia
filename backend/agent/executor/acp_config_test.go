package executor

import (
	"testing"

	"github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestBuildACPConfigDerivesCommandFromProvider(t *testing.T) {
	cfg := BuildACPConfig(&v1pb.AgentACPConfig{
		Provider:  "opencode",
		Model:     "gpt-4o",
		CustomEnv: map[string]string{"FOO": "bar"},
		AllowEnv:  []string{"PATH"},
	}, "agent-123")
	require.NotNil(t, cfg)
	assert.Equal(t, "opencode", cfg.Executable)
	// opencode provider builds `opencode acp --pure --cwd <workingDir>`.
	assert.Contains(t, cfg.Args, "acp")
	assert.Equal(t, "gpt-4o", cfg.Model)
	assert.Equal(t, "bar", cfg.CustomEnv["FOO"])
	assert.Contains(t, cfg.WorkingDir, "agent-123")
}

func TestBuildACPConfigFallsBackToRawExecutableForCustom(t *testing.T) {
	cfg := BuildACPConfig(&v1pb.AgentACPConfig{
		Provider:   "custom",
		Executable: "npx",
		Args:       []string{"-y", "some-acp@latest"},
	}, "agent-1")
	require.NotNil(t, cfg)
	assert.Equal(t, "npx", cfg.Executable)
	assert.Equal(t, []string{"-y", "some-acp@latest"}, cfg.Args)
}

func TestBuildACPConfigNilWhenUnconfigured(t *testing.T) {
	// No provider and no executable -> not configured.
	assert.Nil(t, BuildACPConfig(&v1pb.AgentACPConfig{}, "agent-1"))
	// Unknown provider with no executable -> still not configured.
	assert.Nil(t, BuildACPConfig(&v1pb.AgentACPConfig{Provider: "no-such"}, "agent-1"))
	// But unknown provider WITH executable falls back to custom path.
	cfg := BuildACPConfig(&v1pb.AgentACPConfig{Provider: "no-such", Executable: "weird"}, "agent-1")
	require.NotNil(t, cfg)
	assert.Equal(t, "weird", cfg.Executable)
}

func TestBuildACPEnvOverlaysCustomEnvOverAllowEnv(t *testing.T) {
	t.Setenv("MY_SHARED", "inherited")
	cfg := &ACPConfig{
		AllowEnv:  []string{"MY_SHARED", "PATH"},
		CustomEnv: map[string]string{"MY_SHARED": "overridden", "EXTRA": "added"},
	}
	env := buildACPEnv(cfg, nil, Request{})
	got := envSliceToMap(env)
	assert.Equal(t, "overridden", got["MY_SHARED"], "custom env must override inherited allow_env value")
	assert.Equal(t, "added", got["EXTRA"])
	assert.NotEmpty(t, got["PATH"], "inherited allow_env value for PATH should remain")
}

func TestBuildACPEnvBootstrapOverridesCustomEnv(t *testing.T) {
	cfg := &ACPConfig{
		CustomEnv: map[string]string{"LAELIA_COMMAND": "hijack"},
	}
	env := buildACPEnv(cfg, nil, Request{CommandID: "cmd-1"})
	got := envSliceToMap(env)
	assert.Equal(t, "cmd-1", got["LAELIA_COMMAND"], "bootstrap LAELIA_* must win over custom env")
}

func TestModelOptionContains(t *testing.T) {
	ungrouped := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "m1", Name: "M1"},
		{Value: "m2", Name: "M2"},
	}
	opts := acp.SessionConfigSelectOptions{Ungrouped: &ungrouped}
	assert.True(t, modelOptionContains(opts, "m1"))
	assert.False(t, modelOptionContains(opts, "mX"))

	grouped := acp.SessionConfigSelectOptionsGrouped{
		{Group: "g", Name: "G", Options: []acp.SessionConfigSelectOption{{Value: "g1", Name: "G1"}}},
	}
	assert.True(t, modelOptionContains(acp.SessionConfigSelectOptions{Grouped: &grouped}, "g1"))
}

// TestResolvedCommandUsesRegistry confirms the registry hook is wired: a
// built-in provider id resolves through provider.Default().Lookup.
func TestResolvedCommandUsesRegistry(t *testing.T) {
	p, ok := provider.Default().Lookup("opencode")
	require.True(t, ok)
	exe, args := p.BuildCommand("/ws")
	assert.Equal(t, "opencode", exe)
	assert.Equal(t, []string{"acp", "--pure", "--cwd", "/ws"}, args)
}

func envSliceToMap(env []string) map[string]string {
	m := map[string]string{}
	for _, e := range env {
		k, v, ok := splitOnce(e, '=')
		if ok {
			m[k] = v
		}
	}
	return m
}

func splitOnce(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
