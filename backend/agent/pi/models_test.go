package pi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// redirectModelsURL points the two known providers at the httptest server for
// the duration of t, so tests never touch the live provider endpoints.
func redirectModelsURL(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prevDeepseek := providerModelsURL[APIProviderDeepseek]
	prevOpenRouter := providerModelsURL[APIProviderOpenRouter]
	providerModelsURL[APIProviderDeepseek] = srv.URL + "/models"
	providerModelsURL[APIProviderOpenRouter] = srv.URL + "/models"
	t.Cleanup(func() {
		providerModelsURL[APIProviderDeepseek] = prevDeepseek
		providerModelsURL[APIProviderOpenRouter] = prevOpenRouter
	})
}

func modelsHandler(t *testing.T, requireBearer string, status int, payload string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if requireBearer != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+requireBearer {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, payload)
	})
}

func TestListModelsDeepseekDecodesAndAuths(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "sk-ds", 200, `{"data":[{"id":"deepseek-chat"},{"id":"deepseek-reasoner"}]}`))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry) // ensure cold cache

	got, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "sk-ds")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 || got[0].ID != "deepseek-chat" || got[1].ID != "deepseek-reasoner" {
		t.Fatalf("unexpected models: %+v", got)
	}
	// DeepSeek omits a display name; the id echoes as the name.
	if got[0].Name != "deepseek-chat" {
		t.Fatalf("expected name to echo id, got %q", got[0].Name)
	}
}

func TestListModelsDeepseekRequiresKey(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "sk-ds", 200, `{}`))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry)

	if _, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, ""); err == nil {
		t.Fatal("expected error for empty api_key")
	}
}

func TestListModelsDeepseekRejectsBadKey(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "sk-ds", 200, `{}`))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry)

	if _, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "wrong"); err == nil {
		t.Fatal("expected error for wrong api_key (server returns 401)")
	}
}

func TestListModelsOpenRouterPublicNoAuth(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "", 200, `{"data":[{"id":"anthropic/claude-3.5-sonnet","name":"Claude 3.5 Sonnet"},{"id":"google/gemini-2.5-flash","name":"Gemini 2.5 Flash"}]}`))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry)

	got, err := ListModels(context.Background(), srv.Client(), APIProviderOpenRouter, "")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected models: %+v", got)
	}
	// Sorted by id.
	if got[0].ID != "anthropic/claude-3.5-sonnet" || got[0].Name != "Claude 3.5 Sonnet" {
		t.Fatalf("unexpected first model: %+v", got[0])
	}
	if got[1].ID != "google/gemini-2.5-flash" {
		t.Fatalf("unexpected second model: %+v", got[1])
	}
}

func TestListModelsUnknownProvider(t *testing.T) {
	if _, err := ListModels(context.Background(), nil, "bogus", "k"); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestListModelsNon2xx(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "sk-ds", 500, `{}`))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry)

	_, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "sk-ds")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 in error, got %v", err)
	}
}

func TestListModelsDedupAndSort(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "sk-ds", 200, `{"data":[{"id":"b"},{"id":"a"},{"id":"a"},{"id":""}]}`))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry)

	got, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "sk-ds")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("expected deduped+sorted [a b], got %+v", got)
	}
}

func TestListModelsCacheHitThenExpiry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"deepseek-chat"}]}`)
	}))
	defer srv.Close()
	redirectModelsURL(t, srv)
	modelsCache.entries = make(map[string]modelsCacheEntry)

	// First call hits the server.
	got, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "sk-ds")
	if err != nil || len(got) != 1 {
		t.Fatalf("first call: got=%+v err=%v", got, err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}
	// Second call is served from cache — no new upstream call.
	if _, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "sk-ds"); err != nil {
		t.Fatalf("cached call: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected cache hit (still 1 call), got %d", calls)
	}
	// Expire the entry; the next call re-fetches.
	key := cacheKey(APIProviderDeepseek, "sk-ds")
	modelsCache.mu.Lock()
	e := modelsCache.entries[key]
	e.expiresAt = time.Now().Add(-time.Second)
	modelsCache.entries[key] = e
	modelsCache.mu.Unlock()
	if _, err := ListModels(context.Background(), srv.Client(), APIProviderDeepseek, "sk-ds"); err != nil {
		t.Fatalf("post-expiry call: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected re-fetch after expiry (2 calls), got %d", calls)
	}
}

func TestListModelsCacheKeyDoesNotStoreRawKey(t *testing.T) {
	k := cacheKey(APIProviderDeepseek, "sk-secret")
	if strings.Contains(k, "sk-secret") {
		t.Fatalf("cache key must not contain the raw key: %q", k)
	}
	// Sanity: the JSON envelope round-trips.
	if err := json.Unmarshal([]byte(`{"data":[{"id":"x"}]}`), &providerModelsResponse{}); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestModelsURL(t *testing.T) {
	if got := modelsURL("https://example.com/v1"); got != "https://example.com/v1/models" {
		t.Fatalf("unexpected modelsURL: %q", got)
	}
	if got := modelsURL("https://example.com/v1/"); got != "https://example.com/v1/models" {
		t.Fatalf("unexpected modelsURL with trailing slash: %q", got)
	}
}

func TestListCustomModelsHitsModelsPath(t *testing.T) {
	srv := httptest.NewServer(modelsHandler(t, "sk-custom", 200, `{"data":[{"id":"my-model"}]}`))
	defer srv.Close()
	modelsCache.entries = make(map[string]modelsCacheEntry) // ensure cold cache

	// The user supplies the base URL without /models; the client must append it.
	got, err := ListCustomModels(context.Background(), srv.Client(), srv.URL, "sk-custom")
	if err != nil {
		t.Fatalf("ListCustomModels: %v", err)
	}
	if len(got) != 1 || got[0].ID != "my-model" {
		t.Fatalf("unexpected models: %+v", got)
	}
}
