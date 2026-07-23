package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// requireCallingAgent returns the authenticated calling agent for an agent-
// callable RPC (no auth_method annotation, identity from GetAgentFromContext).
func requireCallingAgent(ctx context.Context) (*store.AgentMessage, error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}
	return agent, nil
}

// ResolveChannelByTitle looks up the unique channel (type 2) with the given
// title, returning NOT_FOUND when absent (it never creates one). Powers the
// "#<title>" address resolver.
func (s *CommandService) ResolveChannelByTitle(ctx context.Context, req *connect.Request[v1pb.ResolveChannelByTitleRequest]) (*connect.Response[v1pb.ResolveChannelByTitleResponse], error) {
	if _, err := requireCallingAgent(ctx); err != nil {
		return nil, err
	}
	if req.Msg.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title must not be empty"))
	}

	conv, err := s.store.FindChannelByTitle(ctx, req.Msg.Title)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve channel by title"))
	}
	if conv == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("channel %q not found", req.Msg.Title))
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
	ownerName := resolveUserName(ctx, s.store, conv.OwnerID)
	return connect.NewResponse(&v1pb.ResolveChannelByTitleResponse{
		Conversation: convertToV1Conversation(conv, ownerName, "", memberCount, 0, conv.Title),
	}), nil
}

// GetOrCreateUserDM opens (or reuses) the type-1 DM between the calling agent
// and a named end user. The peer is resolved by principal display name; an
// ambiguous (non-unique) or unknown name fails. Agent-callable twin of the
// user-only GetOrCreateConversation. Powers the "dm:@<user>" address resolver.
func (s *CommandService) GetOrCreateUserDM(ctx context.Context, req *connect.Request[v1pb.GetOrCreateUserDMRequest]) (*connect.Response[v1pb.GetOrCreateUserDMResponse], error) {
	agent, err := requireCallingAgent(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.PeerUserName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("peer_user_name must not be empty"))
	}

	users, err := s.store.FindUsersByName(ctx, req.Msg.PeerUserName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve peer user"))
	}
	switch len(users) {
	case 0:
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", req.Msg.PeerUserName))
	case 1:
	default:
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("user name %q is ambiguous; address by a unique name", req.Msg.PeerUserName))
	}

	conv, err := s.store.GetOrCreateDirectConversation(ctx, agent.ID, users[0].ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get or create user DM"))
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
	return connect.NewResponse(&v1pb.GetOrCreateUserDMResponse{
		Conversation: convertToV1Conversation(conv, users[0].Name, users[0].Name, memberCount, 0, users[0].Name),
	}), nil
}

// GetOrCreateAgentDM opens (or reuses) the type-3 agent-to-agent DM between the
// calling agent and a peer agent. Self-address is rejected. The pair is
// canonicalized by the store. Powers the "dm:@<agent>" address resolver.
func (s *CommandService) GetOrCreateAgentDM(ctx context.Context, req *connect.Request[v1pb.GetOrCreateAgentDMRequest]) (*connect.Response[v1pb.GetOrCreateAgentDMResponse], error) {
	agent, err := requireCallingAgent(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.PeerAgent == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("peer_agent must not be empty"))
	}

	peerResourceID := parseAgentResourceID(req.Msg.PeerAgent)
	peer, err := s.store.GetAgentByResourceID(ctx, peerResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve peer agent"))
	}
	if peer == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", peerResourceID))
	}
	if peer.ID == agent.ID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("an agent cannot open a DM with itself"))
	}

	conv, err := s.store.GetOrCreateAgentDM(ctx, agent.ID, peer.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get or create agent DM"))
	}

	memberCount, _ := s.store.GetConversationMemberCount(ctx, conv.ID)
	return connect.NewResponse(&v1pb.GetOrCreateAgentDMResponse{
		Conversation: convertToV1Conversation(conv, resolveUserName(ctx, s.store, conv.OwnerID), peer.Name, memberCount, 0, peer.Name),
	}), nil
}

// ListPeerAgents returns every other agent (the caller excluded) with the
// display name, persona, and connection state an agent needs to decide whom to
// address. Powers the "agent list" discovery tool.
func (s *CommandService) ListPeerAgents(ctx context.Context, _ *connect.Request[v1pb.ListPeerAgentsRequest]) (*connect.Response[v1pb.ListPeerAgentsResponse], error) {
	agent, err := requireCallingAgent(ctx)
	if err != nil {
		return nil, err
	}

	agents, err := s.store.ListAgents(ctx, &store.FindAgentMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list agents"))
	}

	peers := make([]*v1pb.PeerAgent, 0, len(agents))
	for _, a := range agents {
		if a.ID == agent.ID {
			continue
		}
		peers = append(peers, &v1pb.PeerAgent{
			Name:          formatAgentName(a.ResourceID),
			DisplayName:   a.Name,
			PersonaPrompt: a.Info.GetAcpConfig().GetPersonaPrompt(),
			// computeConnectionState returns the same enum as
			// convertToV1AgentStatus(...).GetState() without allocating the
			// wrapping AgentStatus + timestamp protos. a.Status is always
			// non-nil (listAgentImpl assigns it), so the deref is safe. The
			// connected flag is the agent's live AgentChannel in the dispatcher
			// (the machine heartbeats, not the agent).
			ConnectionState: computeConnectionState(a.Status, a.Deleted, agentReachable(s.dispatcher, a.ID, a.MachineID)),
		})
	}
	return connect.NewResponse(&v1pb.ListPeerAgentsResponse{Agents: peers}), nil
}
