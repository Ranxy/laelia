// Package version carries the machine binary version. It is injected at
// build time via -ldflags -X (see scripts/build-embedded-machines.sh) so the
// running binary reports exactly the version recorded in the manager's
// embedded manifest.
package version

// Version is the machine binary version. "dev" for local go build/run;
// release builds overwrite it with the build VERSION.
var Version = "dev"
