package store

import (
	"context"
	"database/sql"
	"time"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

// MachineTokenMessage is the storage-layer representation of a machine token
// (registration/access/refresh). Mirrors AgentTokenMessage.
type MachineTokenMessage struct {
	ID          int
	MachineID   int
	TokenHash   string
	TokenType   storepb.MachineTokenType
	TokenFamily string
	State       storepb.MachineTokenState
	Fingerprint string
	SourceIP    string
	IssuedAt    time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	RevokedAt   *time.Time
	LastUsedAt  *time.Time
	CreatedBy   string
}

func (s *Store) CreateMachineToken(ctx context.Context, token *MachineTokenMessage) error {
	tokenType := machineTokenTypeToString(token.TokenType)
	state := machineTokenStateToString(token.State)

	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO machine_token (
			machine_id, token_hash, token_type, token_family, state,
			fingerprint, source_ip, expires_at, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, token.MachineID, token.TokenHash, tokenType, token.TokenFamily, state,
		token.Fingerprint, token.SourceIP, token.ExpiresAt, token.CreatedBy)
	return err
}

func (s *Store) GetMachineTokenByHash(ctx context.Context, tokenHash string) (*MachineTokenMessage, error) {
	query := `SELECT
			id, machine_id, token_hash, token_type, token_family, state,
			fingerprint, source_ip, issued_at, expires_at,
			consumed_at, revoked_at, last_used_at, created_by
		FROM machine_token WHERE token_hash = $1`

	var token MachineTokenMessage
	var tokenType, state string
	var consumedAt, revokedAt, lastUsedAt sql.NullTime
	err := s.GetDB().QueryRowContext(ctx, query, tokenHash).Scan(
		&token.ID, &token.MachineID, &token.TokenHash, &tokenType, &token.TokenFamily, &state,
		&token.Fingerprint, &token.SourceIP, &token.IssuedAt, &token.ExpiresAt,
		&consumedAt, &revokedAt, &lastUsedAt, &token.CreatedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	token.TokenType = stringToMachineTokenType(tokenType)
	token.State = stringToMachineTokenState(state)
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

func (s *Store) UpdateMachineTokenState(ctx context.Context, tokenID int, state storepb.MachineTokenState, consumedAt *time.Time) error {
	stateStr := machineTokenStateToString(state)
	if state == storepb.MachineTokenState_MACHINE_TOKEN_CONSUMED && consumedAt != nil {
		_, err := s.GetDB().ExecContext(ctx, `
			UPDATE machine_token SET state = $2, consumed_at = $3 WHERE id = $1
		`, tokenID, stateStr, consumedAt)
		return err
	}
	if state == storepb.MachineTokenState_MACHINE_TOKEN_REVOKED {
		_, err := s.GetDB().ExecContext(ctx, `
			UPDATE machine_token SET state = $2, revoked_at = now() WHERE id = $1
		`, tokenID, stateStr)
		return err
	}
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine_token SET state = $2 WHERE id = $1
	`, tokenID, stateStr)
	return err
}

func (s *Store) RevokeAllMachineTokens(ctx context.Context, machineID int) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine_token SET state = 'REVOKED', revoked_at = now()
		WHERE machine_id = $1 AND state IN ('ACTIVE', 'CONSUMED')
	`, machineID)
	return err
}

func (s *Store) RevokeMachineTokenFamily(ctx context.Context, tokenFamily string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine_token SET state = 'REVOKED', revoked_at = now()
		WHERE token_family = $1 AND state IN ('ACTIVE', 'CONSUMED')
	`, tokenFamily)
	return err
}

func machineTokenTypeToString(t storepb.MachineTokenType) string {
	switch t {
	case storepb.MachineTokenType_MACHINE_BOOTSTRAP:
		return "BOOTSTRAP"
	case storepb.MachineTokenType_MACHINE_REFRESH:
		return "REFRESH"
	case storepb.MachineTokenType_MACHINE_ACCESS:
		return "ACCESS"
	default:
		return "UNKNOWN"
	}
}

func stringToMachineTokenType(s string) storepb.MachineTokenType {
	switch s {
	case "BOOTSTRAP":
		return storepb.MachineTokenType_MACHINE_BOOTSTRAP
	case "REFRESH":
		return storepb.MachineTokenType_MACHINE_REFRESH
	case "ACCESS":
		return storepb.MachineTokenType_MACHINE_ACCESS
	default:
		return storepb.MachineTokenType_MACHINE_TOKEN_TYPE_UNSPECIFIED
	}
}

func machineTokenStateToString(s storepb.MachineTokenState) string {
	switch s {
	case storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE:
		return "ACTIVE"
	case storepb.MachineTokenState_MACHINE_TOKEN_CONSUMED:
		return "CONSUMED"
	case storepb.MachineTokenState_MACHINE_TOKEN_REVOKED:
		return "REVOKED"
	default:
		return "UNKNOWN"
	}
}

func stringToMachineTokenState(s string) storepb.MachineTokenState {
	switch s {
	case "ACTIVE":
		return storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE
	case "CONSUMED":
		return storepb.MachineTokenState_MACHINE_TOKEN_CONSUMED
	case "REVOKED":
		return storepb.MachineTokenState_MACHINE_TOKEN_REVOKED
	default:
		return storepb.MachineTokenState_MACHINE_TOKEN_STATE_UNSPECIFIED
	}
}
