package v1

// Provisioner control-plane integration test — the Phase 2 milestone demo.
//
// A fake provisioner (a real Connect client carrying a minted provisioner
// token) drives the full job state machine against a real manager stack
// (auth + IAM interceptors, real Postgres, real dispatcher):
//
//	create (token shown once) → connect (ProvisionerReady) → ProvisionMachine
//	→ job received → progress PROVISIONING → PROVISIONED → stream killed →
//	reconnect → job replayed → DeleteProvisioner refused while bound →
//	DeleteMachine → deprovision replayed → DeleteProvisioner succeeds.
//
// The test is gated like the migration integration tests: set
// LAELIA_RUN_PROVISIONER_TESTS=1 and LAELIA_TEST_PG_URL=<postgres url with
// CREATEDB>; otherwise it skips.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" stdlib driver.
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/migration"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func requireProvisionerTests(t *testing.T) string {
	t.Helper()
	if os.Getenv("LAELIA_RUN_PROVISIONER_TESTS") != "1" {
		t.Skip("set LAELIA_RUN_PROVISIONER_TESTS=1 to run the provisioner integration test")
	}
	rootURL := os.Getenv("LAELIA_TEST_PG_URL")
	if rootURL == "" {
		t.Skip("set LAELIA_TEST_PG_URL to a Postgres URL for the provisioner integration test")
	}
	return rootURL
}

var provisionerTestDBCounter int64

// testManifestSha is the gz sha256 the seeded embedded manifest advertises for
// linux-x64; the bootstrap script must carry it.
const testManifestGzSha = "d4c3b2a1d4c3b2a1d4c3b2a1d4c3b2a1d4c3b2a1d4c3b2a1d4c3b2a1d4c3b2a1"

// provisionerTestEnv is the full manager stack under test.
type provisionerTestEnv struct {
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
	server     *httptest.Server
	secret     string
	admin      *store.UserMessage
	member     *store.UserMessage
}

// replaceDatabaseName swaps the database name in a Postgres URL.
func replaceDatabaseName(pgURL, name string) string {
	i := strings.LastIndex(pgURL, "/")
	if i == -1 {
		return pgURL
	}
	base := pgURL[:i+1]
	rest := pgURL[i+1:]
	if j := strings.IndexAny(rest, "?"); j != -1 {
		return base + name + rest[j:]
	}
	return base + name
}

type provisionerTestExpireCache struct{}

func (*provisionerTestExpireCache) Get(string) (bool, bool) { return false, false }

// newProvisionerTestEnv builds a throwaway database (migrated), the api/v1
// service stack behind a real Connect server, and seeds the workspace state
// the flows need: an admin user, a member bound to machineProvisioner, the
// external URL, the provisioning setting, and an embedded machine manifest.
func newProvisionerTestEnv(t *testing.T) *provisionerTestEnv {
	t.Helper()
	rootURL := requireProvisionerTests(t)

	rootDB, err := sql.Open("pgx", rootURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rootDB.Close() })

	var canCreateDB bool
	require.NoError(t, rootDB.QueryRow(`SELECT rolcreatedb FROM pg_roles WHERE rolname = current_user`).Scan(&canCreateDB))
	require.True(t, canCreateDB, "LAELIA_TEST_PG_URL user must have CREATEDB")

	name := fmt.Sprintf("laelia_provtest_%d_%d", os.Getpid(), atomic.AddInt64(&provisionerTestDBCounter, 1))
	_, err = rootDB.Exec(fmt.Sprintf(`CREATE DATABASE %q`, name))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = rootDB.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name))
	})

	testURL := replaceDatabaseName(rootURL, name)
	db, err := sql.Open("pgx", testURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	require.NoError(t, migration.MigrateSchema(ctx, db))

	stores, err := store.New(ctx, testURL, true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stores.Close() })

	// ---- seed workspace state ----
	secret := "provisioner-test-secret"
	profile := &config.Profile{Mode: common.ReleaseModeDev}

	admin, err := stores.CreateUser(ctx, &store.UserMessage{
		Name:  "Admin",
		Email: fmt.Sprintf("admin-%s@laelia.test", strings.ToLower(name)),
		Type:  storepb.PrincipalType_END_USER,
	})
	require.NoError(t, err)
	member, err := stores.CreateUser(ctx, &store.UserMessage{
		Name:  "Member",
		Email: fmt.Sprintf("member-%s@laelia.test", strings.ToLower(uuid8())),
		Type:  storepb.PrincipalType_END_USER,
	})
	require.NoError(t, err)

	// admin → workspaceAdmin; member → machineProvisioner (self-service only).
	policy := &storepb.IamPolicy{Bindings: []*storepb.Binding{
		{Role: common.FormatRole(store.WorkspaceAdminRole), Members: []string{common.FormatUserHandle(admin.Handle)}},
		{Role: common.FormatRole(store.MachineProvisionerRole), Members: []string{common.FormatUserHandle(member.Handle)}},
	}}
	_, err = stores.SetWorkspaceIamPolicy(ctx, policy, "")
	require.NoError(t, err)

	require.NoError(t, stores.UpsertSettingValue(ctx, storepb.SettingName_WORKSPACE_PROFILE, &storepb.WorkspaceProfileSetting{
		ExternalUrl: "https://manager.test",
	}))
	require.NoError(t, stores.UpsertSettingValue(ctx, storepb.SettingName_PROVISIONING, &storepb.ProvisioningSetting{
		RuntimeImage: "laelia/machine-runtime:test",
		BinaryTarget: "linux-x64",
	}))

	// Embedded machine manifest for the binary guards + bootstrap checksums.
	manifest, err := json.Marshal(map[string]any{
		"version":               "1.2.3",
		"prompt_bundle_version": "p1",
		"targets": map[string]any{
			"linux-x64": map[string]any{
				"file":   "laelia-machine-linux-x64",
				"sha256": "a1b2c3d4e5f6a7b8a1b2c3d4e5f6a7b8a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4",
				"gz":     map[string]any{"file": "laelia-machine-linux-x64.gz", "sha256": testManifestGzSha},
			},
		},
	})
	require.NoError(t, err)
	machinebuild.SetManifest(manifest)
	t.Cleanup(func() { machinebuild.SetManifest(nil) })

	d := dispatcher.New(stores)
	t.Cleanup(d.Stop)

	// ---- wire the real interceptor chain + handlers over httptest ----
	interceptors := []connect.Interceptor{
		auth.New(stores, secret, &provisionerTestExpireCache{}, profile),
		NewIAMInterceptor(iam.NewManager(stores)),
	}
	handlerOpts := connect.WithHandlerOptions(connect.WithInterceptors(interceptors...))

	mux := http.NewServeMux()
	provisionerService := NewProvisionerService(stores, secret, profile, d, iam.NewManager(stores))
	provisionerStreamService := NewProvisionerStreamService(stores, secret, profile, d)
	machineService := NewMachineService(stores, secret, profile, nil, d, iam.NewManager(stores))
	mux.Handle(v1connect.NewProvisionerServiceHandler(provisionerService, handlerOpts))
	mux.Handle(v1connect.NewProvisionerStreamServiceHandler(provisionerStreamService, handlerOpts))
	mux.Handle(v1connect.NewMachineServiceHandler(machineService, handlerOpts))

	// The Connect protocol's bidi streaming (the provisioner channel) runs over
	// HTTP/2; httptest's default server is HTTP/1.1 only, which kills the
	// stream with a 505 before the handler runs. Enable HTTP/2 (h2 over TLS)
	// and hand the client a trust-all TLS config, mirroring how the machine
	// MachineChannel is exercised in production.
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	return &provisionerTestEnv{
		store:      stores,
		dispatcher: d,
		server:     server,
		secret:     secret,
		admin:      admin,
		member:     member,
	}
}

func uuid8() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

// ---- fake provisioner ----

// fakeProvisioner is a real Connect bidi client playing the provisioner side.
type fakeProvisioner struct {
	t    *testing.T
	conn *connect.BidiStreamForClient[v1pb.ProvisionerStreamMessage, v1pb.ManagerProvisionerStreamMessage]
}

func (e *provisionerTestEnv) dialFakeProvisioner(t *testing.T, token string, ready *v1pb.ProvisionerReady) *fakeProvisioner {
	t.Helper()
	client := v1connect.NewProvisionerStreamServiceClient(e.server.Client(), e.server.URL)
	conn := client.ProvisionerChannel(context.Background())
	conn.RequestHeader().Set("Authorization", "Bearer "+token)
	if ready != nil {
		require.NoError(t, conn.Send(&v1pb.ProvisionerStreamMessage{
			Message: &v1pb.ProvisionerStreamMessage_Ready{Ready: ready},
		}))
	}
	return &fakeProvisioner{t: t, conn: conn}
}

// recv waits for the next manager→provisioner frame. It returns nil on
// timeout — assertions about the received frame live in the caller so a
// missing frame fails with the surrounding test's message.
func (f *fakeProvisioner) recv(timeout time.Duration) *v1pb.ManagerProvisionerStreamMessage {
	f.t.Helper()
	type result struct {
		msg *v1pb.ManagerProvisionerStreamMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		msg, err := f.conn.Receive()
		ch <- result{msg, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil
		}
		return r.msg
	case <-time.After(timeout):
		return nil
	}
}

// recvJob waits until a ProvisionJob frame arrives (other frames are skipped).
func (f *fakeProvisioner) recvJob(timeout time.Duration) *v1pb.ProvisionMachineJob {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Until(deadline) > 0 {
		msg := f.recv(time.Until(deadline))
		if msg == nil {
			return nil
		}
		if p, ok := msg.Message.(*v1pb.ManagerProvisionerStreamMessage_ProvisionJob); ok {
			return p.ProvisionJob
		}
	}
	return nil
}

func (f *fakeProvisioner) recvDeprovisionJob(timeout time.Duration) *v1pb.DeprovisionMachineJob {
	f.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Until(deadline) > 0 {
		msg := f.recv(time.Until(deadline))
		if msg == nil {
			return nil
		}
		if p, ok := msg.Message.(*v1pb.ManagerProvisionerStreamMessage_DeprovisionJob); ok {
			return p.DeprovisionJob
		}
	}
	return nil
}

func (f *fakeProvisioner) sendProgress(machine string, phase v1pb.ProvisioningPhase, errMsg, workload string) {
	f.t.Helper()
	require.NoError(f.t, f.conn.Send(&v1pb.ProvisionerStreamMessage{
		Message: &v1pb.ProvisionerStreamMessage_JobProgress{
			JobProgress: &v1pb.ProvisionJobProgress{
				Machine: machine, Phase: phase, Error: errMsg, WorkloadName: workload,
			},
		},
	}))
}

func (f *fakeProvisioner) close() {
	// Half-close the request side first so the manager's Receive unblocks with
	// EOF (mirrors how the provisioner binary tears its stream down); closing
	// the response side alone never reaches the manager's Receive.
	_ = f.conn.CloseRequest()
	_ = f.conn.CloseResponse()
}

// ---- helpers ----

func (e *provisionerTestEnv) adminToken(t *testing.T) string {
	t.Helper()
	token, err := auth.GenerateAccessToken("admin", e.admin.ID, common.ReleaseModeDev, e.secret, time.Hour)
	require.NoError(t, err)
	return token
}

func (e *provisionerTestEnv) memberToken(t *testing.T) string {
	t.Helper()
	token, err := auth.GenerateAccessToken("member", e.member.ID, common.ReleaseModeDev, e.secret, time.Hour)
	require.NoError(t, err)
	return token
}

func (e *provisionerTestEnv) provisionerServiceClient(t *testing.T, token string) v1connect.ProvisionerServiceClient {
	t.Helper()
	return v1connect.NewProvisionerServiceClient(authedHTTPClient(e.server, token), e.server.URL)
}

func (e *provisionerTestEnv) machineServiceClient(t *testing.T, token string) v1connect.MachineServiceClient {
	t.Helper()
	return v1connect.NewMachineServiceClient(authedHTTPClient(e.server, token), e.server.URL)
}

// bearerTransport injects the Authorization header on every request, so both
// the unary and the streaming clients authenticate like the real provisioner
// would with its one-time token.
type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

func authedHTTPClient(server *httptest.Server, token string) *http.Client {
	return &http.Client{Transport: bearerTransport{base: server.Client().Transport, token: token}}
}

// provisionerStatus reads the provisioner's stored runtime status.
func (e *provisionerTestEnv) provisionerStatus(t *testing.T, provName string) *storepb.ProvisionerStatus {
	t.Helper()
	p, err := e.store.GetProvisionerByResourceID(context.Background(), strings.TrimPrefix(provName, common.ProvisionerNamePrefix))
	require.NoError(t, err)
	require.NotNil(t, p)
	return p.Status
}

// machineProvisioning reads the machine's provisioning state from the store
// (the assertions run against the DB, not a response).
func (e *provisionerTestEnv) machineProvisioning(t *testing.T, resourceID string) *storepb.ProvisioningStatus {
	t.Helper()
	machine, err := e.store.GetMachineByResourceID(context.Background(), resourceID)
	require.NoError(t, err)
	require.NotNil(t, machine)
	return machine.Provisioning
}

// ---- the journey ----

func TestProvisionerControlPlane(t *testing.T) {
	env := newProvisionerTestEnv(t)
	ctx := context.Background()

	// ---- 1. admin registers a provisioner; the token is shown once ----
	adminClient := env.provisionerServiceClient(t, env.adminToken(t))
	created, err := adminClient.CreateProvisioner(ctx, connect.NewRequest(&v1pb.CreateProvisionerRequest{
		Provisioner: &v1pb.Provisioner{Title: "prod-cluster", Backend: "kubernetes"},
	}))
	require.NoError(t, err)
	require.NotEmpty(t, created.Msg.GetToken(), "the one-time token must be returned at create")
	provName := created.Msg.GetProvisioner().GetName()
	require.NotEmpty(t, provName)
	require.Equal(t, int32(0), created.Msg.GetProvisioner().GetMachineCount())

	listed, err := adminClient.ListProvisioners(ctx, connect.NewRequest(&v1pb.ListProvisionersRequest{}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetProvisioners(), 1)
	assert.Equal(t, "prod-cluster", listed.Msg.GetProvisioners()[0].GetTitle())

	// ---- 2. the provisioner connects and reports ready ----
	ready := &v1pb.ProvisionerReady{Version: "0.9.0", Backend: "kubernetes", AutoUpgrade: true, ConfigDigest: "cfg-1"}
	fake := env.dialFakeProvisioner(t, created.Msg.GetToken(), ready)
	defer fake.close()

	require.Eventually(t, func() bool {
		st := env.provisionerStatus(t, provName)
		return st != nil && st.Connected && st.Version == "0.9.0" && st.AutoUpgrade
	}, 5*time.Second, 50*time.Millisecond, "provisioner status must be stamped from the ready frame")

	// ---- 3. member (machineProvisioner role) provisions a machine ----
	memberClient := env.provisionerServiceClient(t, env.memberToken(t))
	provResp, err := memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: provName,
		Title:       "team workload",
	}))
	require.NoError(t, err)
	machine := provResp.Msg
	require.Equal(t, provName, machine.GetProvisioner())
	require.Equal(t, v1pb.ProvisioningPhase_PROVISIONING_PHASE_PENDING, machine.GetProvisioning().GetPhase())
	require.NotNil(t, machine.GetProvisioning().GetPendingAt())
	machineResourceID := strings.TrimPrefix(machine.GetName(), common.MachineNamePrefix)

	// The machine row carries the binding + seeded info.
	row, err := env.store.GetMachineByResourceID(ctx, machineResourceID)
	require.NoError(t, err)
	require.NotZero(t, row.ProvisionerID)
	require.Equal(t, "laelia-machine-"+machineResourceID[:8], row.Info.GetHostname())
	require.Equal(t, "linux", row.Info.GetOs())
	require.Equal(t, "amd64", row.Info.GetArch())

	// ---- 4. the provisioner receives the job on the live stream ----
	job := fake.recvJob(10 * time.Second)
	require.NotNil(t, job, "the provisioned machine's job must be pushed to the live stream")
	require.Equal(t, machine.GetName(), job.GetMachine())
	require.NotEmpty(t, job.GetRefreshToken())
	require.Len(t, job.GetFingerprint(), 16)
	require.Equal(t, "https://manager.test", job.GetManagerUrl())
	require.Equal(t, "laelia/machine-runtime:test", job.GetRuntimeImage())
	require.Equal(t, "linux-x64", job.GetBinaryTarget())
	require.Contains(t, job.GetBootstrapScript(), `MANAGER_URL="https://manager.test"`)
	require.Contains(t, job.GetBootstrapScript(), testManifestGzSha)

	// ---- 5. a progress frame moves the machine into PROVISIONING ----
	fake.sendProgress(job.GetMachine(), v1pb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING, "", "ns/laelia-machine-x")
	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, machineResourceID)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING
	}, 5*time.Second, 50*time.Millisecond)

	// ---- 6. kill the stream; the replay re-delivers the in-flight job ----
	// The machine is mid-provisioning (a replayable phase), so the reconnect
	// must re-deliver the job.
	fake.close()
	require.Eventually(t, func() bool {
		st := env.provisionerStatus(t, provName)
		return st != nil && !st.Connected
	}, 5*time.Second, 50*time.Millisecond, "a dead stream must mark the provisioner offline")

	fake2 := env.dialFakeProvisioner(t, created.Msg.GetToken(), ready)
	defer fake2.close()

	replayed := fake2.recvJob(10 * time.Second)
	require.NotNil(t, replayed, "the in-flight job must replay on reconnect")
	require.Equal(t, machine.GetName(), replayed.GetMachine())
	require.NotEqual(t, job.GetRefreshToken(), replayed.GetRefreshToken(),
		"the replay re-mints the refresh token (the original plaintext only existed at provision time)")
	require.Equal(t, job.GetFingerprint(), replayed.GetFingerprint(),
		"the fingerprint is deterministic and survives replay")

	// ---- 7. the replayed job's progress drives the machine to PROVISIONED ----
	fake2.sendProgress(replayed.GetMachine(), v1pb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED, "", "ns/laelia-machine-x")
	require.Eventually(t, func() bool {
		p := env.machineProvisioning(t, machineResourceID)
		return p != nil && p.Phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED && p.ProvisionedAt > 0
	}, 5*time.Second, 50*time.Millisecond)

	// ---- 8. DeleteProvisioner is refused while a machine is bound ----
	_, err = adminClient.DeleteProvisioner(ctx, connect.NewRequest(&v1pb.DeleteProvisionerRequest{Name: provName}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())

	// ---- 9. DeleteMachine: row deleted immediately, teardown replayed ----
	machineClient := env.machineServiceClient(t, env.adminToken(t))
	_, err = machineClient.DeleteMachine(ctx, connect.NewRequest(&v1pb.DeleteMachineRequest{Name: machine.GetName()}))
	require.NoError(t, err)

	// The row is soft-deleted and parked at DEPROVISIONING for the replay.
	rowAfterDelete, err := env.store.GetMachineByResourceID(ctx, machineResourceID)
	require.NoError(t, err)
	require.NotNil(t, rowAfterDelete)
	require.True(t, rowAfterDelete.Deleted)
	require.Equal(t, storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING, rowAfterDelete.Provisioning.Phase)

	deprovision := fake2.recvDeprovisionJob(10 * time.Second)
	require.NotNil(t, deprovision, "the pending teardown must replay on the live stream")
	require.Equal(t, machine.GetName(), deprovision.GetMachine())
	require.False(t, deprovision.GetKeepData())

	// ---- 9. with the machine gone, the provisioner can be deleted ----
	_, err = adminClient.DeleteProvisioner(ctx, connect.NewRequest(&v1pb.DeleteProvisionerRequest{Name: provName}))
	require.NoError(t, err)
	p, err := env.store.GetProvisionerByResourceID(ctx, strings.TrimPrefix(provName, common.ProvisionerNamePrefix))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.True(t, p.Deleted)
	assert.Greater(t, p.TokenVersion, 1, "the version must be bumped on delete")

	// The old token dies at its next use (version-based revocation).
	fake3 := env.dialFakeProvisioner(t, created.Msg.GetToken(), ready)
	defer fake3.close()
	assert.Nil(t, fake3.recv(2*time.Second), "a deleted provisioner's token must not open a stream")
}

// TestProvisionMachineFailsFast covers the clear-code rejection paths of
// ProvisionMachine (plan exit criterion).
func TestProvisionMachineFailsFast(t *testing.T) {
	env := newProvisionerTestEnv(t)
	ctx := context.Background()
	adminClient := env.provisionerServiceClient(t, env.adminToken(t))
	memberClient := env.provisionerServiceClient(t, env.memberToken(t))

	newProvisioner := func(t *testing.T, backend string) string {
		t.Helper()
		resp, err := adminClient.CreateProvisioner(ctx, connect.NewRequest(&v1pb.CreateProvisionerRequest{
			Provisioner: &v1pb.Provisioner{Title: "p-" + uuid8(), Backend: backend},
		}))
		require.NoError(t, err)
		return resp.Msg.GetProvisioner().GetName()
	}

	// Unknown backend: the registry accepts it, provisioning rejects it.
	unknown := newProvisioner(t, "docker")
	_, err := memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: unknown, Title: "m",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErrCode(t, err))

	// Missing runtime image.
	k8sProv := newProvisioner(t, "kubernetes")
	setting, err := env.store.GetProvisioningSetting(ctx)
	require.NoError(t, err)
	runtimeImage := setting.RuntimeImage
	require.NoError(t, env.store.UpsertSettingValue(ctx, storepb.SettingName_PROVISIONING, &storepb.ProvisioningSetting{BinaryTarget: "linux-x64"}))
	_, err = memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: k8sProv, Title: "m",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErrCode(t, err))
	require.NoError(t, env.store.UpsertSettingValue(ctx, storepb.SettingName_PROVISIONING, &storepb.ProvisioningSetting{
		RuntimeImage: runtimeImage, BinaryTarget: "linux-x64",
	}))

	// Manager with no embedded machine binaries: an empty targets manifest
	// (SetManifest(nil) is a no-op) leaves LatestVersion set but no target.
	emptyManifest, err := json.Marshal(map[string]any{
		"version": "1.2.3", "prompt_bundle_version": "p1", "targets": map[string]any{},
	})
	require.NoError(t, err)
	machinebuild.SetManifest(emptyManifest)
	_, err = memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: k8sProv, Title: "m",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErrCode(t, err))
	manifest, err := json.Marshal(map[string]any{
		"version": "1.2.3", "prompt_bundle_version": "p1",
		"targets": map[string]any{"linux-x64": map[string]any{
			"file": "f", "sha256": "a", "gz": map[string]any{"file": "f.gz", "sha256": testManifestGzSha},
		}},
	})
	require.NoError(t, err)
	machinebuild.SetManifest(manifest)

	// Non-admin naming another owner.
	_, err = memberClient.ProvisionMachine(ctx, connect.NewRequest(&v1pb.ProvisionMachineRequest{
		Provisioner: k8sProv, Title: "m",
		Owner: common.FormatUserHandle(env.admin.Handle),
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connectErrCode(t, err))
}

// TestProvisionerTokenVersionKillsAccess verifies the rotate path end to end:
// the new token works, the old token is rejected at its next use.
func TestProvisionerTokenVersionKillsAccess(t *testing.T) {
	env := newProvisionerTestEnv(t)
	ctx := context.Background()
	adminClient := env.provisionerServiceClient(t, env.adminToken(t))

	created, err := adminClient.CreateProvisioner(ctx, connect.NewRequest(&v1pb.CreateProvisionerRequest{
		Provisioner: &v1pb.Provisioner{Title: "rotate-me", Backend: "kubernetes"},
	}))
	require.NoError(t, err)
	oldToken := created.Msg.GetToken()
	provName := created.Msg.GetProvisioner().GetName()

	rotated, err := adminClient.RotateProvisionerToken(ctx, connect.NewRequest(&v1pb.RotateProvisionerTokenRequest{Name: provName}))
	require.NoError(t, err)
	require.NotEmpty(t, rotated.Msg.GetToken())
	require.NotEqual(t, oldToken, rotated.Msg.GetToken())

	// The old token is invalidated immediately at its next use (the version
	// bump in rotate): the stream request is rejected by the interceptor. A
	// ping kicks the lazy bidi request off so the rejection is observable.
	fakeOld := env.dialFakeProvisioner(t, oldToken, nil)
	defer fakeOld.close()
	require.NoError(t, fakeOld.conn.Send(&v1pb.ProvisionerStreamMessage{
		Message: &v1pb.ProvisionerStreamMessage_Ping{Ping: &v1pb.Ping{}},
	}))
	_, err = fakeOld.conn.Receive()
	require.Error(t, err, "a rotated token must be rejected by the interceptor")

	// The new token authenticates and gets its ping answered.
	fakeNew := env.dialFakeProvisioner(t, rotated.Msg.GetToken(), nil)
	defer fakeNew.close()
	require.NoError(t, fakeNew.conn.Send(&v1pb.ProvisionerStreamMessage{
		Message: &v1pb.ProvisionerStreamMessage_Ping{Ping: &v1pb.Ping{}},
	}))
	deadline := time.Now().Add(10 * time.Second)
	for {
		msg := fakeNew.recv(time.Until(deadline))
		if msg == nil {
			t.Fatal("the new token's stream must answer pings")
		}
		if _, ok := msg.Message.(*v1pb.ManagerProvisionerStreamMessage_Pong); ok {
			break
		}
	}
}

func connectErrCode(t *testing.T, err error) connect.Code {
	t.Helper()
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	return connectErr.Code()
}
