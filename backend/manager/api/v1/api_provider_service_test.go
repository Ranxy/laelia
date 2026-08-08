package v1

import (
	"context"
	"strings"
	"testing"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/pi"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestValidateAndNormalizeAPIProviderMembers verifies member format validation
// and dedup: users, groups (email or id), allUsers are accepted; anything else
// is rejected.
func TestValidateAndNormalizeAPIProviderMembers(t *testing.T) {
	got, err := validateAndNormalizeMembers([]string{
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

	if _, err := validateAndNormalizeMembers([]string{"not-a-member"}); err == nil {
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
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestValidateAgentACPConfigProtocol verifies the protocol field: value
// whitelist, custom providers may declare either generation, and a built-in
// provider may only declare acp-v2 when it actually speaks the thread protocol.
func TestValidateAgentACPConfigProtocol(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *v1pb.AgentACPConfig
		wantErr bool
	}{
		{
			name: "custom v2 ok",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
				Protocol:   executor.ProtocolV2,
			},
		},
		{
			name: "custom v1 ok",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
				Protocol:   executor.ProtocolV1,
			},
		},
		{
			name: "custom empty protocol ok",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
			},
		},
		{
			name: "custom v2 missing executable",
			cfg: &v1pb.AgentACPConfig{
				Provider: "custom",
				Protocol: executor.ProtocolV2,
			},
			wantErr: true,
		},
		{
			name: "unknown protocol value",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
				Protocol:   "acp-v3",
			},
			wantErr: true,
		},
		{
			name: "builtin thread provider v2 ok",
			cfg: &v1pb.AgentACPConfig{
				Provider: "codex",
				Protocol: executor.ProtocolV2,
			},
		},
		{
			name: "builtin non-thread v2 rejected",
			cfg: &v1pb.AgentACPConfig{
				Provider: "opencode",
				Protocol: executor.ProtocolV2,
			},
			wantErr: true,
		},
		{
			name: "builtin v1 rejected for thread provider",
			cfg: &v1pb.AgentACPConfig{
				Provider: "codex",
				Protocol: executor.ProtocolV1,
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAgentACPConfig(tc.cfg, nil)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
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
	protocolOnly := &v1pb.AgentACPConfig{Protocol: executor.ProtocolV2}
	if isEmptyAgentACPConfig(protocolOnly) {
		t.Fatal("expected a protocol-only config to count as provided (it fails validation, not silently defaulted)")
	}
}

// TestAgentACPConfigProtocolRoundTrip verifies the v1↔store conversion
// preserves the protocol declaration.
func TestAgentACPConfigProtocolRoundTrip(t *testing.T) {
	in := &v1pb.AgentACPConfig{
		Provider:   "custom",
		Executable: "my-agent",
		Protocol:   executor.ProtocolV2,
	}
	stored := convertToStoreAgentACPConfig(in)
	if stored.GetProtocol() != in.Protocol {
		t.Fatalf("store conversion dropped protocol: %+v", stored)
	}
	out := convertToV1AgentACPConfig(stored)
	if out.GetProtocol() != in.Protocol {
		t.Fatalf("v1 conversion dropped protocol: %+v", out)
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
	masked := maskSecret("sk-abcdefgh1234")
	if !strings.HasSuffix(masked, "1234") || !strings.HasPrefix(masked, secretMaskPrefix) {
		t.Fatalf("masked key should retain last 4, got %q", masked)
	}
}

// TestMaskKeyPreview locks in the owner-facing inline key preview: it shows a
// fragment (first 5 + last 3) but not the full key, and is prefixed with the
// "****" sentinel so the update handler treats a save echoing it back as "keep
// existing" rather than storing the masked string as the real key.
func TestMaskKeyPreview(t *testing.T) {
	if got := maskKeyPreview(""); got != secretMaskPrefix {
		t.Fatalf("empty key should mask to the bare sentinel, got %q", got)
	}
	if got := maskKeyPreview("sk-abcdefgh1234"); got != "****sk-ab***234" {
		t.Fatalf("unexpected preview %q, want %q", got, "****sk-ab***234")
	}
	// A preview always carries the sentinel prefix so it round-trips as "keep".
	preview := maskKeyPreview("sk-abcdefgh1234")
	if !strings.HasPrefix(preview, secretMaskPrefix) {
		t.Fatalf("preview must start with the keep-existing sentinel, got %q", preview)
	}
	if strings.Contains(preview, "sk-abcdefgh1234") {
		t.Fatalf("preview must not leak the full key, got %q", preview)
	}
}
