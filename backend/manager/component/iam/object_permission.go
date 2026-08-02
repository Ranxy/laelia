package iam

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// checkCommandPermission authorizes a command-scoped permission for the caller.
// Access mirrors the former handler-level requireCommandAccess: the command's
// owning agent, a user holding conversations.reviewAll (cross-conversation
// oversight), or any member of a conversation the command is linked to. A
// missing or malformed command denies rather than errors (fail-closed, no
// resource-existence probing).
func (m *Manager) checkCommandPermission(ctx context.Context, _ permission.Permission, user *store.UserMessage, agent *store.AgentMessage, name string) (bool, error) {
	cmd, err := m.store.GetCommandByName(ctx, name)
	if err != nil {
		return false, nil //nolint:nilerr
	}
	if agent != nil && agent.ID == cmd.AgentID {
		return true, nil
	}
	if user != nil {
		if ok, rErr := m.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil); rErr == nil && ok {
			return true, nil
		}
	}
	memberType, memberID, ok := callerMemberInfo(user, agent)
	if !ok {
		return false, nil
	}
	isMember, err := m.store.IsCommandConversationMember(ctx, cmd.ID, memberType, memberID)
	if err != nil {
		return false, err
	}
	return isMember, nil
}

// checkReminderPermission authorizes a reminder-scoped permission for the
// caller. Access mirrors the former handler-level requireReminderAccess: the
// assignee agent, a user holding conversations.reviewAll, or any member of the
// reminder's conversation. Owner-only mutations (complete/fail) are still
// gated by the handler's requireReminderOwner.
func (m *Manager) checkReminderPermission(ctx context.Context, _ permission.Permission, user *store.UserMessage, agent *store.AgentMessage, name string) (bool, error) {
	msgID, err := parseReminderResourceID(name)
	if err != nil {
		return false, nil //nolint:nilerr
	}
	r, err := m.store.GetReminder(ctx, msgID)
	if err != nil {
		return false, nil //nolint:nilerr
	}
	if agent != nil && agent.ID == r.AssigneeAgentID {
		return true, nil
	}
	if user != nil {
		if ok, rErr := m.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil); rErr == nil && ok {
			return true, nil
		}
	}
	memberType, memberID, ok := callerMemberInfo(user, agent)
	if !ok {
		return false, nil
	}
	role, _, err := m.store.GetConversationMembership(ctx, r.ConversationID, memberType, memberID)
	if err != nil {
		return false, err
	}
	return role != 0, nil
}

// checkFilePermission authorizes a file-scoped permission for the caller. A
// conversation-linked file requires conversation membership (any role); an
// unlinked file is only accessible to the user who uploaded it (agents never
// qualify, matching the former requireFileMember fallback).
func (m *Manager) checkFilePermission(ctx context.Context, _ permission.Permission, user *store.UserMessage, agent *store.AgentMessage, name string) (bool, error) {
	fileID, err := parseFileResourceID(name)
	if err != nil {
		return false, nil //nolint:nilerr
	}
	f, err := m.store.GetFile(ctx, fileID)
	if err != nil {
		return false, err
	}
	if f == nil {
		return false, nil
	}
	if f.ConversationID.Valid {
		memberType, memberID, ok := callerMemberInfo(user, agent)
		if !ok {
			return false, nil
		}
		return m.store.IsConversationMember(ctx, f.ConversationID.UUID, memberType, memberID)
	}
	return user != nil && user.ID == f.UploaderPrincipalID, nil
}

// parseReminderResourceID parses "reminders/{message_id}".
func parseReminderResourceID(name string) (uuid.UUID, error) {
	rest, ok := strings.CutPrefix(name, "reminders/")
	if !ok || rest == "" {
		return uuid.Nil, errors.Errorf("invalid reminder name %q", name)
	}
	return uuid.Parse(rest)
}

// parseFileResourceID parses "files/{file_id}" or a bare file UUID (the
// DownloadFile request historically carried a bare id).
func parseFileResourceID(name string) (uuid.UUID, error) {
	rest, ok := strings.CutPrefix(name, "files/")
	if !ok {
		rest = name
	}
	return uuid.Parse(rest)
}
