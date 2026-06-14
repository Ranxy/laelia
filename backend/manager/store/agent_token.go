package store

import (
	"context"
	"database/sql"
	"time"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

type AgentTokenMessage struct {
	ID          int
	AgentID     int
	TokenHash   string
	TokenType   storepb.AgentTokenType
	TokenFamily string
	State       storepb.AgentTokenState
	Fingerprint string
	SourceIP    string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	RevokedAt   *time.Time
	LastUsedAt  *time.Time
	CreatedBy   string
}

func (s *Store) CreateAgentToken(ctx context.Context, token *AgentTokenMessage) error {
	tokenType := tokenTokenTypeToString(token.TokenType)
	state := tokenStateToString(token.State)

	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO agent_token (
			agent_id, token_hash, token_type, token_family, state,
			fingerprint, source_ip, expires_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, token.AgentID, token.TokenHash, tokenType, token.TokenFamily, state,
		token.Fingerprint, token.SourceIP, token.ExpiresAt, token.CreatedBy)
	return err
}

func (s *Store) GetAgentTokenByHash(ctx context.Context, tokenHash string) (*AgentTokenMessage, error) {
	query := `SELECT
		id, agent_id, token_hash, token_type, token_family, state,
		fingerprint, source_ip, issued_at, expires_at,
		consumed_at, revoked_at, last_used_at, created_by
	FROM agent_token WHERE token_hash = $1`

	var token AgentTokenMessage
	var tokenType, state string
	var consumedAt, revokedAt, lastUsedAt sql.NullTime
	err := s.GetDB().QueryRowContext(ctx, query, tokenHash).Scan(
		&token.ID, &token.AgentID, &token.TokenHash, &tokenType, &token.TokenFamily, &state,
		&token.Fingerprint, &token.SourceIP, &token.IssuedAt, &token.ExpiresAt,
		&consumedAt, &revokedAt, &lastUsedAt, &token.CreatedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	token.TokenType = stringToAgentTokenType(tokenType)
	token.State = stringToAgentTokenState(state)
	if consumedAt.Valid {
		token.ConsumedAt = &consumedAt.Time
	}
	if revokedAt.Valid {
		token.RevokedAt = &revokedAt.Time
	}
	if lastUsedAt.Valid {
		token.LastUsedAt = &lastUsedAt.Time
	}
	return &token, nil
}

func (s *Store) UpdateAgentTokenState(ctx context.Context, tokenID int, state storepb.AgentTokenState, consumedAt *time.Time) error {
	stateStr := tokenStateToString(state)
	if state == storepb.AgentTokenState_CONSUMED && consumedAt != nil {
		_, err := s.GetDB().ExecContext(ctx, `
			UPDATE agent_token SET state = $2, consumed_at = $3 WHERE id = $1
		`, tokenID, stateStr, consumedAt)
		return err
	}
	if state == storepb.AgentTokenState_REVOKED {
		_, err := s.GetDB().ExecContext(ctx, `
			UPDATE agent_token SET state = $2, revoked_at = now() WHERE id = $1
		`, tokenID, stateStr)
		return err
	}
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_token SET state = $2 WHERE id = $1
	`, tokenID, stateStr)
	return err
}

func (s *Store) RevokeAllAgentTokens(ctx context.Context, agentID int) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_token SET state = 'REVOKED', revoked_at = now()
		WHERE agent_id = $1 AND state IN ('ACTIVE', 'CONSUMED')
	`, agentID)
	return err
}

func (s *Store) RevokeTokenFamily(ctx context.Context, tokenFamily string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE agent_token SET state = 'REVOKED', revoked_at = now()
		WHERE token_family = $1 AND state IN ('ACTIVE', 'CONSUMED')
	`, tokenFamily)
	return err
}

func tokenTokenTypeToString(t storepb.AgentTokenType) string {
	switch t {
	case storepb.AgentTokenType_BOOTSTRAP:
		return "BOOTSTRAP"
	case storepb.AgentTokenType_REFRESH:
		return "REFRESH"
	case storepb.AgentTokenType_ACCESS:
		return "ACCESS"
	default:
		return "UNKNOWN"
	}
}

func stringToAgentTokenType(s string) storepb.AgentTokenType {
	switch s {
	case "BOOTSTRAP":
		return storepb.AgentTokenType_BOOTSTRAP
	case "REFRESH":
		return storepb.AgentTokenType_REFRESH
	case "ACCESS":
		return storepb.AgentTokenType_ACCESS
	default:
		return storepb.AgentTokenType_TOKEN_TYPE_UNSPECIFIED
	}
}

func tokenStateToString(s storepb.AgentTokenState) string {
	switch s {
	case storepb.AgentTokenState_ACTIVE:
		return "ACTIVE"
	case storepb.AgentTokenState_CONSUMED:
		return "CONSUMED"
	case storepb.AgentTokenState_REVOKED:
		return "REVOKED"
	default:
		return "UNKNOWN"
	}
}

func stringToAgentTokenState(s string) storepb.AgentTokenState {
	switch s {
	case "ACTIVE":
		return storepb.AgentTokenState_ACTIVE
	case "CONSUMED":
		return storepb.AgentTokenState_CONSUMED
	case "REVOKED":
		return storepb.AgentTokenState_REVOKED
	default:
		return storepb.AgentTokenState_STATE_UNSPECIFIED
	}
}
