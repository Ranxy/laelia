//go:build release && linux && amd64

package pi

import "embed"

//go:embed embedded/dist-linux-amd64
var embeddedDist embed.FS

// ResolveBinary extracts the embedded pi distribution for linux/amd64.
func ResolveBinary() (string, error) {
	return resolveBinary(embeddedDist, "embedded/dist-linux-amd64")
}
