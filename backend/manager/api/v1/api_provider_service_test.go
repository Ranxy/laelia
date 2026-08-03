package v1

import (
	"context"
	"strings"
	"testing"

	"github.com/Ranxy/laelia/backend/agent/pi"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestValidateAndNormalizeAPIProviderMembers verifies member format validation
// and dedup: users, groups (email or id), allUsers are accepted; anything else
// is rejected.
func TestValidateAndNormalizeAPIProviderMembers(t *testing.T) {
	got, err := validateAndNormalizeAPIProviderMembers([]string{
		"users/101",
		"groups/eng@example.com",
		"groups/group-id",
		"allUsers",
		"users/101", // duplicate, dropped
		"  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"users/101", "groups/eng@example.com", "groups/group-id", "allUsers"}
	if len(got) != len(want) {
		t.Fatalf("expected %d members, got %d: %v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("member %d = %q, want %q", i, got[i], w)
		}
	}

	if _, err := validateAndNormalizeAPIProviderMembers([]string{"not-a-member"}); err == nil {
		t.Fatal("expected invalid member to be rejected")
	}
}

// TestValidateAPIProviderUpdateMask verifies the mutable-field whitelist.
func TestValidateAPIProviderUpdateMask(t *testing.T) {
	if err := validateAPIProviderUpdateMask([]string{"title", "entries", "members"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateAPIProviderUpdateMask([]string{"name"}); err == nil {
		t.Fatal("expected immutable field name to be rejected")
	}
	if err := validateAPIProviderUpdateMask(nil); err != nil {
		t.Fatalf("empty mask should pass: %v", err)
	}
}

// TestValidateAgentACPConfigGlobalProvider verifies the global-provider branch
// of builtin-pi config validation: both references required, consistent, and
// the legacy inline branch still enforced.
func TestValidateAgentACPConfigGlobalProvider(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *v1pb.AgentACPConfig
		wantErr bool
	}{
		{
			name: "global mode ok",
			cfg: &v1pb.AgentACPConfig{
				Provider:            pi.BuiltinPiProvider,
				GlobalProvider:      "apiProviders/abc",
				GlobalProviderEntry: "apiProviders/abc/entries/1",
			},
			wantErr: false,
		},
		{
			name: "missing entry",
			cfg: &v1pb.AgentACPConfig{
				Provider:       pi.BuiltinPiProvider,
				GlobalProvider: "apiProviders/abc",
			},
			wantErr: true,
		},
		{
			name: "entry not in provider",
			cfg: &v1pb.AgentACPConfig{
				Provider:            pi.BuiltinPiProvider,
				GlobalProvider:      "apiProviders/abc",
				GlobalProviderEntry: "apiProviders/xyz/entries/1",
			},
			wantErr: true,
		},
		{
			name: "malformed entry name",
			cfg: &v1pb.AgentACPConfig{
				Provider:            pi.BuiltinPiProvider,
				GlobalProvider:      "apiProviders/abc",
				GlobalProviderEntry: "bogus",
			},
			wantErr: true,
		},
		{
			name: "legacy inline ok",
			cfg: &v1pb.AgentACPConfig{
				Provider:    pi.BuiltinPiProvider,
				ApiProvider: pi.APIProviderDeepseek,
				ApiKey:      "sk-test",
				Model:       "deepseek-chat",
			},
			wantErr: false,
		},
		{
			name: "legacy inline missing key",
			cfg: &v1pb.AgentACPConfig{
				Provider:    pi.BuiltinPiProvider,
				ApiProvider: pi.APIProviderDeepseek,
				Model:       "deepseek-chat",
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAgentACPConfig(tc.cfg, nil)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestIsEmptyAgentACPConfig verifies that a config carrying only a global
// provider reference counts as configured (not empty), so CreateAgent stores it.
func TestIsEmptyAgentACPConfig(t *testing.T) {
	empty := &v1pb.AgentACPConfig{}
	if !isEmptyAgentACPConfig(empty) {
		t.Fatal("expected zero-value config to be empty")
	}
	global := &v1pb.AgentACPConfig{
		GlobalProvider: "apiProviders/abc",
	}
	if isEmptyAgentACPConfig(global) {
		t.Fatal("expected a global-provider config to be non-empty")
	}
}

// TestAgentACPConfigGlobalRoundTrip verifies the v1↔store conversion preserves
// the global provider references.
func TestAgentACPConfigGlobalRoundTrip(t *testing.T) {
	in := &v1pb.AgentACPConfig{
		Provider:            pi.BuiltinPiProvider,
		GlobalProvider:      "apiProviders/abc",
		GlobalProviderEntry: "apiProviders/abc/entries/1",
		PersonaPrompt:       "You are helpful.",
	}
	stored := convertToStoreAgentACPConfig(in)
	if stored.GetGlobalProvider() != in.GlobalProvider || stored.GetGlobalProviderEntry() != in.GlobalProviderEntry {
		t.Fatalf("store conversion dropped global refs: %+v", stored)
	}
	out := convertToV1AgentACPConfig(stored)
	if out.GetGlobalProvider() != in.GlobalProvider || out.GetGlobalProviderEntry() != in.GlobalProviderEntry {
		t.Fatalf("v1 conversion dropped global refs: %+v", out)
	}
}

// TestResolveAcpConfigForDaemonPassthrough verifies the resolver leaves
// non-global configs untouched (nil, non-pi, no global reference) without a
// store lookup.
func TestResolveAcpConfigForDaemonPassthrough(t *testing.T) {
	ctx := context.Background()
	var s *store.Store // nil store: only passthrough paths are exercised

	if out, err := resolveAcpConfigForDaemon(ctx, s, nil); err != nil || out != nil {
		t.Fatalf("nil config: got (%v, %v), want (nil, nil)", out, err)
	}
	acp := &v1pb.AgentACPConfig{Provider: "opencode", Model: "m"}
	if out, err := resolveAcpConfigForDaemon(ctx, s, acp); err != nil || out != acp {
		t.Fatalf("non-pi config: got (%v, %v), want passthrough", out, err)
	}
	legacy := &v1pb.AgentACPConfig{
		Provider:    pi.BuiltinPiProvider,
		ApiProvider: pi.APIProviderDeepseek,
		ApiKey:      "sk-test",
		Model:       "deepseek-chat",
	}
	if out, err := resolveAcpConfigForDaemon(ctx, s, legacy); err != nil || out != legacy {
		t.Fatalf("legacy inline config: got (%v, %v), want passthrough", out, err)
	}
	globalNoRef := &v1pb.AgentACPConfig{Provider: pi.BuiltinPiProvider}
	if out, err := resolveAcpConfigForDaemon(ctx, s, globalNoRef); err != nil || out != globalNoRef {
		t.Fatalf("global provider empty: got (%v, %v), want passthrough", out, err)
	}
}

// TestMaskSecretBoundary locks in the masked-secret sentinel semantics for
// short keys and keys that begin with the sentinel prefix (review edge cases).
func TestMaskSecretBoundary(t *testing.T) {
	if got := maskSecret(""); got != "" {
		t.Fatalf("empty secret should stay empty, got %q", got)
	}
	short := maskSecret("ab")
	if !strings.HasPrefix(short, secretMaskPrefix) {
		t.Fatalf("short key should be masked, got %q", short)
	}
	if short != secretMaskPrefix {
		t.Fatalf("a key of length <= 4 masks to exactly the sentinel, got %q", short)
	}
	real := maskSecret("sk-abcdefgh1234")
	if !strings.HasSuffix(real, "1234") || !strings.HasPrefix(real, secretMaskPrefix) {
		t.Fatalf("masked key should retain last 4, got %q", real)
	}
}
