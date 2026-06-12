package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// AgentService implements the agent service.
type AgentService struct {
	v1connect.UnimplementedAgentServiceHandler
	store    *store.Store
	profile  *config.Profile
	stateCfg *state.State
}

// NewAgentService creates a new AgentService.
func NewAgentService(store *store.Store, profile *config.Profile, stateCfg *state.State) *AgentService {
	return &AgentService{
		store:    store,
		profile:  profile,
		stateCfg: stateCfg,
	}
}

// Hello responds with a greeting.
func (s *AgentService) Hello(ctx context.Context, req *connect.Request[v1.HelloRequest]) (*connect.Response[v1.HelloResponse], error) {
	response := &v1.HelloResponse{
		CurrentTime: time.Now().Unix(),
	}
	return connect.NewResponse(response), nil
}
