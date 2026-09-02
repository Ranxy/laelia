// Package docker is the registry stub for a future docker/compose backend
// (design §9): it registers the "docker" backend name so configuration errors
// name it as known, but constructing one always reports
// backend.ErrUnsupportedBackend until the implementation lands.
package docker

import (
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// New always fails: the docker backend is not implemented yet.
func New(_ backend.Config) (backend.Backend, error) {
	return nil, backend.ErrUnsupportedBackend
}

func init() {
	backend.Register("docker", New)
}
