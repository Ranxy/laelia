package v1

import (
	"context"
	"fmt"
	"strconv"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// activityNamePrefix is the resource-name form for one activity row:
// "users/{uid}/activities/{activity_key}". The uid is the owning user's principal
// id; the activity_key is the row identity — the mentioning message id for a
// MENTION, or the thread root for a folded TASK/REMINDER/THREAD row. It is stable
// across bumps (a thread row's message pointer advances to the latest reply, but
// its activity_key stays the thread root), so the name a client holds remains
// valid for MarkActivityDone even after newer replies arrive.
const activityNamePrefix = "activities/"

// parseActivityName parses "users/{uid}/activities/{activity_key}" into the owning
// user id and the activity's key. The uid is a decimal principal id; the
// activity_key is a UUID. Used by MarkActivityDone to scope the mutation to the
// caller's own row.
func parseActivityName(name string) (userID int, activityKey uuid.UUID, err error) {
	tokens, err := common.GetNameParentTokens(name, common.UserNamePrefix, activityNamePrefix)
	if err != nil {
		return 0, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	uid, err := strconv.Atoi(tokens[0])
	if err != nil {
		return 0, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user id in activity name %q", name))
	}
	key, parseErr := uuid.Parse(tokens[1])
	if parseErr != nil {
		return 0, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(parseErr, "invalid activity key in activity name %q", name))
	}
	return uid, key, nil
}

// storeToV1Activity maps a store.Activity row to its proto form. The Activity
// carries the per-user state (categories/read/done) plus joined content/sender
// columns so the handler can render the list and detail without another round
// trip. state is derived from done/read_at: DONE takes precedence, then READ
// (read_at set), else UNREAD. summary is the truncated content prefixed with the
// sender's display name for the left-list preview. name carries the stable
// activity_key; message carries the latest message the row points at (the
// thread's newest reply for a folded row).
func storeToV1Activity(a *store.Activity, viewerUserID int) *v1pb.Activity {
	state := v1pb.ActivityState_ACTIVITY_STATE_UNREAD
	if a.Done {
		state = v1pb.ActivityState_ACTIVITY_STATE_DONE
	} else if a.ReadAt.Valid {
		state = v1pb.ActivityState_ACTIVITY_STATE_READ
	}
	activity := &v1pb.Activity{
		Name:         fmt.Sprintf("%s%d/%s%s", common.UserNamePrefix, viewerUserID, activityNamePrefix, a.ActivityKey),
		Conversation: fmt.Sprintf("conversations/%s", a.ConversationID),
		Message:      fmt.Sprintf("conversations/%s/messages/%s", a.ConversationID, a.MessageID),
		Categories:   a.Categories,
		State:        state,
		RoomVersion:  a.RoomVersion,
		Summary:      truncateContent(a.Content),
		SenderName:   a.SenderName,
		SenderType:   v1pb.SenderType(a.SenderType),
		CreatedAt:    timestamppb.New(a.CreatedAt),
	}
	if a.ThreadRootMessageID.Valid {
		activity.ThreadRoot = a.ThreadRootMessageID.UUID.String()
	}
	if a.ReadAt.Valid {
		activity.ReadAt = timestamppb.New(a.ReadAt.Time)
	}
	if a.DoneAt.Valid {
		activity.DoneAt = timestamppb.New(a.DoneAt.Time)
	}
	return activity
}

// ListActivities returns the authenticated user's activity feed: chat messages
// relevant to the caller, tagged with one or more categories (mention/task/
// reminder/thread). The feed is inherently per-user — the caller's principal_id
// is the implicit filter, so the laelia.activities.list permission is
// workspace-scoped and the interceptor does not resolve a conversation resource.
//
// read_state_filter selects the lifecycle slice: UNSPECIFIED = all not-done
// (the "All" view — read or unread, never dismissed); UNREAD = only unread;
// READ = only read; DONE = only dismissed. The product's default Unread view is
// driven by the frontend, which sends UNREAD explicitly for the Unread tab, so
// the backend honors UNSPECIFIED as all-not-done rather than collapsing it to
// UNREAD (collapsing made the All view silently hide READ rows, so opening an
// activity — which marks it READ — made it vanish from All too). filter, when
// non-empty, restricts to items whose categories intersect any requested flag.
// Pagination is offset-based (page_token is the decimal offset), mirroring
// ListReminders. Only authenticated users have an activity feed; an agent
// caller is rejected.
func (s *CommandService) ListActivities(ctx context.Context, req *connect.Request[v1pb.ListActivitiesRequest]) (*connect.Response[v1pb.ListActivitiesResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("ListActivities is for authenticated users"))
	}

	// Pass read_state_filter straight through: the store's activityReadStateClause
	// maps UNSPECIFIED to "all not-done", UNREAD/READ/DONE to their slices.
	readState := int32(req.Msg.ReadStateFilter)

	categoryFilter := make([]int32, 0, len(req.Msg.Filter))
	for _, c := range req.Msg.Filter {
		if c == v1pb.ActivityCategory_ACTIVITY_CATEGORY_UNSPECIFIED {
			continue
		}
		categoryFilter = append(categoryFilter, int32(c))
	}

	pageSize := int(req.Msg.PageSize)
	activities, nextToken, err := s.store.ListActivities(ctx, user.ID, categoryFilter, readState, pageSize, req.Msg.PageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list activities"))
	}

	out := make([]*v1pb.Activity, 0, len(activities))
	for _, a := range activities {
		out = append(out, storeToV1Activity(a, user.ID))
	}
	return connect.NewResponse(&v1pb.ListActivitiesResponse{Activities: out, NextPageToken: nextToken}), nil
}

// MarkActivityDone marks one of the caller's activity rows DONE, hiding it from
// both the Unread and All views. The name scopes the mutation to the owning user
// (users/{uid}/activities/{activity_key}); a caller may only mark its own row — a
// uid that does not match the authenticated user is PermissionDenied, and the
// store scopes by principal_id as a second line of defense. Returns NotFound
// when no not-done row exists for the (user, activity_key) pair. A DONE row is
// resurrected as UNREAD if a newer reply later bumps the same activity_key (see
// UpsertActivity), so marking done dismisses the current state, not future replies.
func (s *CommandService) MarkActivityDone(ctx context.Context, req *connect.Request[v1pb.MarkActivityDoneRequest]) (*connect.Response[v1pb.MarkActivityDoneResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("MarkActivityDone is for authenticated users"))
	}
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	uid, activityKey, err := parseActivityName(name)
	if err != nil {
		return nil, err
	}
	// Enforce ownership: the name's uid must be the caller's own id. Rejecting a
	// mismatch here prevents a user from marking another user's activity done
	// even if they guessed the activity key.
	if uid != user.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot mark another user's activity done"))
	}

	activity, err := s.store.MarkActivityDone(ctx, user.ID, activityKey)
	if err != nil {
		if errors.Is(err, store.ErrActivityNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("activity not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to mark activity done"))
	}
	return connect.NewResponse(&v1pb.MarkActivityDoneResponse{Activity: storeToV1Activity(activity, user.ID)}), nil
}
