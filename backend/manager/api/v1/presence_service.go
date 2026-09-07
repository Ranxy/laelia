package v1

import (
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// maxPresenceResults caps one ListPresence response. A workspace's presence
// row count is bounded by its user count (far below this); the cap is a
// safety net against unbounded result sets. Workspaces that outgrow it need
// pagination, at which point the client's whole-map semantics must be
// revisited with it.
const maxPresenceResults = 2000

// PresenceStore is the presence-facing slice of the store. Narrowed to an
// interface so handler tests can fake it hermetically.
type PresenceStore interface {
	TouchPresence(ctx context.Context, handle string, now time.Time) error
	ListPresence(ctx context.Context) ([]store.PresenceRow, error)
}

type PresenceService struct {
	v1connect.UnimplementedPresenceServiceHandler
	store PresenceStore
}

func NewPresenceService(s PresenceStore) *PresenceService {
	return &PresenceService{store: s}
}

// SendHeartbeat records the calling human user's presence heartbeat.
// Identity comes from the auth context, so a caller can never report
// presence for someone else. Agent callers are answered OK as a no-op:
// agents are not tracked here (their connection state is authoritative in
// AgentService.ListAgents).
func (s *PresenceService) SendHeartbeat(ctx context.Context, _ *connect.Request[v1pb.SendHeartbeatRequest]) (*connect.Response[v1pb.SendHeartbeatResponse], error) {
	if user, ok := GetUserFromContext(ctx); ok {
		if err := s.store.TouchPresence(ctx, user.Handle, time.Now()); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to record heartbeat"))
		}
	}
	return connect.NewResponse(&v1pb.SendHeartbeatResponse{}), nil
}

// ListPresence answers the presence of every tracked human user. The set is
// defined by the server, so clients never tell the server what to query —
// the read path cannot couple to whatever a client happens to have loaded.
// A user absent from the response has never sent a heartbeat.
func (s *PresenceService) ListPresence(ctx context.Context, _ *connect.Request[v1pb.ListPresenceRequest]) (*connect.Response[v1pb.ListPresenceResponse], error) {
	rows, err := s.store.ListPresence(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list presence"))
	}

	return connect.NewResponse(&v1pb.ListPresenceResponse{
		Presences: convertToV1Presences(rows, time.Now()),
	}), nil
}

// convertToV1Presences maps store rows to the wire shape, sorted by handle
// for deterministic responses, capped at maxPresenceResults.
func convertToV1Presences(rows []store.PresenceRow, now time.Time) []*v1pb.Presence {
	slices.SortFunc(rows, func(a, b store.PresenceRow) int {
		return strings.Compare(a.Handle, b.Handle)
	})
	if len(rows) > maxPresenceResults {
		rows = rows[:maxPresenceResults]
	}

	presences := make([]*v1pb.Presence, 0, len(rows))
	for _, row := range rows {
		presences = append(presences, &v1pb.Presence{
			Name:       common.FormatUserHandle(row.Handle),
			Online:     store.PresenceOnline(row.LastSeenAt, now, store.PresenceTTL),
			LastSeenAt: timestamppb.New(row.LastSeenAt),
		})
	}
	return presences
}
