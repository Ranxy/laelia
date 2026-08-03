//go:build release

package pi

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// embeddedDist is the standalone pi distribution baked in at compile time.
// The release build script (scripts/build-pi.sh) downloads and extracts the
// target-platform distribution into backend/agent/pi/embedded/dist before
// `go build -tags release`. pi resolves its runtime assets (theme/,
// package.json, node_modules/, ...) relative to its executable, so the whole
// distribution is embedded and materialized together, not just the binary.
//
//go:embed embedded/dist
var embeddedDist embed.FS

// ResolveBinary extracts the embedded pi distribution to a per-machine cache
// directory (content-addressed by a hash so it is written once and reused)
// and returns the pi binary path. An embedded blob cannot be exec'd directly,
// so the binary is materialized on disk with mode 0700 alongside its assets.
func ResolveBinary() (string, error) {
	distFS, err := fs.Sub(embeddedDist, "embedded/dist")
	if err != nil {
		return "", err
	}
	sum, err := distributionHash(distFS)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".laelia", "bin", "pi-"+hex.EncodeToString(sum[:8])+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	binPath := filepath.Join(dir, "pi")
	if info, err := os.Stat(binPath); err == nil && info.Size() > 0 {
		return binPath, nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	if err := fs.WalkDir(distFS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, name)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := fs.ReadFile(distFS, name)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o600)
		if name == "pi" {
			mode = 0o700
		}
		return os.WriteFile(target, data, mode)
	}); err != nil {
		return "", err
	}
	return binPath, nil
}

// distributionHash returns a content hash over every embedded file so a
// distribution change (e.g. theme assets updated with the same pi binary)
// produces a fresh cache directory.
func distributionHash(distFS fs.FS) ([32]byte, error) {
	h := sha256.New()
	err := fs.WalkDir(distFS, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(distFS, name)
		if err != nil {
			return err
		}
		h.Write([]byte(name))
		h.Write(data)
		return nil
	})
	if err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
