package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/state"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// stubMachineService serves RefreshMachineToken with a scripted outcome for
// the `setup --provisioned` probe tests; every other RPC stays unimplemented.
type stubMachineService struct {
	v1connect.UnimplementedMachineServiceHandler
	unauthenticated bool
	renewed         string
}

func (s *stubMachineService) RefreshMachineToken(_ context.Context, _ *connect.Request[v1pb.RefreshMachineTokenRequest]) (*connect.Response[v1pb.RefreshMachineTokenResponse], error) {
	if s.unauthenticated {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("token rejected"))
	}
	return connect.NewResponse(&v1pb.RefreshMachineTokenResponse{RefreshToken: s.renewed}), nil
}

// newStubManager serves the machine API at a throwaway URL. The probe is a
// unary RPC, so plain HTTP/1.1 suffices (no EnableHTTP2 dance).
func newStubManager(t *testing.T, stub *stubMachineService) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	pattern, handler := v1connect.NewMachineServiceHandler(stub)
	mux.Handle(pattern, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func provisionedState() *state.State {
	return &state.State{ManagerURL: "http://manager.test", MachineID: "mach-1", RefreshToken: "tok", Hostname: "pod-0"}
}

// The provisioned path must never fall into the interactive device-code
// login: a missing credential fails fast with an actionable message.
func TestSetupProvisionedFailsWithoutCredential(t *testing.T) {
	t.Setenv(home.EnvDir, t.TempDir())
	err := setupProvisioned("http://manager.test", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete and re-provision")

	err = setupProvisioned("http://manager.test", &state.State{MachineID: "mach-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete and re-provision")
}

// A dead credential fails fast with the re-provision recovery path instead of
// starting the device-code login or wiping the state.
func TestSetupProvisionedFailsFastOnDeadCredential(t *testing.T) {
	t.Setenv(home.EnvDir, t.TempDir())
	srv := newStubManager(t, &stubMachineService{unauthenticated: true})

	err := setupProvisioned(srv.URL, provisionedState())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no longer valid")
}

// A still-valid credential probes OK and a rolling renewal is persisted back
// to the state file (the PVC copy is authoritative).
func TestProbeRefreshTokenRollingRenewal(t *testing.T) {
	t.Setenv(home.EnvDir, t.TempDir())
	srv := newStubManager(t, &stubMachineService{renewed: "renewed-token"})

	st := provisionedState()
	result := probeRefreshToken(srv.URL, st)
	require.Equal(t, probeOK, result)
	require.Equal(t, "renewed-token", st.RefreshToken)

	saved, err := state.Load()
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Equal(t, "renewed-token", saved.RefreshToken)
}

// A dead credential probes as permanent (revoked/rotated/deleted machine).
func TestProbeRefreshTokenPermanent(t *testing.T) {
	t.Setenv(home.EnvDir, t.TempDir())
	srv := newStubManager(t, &stubMachineService{unauthenticated: true})

	result := probeRefreshToken(srv.URL, provisionedState())
	require.Equal(t, probePermanent, result)
}

// An unreachable manager is transient, not permanent: the pod boots and lets
// the run loop retry with backoff.
func TestProbeRefreshTokenTransient(t *testing.T) {
	t.Setenv(home.EnvDir, t.TempDir())
	srv := httptest.NewServer(http.NewServeMux())
	url := srv.URL
	srv.Close() // connection refused afterwards

	result := probeRefreshToken(url, provisionedState())
	require.Equal(t, probeTransient, result)
}
