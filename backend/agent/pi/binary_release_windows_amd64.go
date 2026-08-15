//go:build release && windows && amd64

package pi

import "embed"

//go:embed embedded/dist-windows-amd64
var embeddedDist embed.FS

// ResolveBinary extracts the embedded pi distribution for windows/amd64.
func ResolveBinary() (string, error) {
	return resolveBinary(embeddedDist, "embedded/dist-windows-amd64")
}
