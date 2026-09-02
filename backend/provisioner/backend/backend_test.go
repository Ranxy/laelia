package backend_test

// The registry lives in package backend; the docker stub registers itself in
// init, so the assertions run from the external test package to avoid an
// import cycle.

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/provisioner/backend"

	// Registers the "docker" backend stub.
	_ "github.com/Ranxy/laelia/backend/provisioner/backend/docker"
)

func TestRegistryUnknownBackend(t *testing.T) {
	be, err := backend.New("does-not-exist", backend.Config{})
	require.Error(t, err)
	require.Nil(t, be)
	require.True(t, errors.Is(err, backend.ErrUnsupportedBackend),
		"unknown backends must report the sentinel so callers can branch on it")
	require.Contains(t, err.Error(), "does-not-exist")
	require.Contains(t, err.Error(), "docker", "the error lists the known backends")
}

func TestRegistryDockerStubFailsWithSentinel(t *testing.T) {
	require.Contains(t, backend.Known(), "docker", "the imported stub must be registered")
	be, err := backend.New("docker", backend.Config{})
	require.Error(t, err)
	require.Nil(t, be)
	require.True(t, errors.Is(err, backend.ErrUnsupportedBackend),
		"the docker stub reports ErrUnsupportedBackend until it is implemented")
}

func TestWorkloadName(t *testing.T) {
	require.Equal(t, "laelia-machines/laelia-machine-1f0a9c2d",
		backend.WorkloadName("laelia-machines", "1f0a9c2d-4e5b-4c6a-8d7e-0f1a2b3c4d5e"))
	require.Equal(t, "/laelia-machine-abc", backend.WorkloadName("", "abc"))
}
