package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// Chat search scope values mirror laelia.v1.SearchScope. Kept as untyped
// int32 constants on the store side so the persistence layer does not depend
// on the generated proto package.
const (
	SearchScopeAll      int32 = 0
	SearchScopeMessages int32 = 1
	SearchScopeFiles    int32 = 2
)

// Chat search match-field values carried on ChatSearchResult.
const (
	SearchMatchContent    int32 = 1
	SearchMatchAttachment int32 = 2
)

// maxSearchTokens caps how many query terms participate in a search. Longer
// queries are truncated to keep the generated SQL and ranking bounded.
const maxSearchTokens = 10

// ChatSearchCaller identifies the caller for the conversation read filter.
// Exactly one of UserHandle / AgentResourceID is set; WorkspaceRead means the
// caller holds conversations.read at workspace scope (e.g. a workspace admin)
// and may search every conversation.
type ChatSearchCaller struct {
	UserHandle       string
	AgentResourceID  string
	AgentOwnerID     int
	AgentFollowOwner bool
	WorkspaceRead    bool
}

// ChatSearchOptions is the search predicate. ConversationID.Valid selects a
// single conversation; otherwise every conversation the caller can read is
// searched.
type ChatSearchOptions struct {
	ConversationID uuid.NullUUID
	Query          string
	From           string
	Scope          int32
	Since          *time.Time
	Until          *time.Time
	Limit          int
	Offset         int
}

// ChatSearchResult is one search hit: the matched message, its conversation,
// the plain-text rendering used for the snippet, and which field matched.
type ChatSearchResult struct {
	Message               *ChatMessage
	Conversation          ConversationMessage
	SearchText            string
	MatchField            int32
	MatchedAttachmentName string
}

// SearchChatMessages searches chat messages the caller can read. The query is
// split into whitespace-separated tokens; a message matches when any token
// matches its markdown-stripped plain text or one of its attachment file names
// (case-insensitive substring). Results are ranked by the number of distinct
// matched tokens, then total term frequency, then newest first. A single
// conversation is searched when opts.ConversationID is set; otherwise the
// caller's readable conversations (membership, owner-follow for agents, or
// workspace-scope read) are searched.
func (s *Store) SearchChatMessages(ctx context.Context, caller ChatSearchCaller, opts ChatSearchOptions) ([]*ChatSearchResult, error) {
	if opts.Limit <= 0 || opts.Limit > 50 {
		opts.Limit = 10
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}

	tokens := tokenizeSearchQuery(opts.Query)

	args := []any{}
	var conds []string

	if opts.ConversationID.Valid {
		args = append(args, opts.ConversationID.UUID)
		conds = append(conds, fmt.Sprintf("cm.conversation_id = $%d", len(args)))
	} else {
		filter, filterArgs, err := s.chatSearchConversationFilter(ctx, caller)
		if err != nil {
			return nil, err
		}
		if filter != "" {
			args = append(args, filterArgs...)
			conds = append(conds, filter)
		}
	}

	// Reserve one pattern and one raw argument per token. The pattern feeds the
	// ILIKE predicates; the raw token feeds chat_occurrences for term frequency.
	base := len(args)
	patternIdxs := make([]int, 0, len(tokens))
	rawIdxs := make([]int, 0, len(tokens))
	for _, token := range tokens {
		args = append(args, "%"+token+"%", token)
		patternIdxs = append(patternIdxs, base+2*len(patternIdxs)+1)
		rawIdxs = append(rawIdxs, base+2*len(rawIdxs)+2)
	}

	contentMatch := ""
	contentDistinct := "0"
	contentTF := "0"
	if len(tokens) > 0 {
		matchParts := make([]string, 0, len(tokens))
		distinctParts := make([]string, 0, len(tokens))
		tfParts := make([]string, 0, len(tokens))
		for i := range tokens {
			matchParts = append(matchParts, fmt.Sprintf("cm.search_text ILIKE $%d", patternIdxs[i]))
			distinctParts = append(distinctParts, fmt.Sprintf("(cm.search_text ILIKE $%d)::int", patternIdxs[i]))
			tfParts = append(tfParts, fmt.Sprintf("chat_occurrences(cm.search_text, $%d)", rawIdxs[i]))
		}
		contentMatch = "(" + strings.Join(matchParts, " OR ") + ")"
		contentDistinct = strings.Join(distinctParts, " + ")
		contentTF = strings.Join(tfParts, " + ")
	}

	// The attachment LATERAL finds the newest matching file name and aggregates
	// the attachment-side ranking contributions across all matching files.
	lateral := ""
	if len(tokens) > 0 && opts.Scope != SearchScopeMessages {
		fileMatchParts := make([]string, 0, len(tokens))
		attDistinctParts := make([]string, 0, len(tokens))
		attTFParts := make([]string, 0, len(tokens))
		for i := range tokens {
			fileMatchParts = append(fileMatchParts, fmt.Sprintf("f.original_name ILIKE $%d", patternIdxs[i]))
			attDistinctParts = append(attDistinctParts, fmt.Sprintf("(count(*) FILTER (WHERE f.original_name ILIKE $%d) > 0)::int", patternIdxs[i]))
			attTFParts = append(attTFParts, fmt.Sprintf("COALESCE(sum(chat_occurrences(f.original_name, $%d)), 0)", rawIdxs[i]))
		}
		lateral = fmt.Sprintf(`LEFT JOIN LATERAL (
			SELECT
				(array_agg(f.original_name ORDER BY f.created_at DESC))[1] AS name,
				(%s) AS distinct_tokens,
				(%s) AS tf
			FROM file f
			WHERE f.conversation_id = cm.conversation_id
			  AND cm.attachments @> jsonb_build_array(jsonb_build_object('id', f.id::text))
			  AND (%s)
		) att ON true`,
			strings.Join(attDistinctParts, " + "),
			strings.Join(attTFParts, " + "),
			strings.Join(fileMatchParts, " OR "))
	}

	if len(tokens) > 0 {
		switch opts.Scope {
		case SearchScopeMessages:
			conds = append(conds, contentMatch)
		case SearchScopeFiles:
			conds = append(conds, "att.name IS NOT NULL")
		default:
			conds = append(conds, "("+contentMatch+" OR att.name IS NOT NULL)")
		}
	}

	if opts.From != "" {
		args = append(args, "%"+opts.From+"%")
		n := len(args)
		conds = append(conds, fmt.Sprintf(
			`((cm.sender_type = 2 AND (a.resource_id ILIKE $%d OR a.name ILIKE $%d)) OR (cm.sender_type <> 2 AND (p.handle ILIKE $%d OR p.name ILIKE $%d)))`,
			n, n, n, n))
	}
	if opts.Since != nil {
		args = append(args, opts.Since)
		conds = append(conds, fmt.Sprintf("cm.created_at >= $%d", len(args)))
	}
	if opts.Until != nil {
		args = append(args, opts.Until)
		conds = append(conds, fmt.Sprintf("cm.created_at <= $%d", len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	matchFieldExpr := "1"
	distinctExpr := "0"
	tfExpr := "0"
	if len(tokens) > 0 {
		switch opts.Scope {
		case SearchScopeMessages:
			matchFieldExpr = "1"
			distinctExpr = contentDistinct
			tfExpr = contentTF
		case SearchScopeFiles:
			matchFieldExpr = "2"
			distinctExpr = "COALESCE(att.distinct_tokens, 0)"
			tfExpr = "COALESCE(att.tf, 0)"
		default:
			matchFieldExpr = fmt.Sprintf("CASE WHEN %s THEN %d ELSE %d END", contentMatch, SearchMatchContent, SearchMatchAttachment)
			distinctExpr = contentDistinct + " + COALESCE(att.distinct_tokens, 0)"
			tfExpr = contentTF + " + COALESCE(att.tf, 0)"
		}
	}
	attNameExpr := "''"
	if lateral != "" {
		attNameExpr = "COALESCE(att.name, '')"
	}

	args = append(args, opts.Limit, opts.Offset)
	limitIdx := len(args) - 1
	offsetIdx := len(args)

	query := fmt.Sprintf(`
		SELECT `+chatMessageColumns+`,
		       c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version,
		       cm.search_text, %s, %s
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		JOIN conversation c ON c.id = cm.conversation_id
		%s
		%s
		ORDER BY (%s) DESC, (%s) DESC, cm.created_at DESC, cm.id DESC
		LIMIT $%d OFFSET $%d`,
		matchFieldExpr, attNameExpr, lateral, where, distinctExpr, tfExpr, limitIdx, offsetIdx)

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to search chat messages")
	}
	defer rows.Close()

	var results []*ChatSearchResult
	for rows.Next() {
		result, scanErr := scanChatSearchResultRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate chat search results")
	}
	return results, nil
}

// tokenizeSearchQuery splits a query into lowercase, deduplicated,
// whitespace-separated tokens, capped at maxSearchTokens.
func tokenizeSearchQuery(query string) []string {
	fields := strings.Fields(query)
	seen := make(map[string]bool, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.ToLower(field)
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
		if len(tokens) >= maxSearchTokens {
			break
		}
	}
	return tokens
}

// scanChatSearchResultRow scans one SearchChatMessages row: the common chat
// message columns followed by the conversation columns, the plain-text search
// rendering, and the match metadata. It mirrors scanChatMessageRow's JSON
// handling for mentions and attachments.
func scanChatSearchResultRow(row interface {
	Scan(dest ...any) error
}) (*ChatSearchResult, error) {
	var msg ChatMessage
	var mentionsBytes, attachmentsBytes []byte
	var conv ConversationMessage
	var searchText string
	var matchField int32
	var matchedAttachmentName string
	if err := row.Scan(
		&msg.ID, &msg.ConversationID, &msg.PrincipalID, &msg.PrincipalName,
		&msg.SenderAgentID, &msg.AgentResourceID, &msg.AgentName,
		&msg.Role, &msg.Content, &msg.CommandID, &msg.CreatedAt, &msg.RoomVersion, &msg.SenderType,
		&mentionsBytes, &attachmentsBytes, &msg.ThreadRootMessageID, &msg.PrincipalHandle,
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
		&searchText, &matchField, &matchedAttachmentName,
	); err != nil {
		return nil, errors.Wrapf(err, "failed to scan chat search result")
	}
	if len(mentionsBytes) > 0 {
		var mentions []*v1pb.Mention
		if err := json.Unmarshal(mentionsBytes, &mentions); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal mentions")
		}
		msg.Mentions = mentions
	}
	if len(attachmentsBytes) > 0 {
		var attachments []*v1pb.Attachment
		if err := json.Unmarshal(attachmentsBytes, &attachments); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal attachments")
		}
		msg.Attachments = attachments
	}
	return &ChatSearchResult{
		Message:               &msg,
		Conversation:          conv,
		SearchText:            searchText,
		MatchField:            matchField,
		MatchedAttachmentName: matchedAttachmentName,
	}, nil
}

// chatSearchConversationFilter builds the SQL predicate restricting a global
// search to conversations the caller can read. It mirrors the membership
// semantics of ListUserConversationsWithUnread / ListAccessibleChannels:
// workspace-scope read sees everything, users see their memberships, and
// agents see their memberships plus (optionally) their owner's memberships.
// The returned clause uses $1..$4 placeholders and must be the first WHERE
// condition so those numbers line up with the caller's argument slice.
func (s *Store) chatSearchConversationFilter(ctx context.Context, caller ChatSearchCaller) (string, []any, error) {
	if caller.WorkspaceRead {
		return "", nil, nil
	}
	switch {
	case caller.UserHandle != "":
		return `EXISTS (
			SELECT 1 FROM conversation_member_meta cmm
			WHERE cmm.conversation_id = cm.conversation_id AND cmm.member_type = $1 AND cmm.member_id = $2
		)`, []any{MemberTypeUser, caller.UserHandle}, nil
	case caller.AgentResourceID != "":
		args := []any{MemberTypeAgent, caller.AgentResourceID}
		ownerClause := "FALSE"
		if caller.AgentFollowOwner {
			ownerHandle, err := s.userMemberHandle(ctx, s.GetDB(), caller.AgentOwnerID)
			if err != nil {
				return "", nil, err
			}
			args = append(args, MemberTypeUser, ownerHandle)
			ownerClause = `EXISTS (
				SELECT 1 FROM conversation_member_meta cmo
				WHERE cmo.conversation_id = cm.conversation_id AND cmo.member_type = $3 AND cmo.member_id = $4
			)`
		}
		return `(EXISTS (
			SELECT 1 FROM conversation_member_meta cmm
			WHERE cmm.conversation_id = cm.conversation_id AND cmm.member_type = $1 AND cmm.member_id = $2
		) OR (` + ownerClause + `))`, args, nil
	default:
		return "FALSE", nil, nil
	}
}
