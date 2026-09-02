package v1

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *CommandService) requireReactionCaller(ctx context.Context, convID, msgID uuid.UUID) (principalID, agentID *int, err error) {
	exists, existsErr := s.store.MessageExistsInConversation(ctx, convID, msgID)
	if existsErr != nil {
		return nil, nil, connect.NewError(connect.CodeInternal, errors.Wrapf(existsErr, "failed to check message existence"))
	}
	if !exists {
		return nil, nil, connect.NewError(connect.CodeNotFound, errors.New("message not found in conversation"))
	}

	if agent, ok := GetAgentFromContext(ctx); ok && agent != nil {
		if _, err := s.requireAgentMemberByConvID(ctx, convID); err != nil {
			return nil, nil, err
		}
		aid := agent.ID
		return nil, &aid, nil
	}
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		uid := user.ID
		return &uid, nil, nil
	}
	return nil, nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authenticated user or agent required"))
}

// AddReaction places the caller's emoji reaction on a message. Idempotent and
// lightweight: it never bumps the conversation's room version, wakes agents,
// counts as unread, or generates activity. See store.AddReaction.
func (s *CommandService) AddReaction(ctx context.Context, req *connect.Request[v1pb.AddReactionRequest]) (*connect.Response[v1pb.AddReactionResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	emoji, err := common.NormalizeReactionEmoji(req.Msg.Emoji)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	principalID, agentID, err := s.requireReactionCaller(ctx, convID, msgID)
	if err != nil {
		return nil, err
	}
	reactions, err := s.store.AddReaction(ctx, convID, msgID, principalID, agentID, emoji)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to add reaction"))
	}
	return connect.NewResponse(&v1pb.AddReactionResponse{Message: req.Msg.Message, Reactions: reactions}), nil
}

// RemoveReaction removes the caller's own emoji reaction from a message.
// Idempotent: removing an emoji the caller did not place is a no-op. Removing
// an emoji that exists but was placed by someone else is PERMISSION_DENIED —
// only the reactor removes its own reaction.
func (s *CommandService) RemoveReaction(ctx context.Context, req *connect.Request[v1pb.RemoveReactionRequest]) (*connect.Response[v1pb.RemoveReactionResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	emoji, err := common.NormalizeReactionEmoji(req.Msg.Emoji)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	principalID, agentID, err := s.requireReactionCaller(ctx, convID, msgID)
	if err != nil {
		return nil, err
	}
	result, err := s.store.RemoveReaction(ctx, convID, msgID, principalID, agentID, emoji)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to remove reaction"))
	}
	if !result.Removed && result.Others {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("you can only remove your own reaction"))
	}
	return connect.NewResponse(&v1pb.RemoveReactionResponse{Message: req.Msg.Message, Reactions: result.Reactions}), nil
}

// reactionCallerFromContext resolves the caller's identity for the
// caller-relative `reacted` reaction flag: the agent id when an agent is
// authenticated, the principal id when a user is, and nil/nil otherwise.
func reactionCallerFromContext(ctx context.Context) (principalID, agentID *int) {
	if agent, ok := GetAgentFromContext(ctx); ok && agent != nil {
		aid := agent.ID
		return nil, &aid
	}
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		uid := user.ID
		return &uid, nil
	}
	return nil, nil
}

// fillReactions attaches each message's aggregated reactions to its v1 view,
// for the read handlers (ListConversationMessages / ListThreadMessages). One
// batch query covers the page so reaction display does not incur an N+1. A
// message with no reactions emits an empty (non-nil) list.
func (s *CommandService) fillReactions(ctx context.Context, msgs []*store.ChatMessage, v1msgs []*v1pb.ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	principalID, agentID := reactionCallerFromContext(ctx)
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	byID, err := s.store.ListReactionsForMessages(ctx, ids, principalID, agentID)
	if err != nil {
		return err
	}
	for i, m := range msgs {
		v1msgs[i].Reactions = byID[m.ID]
	}
	return nil
}

func storeToV1ChatMessage(msg *store.ChatMessage) *v1pb.ChatMessage {
	senderName := msg.PrincipalName
	senderType := v1pb.SenderType(msg.SenderType)
	if msg.SenderType == store.SenderTypeAgent && msg.SenderAgentID.Valid {
		senderName = msg.AgentName
	}
	v1m := &v1pb.ChatMessage{
		Name:             msg.ID.String(),
		Conversation:     msg.ConversationID.String(),
		PrincipalName:    msg.PrincipalName,
		Role:             msg.Role,
		Content:          msg.Content,
		CreatedAt:        timestamppb.New(msg.CreatedAt),
		SenderName:       senderName,
		SenderType:       senderType,
		PrincipalId:      msg.PrincipalHandle,
		RoomVersion:      msg.RoomVersion,
		Mentions:         msg.Mentions,
		Attachments:      msg.Attachments,
		ThreadReplyCount: msg.ThreadReplyCount,
		Task:             storeToV1TaskInfo(msg.TaskInfo),
		Reactions:        msg.Reactions,
	}
	if msg.CommandID.Valid {
		v1m.CommandId = msg.CommandID.UUID.String()
	}
	v1m.AgentId = msg.AgentResourceID
	if msg.ThreadRootMessageID.Valid {
		v1m.ThreadRoot = msg.ThreadRootMessageID.UUID.String()
	}
	return v1m
}

// storeToV1TaskInfo converts the store-layer TaskInfo join into the proto
// TaskInfo carried on a ChatMessage. Returns nil when the message is not a
// task, so non-task messages omit the field.
func storeToV1TaskInfo(ti *store.TaskInfo) *v1pb.TaskInfo {
	if ti == nil {
		return nil
	}
	return &v1pb.TaskInfo{
		TaskNumber:         ti.TaskNumber,
		Status:             v1pb.TaskStatus(ti.Status),
		AssigneeName:       ti.AssigneeName,
		AssigneeResourceId: ti.AssigneeResourceID,
		AssigneeType:       int32(ti.AssigneeType),
	}
}

func toNullInt32(v int32) sql.NullInt32 {
	return sql.NullInt32{Int32: v, Valid: true}
}

func buildLightChatContext(msgs []*store.ChatMessage) string {
	var b strings.Builder
	_, _ = b.WriteString("## Recent conversation (use search_chat_history for older messages)\n")
	count := 0
	for i := len(msgs) - 1; i >= 0 && count < 6; i-- {
		msg := msgs[i]
		if msg.Role == 1 {
			sender := msg.PrincipalName
			if sender == "" {
				sender = "User"
			}
			_, _ = fmt.Fprintf(&b, "- %s: %s\n", sender, msg.Content)
		} else {
			sender := msg.AgentResourceID
			if sender == "" {
				sender = "Assistant"
			}
			_, _ = fmt.Fprintf(&b, "- %s: %s\n", sender, msg.Content)
		}
		count++
	}
	return b.String()
}
