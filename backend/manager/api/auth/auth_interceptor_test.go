package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// fakeStore implements the auth.Store interface with just the principal
// lookups the interceptor needs, demonstrating that tests no longer have to
// mock the whole *store.Store.
type fakeStore struct {
	user        *store.UserMessage
	agent       *store.AgentMessage
	machine     *store.MachineMessage
	provisioner *store.ProvisionerMessage
}

func (f *fakeStore) GetUserByID(_ context.Context, _ int) (*store.UserMessage, error) {
	return f.user, nil
}

func (f *fakeStore) GetAgentByResourceID(_ context.Context, _ string) (*store.AgentMessage, error) {
	return f.agent, nil
}

func (f *fakeStore) GetMachineByResourceID(_ context.Context, _ string) (*store.MachineMessage, error) {
	return f.machine, nil
}

func (f *fakeStore) GetProvisionerByResourceID(_ context.Context, _ string) (*store.ProvisionerMessage, error) {
	return f.provisioner, nil
}

type fakeTokenExpireCache struct {
	expired map[string]bool
}

func (f *fakeTokenExpireCache) Get(key string) (bool, bool) {
	value, ok := f.expired[key]
	return value, ok
}

func TestAuthenticateInjectsUserAndSourceIP(t *testing.T) {
	const secret = "test-secret"
	profile := &config.Profile{Mode: common.ReleaseModeDev}

	token, err := GenerateAccessToken("alice", 1, common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)

	in := New(
		&fakeStore{user: &store.UserMessage{ID: 1}},
		secret,
		&fakeTokenExpireCache{expired: map[string]bool{}},
		profile,
	)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	ctx, err := in.authenticate(
		context.Background(),
		header,
		connect.Peer{Addr: "203.0.113.7:1234"},
		"/laelia.v1.AgentService/AgentHeartbeat",
	)
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", ctx.Value(common.SourceIPContextKey))
	user, ok := ctx.Value(common.UserContextKey).(*store.UserMessage)
	require.True(t, ok)
	assert.Equal(t, 1, user.ID)
}

func TestAuthenticateRejectsExpiredCacheToken(t *testing.T) {
	const secret = "test-secret"
	profile := &config.Profile{Mode: common.ReleaseModeDev}

	token, err := GenerateAccessToken("alice", 1, common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)

	in := New(
		&fakeStore{user: &store.UserMessage{ID: 1}},
		secret,
		&fakeTokenExpireCache{expired: map[string]bool{token: true}},
		profile,
	)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	_, err = in.authenticate(
		context.Background(),
		header,
		connect.Peer{Addr: "203.0.113.7:1234"},
		"/laelia.v1.UserService/GetUser",
	)
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestAuthenticateInjectsProvisioner(t *testing.T) {
	const secret = "test-secret"
	profile := &config.Profile{Mode: common.ReleaseModeDev}

	token, err := GenerateProvisionerToken("cluster-1", "prov-resource-id", 1, common.ReleaseModeDev, secret)
	require.NoError(t, err)

	in := New(
		&fakeStore{provisioner: &store.ProvisionerMessage{ID: 7, ResourceID: "prov-resource-id", TokenVersion: 1}},
		secret,
		&fakeTokenExpireCache{expired: map[string]bool{}},
		profile,
	)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	ctx, err := in.authenticate(
		context.Background(),
		header,
		connect.Peer{Addr: "203.0.113.7:1234"},
		"/laelia.v1.ProvisionerStreamService/ProvisionerChannel",
	)
	require.NoError(t, err)

	provisioner, ok := ctx.Value(common.ProvisionerContextKey).(*store.ProvisionerMessage)
	require.True(t, ok)
	assert.Equal(t, 7, provisioner.ID)
}

func TestAuthenticateRejectsStaleProvisionerToken(t *testing.T) {
	const secret = "test-secret"
	profile := &config.Profile{Mode: common.ReleaseModeDev}

	// Minted for token_version 1 but the row has rotated to 2.
	token, err := GenerateProvisionerToken("cluster-1", "prov-resource-id", 1, common.ReleaseModeDev, secret)
	require.NoError(t, err)

	in := New(
		&fakeStore{provisioner: &store.ProvisionerMessage{ID: 7, ResourceID: "prov-resource-id", TokenVersion: 2}},
		secret,
		&fakeTokenExpireCache{expired: map[string]bool{}},
		profile,
	)

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	_, err = in.authenticate(
		context.Background(),
		header,
		connect.Peer{Addr: "203.0.113.7:1234"},
		"/laelia.v1.ProvisionerStreamService/ProvisionerChannel",
	)
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}
