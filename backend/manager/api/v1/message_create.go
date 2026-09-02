package v1

import (
	"context"
	"database/sql"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// createMessageInput carries the sender-specific differences between the user
// SendMessage path and the agent PostMessage path, so both share one message
// creation pipeline.
type createMessageInput struct {
	convID      uuid.UUID
	content     string
	attachments []*v1pb.Attachment
	mentions    []*v1pb.Mention
	threadRoot  uuid.NullUUID
	asTask      bool
	commandID   uuid.NullUUID

	principalID     int
	principalName   string
	principalHandle string
	senderAgentID   sql.NullInt32
	role            int32
	senderType      int32

	// exceptAgentID excludes the posting agent from agent notifications; nil
	// notifies all agents.
	exceptAgentID *int
	// posterUserID is set for user senders so thread subscription can add the
	// posting user.
	posterUserID *int
}

// createMessage writes a chat message (or task message), bumps the
// conversation version, wakes agents/thread subscribers, and generates
// activity. It is the shared pipeline behind SendMessage (users) and
// PostMessage (agents).
func (s *CommandService) createMessage(ctx context.Context, in createMessageInput) (*store.ChatMessage, int64, error) {
	var msg *store.ChatMessage
	var newVersion int64
	var err error

	if in.asTask {
		msg, newVersion, err = s.store.CreateTaskMessageBumpVersion(ctx, &store.ChatMessage{
			ConversationID:  in.convID,
			PrincipalID:     in.principalID,
			PrincipalName:   in.principalName,
			PrincipalHandle: in.principalHandle,
			Role:            in.role,
			Content:         in.content,
			SenderType:      in.senderType,
			Mentions:        in.mentions,
			Attachments:     in.attachments,
		})
	} else {
		msg, newVersion, err = s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
			ConversationID:      in.convID,
			PrincipalID:         in.principalID,
			PrincipalName:       in.principalName,
			PrincipalHandle:     in.principalHandle,
			SenderAgentID:       in.senderAgentID,
			Role:                in.role,
			Content:             in.content,
			CommandID:           in.commandID,
			SenderType:          in.senderType,
			Mentions:            in.mentions,
			Attachments:         in.attachments,
			ThreadRootMessageID: in.threadRoot,
		})
	}
	if err != nil {
		return nil, 0, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create message"))
	}

	if in.threadRoot.Valid {
		s.subscribeAndNotifyThread(ctx, in.convID, in.threadRoot.UUID, newVersion, in.mentions, in.exceptAgentID, in.posterUserID)
	} else {
		s.notifyConversationAgents(ctx, in.convID, newVersion, in.exceptAgentID)
	}

	// Generate per-user activity for this message (mention/task/reminder/thread).
	// Best-effort: failures are logged, never fatal.
	rootIsTask := in.asTask
	rootIsReminder := false
	if in.threadRoot.Valid {
		rootIsTask, rootIsReminder, err = s.store.RootMessageKinds(ctx, in.threadRoot.UUID)
		if err != nil {
			slog.Warn("failed to resolve thread root kinds for activity", "rootID", in.threadRoot.UUID, "error", err)
		}
	}
	s.store.GenerateActivityForMessage(msg, rootIsTask, rootIsReminder)

	return msg, newVersion, nil
}
