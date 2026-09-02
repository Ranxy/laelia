// Package version carries the provisioner binary build metadata. It is
// injected at build time via -ldflags -X so the running provisioner reports
// exactly the version shown in the manager's provisioner status:
//
//	go build -ldflags "-X github.com/Ranxy/laelia/backend/provisioner/version.Version=x.y.z ..."
package version

// Version is the provisioner binary version. "dev" for local go build/run;
// release builds overwrite it with the build VERSION.
var Version = "dev"

// GitCommit is the git commit hash the provisioner binary was built from.
var GitCommit = "unknown"

// BuildTime is the UTC build timestamp of the provisioner binary.
var BuildTime = "unknown"
