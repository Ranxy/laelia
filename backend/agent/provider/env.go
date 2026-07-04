package provider

import "os"

// probeEnv returns the environment for a provider probe subprocess. The probe
// inherits the host environment so the provider binary resolves on PATH and
// its own config (~/.opencode, ~/.claude, ...) is reachable. Per-agent
// allow_env / custom_env filtering does not apply during probing.
func probeEnv() []string {
	return os.Environ()
}
