package v1

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *CommandService) GetOrCreateConversation(ctx context.Context, req *connect.Request[v1pb.GetOrCreateConversationRequest]) (*connect.Response[v1pb.GetOrCreateConversationResponse], error) {
	agentResourceID := parseAgentResourceID(req.Msg.Agent)
	agent, err := s.store.GetAgentByResourceID(ctx, agentResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get agent"))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", agentResourceID))
	}

	// GetOrCreateConversation starts a direct conversation between the caller
	// and an agent. It is a user-facing action; an agent token must not create a
	// direct conversation owned by the system principal.
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("GetOrCreateConversation is for authenticated users"))
	}

	conv, err := s.store.GetOrCreateDirectConversation(ctx, agent.ID, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get or create conversation"))
	}

	return connect.NewResponse(&v1pb.GetOrCreateConversationResponse{
		Name: fmt.Sprintf("conversations/%s", conv.ID.String()),
	}), nil
}

func (s *CommandService) GetOrCreateUserUserDM(ctx context.Context, req *connect.Request[v1pb.GetOrCreateUserUserDMRequest]) (*connect.Response[v1pb.GetOrCreateUserUserDMResponse], error) {
	// GetOrCreateUserUserDM starts a 1:1 DM between the caller and another
	// user. It is a user-facing action; an agent token must not create a
	// user-user DM owned by the system principal.
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("GetOrCreateUserUserDM is for authenticated users"))
	}
	if req.Msg.PeerUser == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("peer_user must not be empty"))
	}

	peerHandle, err := common.GetUserHandle(req.Msg.PeerUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid peer_user"))
	}
	peer, err := s.store.GetUserByHandle(ctx, peerHandle)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve peer user"))
	}
	if peer == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %s not found", req.Msg.PeerUser))
	}
	if peer.ID == user.ID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a user cannot open a DM with themselves"))
	}

	conv, err := s.store.GetOrCreateUserUserDM(ctx, user.ID, peer.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get or create user DM"))
	}

	return connect.NewResponse(&v1pb.GetOrCreateUserUserDMResponse{
		Name: fmt.Sprintf("conversations/%s", conv.ID.String()),
	}), nil
}

// maxLongPollWaitMs caps wait_ms so a single request can never pin a
// connection (and a room-hub waiter) for more than 30s.
const maxLongPollWaitMs = 30000

// longPollDelta holds a delta read until new messages are visible, the wait
// elapses, or the request is canceled. It subscribes to the room hub before
// re-reading so a change that landed between the caller's first read and the
// subscription is not missed, and re-reads on every wake because not every
// room-version bump produces messages this read can see (e.g. a thread reply
// bumps the conversation version but is excluded from ListConversationMessages).
// On timeout or cancellation it returns the empty delta with the last-read
// current version so the client can advance its cursor and re-issue — a canceled
// long poll (client aborts the in-flight request, e.g. on unmount) is a normal
// exit, not an error, and must not surface as a CodeCanceled. A nil hub (unit
// tests) skips the wait and returns the re-read immediately.
func (s *CommandService) longPollDelta(ctx context.Context, convID uuid.UUID, waitMs int32, readDelta func() ([]*store.ChatMessage, int64, error), hasNew func([]*store.ChatMessage) bool) ([]*store.ChatMessage, int64, error) {
	if s.roomhub == nil {
		return readDelta()
	}
	ch := s.roomhub.Subscribe(convID)
	defer s.roomhub.Unsubscribe(convID, ch)

	msgs, currentVersion, err := readDelta()
	if err != nil {
		return nil, 0, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list messages"))
	}
	if hasNew(msgs) {
		return msgs, currentVersion, nil
	}

	timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ch:
			msgs, currentVersion, err = readDelta()
			if err != nil {
				return nil, 0, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list messages"))
			}
			if hasNew(msgs) {
				return msgs, currentVersion, nil
			}
			// Spurious wake (a bump this read cannot see): keep waiting on the
			// same timer so the total hold stays bounded by wait_ms.
		case <-timer.C:
			return nil, currentVersion, nil
		case <-ctx.Done():
			return nil, currentVersion, nil
		}
	}
}

func (s *CommandService) ListConversationMessages(ctx context.Context, req *connect.Request[v1pb.ListConversationMessagesRequest]) (*connect.Response[v1pb.ListConversationMessagesResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	if req.Msg.AfterVersion > 0 && req.Msg.BeforeVersion > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("after_version and before_version are mutually exclusive"))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 100,
	})
	if err != nil {
		return nil, err
	}

	// Three read modes, all returned in chronological (oldest -> newest) order:
	//   - before_version: one-shot history lookup of the `limit` messages
	//     immediately before the pivot (no backward page token).
	//   - after_version: incremental delta (room_version > after_version),
	//     paginated with a forward page token.
	//   - neither (default): one-shot latest-N — the newest `limit` messages,
	//     so opening a conversation shows recent history, not the oldest page.
	// wait_ms turns the after_version read into a long poll: when the delta is
	// empty the request is held (via the room hub) until a new message lands or
	// wait_ms elapses, then returns the empty delta with the current version so
	// the client can advance its cursor and re-issue. Capped server-side; only
	// meaningful with after_version > 0 (ignored otherwise).
	waitMs := req.Msg.WaitMs
	if waitMs > maxLongPollWaitMs {
		waitMs = maxLongPollWaitMs
	}

	var msgs []*store.ChatMessage
	var currentVersion int64
	switch {
	case req.Msg.BeforeVersion > 0:
		msgs, currentVersion, err = s.store.ListConversationMessages(ctx, convID, 0, req.Msg.BeforeVersion, offset.limit, 0)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
		}
	case req.Msg.AfterVersion > 0:
		limitPlusOne := offset.limit + 1
		readDelta := func() ([]*store.ChatMessage, int64, error) {
			return s.store.ListConversationMessages(ctx, convID, req.Msg.AfterVersion, 0, limitPlusOne, offset.offset)
		}
		msgs, currentVersion, err = readDelta()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
		}
		if len(msgs) == 0 && waitMs > 0 {
			msgs, currentVersion, err = s.longPollDelta(ctx, convID, waitMs, readDelta, func(msgs []*store.ChatMessage) bool { return len(msgs) > 0 })
			if err != nil {
				return nil, err
			}
		}
	default:
		msgs, currentVersion, err = s.store.ListConversationMessages(ctx, convID, 0, 0, offset.limit, 0)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list conversation messages"))
		}
	}

	// Forward pagination only applies to the after_version delta path: the store
	// returned one extra (limit+1) as a "has more" indicator, which we trim. The
	// before_version and default latest-N paths are one-shot (no +1), so len(msgs)
	// never reaches offset.limit+1 and this is a no-op for them.
	nextPageToken := ""
	if req.Msg.BeforeVersion == 0 && req.Msg.AfterVersion > 0 && len(msgs) == offset.limit+1 {
		msgs = msgs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	// is_own is caller-relative: only the agent itself (authenticated via the MCP
	// server's bearer token) can mark its own messages. Frontend/user callers get
	// is_own=false for every message.
	callerAgent, _ := GetAgentFromContext(ctx)

	// An agent reading a channel is committing to work on it. Link the session's
	// running command to this conversation now (not at ack time, which runs after
	// the work is done) so FetchConversationActivity can surface the agent's live
	// status while it works — without this the channel status bar stays "idle"
	// throughout execution. User/frontend callers have no session command and are
	// skipped. LinkCommandConversation is an idempotent insert (+ first-wins
	// column set) so a multi-channel turn links its command to every channel.
	if callerAgent != nil {
		if cmdID := s.dispatcher.CurrentCommandID(callerAgent.ID); cmdID != "" {
			if cid, parseErr := uuid.Parse(cmdID); parseErr == nil {
				if linkErr := s.store.LinkCommandConversation(ctx, cid, convID); linkErr != nil {
					slog.Warn("failed to link command to conversation on read", "commandID", cmdID, "conversationID", convID, "error", linkErr)
				}
			}
		}
	}

	var v1msgs []*v1pb.ChatMessage
	for _, msg := range msgs {
		v1m := storeToV1ChatMessage(msg)
		v1m.IsOwn = callerAgent != nil && msg.SenderAgentID.Valid && int(msg.SenderAgentID.Int32) == callerAgent.ID
		v1msgs = append(v1msgs, v1m)
	}
	if err := s.fillReactions(ctx, msgs, v1msgs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to fill reactions"))
	}

	return connect.NewResponse(&v1pb.ListConversationMessagesResponse{
		Messages:       v1msgs,
		NextPageToken:  nextPageToken,
		CurrentVersion: currentVersion,
	}), nil
}

// ListThreadMessages returns the root message of a thread followed by its
// replies in room_version order, with the same cursor model as
// ListConversationMessages. The caller must be a member of the conversation.
func (s *CommandService) ListThreadMessages(ctx context.Context, req *connect.Request[v1pb.ListThreadMessagesRequest]) (*connect.Response[v1pb.ListThreadMessagesResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	if req.Msg.ThreadRoot == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root must not be empty"))
	}
	rootID, err := uuid.Parse(req.Msg.ThreadRoot)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid thread_root"))
	}
	if req.Msg.AfterVersion > 0 && req.Msg.BeforeVersion > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("after_version and before_version are mutually exclusive"))
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 100,
	})
	if err != nil {
		return nil, err
	}

	// wait_ms turns the after_version read into a long poll, mirroring
	// ListConversationMessages: an empty delta holds the request until a new
	// reply lands or wait_ms elapses. Capped server-side; only meaningful with
	// after_version > 0 (ignored otherwise).
	waitMs := req.Msg.WaitMs
	if waitMs > maxLongPollWaitMs {
		waitMs = maxLongPollWaitMs
	}

	var msgs []*store.ChatMessage
	var currentVersion int64
	switch {
	case req.Msg.BeforeVersion > 0:
		msgs, currentVersion, err = s.store.ListThreadMessages(ctx, convID, rootID, 0, req.Msg.BeforeVersion, offset.limit, 0)
	case req.Msg.AfterVersion > 0:
		limitPlusOne := offset.limit + 1
		readDelta := func() ([]*store.ChatMessage, int64, error) {
			return s.store.ListThreadMessages(ctx, convID, rootID, req.Msg.AfterVersion, 0, limitPlusOne, offset.offset)
		}
		msgs, currentVersion, err = readDelta()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list thread messages"))
		}
		// ListThreadMessages always includes the thread root as the first
		// element, even on a delta read, so the thread's delta is "empty" when
		// it holds only the root (no new replies). A `len(msgs) == 0` gate here
		// would never be true and the long poll would never engage, turning the
		// watcher into a tight request loop. Only long-poll when no replies
		// arrived after afterVersion.
		if len(msgs) <= 1 && waitMs > 0 {
			msgs, currentVersion, err = s.longPollDelta(ctx, convID, waitMs, readDelta, func(msgs []*store.ChatMessage) bool { return len(msgs) > 1 })
			if err != nil {
				return nil, err
			}
		}
	default:
		msgs, currentVersion, err = s.store.ListThreadMessages(ctx, convID, rootID, 0, 0, offset.limit, 0)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list thread messages"))
	}

	nextPageToken := ""
	if req.Msg.BeforeVersion == 0 && req.Msg.AfterVersion > 0 && len(msgs) == offset.limit+1 {
		msgs = msgs[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	callerAgent, _ := GetAgentFromContext(ctx)
	// Link the session's command to this conversation on read, same as
	// ListConversationMessages, so FetchConversationActivity surfaces the
	// agent's live status while it works on the thread.
	if callerAgent != nil {
		if cmdID := s.dispatcher.CurrentCommandID(callerAgent.ID); cmdID != "" {
			if cid, parseErr := uuid.Parse(cmdID); parseErr == nil {
				if linkErr := s.store.LinkCommandConversation(ctx, cid, convID); linkErr != nil {
					slog.Warn("failed to link command to conversation on thread read", "commandID", cmdID, "conversationID", convID, "error", linkErr)
				}
			}
		}
	}

	var v1msgs []*v1pb.ChatMessage
	for _, msg := range msgs {
		v1m := storeToV1ChatMessage(msg)
		v1m.IsOwn = callerAgent != nil && msg.SenderAgentID.Valid && int(msg.SenderAgentID.Int32) == callerAgent.ID
		v1msgs = append(v1msgs, v1m)
	}
	if err := s.fillReactions(ctx, msgs, v1msgs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to fill reactions"))
	}

	return connect.NewResponse(&v1pb.ListThreadMessagesResponse{
		Messages:       v1msgs,
		NextPageToken:  nextPageToken,
		CurrentVersion: currentVersion,
	}), nil
}

// ListChannelThreads returns a summary (root id, reply count, latest reply
// version/time, newest replies, and a per-user unread count) for every active
// thread in a conversation. The channel page polls this to keep root-message
// reply-count badges fresh — including replies that arrive while the thread
// panel is closed (e.g. an async agent reply), which the message watcher
// cannot observe because ListConversationMessages excludes thread replies —
// and to render the root message's inline thread preview (the 3 most recent
// replies) plus its "M new" hint. Agent callers get fresh-replies but never an
// unread count (that is a user-cursor concept).
func (s *CommandService) ListChannelThreads(ctx context.Context, req *connect.Request[v1pb.ListChannelThreadsRequest]) (*connect.Response[v1pb.ListChannelThreadsResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	userPrincipalID, userHandle := 0, ""
	if user, ok := GetUserFromContext(ctx); ok {
		userPrincipalID, userHandle = user.ID, user.Handle
	}
	threads, err := s.store.ListChannelThreads(ctx, convID, userPrincipalID, userHandle)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list channel threads"))
	}
	v1threads := make([]*v1pb.ChannelThread, 0, len(threads))
	for _, t := range threads {
		ct := &v1pb.ChannelThread{
			RootMessage:        t.RootMessageID.String(),
			ReplyCount:         t.ReplyCount,
			LatestReplyVersion: t.LatestVersion,
			NewReplyCount:      t.NewReplyCount,
		}
		if !t.LatestAt.IsZero() {
			ct.LatestReplyAt = timestamppb.New(t.LatestAt)
		}
		for _, msg := range t.RecentReplies {
			ct.RecentReply = append(ct.RecentReply, storeToV1ChatMessage(msg))
		}
		v1threads = append(v1threads, ct)
	}
	return connect.NewResponse(&v1pb.ListChannelThreadsResponse{Threads: v1threads}), nil
}

func (s *CommandService) PostMessage(ctx context.Context, req *connect.Request[v1pb.PostMessageRequest]) (*connect.Response[v1pb.PostMessageResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}

	convUUID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation id"))
	}

	conv, err := s.store.GetConversation(ctx, convUUID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// An agent may only post to a conversation it is a member of.
	isMember, err := s.store.IsConversationMember(ctx, convUUID, store.MemberTypeAgent, agent.ResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation membership"))
	}
	if !isMember {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("not a conversation member"))
	}
	// An archived channel is read-only: the owner froze it, so no agent may
	// post new messages into it either. Messages remain visible and searchable.
	if conv.Archived {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("this channel has been archived by the owner; new messages are not allowed"))
	}

	currentVersion := conv.Version
	if req.Msg.BaseVersion == currentVersion {
		principalID := 1
		if conv.OwnerID > 0 {
			principalID = conv.OwnerID
		}

		var commandID uuid.NullUUID
		if req.Msg.CommandId != "" {
			if cid, parseErr := uuid.Parse(req.Msg.CommandId); parseErr == nil {
				commandID = uuid.NullUUID{UUID: cid, Valid: true}
			}
		}

		attachments, err := s.resolveAttachments(ctx, convUUID, req.Msg.Attachments)
		if err != nil {
			return nil, err
		}

		// thread_root, when set, makes this agent reply a message in an
		// existing thread rooted at the given message id. Validate the root
		// belongs to this conversation and is itself a root.
		var threadRoot uuid.NullUUID
		if req.Msg.ThreadRoot != "" {
			rootID, parseErr := uuid.Parse(req.Msg.ThreadRoot)
			if parseErr != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(parseErr, "invalid thread_root"))
			}
			isRoot, rootErr := s.store.IsThreadRoot(ctx, convUUID, rootID)
			if rootErr != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(rootErr, "failed to validate thread root"))
			}
			if !isRoot {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root is not a root message in this conversation"))
			}
			threadRoot = uuid.NullUUID{UUID: rootID, Valid: true}
		}

		// The agent addresses other agents/users by typing content-only
		// `@someone`; the manager parses those tokens into structured Mentions
		// (the agent never sets Mentions itself). Parsed mentions are persisted on
		// the message and drive thread subscription / wake routing below.
		mentions := s.parseContentMentions(ctx, convUUID, req.Msg.Content)

		msg, newVersion, createErr := s.createMessage(ctx, createMessageInput{
			convID:          convUUID,
			content:         req.Msg.Content,
			attachments:     attachments,
			mentions:        mentions,
			threadRoot:      threadRoot,
			commandID:       commandID,
			principalID:     principalID,
			principalHandle: resolveUserHandle(ctx, s.store, principalID),
			senderAgentID:   toNullInt32(int32(agent.ID)),
			role:            2,
			senderType:      store.SenderTypeAgent,
			exceptAgentID:   &agent.ID,
		})
		if createErr != nil {
			return nil, createErr
		}

		// The posting agent has produced this message, so it has processed up to
		// and including newVersion. Advance its durable cursor now so its own
		// reply is never re-surfaced as "new" on the next BeginSession (which
		// would make the agent re-read and mistake its own message for another
		// agent's). UpsertCursor is monotonic (GREATEST), so a later explicit
		// ack_processed_version from the agent cannot rewind it.
		if _, err := s.store.UpsertCursor(ctx, agent.ID, convUUID, newVersion); err != nil {
			slog.Warn("failed to advance posting agent cursor", "agentID", agent.ID, "conversationID", convUUID, "version", newVersion, "error", err)
		}

		return connect.NewResponse(&v1pb.PostMessageResponse{
			Committed:      true,
			Message:        storeToV1ChatMessage(msg),
			CurrentVersion: newVersion,
		}), nil
	}

	newMsgs, _, listErr := s.store.ListConversationMessages(ctx, convUUID, req.Msg.BaseVersion, 0, 50, 0)
	if listErr != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(listErr, "failed to list new messages"))
	}

	var v1NewMsgs []*v1pb.ChatMessage
	for _, m := range newMsgs {
		v1m := storeToV1ChatMessage(m)
		v1m.IsOwn = m.SenderAgentID.Valid && int(m.SenderAgentID.Int32) == agent.ID
		v1NewMsgs = append(v1NewMsgs, v1m)
	}

	return connect.NewResponse(&v1pb.PostMessageResponse{
		Committed:           false,
		CurrentVersion:      currentVersion,
		NewMessages:         v1NewMsgs,
		ConflictDescription: fmt.Sprintf("Version conflict: base_version=%d, current=%d. %d new messages arrived.", req.Msg.BaseVersion, currentVersion, len(v1NewMsgs)),
	}), nil
}
