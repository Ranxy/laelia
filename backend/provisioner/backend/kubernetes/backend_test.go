package kubernetes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/Ranxy/laelia/backend/provisioner/backend"
	laeliav1 "github.com/Ranxy/laelia/backend/provisioner/backend/kubernetes/api/v1"
)

func TestRegisteredAsKubernetes(t *testing.T) {
	// The registry lookup precedes namespace/rest-config resolution, so an
	// empty config fails inside the factory — proving it was registered
	// (an unregistered name would fail with ErrUnsupportedBackend instead).
	_, err := New(backend.Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace is required")
	assert.NotContains(t, err.Error(), "unsupported provisioner backend")
}

func TestWorkloadStem(t *testing.T) {
	assert.Equal(t, "laelia-machine-a1b2c3d4", workloadStem("a1b2c3d4-e5f6-7890-abcd-ef0123456789"))
	assert.Equal(t, "laelia-machine-abc123", workloadStem("abc123"))
}

func TestMachineStateJSONShape(t *testing.T) {
	spec := backend.MachineSpec{
		MachineID:  "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		ManagerURL: "http://manager:8181",
	}
	now := time.Now()
	data := machineStateJSON(spec, "llmach_token")
	var st struct {
		ManagerURL   string    `json:"manager_url"`
		MachineID    string    `json:"machine_id"`
		RefreshToken string    `json:"refresh_token"`
		Hostname     string    `json:"hostname"`
		CreatedAt    time.Time `json:"created_at"`
	}
	require.NoError(t, json.Unmarshal(data, &st))
	assert.Equal(t, "http://manager:8181", st.ManagerURL)
	assert.Equal(t, spec.MachineID, st.MachineID)
	assert.Equal(t, "llmach_token", st.RefreshToken)
	assert.Equal(t, "laelia-machine-a1b2c3d4", st.Hostname)
	assert.WithinDuration(t, now, st.CreatedAt, time.Minute)
}

func TestEnvVarsForAndResourcesFor(t *testing.T) {
	env := envVarsFor(map[string]string{"LAELIA_INSECURE": "true", "LAELIA_DEBUG": "1"})
	require.Len(t, env, 2)
	assert.Equal(t, "LAELIA_DEBUG", env[0].Name) // sorted
	assert.Equal(t, "LAELIA_INSECURE", env[1].Name)
	assert.Nil(t, envVarsFor(nil))

	res := resourcesFor(backend.Resources{
		Requests: map[string]string{"cpu": "1", "memory": "2Gi"},
		Limits:   map[string]string{"memory": "4Gi"},
	})
	require.NotNil(t, res)
	assert.Equal(t, resource.MustParse("2Gi"), res.Requests[corev1.ResourceMemory])
	assert.Equal(t, resource.MustParse("1"), res.Requests[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("4Gi"), res.Limits[corev1.ResourceMemory])
	assert.Nil(t, resourcesFor(backend.Resources{}))
}

func TestLabelsCarryMachineIdentity(t *testing.T) {
	spec := backend.MachineSpec{
		MachineID: "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		Labels:    map[string]string{"owner": "alice"},
	}
	cr := crLabels(spec)
	assert.Equal(t, "laelia-machine-a1b2c3d4", cr[laeliav1.MachineNameLabel])
	assert.Equal(t, laeliav1.ManagedByValue, cr[laeliav1.ManagedByLabel])
	assert.Equal(t, "alice", cr["owner"])

	secret := secretLabels(spec)
	assert.Equal(t, "laelia-machine-a1b2c3d4", secret[laeliav1.MachineNameLabel])
}
