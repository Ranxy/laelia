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

	// The kubernetes backend hosts the operator; the docker stub registers
	// under the same name and fails fast with ErrUnsupportedBackend until its
	// implementation lands.
	_ "github.com/Ranxy/laelia/backend/provisioner/backend/docker"
	_ "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes"
)

func main() {
	if err := provisionercmd.Execute(); err != nil {
		// A manager-requested shutdown (token rotated / provisioner deleted)
		// exits with a specific log line instead of retrying with a dead
		// credential; a permanent auth rejection logs why it was rejected and
		// how to fix it; anything else logs the failure.
		switch {
		case errors.Is(err, client.ErrShutdown):
			slog.Error("provisioner stopped: the manager shut it down; register or rotate the provisioner to get a new token")
		case client.IsPermanentAuthFailure(err):
			slog.Error("laelia-provisioner will not start: the manager rejected its credential; fix the token and restart, see REASON",
				"error", err, "REASON", client.DescribeAuthFailure(err))
		default:
			slog.Error("laelia-provisioner exited", "error", err)
		}
		os.Exit(1)
	}
}
