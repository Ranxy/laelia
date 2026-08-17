// Package machinebuild exposes the machine binary build embedded in this
// manager (manifest version + per-platform checksums) to the API layer. The
// manifest bytes are owned by the server package (build-tag embed); the route
// registration pushes them here at startup so api/v1 can read them without an
// import cycle.
package machinebuild

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

// Target is one platform entry of the embedded machine manifest.
type Target struct {
	File   string `json:"file"`
	Sha256 string `json:"sha256"`
	Gz     struct {
		File   string `json:"file"`
		Sha256 string `json:"sha256"`
	} `json:"gz"`
}

type manifest struct {
	Version string            `json:"version"`
	Targets map[string]Target `json:"targets"`
}

var (
	mu      sync.RWMutex
	current *manifest
)

// SetManifest installs the embedded manifest (parsed once). An empty or
// unparseable payload disables upgrade offering.
func SetManifest(data []byte) {
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil || m.Version == "" {
		return
	}
	mu.Lock()
	current = &m
	mu.Unlock()
}

// LatestVersion returns the embedded machine binary version, or "" when the
// manager has no embedded binaries.
func LatestVersion() string {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return ""
	}
	return current.Version
}

// GetTarget returns the manifest entry for a target (e.g. "linux-x64").
func GetTarget(target string) (Target, bool) {
	mu.RLock()
	defer mu.RUnlock()
	if current == nil {
		return Target{}, false
	}
	t, ok := current.Targets[target]
	return t, ok
}

// UpgradeAvailable reports whether the running machine version is older than
// the embedded latest. Versions that do not parse as dotted numerics (e.g.
// "dev", "local") never trigger an upgrade prompt.
func UpgradeAvailable(currentVersion, latestVersion string) bool {
	if currentVersion == "" || latestVersion == "" {
		return false
	}
	return CompareVersions(currentVersion, latestVersion) < 0
}

// CompareVersions compares two dotted-numeric versions (an optional leading
// "v" is accepted), returning -1/0/+1. Unparseable versions compare equal
// (0), so a dev build ("dev", "local") never looks older than a release.
func CompareVersions(a, b string) int {
	as, aok := parseVersion(a)
	bs, bok := parseVersion(b)
	if !aok || !bok {
		return 0
	}
	for i := 0; i < 3; i++ {
		if as[i] != bs[i] {
			if as[i] < bs[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parseVersion extracts up to three numeric components. ok is false when the
// version is not a dotted-numeric string (e.g. "dev", "local", ""), in which
// case callers treat it as incomparable (equal to everything).
func parseVersion(v string) (out [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return [3]int{}, false
	}
	parts := strings.SplitN(v, ".", 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
