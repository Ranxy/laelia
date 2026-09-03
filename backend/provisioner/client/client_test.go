package client

// Unit tests for the client's pure translation layer: job → spec, event →
// progress frame, permanent-failure classification, and construction
// validation. The full loop is exercised by the manager integration test.

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/provisioner/backend"
)

func TestNewValidatesConfiguration(t *testing.T) {
	be := &fakeBackend{}
	tests := []struct {
		name    string
		cfg     Config
		impl    backend.Backend
		wantErr string
	}{
		{"missing manager url", Config{Token: "t", Backend: "b"}, be, "manager_url"},
		{"missing token", Config{ManagerURL: "https://m.test", Backend: "b"}, be, "token"},
		{"missing backend", Config{ManagerURL: "https://m.test", Token: "t"}, be, "backend"},
		{"missing backend impl", Config{ManagerURL: "https://m.test", Token: "t", Backend: "b"}, nil, "backend implementation"},
		{"plain http refused", Config{ManagerURL: "http://m.test", Token: "t", Backend: "b"}, be, "plain HTTP"},
	}
	for _, tt := range tests {
		c, err := New(tt.cfg, tt.impl)
		require.Error(t, err, tt.name)
		assert.Nil(t, c, tt.name)
		assert.Contains(t, err.Error(), tt.wantErr, tt.name)
	}

	// allowHTTP unlocks plain http.
	c, err := New(Config{ManagerURL: "http://m.test", Token: "t", Backend: "b", AllowHTTP: true}, be)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestMachineSpecFromJobAppliesManagerURLOverride(t *testing.T) {
	c := &Client{cfg: Config{ManagerURLOverride: "http://manager.svc:8181"}}
	job := &v1pb.ProvisionMachineJob{
		Machine:      "machines/1f0a9c2d-4e5b-4c6a-8d7e-0f1a2b3c4d5e",
		Title:        "team workload",
		ManagerUrl:   "https://manager.test",
		Fingerprint:  "fingerprint-16",
		RuntimeImage: "laelia/machine-runtime:test",
		BinaryTarget: "linux-x64",
		MachineLabels: map[string]string{
			"provisioner": "prod-cluster", "owner": "ran",
		},
	}
	job.BootstrapScript = "#!/bin/sh\necho bootstrap\n"
	spec := c.machineSpecFromJob(job, "1f0a9c2d-4e5b-4c6a-8d7e-0f1a2b3c4d5e")
	assert.Equal(t, "1f0a9c2d-4e5b-4c6a-8d7e-0f1a2b3c4d5e", spec.MachineID)
	assert.Equal(t, "http://manager.svc:8181", spec.ManagerURL,
		"the override replaces the job's public manager URL")
	assert.Equal(t, "team workload", spec.Title)
	assert.Equal(t, "fingerprint-16", spec.Fingerprint)
	assert.Equal(t, "laelia/machine-runtime:test", spec.RuntimeImage)
	assert.Equal(t, "linux-x64", spec.BinaryTarget)
	assert.Equal(t, "#!/bin/sh\necho bootstrap\n", spec.BootstrapScript,
		"the manager-rendered bootstrap script travels to the backend verbatim")
	assert.Equal(t, job.GetMachineLabels(), spec.Labels)

	// Without the override the job's URL passes through.
	plain := &Client{cfg: Config{}}
	assert.Equal(t, "https://manager.test", plain.machineSpecFromJob(job, spec.MachineID).ManagerURL)
}

func TestHandleProvisionJobRejectsMalformedMachine(_ *testing.T) {
	c := &Client{cfg: Config{}}
	// Must not panic; malformed names are dropped with a log.
	c.handleProvisionJob(context.Background(), &v1pb.ProvisionMachineJob{Machine: "not-a-resource-name"})
	c.handleProvisionJob(context.Background(), nil)
}

func TestProgressFrameRoundTrip(t *testing.T) {
	e := backend.Event{
		MachineID:    "1f0a9c2d-4e5b-4c6a-8d7e-0f1a2b3c4d5e",
		Phase:        storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED,
		Error:        "",
		WorkloadName: "laelia-machines/laelia-machine-1f0a9c2d",
	}
	frame := progressFrame(e)
	p, ok := frame.Message.(*v1pb.ProvisionerStreamMessage_JobProgress)
	require.True(t, ok)
	assert.Equal(t, common.FormatMachineUID(e.MachineID), p.JobProgress.GetMachine())
	assert.Equal(t, v1pb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED, p.JobProgress.GetPhase())
	assert.Equal(t, e.WorkloadName, p.JobProgress.GetWorkloadName())
}

func TestFailedProgressFrameCarriesError(t *testing.T) {
	frame := progressFrame(backend.Event{
		MachineID: "abc",
		Phase:     storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED,
		Error:     "boom",
	})
	p, ok := frame.Message.(*v1pb.ProvisionerStreamMessage_JobProgress)
	require.True(t, ok)
	assert.Equal(t, v1pb.ProvisioningPhase_PROVISIONING_PHASE_FAILED, p.JobProgress.GetPhase())
	assert.Equal(t, "boom", p.JobProgress.GetError())
}

func TestIsPermanentAuthFailure(t *testing.T) {
	assert.True(t, IsPermanentAuthFailure(connect.NewError(connect.CodeUnauthenticated, errors.New("bad token"))))
	assert.True(t, IsPermanentAuthFailure(connect.NewError(connect.CodePermissionDenied, errors.New("deleted"))))
	assert.False(t, IsPermanentAuthFailure(connect.NewError(connect.CodeUnavailable, errors.New("manager down"))))
	assert.False(t, IsPermanentAuthFailure(context.DeadlineExceeded))
}

func TestDescribeAuthFailureMapsDistinctCauses(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string // a fragment unique to each remediation
	}{
		{
			name: "wrong signing secret",
			msg:  "invalid provisioner access token: token signature is invalid: signature is invalid",
			want: "was NOT issued by this manager",
		},
		{
			name: "deleted provisioner",
			msg:  "provisioner 7bb54271-8f83-4110-afc6-9501bf0b6126 not exists",
			want: "no longer exists on this manager",
		},
		{
			name: "rotated provisioner",
			msg:  "provisioner token version mismatch",
			want: "was rotated on the manager",
		},
		{
			name: "audience mismatch",
			msg:  "invalid access token, audience mismatch, expected ...",
			want: "different release mode or environment",
		},
		{
			name: "generic rejection",
			msg:  "credential rejected for an unrelated auth reason",
			want: "the provisioner credential was rejected",
		},
	}
	for _, tt := range tests {
		got := DescribeAuthFailure(connect.NewError(connect.CodeUnauthenticated, errors.New(tt.msg)))
		assert.Contains(t, got, tt.want, tt.name)
	}

	// Non-auth errors (and nil) fall back to the raw error / a neutral message.
	assert.Contains(t, DescribeAuthFailure(errors.New("raw boom")), "raw boom")
	assert.Contains(t, DescribeAuthFailure(nil), "unknown authentication error")
}

func TestStreamResult(t *testing.T) {
	res := &streamResult{}
	require.NoError(t, res.get())
	res.set(errors.New("boom"))
	require.EqualError(t, res.get(), "boom")
}

func TestSendStreamWithoutConnectionFails(t *testing.T) {
	c := &Client{}
	err := c.sendStream(&v1pb.ProvisionerStreamMessage{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestExponentialBackoff(t *testing.T) {
	eb := NewExponentialBackoff(10*time.Millisecond, 40*time.Millisecond)
	ctx := context.Background()
	require.NoError(t, eb.Wait(ctx)) // 10ms
	require.NoError(t, eb.Wait(ctx)) // 20ms
	require.NoError(t, eb.Wait(ctx)) // 40ms (capped)
	require.NoError(t, eb.Wait(ctx)) // 40ms (capped)
	eb.Reset()
	require.NoError(t, eb.Wait(ctx)) // back to 10ms

	ctxCanceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, eb.Wait(ctxCanceled), context.Canceled)
}

// fakeBackend satisfies the Backend interface for construction tests.
type fakeBackend struct{}

func (*fakeBackend) Name() string { return "fake" }
func (*fakeBackend) Start(context.Context, chan<- backend.Event) error {
	return nil
}
func (*fakeBackend) Provision(context.Context, backend.MachineSpec, string) error {
	return nil
}
func (*fakeBackend) Deprovision(context.Context, string, bool) error { return nil }
func (*fakeBackend) Shutdown(context.Context) error                  { return nil }
