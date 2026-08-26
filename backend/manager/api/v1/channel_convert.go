package v1

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func convertToV1Conversation(conv *store.ConversationMessage, ownerName string, ownerHandle string, peerName string, peerResourceName string, memberCount int, unreadCount int32, title string, readVersion int64) *v1pb.Conversation {
	var address string
	switch conv.Type {
	case store.ConversationTypeChannel:
		address = "#" + title
	case store.ConversationTypeDM, store.ConversationTypeAgentDM, store.ConversationTypeUserDM:
		if peerName != "" {
			address = "dm:@" + peerName
		}
	default:
		// no address for unknown types
	}
	return &v1pb.Conversation{
		Name:        fmt.Sprintf("conversations/%s", conv.ID.String()),
		Title:       title,
		Type:        conv.Type,
		MemberCount: int32(memberCount),
		OwnerId:     ownerHandle,
		OwnerName:   ownerName,
		CreatedAt:   timestamppb.New(conv.CreatedAt),
		UpdatedAt:   timestamppb.New(conv.UpdatedAt),
		UnreadCount: unreadCount,
		Address:     address,
		ReadVersion: readVersion,
		Peer:        peerResourceName,
		Archived:    conv.Archived,
	}
}

func resolveUserName(ctx context.Context, s *store.Store, principalID int) string {
	if principalID == 0 {
		return ""
	}
	u, err := s.GetUserByID(ctx, principalID)
	if err != nil || u == nil {
		slog.Warn("failed to resolve user name", "principalID", principalID, "error", err)
		return fmt.Sprintf("%d", principalID)
	}
	return u.Name
}

// resolveUserHandle returns the user's mention handle ("ran-user-1") for the
// given principal id, or "" when the user cannot be resolved. Handles are the
// conversation member ids for users, so owner comparisons and DM addresses use
// them instead of the numeric principal id.
func resolveUserHandle(ctx context.Context, s *store.Store, principalID int) string {
	if principalID == 0 {
		return ""
	}
	u, err := s.GetUserByID(ctx, principalID)
	if err != nil || u == nil {
		slog.Warn("failed to resolve user handle", "principalID", principalID, "error", err)
		return ""
	}
	return u.Handle
}

// resolveUserResource returns the user's resource name ("users/<handle>") for
// the given principal id, or "" when the user cannot be resolved. Used for
// created_by/owner fields that surface a user identity on agents, machines,
// and providers.
func resolveUserResource(ctx context.Context, s *store.Store, principalID int) string {
	handle := resolveUserHandle(ctx, s, principalID)
	if handle == "" {
		return ""
	}
	return common.FormatUserHandle(handle)
}

// resolveAgentDMPeer returns the other agent in a type-3 agent DM — the first
// agent member whose resource_id is not selfResourceID. When selfResourceID is
// empty (no caller-agent perspective, e.g. an admin fetching a single type-3
// conversation via GetChannel) the first resolvable agent member is returned.
// Returns nil when there is no resolvable non-self peer agent (well-formed
// type-3 DMs always have two agent members, so this only happens on a
// degenerate roster); it never returns the viewer's own agent.
func (s *CommandService) resolveAgentDMPeer(ctx context.Context, convID uuid.UUID, selfResourceID string) *store.AgentMessage {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list members for agent-DM peer", "conversationID", convID, "error", err)
		return nil
	}
	for _, m := range members {
		if m.MemberType != store.MemberTypeAgent {
			continue
		}
		if selfResourceID != "" && m.MemberID == selfResourceID {
			continue
		}
		agent, err := s.store.GetAgentByResourceID(ctx, m.MemberID)
		if err != nil || agent == nil || agent.Name == "" {
			continue
		}
		return agent
	}
	return nil
}

// resolvePeerNameForViewer resolves the DM peer display name for
// convertToV1Conversation from the viewer's perspective:
//   - type 1 (user DM): when the viewer is the agent participant, the peer is
//     the user (owner); otherwise the peer is the agent (conv.AgentID).
//   - type 3 (agent DM): the other agent (the agent member that is not the
//     viewer; the first agent when the viewer is not an agent).
//   - type 4 (user DM): the other user (the user member that is not the
//     viewer; the first user when the viewer is not a user).
//   - type 2 (channel): zero value (channels have no peer).
//
// The viewer's agent resource id is "" when the caller is a user/admin; the
// viewer's user id is 0 when the caller is an agent. Used by GetChannel; the
// list endpoints resolve the peer per-row from their own viewer context.
func (s *CommandService) resolvePeerForViewer(ctx context.Context, conv *store.ConversationMessage, viewerAgentResourceID string, viewerUserID int) conversationPeer {
	switch conv.Type {
	case store.ConversationTypeDM:
		if viewerAgentResourceID != "" {
			// The agent viewer's peer is the user owner.
			peerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
			return conversationPeer{handle: peerHandle, displayName: resolveUserName(ctx, s.store, conv.OwnerID), resource: common.FormatUserHandle(peerHandle)}
		}
		if !conv.AgentID.Valid {
			return conversationPeer{}
		}
		agent, err := s.store.GetAgent(ctx, int(conv.AgentID.Int32))
		if err != nil || agent == nil {
			return conversationPeer{}
		}
		return conversationPeer{handle: agent.ResourceID, displayName: agent.Name, resource: common.FormatAgentUID(agent.ResourceID)}
	case store.ConversationTypeAgentDM:
		peer := s.resolveAgentDMPeer(ctx, conv.ID, viewerAgentResourceID)
		if peer == nil {
			return conversationPeer{}
		}
		return conversationPeer{handle: peer.ResourceID, displayName: peer.Name, resource: common.FormatAgentUID(peer.ResourceID)}
	case store.ConversationTypeUserDM:
		peer := s.resolveUserDMPeer(ctx, conv.ID, viewerUserID)
		if peer == nil {
			return conversationPeer{}
		}
		return conversationPeer{handle: peer.Handle, displayName: peer.Name, resource: common.FormatUserHandle(peer.Handle)}
	}
	return conversationPeer{}
}

// resolveUserDMPeer returns the other user in a type-4 user DM — the first
// user member whose principal id is not viewerUserID. When viewerUserID is 0
// (no caller-user perspective, e.g. an admin fetching a single type-4
// conversation via GetChannel) the first resolvable user member is returned.
// Returns nil when there is no resolvable non-self peer user (well-formed
// type-4 DMs always have two user members, so this only happens on a
// degenerate roster); it never returns the viewer's own user.
func (s *CommandService) resolveUserDMPeer(ctx context.Context, convID uuid.UUID, viewerUserID int) *store.UserMessage {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list members for user-DM peer", "conversationID", convID, "error", err)
		return nil
	}
	for _, m := range members {
		if m.MemberType != store.MemberTypeUser {
			continue
		}
		user, err := s.store.GetUserByHandle(ctx, m.MemberID)
		if err != nil || user == nil || user.Name == "" {
			continue
		}
		if viewerUserID != 0 && user.ID == viewerUserID {
			continue
		}
		return user
	}
	return nil
}

// FetchConversationActivity returns the execution status of every agent in a
// conversation. The frontend polls this to show real-time agent status in the
// channel header.
func (s *CommandService) FetchConversationActivity(ctx context.Context, req *connect.Request[v1pb.FetchConversationActivityRequest]) (*connect.Response[v1pb.FetchConversationActivityResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	activities, err := s.dispatcher.FetchConversationActivity(ctx, convID.String())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to fetch conversation activity"))
	}
	return connect.NewResponse(&v1pb.FetchConversationActivityResponse{Activities: activities}), nil
}

// MarkConversationRead advances the requesting user's read cursor for a
// conversation to its current room_version, clearing the user-facing unread
// badge. The caller must be a member of the conversation.
func (s *CommandService) MarkConversationRead(ctx context.Context, req *connect.Request[v1pb.MarkConversationReadRequest]) (*connect.Response[v1pb.MarkConversationReadResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("MarkConversationRead is for authenticated users"))
	}
	conv, err := s.store.GetConversation(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to read conversation version"))
	}
	readVersion, err := s.store.UpsertUserReadCursor(ctx, user.ID, convID, conv.Version)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to mark conversation read"))
	}
	// Advance the user's unread activity rows in this conversation to READ.
	// Threads share the conversation's room_version space, so reading the
	// channel marks all thread activity in it read too. Best-effort: a failure
	// only leaves an activity as UNREAD (still visible under Unread), never data
	// corruption, so it is logged rather than failing the read itself.
	if err := s.store.MarkConversationActivitiesRead(ctx, user.ID, convID, readVersion); err != nil {
		slog.Warn("failed to mark conversation activities read", "conversationID", convID, "userID", user.ID, "error", err)
	}
	return connect.NewResponse(&v1pb.MarkConversationReadResponse{ReadVersion: readVersion}), nil
}

func (s *CommandService) SetConversationPinned(ctx context.Context, req *connect.Request[v1pb.SetConversationPinnedRequest]) (*connect.Response[v1pb.SetConversationPinnedResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SetConversationPinned is for authenticated users"))
	}
	// SetConversationPinned updates the caller's own membership row; a missing
	// row (non-member) returns ErrConversationMemberNotFound, which doubles as
	// the membership gate so only members can pin.
	if err := s.store.SetConversationPinned(ctx, convID, user.ID, req.Msg.Pinned); err != nil {
		if errors.Is(err, store.ErrConversationMemberNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("must be a member to pin a conversation"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to set conversation pinned"))
	}
	return connect.NewResponse(&v1pb.SetConversationPinnedResponse{}), nil
}

func (s *CommandService) SetConversationClosed(ctx context.Context, req *connect.Request[v1pb.SetConversationClosedRequest]) (*connect.Response[v1pb.SetConversationClosedResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SetConversationClosed is for authenticated users"))
	}
	// SetConversationClosed updates the caller's own membership row; a missing
	// row (non-member) returns ErrConversationMemberNotFound, which doubles as
	// the membership gate so only members can close.
	if err := s.store.SetConversationClosed(ctx, convID, user.ID, req.Msg.Closed); err != nil {
		if errors.Is(err, store.ErrConversationMemberNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("must be a member to close a conversation"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to set conversation closed"))
	}
	return connect.NewResponse(&v1pb.SetConversationClosedResponse{}), nil
}

func (s *CommandService) SetConversationMuted(ctx context.Context, req *connect.Request[v1pb.SetConversationMutedRequest]) (*connect.Response[v1pb.SetConversationMutedResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("SetConversationMuted is for authenticated users"))
	}
	// SetConversationMuted updates the caller's own membership row; a missing
	// row (non-member) returns ErrConversationMemberNotFound, which doubles as
	// the membership gate so only members can mute.
	if err := s.store.SetConversationMuted(ctx, convID, user.ID, req.Msg.Muted); err != nil {
		if errors.Is(err, store.ErrConversationMemberNotFound) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("must be a member to mute a conversation"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to set conversation muted"))
	}
	return connect.NewResponse(&v1pb.SetConversationMutedResponse{}), nil
}

func resolveMemberDisplayName(ctx context.Context, s *store.Store, memberType int32, memberID string) string {
	if memberType == store.MemberTypeUser {
		user, err := s.GetUserByHandle(ctx, memberID)
		if err != nil || user == nil {
			return memberID
		}
		return user.Name
	}
	if memberType == store.MemberTypeAgent {
		agent, err := s.GetAgentByResourceID(ctx, memberID)
		if err != nil || agent == nil {
			return memberID
		}
		return agent.Name
	}
	return memberID
}

// resolveMemberProfile returns a member's public description, avatar resource
// name, and preferred language from a single user/agent lookup. For users the
// description is User.description, the avatar is users/{handle}/avatar when the
// user has uploaded one (empty otherwise), and the language is the user's
// chat_preferences preferred_language (UNSPECIFIED when unset); for agents the
// description is Agent.description (the public intro), the avatar is
// agents/{agent}/avatar when uploaded (empty otherwise), and the language is
// always UNSPECIFIED. The agent's private persona_prompt is intentionally NOT
// exposed here. Surfaced in channel/thread rosters so an agent can perceive who
// a member is, render avatars without a per-user lookup, and converse in the
// member's preferred language.
func resolveMemberProfile(ctx context.Context, s *store.Store, memberType int32, memberID string) (string, string, v1pb.PreferredLanguage) {
	if memberType == store.MemberTypeUser {
		u, err := s.GetUserByHandle(ctx, memberID)
		if err != nil || u == nil {
			return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
		}
		avatar := ""
		if u.AvatarS3Key != "" {
			avatar = common.FormatUserAvatar(u.Handle)
		}
		return u.Description, avatar, v1pb.PreferredLanguage(u.ChatPreferences.GetPreferredLanguage())
	}
	if memberType == store.MemberTypeAgent {
		agent, err := s.GetAgentByResourceID(ctx, memberID)
		if err != nil || agent == nil {
			return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
		}
		avatar := ""
		if agent.AvatarS3Key != "" {
			avatar = common.FormatAgentAvatar(agent.ResourceID)
		}
		return agent.Description, avatar, v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
	}
	return "", "", v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED
}

// buildChannelMember assembles a v1 ChannelMember from a membership row, resolving
// the display name and public description. Shared by ListChannelMembers and
// ListThreadParticipants so both rosters render identity consistently.
func buildChannelMember(ctx context.Context, s *store.Store, memberType int32, memberID string, role int32, joinedAt time.Time) *v1pb.ChannelMember {
	description, avatar, language := resolveMemberProfile(ctx, s, memberType, memberID)
	m := &v1pb.ChannelMember{
		MemberType:        memberType,
		MemberId:          memberID,
		Handle:            memberID,
		DisplayName:       resolveMemberDisplayName(ctx, s, memberType, memberID),
		MemberRole:        role,
		Description:       description,
		Avatar:            avatar,
		PreferredLanguage: language,
	}
	if !joinedAt.IsZero() {
		m.JoinedAt = timestamppb.New(joinedAt)
	}
	return m
}
