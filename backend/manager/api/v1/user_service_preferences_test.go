package v1

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/stretchr/testify/assert"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestValidatePreferredLanguage guards the UpdateUser chat_preferences write:
// only the known enum values may be persisted to the jsonb column; any other
// raw int (proto3 enums arrive unvalidated) must be rejected.
func TestValidatePreferredLanguage(t *testing.T) {
	for _, lang := range []v1pb.PreferredLanguage{
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED,
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_ZH_CN,
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_EN_US,
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_JA_JP,
	} {
		assert.NoError(t, validatePreferredLanguage(lang), "lang %d", lang)
	}
	for _, lang := range []v1pb.PreferredLanguage{-1, 4, 99} {
		got := validatePreferredLanguage(lang)
		assert.Error(t, got, "lang %d", lang)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(got))
	}
}
