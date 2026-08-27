//go:build release && no_embed_pi

package pi

import "errors"

// ResolveBinary returns an error in a release build that intentionally does
// not embed the pi runtime. The builtin-pi provider is therefore unavailable;
// a user-installed pi on PATH is still detected and used through the separate
// user-pi provider path.
func ResolveBinary() (string, error) {
	return "", errors.New("pi: this machine build does not embed the pi runtime; use a pi-embedded machine or install pi yourself and use the user-pi provider")
}
