package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/common"
)

// TestParseAgentToken_VerifiesSignature is the core T12 guard: RefreshAgentToken
// trusts claims only after ParseAgentToken verifies the HS256 signature. A
// token whose payload is tampered (here: re-signed with a different secret, or
// with a forged token_version under the wrong key) must be rejected — otherwise
// a refresh token could "upgrade" itself to the current token_version purely
// via a hash lookup.
func TestParseAgentToken_VerifiesSignature(t *testing.T) {
	const secret = "test-secret"
	const agentName = "agent-1"
	const resourceID = "agents/agent-1"
	const tokenVersion = 3

	tok, err := GenerateAgentTokenWithSession(agentName, resourceID, tokenVersion, TokenTypeRefresh, "sess-1", common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)

	claims, err := ParseAgentToken(tok, secret)
	require.NoError(t, err)
	assert.Equal(t, TokenTypeRefresh, claims.TokenType)
	assert.Equal(t, tokenVersion, claims.TokenVersion)
	assert.Equal(t, resourceID, claims.Subject)
	assert.Equal(t, "sess-1", claims.SessionID)
}

func TestParseAgentToken_RejectsWrongSecret(t *testing.T) {
	tok, err := GenerateAgentTokenWithSession("agent-1", "agents/agent-1", 3, TokenTypeRefresh, "", common.ReleaseModeDev, "real-secret", time.Hour)
	require.NoError(t, err)

	_, err = ParseAgentToken(tok, "different-secret")
	assert.Error(t, err, "token signed with a different secret must not verify")
}

func TestParseAgentToken_RejectsTampered(t *testing.T) {
	tok, err := GenerateAgentTokenWithSession("agent-1", "agents/agent-1", 3, TokenTypeRefresh, "", common.ReleaseModeDev, "secret", time.Hour)
	require.NoError(t, err)

	// Tamper the payload's first character (always 'e' — the base64 of the
	// leading '{' of the claims JSON — so the decoded payload bytes change and
	// the signature no longer matches). Flipping the signature's last character
	// instead is unreliable: for a 32-byte HS256 signature (length %3 == 2) the
	// final base64url character's low two bits are padding, so swapping between
	// e.g. 'A' and 'B' decodes to the same bytes and the "tampered" token still
	// verifies.
	tampered := "f" + tok[1:]
	_, err = ParseAgentToken(tampered, "secret")
	assert.Error(t, err, "a token whose payload no longer matches its signature must not verify")
}
