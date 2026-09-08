package kubernetes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Ranxy/laelia/backend/common/machineparam"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
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
	assert.Equal(t, "laelia-machine-a1b2c3d4", backend.WorkloadStem("a1b2c3d4-e5f6-7890-abcd-ef0123456789"))
	assert.Equal(t, "laelia-machine-abc123", backend.WorkloadStem("abc123"))
}

func TestMachineStateJSONShape(t *testing.T) {
	spec := backend.MachineSpec{
		MachineID:  "a1b2c3d4-e5f6-7890-abcd-ef0123456789",
		ManagerURL: "http://manager:8181",
	}
	now := time.Now()
	data := backend.MachineStateJSON(spec, "llmach_token")
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

func TestOwnerRefName(t *testing.T) {
	pod := &corev1.Pod{}
	pod.SetOwnerReferences([]metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "laelia-provisioner-7b8c9d"},
		{Kind: "ConfigMap", Name: "ignored"},
	})
	assert.Equal(t, "laelia-provisioner-7b8c9d", ownerRefName(pod, "ReplicaSet"))
	assert.Equal(t, "", ownerRefName(pod, "Deployment"))
	assert.Equal(t, "", ownerRefName(&corev1.Pod{}, "ReplicaSet"))
}

func TestApplyMachineParams(t *testing.T) {
	t.Run("overrides config defaults", func(t *testing.T) {
		res := resourcesFor(backend.Resources{
			Requests: map[string]string{"cpu": "1", "memory": "2Gi"},
			Limits:   map[string]string{"memory": "4Gi"},
		})
		storage := &laeliav1.LaeliaMachineStorageSpec{
			Size:             resource.MustParse("10Gi"),
			StorageClassName: "standard",
		}
		res, err := applyMachineParams(res, storage, map[string]string{
			"cpu": "4", "memory": "8Gi", "disk": "20Gi", "storage_class": "fast-ssd",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		// One knob sets request AND limit to the same value (Guaranteed QoS).
		assert.Equal(t, resource.MustParse("4"), res.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("4"), res.Limits[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("8Gi"), res.Requests[corev1.ResourceMemory])
		assert.Equal(t, resource.MustParse("8Gi"), res.Limits[corev1.ResourceMemory])
		assert.Equal(t, resource.MustParse("20Gi"), storage.Size)
		assert.Equal(t, "fast-ssd", storage.StorageClassName)
	})

	t.Run("creates resources when config has none", func(t *testing.T) {
		res, err := applyMachineParams(nil, &laeliav1.LaeliaMachineStorageSpec{}, map[string]string{"cpu": "2"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, resource.MustParse("2"), res.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("2"), res.Limits[corev1.ResourceCPU])
	})

	t.Run("absent keys keep config defaults", func(t *testing.T) {
		res := resourcesFor(backend.Resources{Requests: map[string]string{"cpu": "1"}})
		storage := &laeliav1.LaeliaMachineStorageSpec{Size: resource.MustParse("10Gi")}
		res, err := applyMachineParams(res, storage, nil)
		require.NoError(t, err)
		assert.Equal(t, resource.MustParse("1"), res.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("10Gi"), storage.Size)
	})

	t.Run("unknown keys ignored", func(t *testing.T) {
		// A key persisted by a newer manager must never reject an older
		// provisioner binary (design Appendix A, finding F2).
		res, storage := resourcesFor(backend.Resources{}), &laeliav1.LaeliaMachineStorageSpec{}
		res, err := applyMachineParams(res, storage, map[string]string{"gpu": "1", "cpu": "1"})
		require.NoError(t, err)
		assert.Equal(t, resource.MustParse("1"), res.Requests[corev1.ResourceCPU])
		assert.True(t, storage.Size.IsZero())
	})

	t.Run("bad quantity fails the provision", func(t *testing.T) {
		res := resourcesFor(backend.Resources{})
		_, err := applyMachineParams(res, &laeliav1.LaeliaMachineStorageSpec{}, map[string]string{"disk": "abc"})
		require.Error(t, err)
	})
}

func TestMachineParamsSchema(t *testing.T) {
	b := &Backend{
		cfg: backend.Config{
			Resources:   backend.Resources{Requests: map[string]string{"cpu": "1", "memory": "2Gi"}},
			Storage:     backend.Storage{Size: "10Gi", StorageClassName: "standard"},
			ParamBounds: map[string]backend.ParamBounds{"cpu": {Min: "250m", Max: "8"}},
		},
	}
	// storageSize is resolved at New; set it directly for the unit.
	b.storageSize = resource.MustParse("10Gi")

	specs := b.MachineParams()
	require.Len(t, specs, 4) // cpu, memory, disk, storage_class

	byKey := map[string]*storepb.MachineParamSpec{}
	for _, s := range specs {
		byKey[s.GetKey()] = s
	}
	assert.Equal(t, "1", byKey[machineparam.CPU].GetDefaultValue())
	assert.Equal(t, "250m", byKey[machineparam.CPU].GetMinValue())
	assert.Equal(t, "8", byKey[machineparam.CPU].GetMaxValue())
	assert.Equal(t, "2Gi", byKey[machineparam.Memory].GetDefaultValue())
	assert.Equal(t, "", byKey[machineparam.Memory].GetMinValue())
	assert.Equal(t, "10Gi", byKey[machineparam.Disk].GetDefaultValue())
	assert.Equal(t, "standard", byKey[machineparam.StorageClass].GetDefaultValue())

	// Without a configured storage class the param is not offered: a
	// free-text class against an unknown cluster is not surfaced to users.
	b.cfg.Storage.StorageClassName = ""
	assert.Len(t, b.MachineParams(), 3)
}
