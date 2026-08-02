package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type AuditLogMessage struct {
	Method    string
	ActorType string
	ActorID   string
	SourceIP  string
	Status    string
	Error     string
	// Resource is the target resource of the audited call, e.g. "agents/{rid}".
	Resource string
	// Payload is the structured change payload as JSON (e.g. IAM binding
	// deltas), stored in the jsonb payload column.
	Payload   string
	CreatedAt time.Time
}

func (s *Store) CreateAuditLog(ctx context.Context, log *AuditLogMessage) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO audit_log (method, actor_type, actor_id, source_ip, status, error, resource, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, log.Method, log.ActorType, log.ActorID, log.SourceIP, log.Status, log.Error, log.Resource, log.Payload, log.CreatedAt)
	return err
}

// AuditLogRecord is a stored audit log row.
type AuditLogRecord struct {
	ID        int64
	Method    string
	ActorType string
	ActorID   string
	SourceIP  string
	Status    string
	Error     string
	Resource  string
	Payload   string
	CreatedAt time.Time
}

// FindAuditLogMessage filters ListAuditLogs. Nil fields are not filtered;
// ordering defaults to create_time DESC.
type FindAuditLogMessage struct {
	Method   *string
	ActorID  *string
	Resource *string
	Status   *string
	Limit    *int
	Offset   *int
	OrderAsc bool
}

// ListAuditLogs returns audit log rows matching the filter, ordered by
// create_time (then id for stable tie-breaking).
func (s *Store) ListAuditLogs(ctx context.Context, find *FindAuditLogMessage) ([]*AuditLogRecord, error) {
	where := []string{"TRUE"}
	args := []any{}
	add := func(column string, v *string) {
		if v == nil {
			return
		}
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, *v)
	}
	add("method", find.Method)
	add("actor_id", find.ActorID)
	add("resource", find.Resource)
	add("status", find.Status)

	order := "DESC"
	if find.OrderAsc {
		order = "ASC"
	}
	query := `
		SELECT id, method, actor_type, actor_id, source_ip, status, error, resource, payload, created_at
		FROM audit_log
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY created_at ` + order + `, id ` + order
	if find.Limit != nil {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, *find.Limit)
	}
	if find.Offset != nil {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, *find.Offset)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*AuditLogRecord
	for rows.Next() {
		r := &AuditLogRecord{}
		var payload []byte
		if err := rows.Scan(
			&r.ID, &r.Method, &r.ActorType, &r.ActorID, &r.SourceIP, &r.Status,
			&r.Error, &r.Resource, &payload, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		r.Payload = string(payload)
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
