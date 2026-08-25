package daemon

import (
	"context"
	"strings"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/agent/chattools"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// BatchDeps returns a Deps for the agent identified by agentBareID (the bare
// agents/{id} tail), for in-process calls from that agent's drain loop (the
// turn-batch builder). The Client carries the live machine access token and the
// X-Laelia-Agent header (agents/<agentBareID>) so the manager resolves the
// caller as that agent; Deps.Agent is the bare id chattools uses to build
// agents/<id>/commands/<id> resource names. Each agent runner passes its own
// agent id.
func (s *Server) BatchDeps(agentBareID string) chattools.Deps {
	return chattools.Deps{
		Client:          s.agentClient(agentBareID),
		UserClient:      s.userClient(agentBareID),
		AgentTeamClient: s.agentTeamClient(agentBareID),
		Agent:           agentBareID,
	}
}

// agentClient returns a cached CommandServiceClient for the agent identified by
// agentBareID (the bare uuid). Every call it makes carries the live machine
// access token (Authorization) and the X-Laelia-Agent header (agents/<id>), so
// the manager — which authenticates the machine token — can route the call to
// this specific agent. One daemon hosts many agents, so the client varies per
// agent even though the token is shared.
func (s *Server) agentClient(agentBareID string) v1connect.CommandServiceClient {
	s.agentClientsMu.Lock()
	defer s.agentClientsMu.Unlock()
	if s.agentClients == nil {
		s.agentClients = make(map[string]v1connect.CommandServiceClient)
	}
	if c, ok := s.agentClients[agentBareID]; ok {
		return c
	}
	c := v1connect.NewCommandServiceClient(
		s.httpClient,
		s.managerURL,
		connect.WithInterceptors(s.authInterceptor(agentBareID)),
	)
	s.agentClients[agentBareID] = c
	return c
}

// userClient returns a cached UserServiceClient for the agent identified by
// agentBareID, stamped with the same token + X-Laelia-Agent header as
// agentClient so the manager resolves the caller as that agent. Used by
// `channel add-member` to resolve user display names to principal ids.
func (s *Server) userClient(agentBareID string) v1connect.UserServiceClient {
	s.userClientsMu.Lock()
	defer s.userClientsMu.Unlock()
	if s.userClients == nil {
		s.userClients = make(map[string]v1connect.UserServiceClient)
	}
	if c, ok := s.userClients[agentBareID]; ok {
		return c
	}
	c := v1connect.NewUserServiceClient(
		s.httpClient,
		s.managerURL,
		connect.WithInterceptors(s.authInterceptor(agentBareID)),
	)
	s.userClients[agentBareID] = c
	return c
}

// agentTeamClient returns a cached AgentTeamServiceClient for the agent,
// stamped with the same token + X-Laelia-Agent header as agentClient.
func (s *Server) agentTeamClient(agentBareID string) v1connect.AgentTeamServiceClient {
	s.agentTeamClientsMu.Lock()
	defer s.agentTeamClientsMu.Unlock()
	if s.agentTeamClients == nil {
		s.agentTeamClients = make(map[string]v1connect.AgentTeamServiceClient)
	}
	if c, ok := s.agentTeamClients[agentBareID]; ok {
		return c
	}
	c := v1connect.NewAgentTeamServiceClient(
		s.httpClient,
		s.managerURL,
		connect.WithInterceptors(s.authInterceptor(agentBareID)),
	)
	s.agentTeamClients[agentBareID] = c
	return c
}

// authInterceptor builds a unary interceptor that stamps every request with the
// live machine access token and the X-Laelia-Agent header (agents/<agentBareID>)
// for the given agent.
func (s *Server) authInterceptor(agentBareID string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token := s.getToken(); token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			}
			if agentBareID != "" {
				req.Header().Set("X-Laelia-Agent", agentResourceName(agentBareID))
			}
			return next(ctx, req)
		}
	}
}

// mcpClient returns a cached McpGatewayServiceClient for the agent identified
// by agentBareID. Every call it makes carries the live machine access token and
// the X-Laelia-Agent header, mirroring agentClient.
func (s *Server) mcpClient(agentBareID string) v1connect.McpGatewayServiceClient {
	s.mcpClientsMu.Lock()
	defer s.mcpClientsMu.Unlock()
	if s.mcpClients == nil {
		s.mcpClients = make(map[string]v1connect.McpGatewayServiceClient)
	}
	if c, ok := s.mcpClients[agentBareID]; ok {
		return c
	}
	c := v1connect.NewMcpGatewayServiceClient(
		s.httpClient,
		s.managerURL,
		connect.WithInterceptors(s.authInterceptor(agentBareID)),
	)
	s.mcpClients[agentBareID] = c
	return c
}

// agentResourceName converts a bare agent id to its full resource name
// (agents/<id>) as the manager's X-Laelia-Agent header expects. A value that is
// already a full name is returned unchanged.
func agentResourceName(agentBareID string) string {
	if agentBareID == "" {
		return ""
	}
	if strings.HasPrefix(agentBareID, "agents/") {
		return agentBareID
	}
	return "agents/" + agentBareID
}

// bareAgentID strips the agents/ prefix from an agent resource name, returning
// the bare handle used to namespace the agent's on-disk state.
func bareAgentID(agent string) string {
	return strings.TrimPrefix(agent, "agents/")
}
