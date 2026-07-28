package pi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	pkgerrors "github.com/pkg/errors"
)

// Model is one model id exposed by an LLM API provider's model-listing API.
// The manager RPC converts this to v1pb.PiModel.
type Model struct {
	ID   string
	Name string
}

// providerModelsURL is the model-listing endpoint each API provider exposes.
// DeepSeek requires a Bearer api_key; OpenRouter's endpoint is public.
var providerModelsURL = map[string]string{
	APIProviderDeepseek:   "https://api.deepseek.com/models",
	APIProviderOpenRouter: "https://api.openrouter.ai/api/v1/models",
}

// providerModelsNeedsKey reports whether the provider's model-listing endpoint
// requires the caller's api_key (DeepSeek does; OpenRouter is public).
func providerModelsNeedsKey(id string) bool {
	return id == APIProviderDeepseek
}

// modelsHTTP is overridable in tests via httptest.NewServer. Nil means use the
// default http.Client with a bounded timeout.
var modelsHTTP = (*http.Client)(nil)

func modelsClient() *http.Client {
	if modelsHTTP != nil {
		return modelsHTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// providerModelsResponse is the OpenAI-style envelope both providers return:
// `{"data":[{"id":...,"name":...}]}` (name optional).
type providerModelsResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"data"`
}

// ListModels fetches the model list for the given API provider. The manager
// proxies this server-side (CORS + key hygiene — the api_key never reaches the
// browser's third-party call). Results are cached for a short TTL keyed by
// provider + api_key hash (the key itself is never stored).
func ListModels(ctx context.Context, apiProvider, apiKey string) ([]Model, error) {
	if _, ok := providerModelsURL[apiProvider]; !ok {
		return nil, pkgerrors.Errorf("unsupported api_provider %q", apiProvider)
	}
	if providerModelsNeedsKey(apiProvider) && strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("api_key is required to list models for this provider")
	}

	if cached, ok := modelsCache.get(apiProvider, apiKey); ok {
		return cached, nil
	}

	models, err := fetchModels(ctx, apiProvider, apiKey)
	if err != nil {
		return nil, err
	}
	modelsCache.set(apiProvider, apiKey, models)
	return models, nil
}

func fetchModels(ctx context.Context, apiProvider, apiKey string) ([]Model, error) {
	url := providerModelsURL[apiProvider]
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to build models request")
	}
	if providerModelsNeedsKey(apiProvider) {
		// api_key is a bearer credential — never log it.
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := modelsClient().Do(req)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to call provider models API")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "failed to read provider models response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, pkgerrors.Errorf("provider %q returned status %d", apiProvider, resp.StatusCode)
	}

	var parsed providerModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, pkgerrors.Wrap(err, "failed to decode provider models response")
	}

	seen := make(map[string]struct{}, len(parsed.Data))
	models := make([]Model, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = id // DeepSeek omits a display name; echo the id.
		}
		models = append(models, Model{ID: id, Name: name})
	}
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return models, nil
}

// --- in-memory TTL cache (mirrors component/state/nonce.go's map+mutex+expiry) ---

const modelsCacheTTL = 5 * time.Minute

type modelsCacheEntry struct {
	models    []Model
	expiresAt time.Time
}

var modelsCache = &modelsCacheStore{entries: make(map[string]modelsCacheEntry)}

type modelsCacheStore struct {
	mu      sync.Mutex
	entries map[string]modelsCacheEntry
}

// cacheKey is provider + a truncated hash of the api_key (the key itself is never
// stored). OpenRouter's endpoint is public, so an empty key hashes to a stable
// per-provider key.
func cacheKey(apiProvider, apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return apiProvider + "/" + hex.EncodeToString(sum[:8])
}

func (s *modelsCacheStore) get(apiProvider, apiKey string) ([]Model, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[cacheKey(apiProvider, apiKey)]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.models, true
}

func (s *modelsCacheStore) set(apiProvider, apiKey string, models []Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[cacheKey(apiProvider, apiKey)] = modelsCacheEntry{
		models:    models,
		expiresAt: time.Now().Add(modelsCacheTTL),
	}
}
