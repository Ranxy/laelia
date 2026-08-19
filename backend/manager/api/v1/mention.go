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

// mentionCandidate is an intermediate resolved mention before the final display
// name is chosen. DisplayName is the member's canonical display name; the final
// Mention.Name may fall back to the handle when two mentioned members share a
// display name.
type mentionCandidate struct {
	Type        string
	ID          string
	DisplayName string
}

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
// Resolution is three-pass:
//  1. Primary: exact, case-insensitive match on the member's mention handle
//     (the member id). Handles are unique per type, so this is unambiguous.
//  2. Fallback: case-insensitive match on the member's display name, used only
//     when the handle did not match and only when the display name is
//     unambiguous (exactly one member carries it). This keeps `@<display name>`
//     working — the natural form agents used before the handle migration and
//     still reach for when the roster leads with a display name. Ambiguous
//     display names (two members sharing one) are never resolved by name, so a
//     display name can never misroute.
//  3. Global fallback: when the token is not a member of this conversation,
//     resolve it against the global agent/user directory by handle (and, when
//     unambiguous, by display name). This lets an agent mention a peer that is
//     not a member of the current channel/DM (e.g. listing members of another
//     channel) and still have the @mention rendered as a badge. The global
//     directory is cached in the store and rebuilt only when an agent/user is
//     created, updated, or deleted.
//
// The member/global fallback indexes are built lazily — only when at least one
// token misses the earlier index — so the common case (the agent typed handles
// of conversation members) pays no extra lookup cost.
//
// The Mention.Name is the display text shown in the badge: normally the
// member's display name, or the handle when the same message mentions two
// different members who share a display name. The canonical handle is always
// in Mention.Id, so the frontend can match both @handle and @display-name
// forms from a single Mention. The posting agent and the sending user are NOT
// excluded here — the routing layer already skips the poster, and activity
// generation skips self-mentions, so keeping them lets the frontend render
// @self as a badge.
func (s *CommandService) parseContentMentions(ctx context.Context, convID uuid.UUID, content string) []*v1pb.Mention {
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

	// display-name -> member, built lazily on the first handle miss. Only
	// unambiguous display names (one member) are included; a name shared by two
	// members is dropped so it can never misroute.
	var byDisplayName map[string]*store.ConversationMember

	// Global agent/user directory, built lazily on the first conversation miss.
	// It lets @mentions of non-members (e.g. agents listed from another
	// channel) still resolve for rendering.
	var globalIndex *store.GlobalMentionIndex
	var candidates []mentionCandidate

	seen := make(map[string]bool)
	addCandidate := func(mType, mID, displayName string) {
		if mID == "" {
			return
		}
		key := mType + ":" + mID
		if seen[key] {
			return
		}
		seen[key] = true
		if displayName == "" {
			displayName = mID
		}
		candidates = append(candidates, mentionCandidate{Type: mType, ID: mID, DisplayName: displayName})
	}

	for _, token := range tokenizeMentions(content) {
		key := normalizeMentionName(token)
		if key == "" {
			continue
		}
		m, ok := byHandle[key]
		if !ok {
			// Fallback: resolve by display name (only when unambiguous). The
			// index is built once, lazily, on the first miss so the common
			// all-handles path never pays the per-member display-name lookups.
			if byDisplayName == nil {
				byDisplayName = buildDisplayNameIndex(ctx, s.store, members)
			}
			m, ok = byDisplayName[key]
			if !ok {
				// Global fallback: the token is not a member of this
				// conversation. First try the cheap, cached handle lookups
				// against the global agent/user directory (this is the common
				// case — an agent typing @<handle> of a peer outside the
				// current channel). Only when the handle is unknown do we
				// build the full directory for the display-name fallback.
				if globalIndex == nil {
					if agent, agentErr := s.store.GetAgentByResourceID(ctx, key); agentErr == nil && agent != nil && !agent.Deleted {
						addCandidate("agent", agent.ResourceID, agent.Name)
						continue
					}
					if user, userErr := s.store.GetUserByHandle(ctx, key); userErr == nil && user != nil && !user.MemberDeleted {
						addCandidate("user", user.Handle, user.Name)
						continue
					}
					globalIndex, _ = s.store.GetGlobalMentionIndex(ctx)
				}
				if globalIndex != nil {
					if gm, ok2 := globalIndex.Get(key); ok2 {
						addCandidate(gm.Type, gm.Id, gm.Name)
						continue
					}
				}
				// Unknown handle and no display-name match anywhere: skip.
				continue
			}
		}
		addCandidate(mentionTypeString(m.MemberType), m.MemberID, resolveMemberDisplayName(ctx, s.store, m.MemberType, m.MemberID))
	}

	return buildMentionsWithDisplayNames(candidates)
}

// buildMentionsWithDisplayNames converts resolved candidates into the final
// Mention list. By default Name is the member's display name. When the same
// message mentions two different members who share a display name, Name falls
// back to the handle so the UI can disambiguate.
func buildMentionsWithDisplayNames(candidates []mentionCandidate) []*v1pb.Mention {
	nameCounts := make(map[string]int, len(candidates))
	for _, c := range candidates {
		displayName := c.DisplayName
		if displayName == "" {
			displayName = c.ID
		}
		nameCounts[normalizeMentionName(displayName)]++
	}
	mentions := make([]*v1pb.Mention, 0, len(candidates))
	for _, c := range candidates {
		displayName := c.DisplayName
		if displayName == "" {
			displayName = c.ID
		}
		name := displayName
		if nameCounts[normalizeMentionName(displayName)] > 1 {
			name = c.ID
		}
		mentions = append(mentions, &v1pb.Mention{
			Type: c.Type,
			Id:   c.ID,
			// Name is the display text shown in the badge. It is normally the
			// member's display name; when two mentioned members share a display
			// name it is the handle instead, so the UI can disambiguate. The
			// canonical handle is always available in Id for click dispatch.
			Name: name,
		})
	}
	return mentions
}

// displayNameResolver returns a member's display name from its type and member
// id. It is a function parameter so buildDisplayNameIndexWithResolver can be
// unit-tested without a live store.
type displayNameResolver func(memberType int32, memberID string) string

// buildDisplayNameIndexWithResolver maps a lowercased, unambiguous display name
// to the single member that carries it. A display name shared by two or more
// members is excluded entirely (left out of the map) so a name-based fallback
// can never misroute a mention. The resolver supplies each member's display
// name (cached store lookups in production; a map in tests).
func buildDisplayNameIndexWithResolver(members []*store.ConversationMember, resolve displayNameResolver) map[string]*store.ConversationMember {
	counts := make(map[string]int, len(members))
	names := make(map[string]*store.ConversationMember, len(members))
	for _, m := range members {
		if m.MemberID == "" {
			continue
		}
		dn := normalizeMentionName(resolve(m.MemberType, m.MemberID))
		if dn == "" {
			continue
		}
		counts[dn]++
		names[dn] = m // last write wins; pruned below when count > 1
	}
	for dn, c := range counts {
		if c > 1 {
			delete(names, dn)
		}
	}
	return names
}

// buildDisplayNameIndex is the production wrapper around
// buildDisplayNameIndexWithResolver that resolves display names via the store
// (cached lookups), matching the pre-handle-change resolution cost only on the
// fallback path.
func buildDisplayNameIndex(ctx context.Context, s *store.Store, members []*store.ConversationMember) map[string]*store.ConversationMember {
	return buildDisplayNameIndexWithResolver(members, func(memberType int32, memberID string) string {
		return resolveMemberDisplayName(ctx, s, memberType, memberID)
	})
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
		// Strip trailing dots: '.' is a valid internal handle separator
		// (e.g. "team.lead-user-1") but a trailing '.' is almost always
		// sentence-ending punctuation ("Waiting for my role from
		// @para-agent-1."). Handles never end with '.' — SlugifyHandle drops
		// dots and FormatHandle always ends in a digit — so any trailing '.' is
		// punctuation, not part of the handle. Without this, the trailing dot
		// is consumed into the token ("para-agent-1.") and the handle lookup
		// misses, silently dropping the mention.
		for j > i+1 && runes[j-1] == '.' {
			j--
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
// client-supplied mentions (e.g. from a mention picker), deduping by type:id.
// A single Mention now carries both the canonical handle (Id) and the display
// text (Name), so one entry per member is enough for the frontend to match
// both @handle and @display-name forms. Self-mentions are NOT dropped here —
// they are kept so the frontend can render @self as a badge; activity
// generation skips the sender so a user never gets a MENTION activity for
// @mentioning themself. The server-parsed set is added first so its display
// name wins when the client also supplied the same member.
func mergeMentions(parsed, client []*v1pb.Mention) []*v1pb.Mention {
	seen := make(map[string]bool, len(parsed)+len(client))
	out := make([]*v1pb.Mention, 0, len(parsed)+len(client))
	add := func(m *v1pb.Mention) {
		if m == nil {
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
