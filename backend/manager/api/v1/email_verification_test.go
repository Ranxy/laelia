package v1

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestEmailVerificationToken guards the token/hash contract: the token is
// unique, URL-safe, and the hash round-trips for lookup.
func TestEmailVerificationToken(t *testing.T) {
	token1, hash1, err := newEmailVerificationToken()
	require.NoError(t, err)
	token2, _, err := newEmailVerificationToken()
	require.NoError(t, err)

	assert.NotEqual(t, token1, token2)
	assert.NotEmpty(t, token1)
	assert.Equal(t, hash1, hashEmailVerificationToken(token1))
	assert.Equal(t, 64, len(hash1))
	assert.False(t, strings.ContainsAny(token1, "/+="), "token must be URL-safe")
}

// TestValidateSMTPSetting guards the SMTP config contract: host and from are
// required, port must be in range.
func TestValidateSMTPSetting(t *testing.T) {
	assert.NoError(t, validateSMTPSetting(&models.SMTPSetting{Host: "smtp.example.com", From: "no-reply@example.com", Port: 587}))
	assert.Error(t, validateSMTPSetting(&models.SMTPSetting{From: "no-reply@example.com"}))
	assert.Error(t, validateSMTPSetting(&models.SMTPSetting{Host: "smtp.example.com"}))
	assert.Error(t, validateSMTPSetting(&models.SMTPSetting{Host: "smtp.example.com", From: "no-reply@example.com", Port: 70000}))
}
