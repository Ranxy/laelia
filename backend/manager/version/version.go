// Package version carries the manager binary build metadata. It is injected
// at build time via -ldflags -X (see scripts/build_laelia.sh and the manager
// Dockerfile) so the running binary reports the exact version, git commit, and
// build time used to produce it.
package version

// Version is the manager binary version. "dev" for local go build/run;
// release builds overwrite it with the build VERSION.
var Version = "dev"

// GitCommit is the git commit hash the manager binary was built from.
var GitCommit = "unknown"

// BuildTime is the UTC build timestamp of the manager binary.
var BuildTime = "unknown"
