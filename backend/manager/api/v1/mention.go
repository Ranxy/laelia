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
// Resolution is two-pass:
//  1. Primary: exact, case-insensitive match on the member's mention handle
//     (the member id). Handles are unique per type, so this is unambiguous.
//  2. Fallback: case-insensitive match on the member's display name, used only
//     when the handle did not match and only when the display name is
//     unambiguous (exactly one member carries it). This keeps `@<display name>`
//     working — the natural form agents used before the handle migration and
//     still reach for when the roster leads with a display name. Ambiguous
//     display names (two members sharing one) are never resolved by name, so a
//     display name can never misroute.
//
// The fallback index is built lazily — only when at least one token misses the
// handle index — so the common case (the agent typed handles) pays no display
// name resolution cost.
//
// The Mention.Name is the literal token the author typed after `@` (not the
// canonical handle), so the frontend can always match the exact text in the
// message body regardless of letter case or whether a handle or display name
// was used. The canonical id (Mention.Id) still carries the handle for click
// dispatch and detail-sheet lookup. The posting agent and the sending user
// are NOT excluded here — the routing layer already skips the poster, and
// activity generation skips self-mentions, so keeping them lets the frontend
// render @self as a badge.
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

	var mentions []*v1pb.Mention
	seen := make(map[string]bool)
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
				// Unknown handle and no display-name match: skip.
				continue
			}
		}
		// Dedup by type:id + the literal token, NOT just type:id: the same
		// member may be @mentioned twice with different text (once by handle
		// "@jane-agent-1" and once by display name "@jane"). Each distinct
		// text token needs its own Mention so the frontend can match every
		// occurrence in the content. Repeated identical tokens ("@x @x") still
		// dedup — the frontend renders all occurrences from a single entry.
		// Duplicate member entries are harmless downstream: thread subscription
		// (addAgent/addUser already dedup by id) and activity generation
		// (mentionCats[id] |= is idempotent) both absorb them.
		dedup := mentionTypeString(m.MemberType) + ":" + m.MemberID + ":" + token
		if seen[dedup] {
			continue
		}
		seen[dedup] = true
		mentions = append(mentions, &v1pb.Mention{
			Type: mentionTypeString(m.MemberType),
			Id:   m.MemberID,
			// Name is the literal text the author typed after @, so the
			// frontend can match it verbatim in the content (handles are
			// lowercased; the author may have typed different case or even a
			// display name). The canonical handle is in Id.
			Name: token,
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
// client-supplied mentions (e.g. from a mention picker), deduping by type:id
// and preserving the first-seen name. Self-mentions are NOT dropped here — they
// are kept so the frontend can render @self as a badge; activity generation
// skips the sender so a user never gets a MENTION activity for @mentioning
// themself. The client set is appended last so its names win on dedup collision
// only when the server did not resolve the same member — in practice the server
// resolves handles itself, so the two sets agree.
func mergeMentions(parsed, client []*v1pb.Mention) []*v1pb.Mention {
	seen := make(map[string]bool, len(parsed)+len(client))
	out := make([]*v1pb.Mention, 0, len(parsed)+len(client))
	add := func(m *v1pb.Mention) {
		if m == nil {
			return
		}
		// Dedup by type:id + Name (the literal token), not just type:id:
		// the same member may be @mentioned with different text (handle and
		// display name), and each distinct token needs its own entry for the
		// frontend to match every occurrence in the content.
		key := m.Type + ":" + m.Id + ":" + m.Name
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
