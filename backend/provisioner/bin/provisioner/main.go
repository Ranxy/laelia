// Command laelia-provisioner is the enterprise provisioner binary: it connects
// out to the laelia manager with its provisioner token and creates machine
// workloads on demand through the configured backend (kubernetes operator
// today; docker/VM backends plug in behind the same interface).
//
// Release builds stamp the version metadata via -ldflags -X, e.g.:
//
//	go build -ldflags "-w -s \
//	  -X github.com/Ranxy/laelia/backend/provisioner/version.Version=$VERSION \
//	  -X github.com/Ranxy/laelia/backend/provisioner/version.GitCommit=$COMMIT" \
//	  ./backend/provisioner/bin/provisioner
package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/Ranxy/laelia/backend/provisioner/client"
	provisionercmd "github.com/Ranxy/laelia/backend/provisioner/cmd"

	// Registry stubs: known backend names fail with a clear
	// ErrUnsupportedBackend message until their implementations land.
	_ "github.com/Ranxy/laelia/backend/provisioner/backend/docker"
)

func main() {
	if err := provisionercmd.Execute(); err != nil {
		// A manager-requested shutdown (token rotated / provisioner deleted)
		// exits with a specific log line instead of retrying with a dead
		// credential; anything else logs the failure.
		if errors.Is(err, client.ErrShutdown) {
			slog.Error("provisioner stopped: its credential is no longer valid; register or rotate the provisioner to get a new token")
		} else {
			slog.Error("laelia-provisioner exited", "error", err)
		}
		os.Exit(1)
	}
}
