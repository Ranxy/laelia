// The env-gated integration suite (design §13): a real Docker Engine daemon
// drives the full backend journey — provision → container running →
// PROVISIONED → replay idempotency → out-of-band removal recovery →
// deprovision with and without data retention. Skips unless
// LAELIA_RUN_DOCKER_TESTS=1 with a reachable daemon (local socket or
// DOCKER_HOST).
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

// testRuntimeImage is the minimal image the integration builds: plain
// busybox, deliberately WITHOUT any laelia entrypoint — the injected startup
// must carry the whole machine launch on any contract image.
const testRuntimeImage = "laelia-test-machine-runtime:integration"

const fakeBootstrap = `#!/bin/sh
set -eu
mkdir -p /data/bin
printf '#!/bin/sh\nwhile true; do sleep 30; done\n' > /data/bin/laelia-machine
chmod 0755 /data/bin/laelia-machine
if [ ! -f /data/laelia/machine.json ]; then
  if [ ! -f /bootstrap/machine.json ]; then
    echo "no machine.json on the volume and no bootstrap payload" >&2
    exit 1
  fi
  mkdir -p /data/laelia
  cp /bootstrap/machine.json /data/laelia/machine.json
fi
`

const testMachineID = "a1b2c3d4-e5f6-7890-abcd-ef0123456789"

func TestDockerBackendIntegration(t *testing.T) {
	if os.Getenv("LAELIA_RUN_DOCKER_TESTS") != "1" {
		t.Skip("set LAELIA_RUN_DOCKER_TESTS=1 with a reachable docker daemon to run")
	}

	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	buildTestRuntimeImage(t, api)

	b, err := New(backend.Config{
		StatePath: filepath.Join(t.TempDir(), "docker-specs.json"),
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	eventCh := make(chan backend.Event, 128)
	require.NoError(t, b.Start(ctx, eventCh))

	spec := backend.MachineSpec{
		MachineID:       testMachineID,
		Title:           "integration machine",
		ManagerURL:      "http://127.0.0.1:1", // never dialed by the fake bootstrap
		Fingerprint:     "integration-fingerprint",
		RuntimeImage:    testRuntimeImage,
		BinaryTarget:    "linux-x64",
		BootstrapScript: fakeBootstrap,
		Labels:          map[string]string{"team": "backend-test"},
		Params:          map[string]string{"cpu": "0.1", "memory": "64Mi"},
	}

	t.Cleanup(func() { _ = b.Deprovision(context.Background(), testMachineID, false) })

	// 1. Provision → PROVISIONING ack → PROVISIONED (container running).
	require.NoError(t, b.Provision(ctx, spec, "llmach_test_token"))
	waitForEvent(t, eventCh, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED)
	name := backend.WorkloadStem(testMachineID)
	insp, err := api.ContainerInspect(ctx, name)
	require.NoError(t, err)
	require.True(t, insp.State.Running, "the machine container must be running")
	assert.Equal(t, container.RestartPolicyUnlessStopped, insp.HostConfig.RestartPolicy.Name)
	_, err = api.VolumeInspect(ctx, dataVolumeName(name))
	require.NoError(t, err, "the data volume must exist")
	// The credential was copied into the created container, never env.
	for _, env := range insp.Config.Env {
		assert.NotContains(t, env, "llmach_test_token")
	}

	// 2. Replay in sync → strict no-op: still exactly one container for this
	//    machine (the daemon may host other provisioners' machines).
	require.NoError(t, b.Provision(ctx, spec, "llmach_rotated_token"))
	list, err := api.ContainerList(ctx, container.ListOptions{All: true,
		Filters: filters.NewArgs(filters.Arg("label", machineIDLabel+"="+testMachineID))})
	require.NoError(t, err)
	require.Len(t, list, 1)

	// 3. Out-of-band removal → journal-based recovery rebuilds the container
	//    (no credential injection — the volume copy is authoritative).
	require.NoError(t, api.ContainerRemove(ctx, name, container.RemoveOptions{Force: true}))
	require.Eventually(t, func() bool {
		insp, err := api.ContainerInspect(ctx, name)
		return err == nil && insp.State.Running
	}, 30*time.Second, 500*time.Millisecond, "the reconcile loop must recreate the removed container")

	// 4. Deprovision keeping data: container gone, volume retained, DELETED.
	require.NoError(t, b.Deprovision(ctx, testMachineID, true))
	waitForEvent(t, eventCh, storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED)
	if _, err := api.ContainerInspect(ctx, name); !cerrdefs.IsNotFound(err) {
		t.Fatalf("the container must be gone after deprovision (err=%v)", err)
	}
	if _, err := api.VolumeInspect(ctx, dataVolumeName(name)); err != nil {
		t.Fatalf("keep_data must retain the data volume: %v", err)
	}

	// 5. Re-provision on the retained volume, then deprovision fully: the
	//    volume is removed with the workload.
	require.NoError(t, b.Provision(ctx, spec, "llmach_test_token2"))
	waitForEvent(t, eventCh, storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED)
	require.NoError(t, b.Deprovision(ctx, testMachineID, false))
	waitForEvent(t, eventCh, storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED)
	if _, err := api.VolumeInspect(ctx, dataVolumeName(name)); !cerrdefs.IsNotFound(err) {
		t.Fatalf("the data volume must be removed with the workload (err=%v)", err)
	}
}

// waitForEvent polls the event stream until one machine's phase arrives
// (PROVISIONING acks may legitimately precede the awaited phase).
func waitForEvent(t *testing.T, ch <-chan backend.Event, phase storepb.ProvisioningPhase) {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.MachineID == testMachineID && e.Phase == phase {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for phase %s", phase)
		}
	}
}

// buildTestRuntimeImage assembles the minimal contract image: busybox plus a
// machine-runtime-entrypoint that execs the bootstrap-installed binary.
func buildTestRuntimeImage(t *testing.T, api *client.Client) {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	files := []struct {
		name string
		mode int64
		body string
	}{
		// Plain busybox: no ENTRYPOINT, no laelia anything — the injected
		// startup must carry the whole machine launch (design §2.2).
		{"Dockerfile", 0o644, "FROM busybox:latest\n"},
	}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: f.name, Mode: f.mode, Size: int64(len(f.body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	resp, err := api.ImageBuild(context.Background(), &buf, build.ImageBuildOptions{
		Tags: []string{testRuntimeImage},
	})
	if err != nil {
		t.Fatalf("failed to build the test runtime image: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("failed to drain the image build response: %v", err)
	}
	// The image list is authoritative for the tag's presence.
	images, err := api.ImageList(context.Background(), image.ListOptions{
		Filters: filters.NewArgs(filters.Arg("reference", testRuntimeImage)),
	})
	if err != nil || len(images) == 0 {
		t.Fatalf("the test runtime image must exist after build (err=%v)", err)
	}
}
