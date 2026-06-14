package store

import (
	"context"
	"time"
)

type AuditLogMessage struct {
	Method    string
	ActorType string
	ActorID   string
	SourceIP  string
	Status    string
	Error     string
	CreatedAt time.Time
}

func (s *Store) CreateAuditLog(ctx context.Context, log *AuditLogMessage) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO audit_log (method, actor_type, actor_id, source_ip, status, error, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, log.Method, log.ActorType, log.ActorID, log.SourceIP, log.Status, log.Error, log.CreatedAt)
	return err
}
