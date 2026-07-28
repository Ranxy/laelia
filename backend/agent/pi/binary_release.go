//go:build release

package pi

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// embeddedPi is the standalone pi binary baked in at compile time. The release
// build script (scripts/build-pi.sh) downloads/builds the target-platform pi
// binary into backend/agent/pi/embedded/pi before `go build -tags release`.
//
//go:embed embedded/pi
var embeddedPi []byte

// ResolveBinary extracts the embedded pi binary to a per-machine cache file
// (content-addressed by a hash so it is written once and reused) and returns
// that path. An embedded blob cannot be exec'd directly, so this materializes
// it on disk with mode 0700.
func ResolveBinary() (string, error) {
	if len(embeddedPi) == 0 {
		return "", errors.New("pi: embedded binary is empty; release build did not embed pi")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(embeddedPi)
	name := "pi-" + hex.EncodeToString(sum[:8]) + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".bin"
	dir := filepath.Join(home, ".laelia", "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if existing, err := os.Stat(path); err == nil && existing.Size() == int64(len(embeddedPi)) {
		return path, nil
	}
	if err := os.WriteFile(path, embeddedPi, 0o700); err != nil {
		return "", err
	}
	return path, nil
}
