package store

import (
	"strings"
	"testing"
)

// TestAddReactionSQL locks in that adding a reaction is idempotent at the SQL
// level: a re-add by the same actor is a no-op, so concurrent or repeated
// calls never produce duplicates. The partial unique indexes
// uq_message_reaction_user/agent (one row per message, actor, emoji) plus
// ON CONFLICT DO NOTHING is what guarantees this race-free without a
// round-trip. A DB-backed concurrency test is T27's remit.
func TestAddReactionSQL(t *testing.T) {
	if !strings.Contains(addReactionSQL, "ON CONFLICT") {
		t.Fatal("AddReaction must use ON CONFLICT to be idempotent against the reaction unique indexes")
	}
	if !strings.Contains(addReactionSQL, "DO NOTHING") {
		t.Fatal("conflict must DO NOTHING so a re-add by the same actor is a no-op")
	}
}

// TestRemoveReactionCallerScopedSQL locks in the "only the reactor removes its
// own reaction" rule at the SQL level: the DELETE is scoped to exactly the
// caller's identity (both nullable actor columns via IS NOT DISTINCT FROM) so
// it can never delete another actor's reaction. A caller who removes an emoji
// they did not place leaves the row untouched; the API layer turns that into
// PERMISSION_DENIED using the Others flag.
func TestRemoveReactionCallerScopedSQL(t *testing.T) {
	if !strings.Contains(removeReactionSQL, "principal_id IS NOT DISTINCT FROM") {
		t.Fatal("RemoveReaction DELETE must scope to the caller's principal id")
	}
	if !strings.Contains(removeReactionSQL, "agent_id IS NOT DISTINCT FROM") {
		t.Fatal("RemoveReaction DELETE must scope to the caller's agent id")
	}
	if !strings.Contains(removeReactionSQL, "emoji = $2") {
		t.Fatal("RemoveReaction DELETE must be scoped to the target emoji")
	}
}

// TestAggregateReactionsSQL locks in the aggregation shape: reactors are the
// display names joined from principal (users) and agent (agents), the count is
// the number of distinct reactors, and the `reacted` flag is caller-relative.
// The 1:1 joins hold because the partial unique indexes guarantee one row per
// (message, actor, emoji).
func TestAggregateReactionsSQL(t *testing.T) {
	if !strings.Contains(aggregateReactionsSQL, "LEFT JOIN principal") {
		t.Fatal("aggregation must resolve user reactor display names")
	}
	if !strings.Contains(aggregateReactionsSQL, "LEFT JOIN agent") {
		t.Fatal("aggregation must resolve agent reactor display names")
	}
	if !strings.Contains(aggregateReactionsSQL, "GROUP BY r.message_id, r.emoji") {
		t.Fatal("aggregation must group per (message, emoji)")
	}
	if !strings.Contains(aggregateReactionsSQL, "bool_or") {
		t.Fatal("aggregation must compute the caller-relative `reacted` flag")
	}
}
