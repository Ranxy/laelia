package store

import (
	"context"
	"log/slog"
	"strings"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// GlobalMentionIndex is a cached, global agent/user directory used as the
// final mention-resolution fallback. It maps both canonical handles and
// unambiguous display names to a mention candidate so @mentions of non-members
// (e.g. an agent listing members of another channel) still render as badges.
type GlobalMentionIndex struct {
	byHandle      map[string]*v1pb.Mention
	byDisplayName map[string]*v1pb.Mention
}

func (i *GlobalMentionIndex) Get(key string) (*v1pb.Mention, bool) {
	if gm, ok := i.byHandle[key]; ok {
		return gm, true
	}
	if gm, ok := i.byDisplayName[key]; ok {
		return gm, true
	}
	return nil, false
}

// GetGlobalMentionIndex returns the cached global agent/user mention index,
// building it on first use. The index is invalidated whenever an agent or user
// is created/updated/deleted, so it stays fresh without a database round-trip
// on every message that mentions a non-member.
func (s *Store) GetGlobalMentionIndex(ctx context.Context) (*GlobalMentionIndex, error) {
	s.globalMentionIndexMu.RLock()
	if s.globalMentionIndex != nil {
		idx := s.globalMentionIndex
		s.globalMentionIndexMu.RUnlock()
		return idx, nil
	}
	s.globalMentionIndexMu.RUnlock()

	s.globalMentionIndexMu.Lock()
	defer s.globalMentionIndexMu.Unlock()
	// Another goroutine may have built it while we waited for the lock.
	if s.globalMentionIndex != nil {
		return s.globalMentionIndex, nil
	}

	agents, err := s.ListAgents(ctx, &FindAgentMessage{})
	if err != nil {
		slog.Warn("failed to list agents for global mention fallback", "error", err)
		return nil, err
	}
	users, err := s.ListUsers(ctx, &FindUserMessage{})
	if err != nil {
		slog.Warn("failed to list users for global mention fallback", "error", err)
		return nil, err
	}
	idx := BuildGlobalMentionIndex(agents, users)
	s.globalMentionIndex = idx
	return idx, nil
}

// InvalidateGlobalMentionIndex drops the cached global agent/user mention
// index. It must be called whenever an agent or user is created, updated, or
// deleted so the next mention parse rebuilds from fresh data.
func (s *Store) InvalidateGlobalMentionIndex() {
	s.globalMentionIndexMu.Lock()
	s.globalMentionIndex = nil
	s.globalMentionIndexMu.Unlock()
}

// BuildGlobalMentionIndex builds the handle/display-name lookup maps from the
// full agent and user directories. Display names are included only when they
// are unambiguous across the whole directory, mirroring the conversation-member
// display-name fallback so a shared name can never misroute a mention.
func BuildGlobalMentionIndex(agents []*AgentMessage, users []*UserMessage) *GlobalMentionIndex {
	idx := &GlobalMentionIndex{
		byHandle:      make(map[string]*v1pb.Mention),
		byDisplayName: make(map[string]*v1pb.Mention),
	}
	counts := make(map[string]int)
	add := func(mType, id, handle, displayName string) {
		if handle == "" {
			return
		}
		name := displayName
		if name == "" {
			name = handle
		}
		hkey := normalizeMentionName(handle)
		if hkey != "" {
			idx.byHandle[hkey] = &v1pb.Mention{Type: mType, Id: id, Name: name}
		}
		dn := normalizeMentionName(displayName)
		if dn == "" {
			return
		}
		counts[dn]++
		idx.byDisplayName[dn] = &v1pb.Mention{Type: mType, Id: id, Name: name}
	}
	for _, a := range agents {
		if a == nil || a.ResourceID == "" || a.Deleted {
			continue
		}
		add("agent", a.ResourceID, a.ResourceID, a.Name)
	}
	for _, u := range users {
		if u == nil || u.Handle == "" || u.MemberDeleted {
			continue
		}
		add("user", u.Handle, u.Handle, u.Name)
	}
	for dn, c := range counts {
		if c > 1 {
			delete(idx.byDisplayName, dn)
		}
	}
	return idx
}

func normalizeMentionName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
