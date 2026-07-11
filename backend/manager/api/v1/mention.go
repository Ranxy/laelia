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

// parseContentMentions scans message content for `@<name>` tokens the agent typed
// and resolves them to conversation members, returning structured Mentions the
// manager then uses for thread subscription and wake routing. The agent itself does
// not construct mentions — it only writes content-only `@someone`, and the manager
// owns the resolution so an agent can proactively address a user or agent.
//
// Token forms:
//   - `@"display name"` — quoted, captures multi-word / spaced names verbatim.
//   - `@name` — a bare run of letters, digits, '_', '-', '.' (e.g. `@alice`,
//     `@张三`, `@backend-bot`). Stops at whitespace or other punctuation, so
//     multi-word names must use the quoted form.
//
// Matching is case-insensitive on the member display name. Ambiguous names (two
// members sharing the same display name) are skipped to avoid misrouting. The
// posting agent (excludeAgentResourceID) is never resolved to a self-mention.
func (s *CommandService) parseContentMentions(ctx context.Context, convID uuid.UUID, content, excludeAgentResourceID string) []*v1pb.Mention {
	members, err := s.store.ListConversationMembers(ctx, convID)
	if err != nil {
		slog.Warn("failed to list conversation members for mention parsing", "conversationID", convID, "error", err)
		return nil
	}

	// name -> member(s). Resolve each member's display name once and key by its
	// lowercased form. Track ambiguity so a shared display name never misroutes.
	byName := make(map[string][]*store.ConversationMember)
	for _, m := range members {
		name := normalizeMentionName(resolveMemberDisplayName(ctx, s.store, m.MemberType, m.MemberID))
		if name == "" {
			continue
		}
		byName[name] = append(byName[name], m)
	}

	var mentions []*v1pb.Mention
	seen := make(map[string]bool)
	for _, token := range tokenizeMentions(content) {
		key := normalizeMentionName(token)
		if key == "" {
			continue
		}
		candidates, ok := byName[key]
		if !ok || len(candidates) != 1 {
			// Unknown or ambiguous: do not manufacture a mention.
			continue
		}
		m := candidates[0]
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
			Name: resolveMemberDisplayName(ctx, s.store, m.MemberType, m.MemberID),
		})
	}
	return mentions
}

// tokenizeMentions extracts `@<name>` and `@"name"` tokens from content, in order.
// A `@` only starts a mention when preceded by the start of content or a boundary
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
		// The quoted form `@"name"` is unambiguously a mention (no email contains
		// `@"`), so it is accepted regardless of what precedes the `@` — this lets
		// CJK text mention a name inline without a leading space, e.g. `转给@"张三"处理`.
		if next == '"' {
			end := -1
			for j := i + 2; j < len(runes); j++ {
				if runes[j] == '"' {
					end = j
					break
				}
			}
			if end == -1 {
				continue
			}
			if name := strings.TrimSpace(string(runes[i+2 : end])); name != "" {
				tokens = append(tokens, name)
			}
			i = end
			continue
		}
		// The bare form `@name` requires a boundary before the `@` (start of content,
		// whitespace, or punctuation) so an email local-part like `alice@` is not
		// mistaken for a mention. CJK bare mentions therefore need a leading space.
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
