package provider

import (
	"context"
	"os"
	"strings"
)

type probeEnvOverlayKey struct{}

// WithProbeEnv returns a context whose provider probe subprocesses and model
// cache lookups inherit overlay (KEY=VALUE entries) on top of the host
// environment. Used by the per-agent model refresh so an agent's custom_env
// (e.g. CODEX_HOME pointing at a profile-specific codex home) is honored during
// probing.
func WithProbeEnv(ctx context.Context, overlay []string) context.Context {
	return context.WithValue(ctx, probeEnvOverlayKey{}, overlay)
}

// probeEnv returns the environment for a provider probe subprocess. It inherits
// the host environment so the provider binary resolves on PATH and its own
// config (~/.opencode, ~/.claude, ~/.codex, ...) is reachable, then overlays
// any probe-env entries carried in ctx (last wins). Per-agent allow_env /
// custom_env filtering does not apply during probing beyond the overlay.
func probeEnv(ctx context.Context) []string {
	env := os.Environ()
	overlay := probeEnvOverlay(ctx)
	if len(overlay) == 0 {
		return env
	}
	merged := make(map[string]string, len(env)+len(overlay))
	for _, kv := range env {
		key, value, ok := strings.Cut(kv, "=")
		if ok {
			merged[key] = value
		}
	}
	for _, kv := range overlay {
		key, value, ok := strings.Cut(kv, "=")
		if ok {
			merged[key] = value
		}
	}
	out := make([]string, 0, len(merged))
	for key, value := range merged {
		out = append(out, key+"="+value)
	}
	return out
}

// probeEnvValue returns the effective value of an env var during probing,
// preferring an overlay entry set with WithProbeEnv over the host environment.
// It lets model-cache lookups (e.g. codex's CODEX_HOME) honor the same overlay
// the probe subprocess runs with.
func probeEnvValue(ctx context.Context, key string) string {
	for _, kv := range probeEnvOverlay(ctx) {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return os.Getenv(key)
}

// probeEnvOverlay returns the probe-env overlay carried in ctx, or nil when none
// was set with WithProbeEnv.
func probeEnvOverlay(ctx context.Context) []string {
	v := ctx.Value(probeEnvOverlayKey{})
	if v == nil {
		return nil
	}
	if overlay, ok := v.([]string); ok {
		return overlay
	}
	return nil
}
