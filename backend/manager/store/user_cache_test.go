package store

import (
	"strconv"
	"testing"

	lru "github.com/hashicorp/golang-lru/v2"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// newTestUserStore builds a Store with only the user caches populated, enough
// to exercise cacheActiveUser/invalidateUserCache without a database.
func newTestUserStore(t *testing.T) *Store {
	t.Helper()
	idCache, err := lru.New[int, *UserMessage](8)
	if err != nil {
		t.Fatalf("new id cache: %v", err)
	}
	emailCache, err := lru.New[string, *UserMessage](8)
	if err != nil {
		t.Fatalf("new email cache: %v", err)
	}
	handleCache, err := lru.New[string, *UserMessage](8)
	if err != nil {
		t.Fatalf("new handle cache: %v", err)
	}
	return &Store{
		enableCache:     true,
		userIDCache:     idCache,
		userEmailCache:  emailCache,
		userHandleCache: handleCache,
	}
}

func activeUser(id int, email string) *UserMessage {
	return &UserMessage{ID: id, Email: email, Name: email, Handle: "ran-user-" + strconv.Itoa(id), Type: models.PrincipalType_END_USER}
}

func deletedUser(id int, email string) *UserMessage {
	u := activeUser(id, email)
	u.MemberDeleted = true
	return u
}

// TestCacheActiveUser_ExcludesSoftDeleted locks in that the LRU never holds a
// soft-deleted user: a deleted user is not cached, while an active one is, and
// invalidation evicts by id and email.
func TestCacheActiveUser_ExcludesSoftDeleted(t *testing.T) {
	s := newTestUserStore(t)

	active := activeUser(1, "a@example.com")
	deleted := deletedUser(2, "b@example.com")

	s.cacheActiveUser(active)
	s.cacheActiveUser(deleted)

	if got, ok := s.userIDCache.Get(active.ID); !ok || got != active {
		t.Fatal("active user must be cached by id")
	}
	if got, ok := s.userEmailCache.Get(active.Email); !ok || got != active {
		t.Fatal("active user must be cached by email")
	}
	if got, ok := s.userHandleCache.Get(active.Handle); !ok || got != active {
		t.Fatal("active user must be cached by handle")
	}
	if _, ok := s.userIDCache.Get(deleted.ID); ok {
		t.Fatal("soft-deleted user must not be cached by id")
	}
	if _, ok := s.userEmailCache.Get(deleted.Email); ok {
		t.Fatal("soft-deleted user must not be cached by email")
	}
	if _, ok := s.userHandleCache.Get(deleted.Handle); ok {
		t.Fatal("soft-deleted user must not be cached by handle")
	}

	// Invalidate removes the active user from all caches.
	s.invalidateUserCache(active.ID, active.Email)
	if _, ok := s.userIDCache.Get(active.ID); ok {
		t.Fatal("invalidate must evict by id")
	}
	if _, ok := s.userEmailCache.Get(active.Email); ok {
		t.Fatal("invalidate must evict by email")
	}
	if _, ok := s.userHandleCache.Get(active.Handle); ok {
		t.Fatal("invalidate must evict by handle")
	}
}

// TestCacheActiveUser_NilNoop ensures a nil result (e.g. a point query that
// found no row) does not panic or pollute the cache.
func TestCacheActiveUser_NilNoop(t *testing.T) {
	s := newTestUserStore(t)
	s.cacheActiveUser(nil)
	if s.userIDCache.Len() != 0 || s.userEmailCache.Len() != 0 {
		t.Fatal("caching nil must leave the caches empty")
	}
}
