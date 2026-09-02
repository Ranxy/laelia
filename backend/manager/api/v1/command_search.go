package v1

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *CommandService) SearchChatHistory(ctx context.Context, req *connect.Request[v1pb.SearchChatHistoryRequest]) (*connect.Response[v1pb.SearchChatHistoryResponse], error) {
	user, hasUser := GetUserFromContext(ctx)
	agent, hasAgent := GetAgentFromContext(ctx)
	if !hasUser && !hasAgent {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	var convID uuid.NullUUID
	if req.Msg.Conversation != "" {
		id, err := parseConversationID(req.Msg.Conversation)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
		}
		convID = uuid.NullUUID{UUID: id, Valid: true}
		ok, err := s.iam.CheckPermission(ctx, permission.ConversationsRead, user, agent, &iam.ResourceRef{
			ResourceType: models.Policy_CONVERSATION,
			Name:         common.FormatConversationName(id.String()),
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation access"))
		}
		if !ok {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("permission \"laelia.conversations.read\" denied"))
		}
	}

	// A workspace-scope conversations.read grant (workspace admin or a custom
	// role) lets the caller search every conversation; otherwise the store
	// filters to the caller's memberships (and owner-follow for agents).
	workspaceRead, err := s.iam.CheckPermission(ctx, permission.ConversationsRead, user, agent, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace read permission"))
	}

	caller := store.ChatSearchCaller{WorkspaceRead: workspaceRead}
	if user != nil {
		caller.UserHandle = user.Handle
	}
	if agent != nil {
		caller.AgentResourceID = agent.ResourceID
		caller.AgentOwnerID = agent.OwnerID
		caller.AgentFollowOwner = agent.FollowOwnerPermissions
	}

	var since, until *time.Time
	if req.Msg.Since != nil {
		st := req.Msg.Since.AsTime()
		since = &st
	}
	if req.Msg.Until != nil {
		ut := req.Msg.Until.AsTime()
		until = &ut
	}

	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.Limit),
		maximum: 50,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	results, err := s.store.SearchChatMessages(ctx, caller, store.ChatSearchOptions{
		ConversationID: convID,
		Query:          req.Msg.Query,
		From:           req.Msg.From,
		Scope:          int32(req.Msg.Scope),
		Since:          since,
		Until:          until,
		Limit:          limitPlusOne,
		Offset:         offset.offset,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to search chat history"))
	}

	nextPageToken := ""
	if len(results) == limitPlusOne {
		results = results[:offset.limit]
		nextPageToken, _ = offset.getNextPageToken()
	}

	callerAgent, _ := GetAgentFromContext(ctx)
	convCache := make(map[uuid.UUID]*v1pb.Conversation, len(results))

	// Load the thread roots for reply hits once so the UI can render each
	// reply nested under its root message without an extra round trip per hit.
	rootIDs := make([]uuid.UUID, 0)
	for _, res := range results {
		if res.Message.ThreadRootMessageID.Valid {
			rootIDs = append(rootIDs, res.Message.ThreadRootMessageID.UUID)
		}
	}
	roots, err := s.store.GetThreadRootMessages(ctx, rootIDs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to load thread context"))
	}

	entries := make([]*v1pb.SearchChatHistoryEntry, 0, len(results))
	for _, res := range results {
		conv, ok := convCache[res.Conversation.ID]
		if !ok {
			conv = s.searchConversationV1(ctx, &res.Conversation, user, agent)
			convCache[res.Conversation.ID] = conv
		}
		v1m := storeToV1ChatMessage(res.Message)
		v1m.IsOwn = callerAgent != nil && res.Message.SenderAgentID.Valid && int(res.Message.SenderAgentID.Int32) == callerAgent.ID
		entry := &v1pb.SearchChatHistoryEntry{
			Message:               v1m,
			Conversation:          conv,
			Snippet:               searchSnippet(res.SearchText, req.Msg.Query),
			MatchField:            res.MatchField,
			MatchedAttachmentName: res.MatchedAttachmentName,
		}
		if res.Message.ThreadRootMessageID.Valid {
			if root, ok := roots[res.Message.ThreadRootMessageID.UUID]; ok {
				entry.ThreadContext = &v1pb.SearchThreadContext{Root: storeToV1ChatMessage(root)}
			}
		}
		entries = append(entries, entry)
	}

	return connect.NewResponse(&v1pb.SearchChatHistoryResponse{
		Entries:       entries,
		NextPageToken: nextPageToken,
	}), nil
}

// searchConversationV1 builds the conversation context for a search hit,
// reusing the same title/peer resolution as the channel list and detail
// endpoints. Member count and read cursor are omitted (search results do not
// render them).
func (s *CommandService) searchConversationV1(ctx context.Context, conv *store.ConversationMessage, user *store.UserMessage, agent *store.AgentMessage) *v1pb.Conversation {
	ownerName := resolveUserName(ctx, s.store, conv.OwnerID)
	ownerHandle := resolveUserHandle(ctx, s.store, conv.OwnerID)
	title := conv.Title
	peerName := ""
	peerResource := ""
	viewerAgentResourceID := ""
	viewerUserID := 0
	if agent != nil {
		viewerAgentResourceID = agent.ResourceID
	}
	if user != nil {
		viewerUserID = user.ID
	}

	switch conv.Type {
	case store.ConversationTypeDM:
		if agent != nil {
			peerName = resolveUserHandle(ctx, s.store, conv.OwnerID)
			peerResource = common.FormatUserHandle(peerName)
			title = resolveUserName(ctx, s.store, conv.OwnerID)
		} else if conv.AgentID.Valid {
			if a, err := s.store.GetAgent(ctx, int(conv.AgentID.Int32)); err == nil && a != nil {
				peerName = a.ResourceID
				peerResource = common.FormatAgentUID(a.ResourceID)
				title = a.Name
			}
		}
	case store.ConversationTypeAgentDM:
		if peer := s.resolveAgentDMPeer(ctx, conv.ID, viewerAgentResourceID); peer != nil {
			peerName = peer.ResourceID
			peerResource = common.FormatAgentUID(peer.ResourceID)
			title = peer.Name
		}
	case store.ConversationTypeUserDM:
		if peer := s.resolveUserDMPeer(ctx, conv.ID, viewerUserID); peer != nil {
			peerName = peer.Handle
			peerResource = common.FormatUserHandle(peer.Handle)
			title = peer.Name
		}
	default:
	}
	return convertToV1Conversation(conv, ownerName, ownerHandle, peerName, peerResource, 0, 0, title, 0)
}

// searchSnippet returns a short excerpt of content around the earliest
// case-insensitive match of any query token. When the query is empty or no
// token occurs in content (e.g. an attachment-name match) it returns the
// beginning of the message.
func searchSnippet(content, query string) string {
	const maxRunes = 200
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	lower := strings.ToLower(content)
	byteIdx := -1
	matchRunes := 0
	for _, field := range strings.Fields(query) {
		token := strings.ToLower(field)
		if token == "" {
			continue
		}
		if idx := strings.Index(lower, token); idx >= 0 && (byteIdx < 0 || idx < byteIdx) {
			byteIdx = idx
			matchRunes = utf8.RuneCountInString(token)
		}
	}
	if byteIdx < 0 {
		return string(runes[:maxRunes]) + "…"
	}
	idx := utf8.RuneCountInString(content[:byteIdx])
	start := idx - 60
	if start < 0 {
		start = 0
	}
	end := idx + matchRunes + 120
	if end > len(runes) {
		end = len(runes)
	}
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	suffix := ""
	if end < len(runes) {
		suffix = "…"
	}
	return prefix + string(runes[start:end]) + suffix
}
