package v1

import (
	"context"
	"log/slog"
	"strings"
	"unicode"

	"github.com/google/uuid"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// parseContentMentions scans message content for `@<handle>` tokens the agent
// typed and resolves them to conversation members, returning structured
// Mentions the manager then uses for thread subscription and wake routing. The
// agent itself does not construct mentions — it only writes content-only
// `@someone`, and the manager owns the resolution so an agent can proactively
// address a user or agent.
//
// Token form:
//   - `@handle` — a bare run of letters, digits, '_', '-', '.' (e.g.
//     `@ran-user-1`, `@rei-agent-1`). Stops at whitespace or other
//     punctuation.
//
// Matching is exact on the member's mention handle (case-insensitive). Handles
// are unique per type, so no ambiguity resolution is needed. The posting agent
// (excludeAgentResourceID) is never resolved to a self-mention.
func (s *CommandService) parseContentMentions(ctx context.Context, convID uuid.UUID, content, excludeAgentResourceID string) []*v1pb.Mention {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list conversation members for mention parsing", "conversationID", convID, "error", err)
		return nil
	}

	// handle -> member. Member ids are already the mention handles for both
	// users and agents, so the lookup key is the member id itself.
	byHandle := make(map[string]*store.ConversationMember, len(members))
	for _, m := range members {
		if m.MemberID == "" {
			continue
		}
		byHandle[normalizeMentionName(m.MemberID)] = m
	}

	var mentions []*v1pb.Mention
	seen := make(map[string]bool)
	for _, token := range tokenizeMentions(content) {
		key := normalizeMentionName(token)
		if key == "" {
			continue
		}
		m, ok := byHandle[key]
		if !ok {
			// Unknown handle: do not manufacture a mention.
			continue
		}
		if m.MemberType == store.MemberTypeAgent && m.MemberID == excludeAgentResourceID {
			continue
		}
		dedup := mentionTypeString(m.MemberType) + ":" + m.MemberID
		if seen[dedup] {
			continue
		}
		seen[dedup] = true
		mentions = append(mentions, &v1pb.Mention{
			Type: mentionTypeString(m.MemberType),
			Id:   m.MemberID,
			Name: m.MemberID,
		})
	}
	return mentions
}

// tokenizeMentions extracts `@<handle>` tokens from content, in order. A `@`
// only starts a mention when preceded by the start of content or a boundary
// rune (whitespace / punctuation), so email addresses are not mistaken for
// mentions.
func tokenizeMentions(content string) []string {
	var tokens []string
	runes := []rune(content)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if i+1 >= len(runes) {
			continue
		}
		next := runes[i+1]
		// The bare form `@handle` requires a boundary before the `@` (start of
		// content, whitespace, or punctuation) so an email local-part like
		// `alice@` is not mistaken for a mention. CJK bare mentions therefore
		// need a leading space.
		if i > 0 && !isMentionBoundary(runes[i-1]) {
			continue
		}
		if !isMentionNameRune(next) {
			continue
		}
		j := i + 1
		for j < len(runes) && isMentionNameRune(runes[j]) {
			j++
		}
		tokens = append(tokens, string(runes[i+1:j]))
		i = j - 1
	}
	return tokens
}

func isMentionNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.'
}

func isMentionBoundary(r rune) bool {
	return unicode.IsSpace(r) || (unicode.IsPunct(r) && r != '_' && r != '-')
}

func normalizeMentionName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func mentionTypeString(memberType int32) string {
	if memberType == store.MemberTypeAgent {
		return "agent"
	}
	return "user"
}

// mergeMentions unions server-parsed mentions (from parseContentMentions) with
// client-supplied mentions (e.g. from a mention picker), deduping by type:id
// and preserving the first-seen name. selfHandle, when non-empty, drops a
// self-mention (type=="user" with the caller's own handle) so a user never
// @mentions themself into their own activity feed. The client set is appended
// last so its names win on dedup collision only when the server did not
// resolve the same member — in practice the server resolves handles itself, so
// the two sets agree.
func mergeMentions(parsed, client []*v1pb.Mention, selfHandle string) []*v1pb.Mention {
	seen := make(map[string]bool, len(parsed)+len(client))
	out := make([]*v1pb.Mention, 0, len(parsed)+len(client))
	add := func(m *v1pb.Mention) {
		if m == nil {
			return
		}
		if selfHandle != "" && m.Type == "user" && m.Id == selfHandle {
			return
		}
		key := m.Type + ":" + m.Id
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, m)
	}
	for _, m := range parsed {
		add(m)
	}
	for _, m := range client {
		add(m)
	}
	return out
}
