// Package home resolves the Laelia data root directory. All machine-local
// state (machine.json, daemon.sock, agent workspaces, pi cache) lives under
// this root. LAELIA_HOME overrides the default ~/.laelia.
package home

import (
	"os"
	"path/filepath"
)

// EnvDir is the environment variable that overrides the Laelia data root.
const EnvDir = "LAELIA_HOME"

// Dir returns the Laelia data root directory. If LAELIA_HOME is set, it is
// used directly (made absolute); otherwise it defaults to ~/.laelia.
func Dir() string {
	if d := os.Getenv(EnvDir); d != "" {
		if abs, err := filepath.Abs(d); err == nil {
			return abs
		}
		return d
	}

	h, err := os.UserHomeDir()
	if err != nil {
		h = os.Getenv("HOME")
	}
	return filepath.Join(h, ".laelia")
}

// Join returns a path under the Laelia data root.
func Join(elem ...string) string {
	return filepath.Join(append([]string{Dir()}, elem...)...)
}
