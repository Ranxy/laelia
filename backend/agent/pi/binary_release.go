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

	"github.com/Ranxy/laelia/backend/agent/home"
)

// resolveBinary extracts the embedded pi distribution to a per-machine cache
// directory (content-addressed by a hash so it is written once and reused)
// and returns the pi binary path. An embedded blob cannot be exec'd directly,
// so the binary is materialized on disk with mode 0700 alongside its assets.
func resolveBinary(embeddedDist embed.FS, subPath string) (string, error) {
	distFS, err := fs.Sub(embeddedDist, subPath)
	if err != nil {
		return "", err
	}
	sum, err := distributionHash(distFS)
	if err != nil {
		return "", err
	}
	dir := home.Join("bin", "pi-"+hex.EncodeToString(sum[:8])+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	binName := "pi"
	if runtime.GOOS == "windows" {
		binName = "pi.exe"
	}
	binPath := filepath.Join(dir, binName)
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
		if name == binName {
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
