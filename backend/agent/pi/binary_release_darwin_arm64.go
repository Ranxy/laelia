//go:build release && darwin && arm64

package pi

import "embed"

//go:embed embedded/dist-darwin-arm64
var embeddedDist embed.FS

// ResolveBinary extracts the embedded pi distribution for darwin/arm64.
func ResolveBinary() (string, error) {
	return resolveBinary(embeddedDist, "embedded/dist-darwin-arm64")
}
