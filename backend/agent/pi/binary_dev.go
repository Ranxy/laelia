//go:build !release

package pi

import (
	"errors"
	"os"
)

// ResolveBinary returns the pi executable path in a dev build: read from the
// LAELIA_PI_BINARY env var (default devPiBinaryDefault). No embedding, no
// download — fast iteration against a local checkout.
func ResolveBinary() (string, error) {
	path := os.Getenv("LAELIA_PI_BINARY")
	if path == "" {
		return "", errors.New("pi: dev binary not config, please config the LAELIA_PI_BINARY env")
	}
	if _, err := os.Stat(path); err != nil {
		return "", errors.New("pi: dev binary not found at " + path + " (set LAELIA_PI_BINARY)")
	}
	return path, nil
}
