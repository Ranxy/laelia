package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestRefreshReuseAction guards the T12 reuse-detection matrix. A refresh
// token is single-use: ACTIVE proceeds, and any non-fresh presentation
// (CONSUMED or REVOKED) is treated as reuse and must revoke the whole family
// rather than silently issuing a new token (the previous behavior).
func TestRefreshReuseAction(t *testing.T) {
	cases := []struct {
		name  string
		state storepb.AgentTokenState
		want  refreshAction
	}{
		{"active proceeds", storepb.AgentTokenState_ACTIVE, refreshActionProceed},
		{"consumed is reuse", storepb.AgentTokenState_CONSUMED, refreshActionRevokeFamily},
		{"revoked is reuse", storepb.AgentTokenState_REVOKED, refreshActionRevokeFamily},
		{"unknown is invalid", storepb.AgentTokenState(999), refreshActionInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, refreshReuseAction(tc.state))
		})
	}
}
