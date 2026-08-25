package v1

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

func validateSendMessageContent(req *v1pb.SendMessageRequest) error {
	if req.Content == "" && len(req.Attachments) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("content or attachments must not be empty"))
	}
	return nil
}

func (s *CommandService) SendMessage(ctx context.Context, req *connect.Request[v1pb.SendMessageRequest]) (*connect.Response[v1pb.ChatMessage], error) {
	if err := validateSendMessageContent(req.Msg); err != nil {
		return nil, err
	}

	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}

	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		// Agents reply via PostMessage; SendMessage is the user-facing path and
		// must never fall back to the system principal (the previous behavior
		// let an agent token post as principalID=1 "system").
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SendMessage is for authenticated users; agents must use PostMessage"))
	}

	// Agent-DM conversations (type 3) are agent-only. Users with
	// conversations.reviewAgentDM may read them but must never send into one.
	conv, convErr := s.store.GetConversation(ctx, convID)
	if convErr != nil {
		return nil, connect.NewError(connect.CodeNotFound, convErr)
	}
	if conv.Type == store.ConversationTypeAgentDM {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("agent-DM conversations are agent-only; users can view but cannot send"))
	}
	// An archived channel is read-only: the owner froze it, so no member
	// (including the owner) may post new messages. Messages remain visible and
	// searchable.
	if conv.Archived {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("this channel has been archived by the owner; new messages are not allowed"))
	}

	// thread_root, when set, makes this message a reply in an existing thread
	// rooted at the given message id. Validate the root belongs to this
	// conversation and is itself a root (not a nested thread reply).
	var threadRoot uuid.NullUUID
	if req.Msg.ThreadRoot != "" {
		rootID, parseErr := uuid.Parse(req.Msg.ThreadRoot)
		if parseErr != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(parseErr, "invalid thread_root"))
		}
		isRoot, rootErr := s.store.IsThreadRoot(ctx, convID, rootID)
		if rootErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(rootErr, "failed to validate thread root"))
		}
		if !isRoot {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("thread_root is not a root message in this conversation"))
		}
		threadRoot = uuid.NullUUID{UUID: rootID, Valid: true}
	}

	// as_task creates this top-level message as a task: a task row is inserted
	// in the same transaction with a per-conversation number and status TODO.
	// Tasks are top-level only, so as_task is incompatible with thread_root.
	if req.Msg.AsTask && threadRoot.Valid {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("as_task is only valid for top-level messages, not thread replies"))
	}

	// Validate attachment ids belong to this conversation and normalize their
	// metadata from the file rows. The agent path (PostMessage) already did
	// this; doing it here too closes the gap where user-sent attachments were
	// never membership-checked, and preserves caller-supplied anchor fields
	// (section_anchor/section_id/quoted_text) used by attachment comments.
	attachments, err := s.resolveAttachments(ctx, convID, req.Msg.Attachments)
	if err != nil {
		return nil, err
	}

	// Server-parse mentions from the content (mirrors the agent PostMessage path)
	// and merge with client-supplied mentions so user→user/@agent mentions reliably
	// drive thread subscription and activity generation even when the client does
	// not construct Mention structs. Self-mention (the caller's own id) is dropped.
	parsedMentions := s.parseContentMentions(ctx, convID, req.Msg.Content)
	mentions := mergeMentions(parsedMentions, req.Msg.Mentions)

	msg, _, err := s.createMessage(ctx, createMessageInput{
		convID:          convID,
		content:         req.Msg.Content,
		attachments:     attachments,
		mentions:        mentions,
		threadRoot:      threadRoot,
		asTask:          req.Msg.AsTask,
		principalID:     user.ID,
		principalName:   user.Name,
		principalHandle: user.Handle,
		role:            1, // USER
		senderType:      store.SenderTypeUser,
		posterUserID:    &user.ID,
	})
	if err != nil {
		return nil, err
	}

	if req.Msg.AsTask {
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("📋 %s created task #%d %q", user.Name, msg.TaskInfo.TaskNumber, truncateContent(req.Msg.Content)))
	}

	return connect.NewResponse(storeToV1ChatMessage(msg)), nil
}

// notifyConversationAgents sends NewMessagesAvailable to every connected
// agent that is a member of the conversation, except the agent identified by
// exceptAgentID (used by PostMessage so an agent's own reply does not wake
// itself). A nil exceptAgentID notifies all agent members. This covers both
// direct conversations (type=1) and multi-agent channels (type=2), and is the
// single wake path that lets agents talk to each other.
func (s *CommandService) notifyConversationAgents(ctx context.Context, convID uuid.UUID, version int64, exceptAgentID *int) {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list conversation members for notification", "conversationID", convID, "error", err)
		return
	}
	for _, m := range members {
		if m.MemberType != store.MemberTypeAgent {
			continue
		}
		agent, agentErr := s.store.GetAgentByResourceID(ctx, m.MemberID)
		if agentErr != nil || agent == nil {
			slog.Warn("failed to resolve agent for notification", "agentResourceID", m.MemberID, "error", agentErr)
			continue
		}
		if exceptAgentID != nil && agent.ID == *exceptAgentID {
			continue
		}
		s.dispatcher.NotifyNewMessages(ctx, agent.ID, convID.String(), version)
	}
}

// subscribeAndNotifyThread handles a thread reply: it subscribes any agent
// @mentioned in the reply (plus the posting agent, when posterAgentID is
// non-nil, plus the agent that authored the thread root, so an agent is woken
// by replies to its own messages) to the thread, and any user @mentioned (plus
// the posting user, when posterUserID is non-nil) via user_thread_participant,
// then wakes every current agent subscriber except posterAgentID that a new
// reply landed. Subscription is persistent — once an agent is subscribed (via
// @mention, its own reply, or its own root message) it is woken on every
// subsequent reply in the thread, even without a fresh @mention; a user
// subscriber gets a THREAD activity on every subsequent reply. Used by
// SendMessage (user, posterAgentID=nil, posterUserID=&user.ID) and PostMessage
// (agent, posterUserID=nil) for thread replies.
func (s *CommandService) subscribeAndNotifyThread(ctx context.Context, convID, rootID uuid.UUID, version int64, mentions []*v1pb.Mention, posterAgentID *int, posterUserID *int) {
	var agentIDs []int
	seen := make(map[int]bool)
	addAgent := func(id int) {
		if id > 0 && !seen[id] {
			seen[id] = true
			agentIDs = append(agentIDs, id)
		}
	}
	var userIDs []int
	userSeen := make(map[int]bool)
	addUser := func(id int) {
		if id > 0 && !userSeen[id] {
			userSeen[id] = true
			userIDs = append(userIDs, id)
		}
	}
	for _, m := range mentions {
		if m.Type == "agent" && m.Id != "" {
			agent, err := s.store.GetAgentByResourceID(ctx, m.Id)
			if err != nil || agent == nil {
				slog.Warn("failed to resolve mentioned agent for thread subscription", "resourceID", m.Id, "error", err)
				continue
			}
			addAgent(agent.ID)
		}
		if m.Type == "user" && m.Id != "" {
			user, err := s.store.GetUserByHandle(ctx, m.Id)
			if err != nil || user == nil {
				slog.Warn("failed to resolve mentioned user for thread subscription", "handle", m.Id, "error", err)
				continue
			}
			addUser(user.ID)
		}
	}
	if posterAgentID != nil {
		addAgent(*posterAgentID)
	}
	if posterUserID != nil {
		addUser(*posterUserID)
	}
	// The thread root's author is an implicit participant: when an agent
	// authored the root (e.g. it uploaded the markdown/html file being
	// commented on), subscribe it so every reply in the thread wakes it even
	// without a fresh @mention. This is what lets a user's anchored comment on
	// an agent's attachment reach the agent. Best-effort: a failed lookup only
	// skips the implicit subscription, never the explicit ones above.
	senderType, senderAgentID, err := s.store.GetThreadRootSender(ctx, rootID)
	if err != nil {
		slog.Warn("failed to resolve thread root sender for subscription", "rootID", rootID, "error", err)
	} else if senderType == store.SenderTypeAgent && senderAgentID.Valid {
		addAgent(int(senderAgentID.Int32))
	}
	if err := s.store.AddThreadParticipants(ctx, rootID, agentIDs); err != nil {
		slog.Warn("failed to subscribe thread participants", "rootID", rootID, "error", err)
		// Still notify existing subscribers below.
	}
	if err := s.store.AddUserThreadParticipants(ctx, rootID, userIDs); err != nil {
		slog.Warn("failed to subscribe user thread participants", "rootID", rootID, "error", err)
	}
	s.notifyThreadParticipants(ctx, convID, rootID, version, posterAgentID)
}

// notifyThreadParticipants wakes every agent subscribed to a thread (except
// exceptAgentID) that a new reply landed, carrying the thread root id so the
// agent can go straight to thread check/read. Best-effort: a missed wake is
// recovered on reconnect via ListThreadUpdates (the durable cursor is the
// source of truth).
func (s *CommandService) notifyThreadParticipants(ctx context.Context, convID, rootID uuid.UUID, version int64, exceptAgentID *int) {
	agentIDs, err := s.store.ListThreadParticipantAgents(ctx, rootID)
	if err != nil {
		slog.Warn("failed to list thread participants for notification", "rootID", rootID, "error", err)
		return
	}
	for _, id := range agentIDs {
		if exceptAgentID != nil && id == *exceptAgentID {
			continue
		}
		s.dispatcher.NotifyThreadMention(ctx, id, convID.String(), version, rootID.String())
	}
}

// conversationPeer carries the DM peer's mention handle, display name, and
// resource name so a single resolver call can populate Conversation.Address
// (from handle), Conversation.Title (from displayName), and Conversation.Peer
// (from resource). Zero value (all empty) means no peer.
type conversationPeer struct {
	handle      string // mention handle ("ran-user-1" / "rei-agent-1"), used for the "dm:@<handle>" address
	displayName string // display name, used for DM row titles
	resource    string // resource name e.g. "users/<handle>" or "agents/<handle>", used for Conversation.Peer
}

// maxListPreviewLen caps the single-line last-message preview embedded in the
// left-rail conversation list. The client truncates visually via CSS; this
// only bounds the payload while keeping the preview one line.
const maxListPreviewLen = 120

// convertToV1Conversation is the single builder for v1 Conversation. It
// populates Address — the handle-based form agents write and read — from the
// conversation type and a caller-resolved peerHandle:
//   - type 2 (channel): "#<title>"; peerHandle is unused.
//   - type 1 (user DM): "dm:@<peerHandle>", where peerHandle is the DM peer's
//     mention handle from the viewer's perspective (the agent for a user
//     viewer, the user for an agent viewer).
//   - type 3 (agent DM): "dm:@<peerHandle>", where peerHandle is the other
//     agent's handle.
//
// peerResourceName is the DM peer's resource name ("users/<handle>" or
// "agents/<handle>") from the viewer's perspective, surfaced on
// Conversation.Peer so list viewers can fetch the peer's avatar without an
// extra member lookup. Empty for channels and when no peer can be resolved.
//
// Callers resolve peerHandle/peerResourceName (they already resolve owner/title
// for their view) so the builder stays free of lookups. Empty peerHandle
// leaves a DM address empty rather than emitting a malformed "dm:@".
