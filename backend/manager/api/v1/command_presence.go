package v1

import (
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/presence"
)

// maxPresenceNames caps one SyncPresence call: the frontend asks for the DM
// peers of its left rail plus the open conversation, far below this bound.
const maxPresenceNames = 200

// SyncPresence records the calling principal's heartbeat and answers the
// online state of the requested principals. The caller's own presence is the
// only heartbeat this RPC accepts — identity comes from the auth context, so
// a caller can never report presence for someone else. Only human users
// ("users/<handle>") are tracked here; agents are answered offline because
// AgentService.ListAgents carries the authoritative agent connection state.
func (s *CommandService) SyncPresence(ctx context.Context, req *connect.Request[v1pb.SyncPresenceRequest]) (*connect.Response[v1pb.SyncPresenceResponse], error) {
	if user, ok := GetUserFromContext(ctx); ok {
		s.presence.Touch(user.GetResourceID(), time.Now())
	} else if agent, ok := GetAgentFromContext(ctx); ok {
		s.presence.Touch(agent.GetResourceID(), time.Now())
	}

	names := make([]string, 0, len(req.Msg.Names))
	for _, name := range req.Msg.Names {
		if name == "" || slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
	}
	if len(names) > maxPresenceNames {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.Errorf("at most %d names per request, got %d", maxPresenceNames, len(names)))
	}

	online := s.presence.Online(usersOnly(names), time.Now(), presence.DefaultTTL)
	presences := make([]*v1pb.Presence, 0, len(names))
	for _, name := range names {
		presences = append(presences, &v1pb.Presence{
			Name:   name,
			Online: online[name],
		})
	}

	return connect.NewResponse(&v1pb.SyncPresenceResponse{Presences: presences}), nil
}

// usersOnly keeps the names the presence registry tracks. Agents are dropped
// so they always answer offline: their connection state is authoritative in
// AgentService.ListAgents, and their own SyncPresence heartbeat must not make
// them appear online here.
func usersOnly(names []string) []string {
	users := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "users/") {
			users = append(users, name)
		}
	}
	return users
}
