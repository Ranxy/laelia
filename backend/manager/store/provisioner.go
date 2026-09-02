package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// ProvisionerMessage is the storage-layer representation of a provisioner: a
// management-plane worker (running in the customer's infrastructure) that
// creates machine workloads in one virtualization backend on the manager's
// behalf. Mirrors MachineMessage.
type ProvisionerMessage struct {
	ID           int
	ResourceID   string
	Name         string
	Backend      string
	Description  string
	TokenVersion int
	CreatedBy    int
	Deleted      bool
	CreatedAt    time.Time
	Status       *models.ProvisionerStatus
}

type FindProvisionerMessage struct {
	ID          *int
	ResourceID  *string
	ShowDeleted bool
	Limit       *int
	Offset      *int
}

type UpdateProvisionerMessage struct {
	Name         *string
	Description  *string
	TokenVersion *int
	Status       *models.ProvisionerStatus
	Delete       *bool
}

func (s *Store) GetProvisioner(ctx context.Context, id int) (*ProvisionerMessage, error) {
	if v, ok := s.provisionerIDCache.Get(id); ok && s.enableCache {
		return v, nil
	}

	provisioner, err := s.findProvisioner(ctx, &FindProvisionerMessage{ID: &id, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if provisioner == nil {
		return nil, nil
	}
	s.cacheProvisioner(provisioner)
	return provisioner, nil
}

func (s *Store) GetProvisionerByResourceID(ctx context.Context, resourceID string) (*ProvisionerMessage, error) {
	if v, ok := s.provisionerResourceIDCache.Get(resourceID); ok && s.enableCache {
		return v, nil
	}

	provisioner, err := s.findProvisioner(ctx, &FindProvisionerMessage{ResourceID: &resourceID, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if provisioner == nil {
		return nil, nil
	}
	s.cacheProvisioner(provisioner)
	return provisioner, nil
}

func (s *Store) ListProvisioners(ctx context.Context, find *FindProvisionerMessage) ([]*ProvisionerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	provisioners, err := listProvisionerImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, provisioner := range provisioners {
		s.cacheProvisioner(provisioner)
	}
	return provisioners, nil
}

// findProvisioner runs a single-provisioner lookup via listProvisionerImpl in
// a read transaction; the cache-miss point-query path.
func (s *Store) findProvisioner(ctx context.Context, find *FindProvisionerMessage) (*ProvisionerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	provisioners, err := listProvisionerImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if len(provisioners) == 0 {
		return nil, nil
	}
	return provisioners[0], nil
}

// cacheProvisioner stores a provisioner in both the ID and resource-id caches.
func (s *Store) cacheProvisioner(provisioner *ProvisionerMessage) {
	if provisioner == nil {
		return
	}
	s.provisionerIDCache.Add(provisioner.ID, provisioner)
	s.provisionerResourceIDCache.Add(provisioner.ResourceID, provisioner)
}

func listProvisionerImpl(ctx context.Context, txn *sql.Tx, find *FindProvisionerMessage) ([]*ProvisionerMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("provisioner.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("provisioner.resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("provisioner.deleted = $%d", len(args)+1)), append(args, false)
	}

	query := `SELECT
			provisioner.id,
			provisioner.resource_id,
			provisioner.name,
			provisioner.backend,
			provisioner.description,
			provisioner.token_version,
			provisioner.created_by,
			provisioner.deleted,
			provisioner.created_at,
			provisioner.status
		FROM provisioner
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY provisioner.created_at ASC`

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var provisionerMessages []*ProvisionerMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var provisionerMessage ProvisionerMessage
		var statusBytes []byte
		if err := rows.Scan(
			&provisionerMessage.ID,
			&provisionerMessage.ResourceID,
			&provisionerMessage.Name,
			&provisionerMessage.Backend,
			&provisionerMessage.Description,
			&provisionerMessage.TokenVersion,
			&provisionerMessage.CreatedBy,
			&provisionerMessage.Deleted,
			&provisionerMessage.CreatedAt,
			&statusBytes,
		); err != nil {
			return nil, err
		}

		status := &models.ProvisionerStatus{}
		if err := json.Unmarshal(statusBytes, status); err != nil {
			return nil, err
		}
		provisionerMessage.Status = status

		provisionerMessages = append(provisionerMessages, &provisionerMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return provisionerMessages, nil
}

func (s *Store) CreateProvisioner(ctx context.Context, create *ProvisionerMessage) (*ProvisionerMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if create.Status == nil {
		create.Status = &models.ProvisionerStatus{}
	}
	statusBytes, err := json.Marshal(create.Status)
	if err != nil {
		return nil, err
	}

	resourceID := uuid.NewString()

	var provisionerID int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO provisioner (
			resource_id, name, backend, description, token_version, created_by, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`,
		resourceID,
		create.Name,
		create.Backend,
		create.Description,
		create.TokenVersion,
		create.CreatedBy,
		statusBytes,
	).Scan(&provisionerID, &create.CreatedAt); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	provisioner := &ProvisionerMessage{
		ID:           provisionerID,
		ResourceID:   resourceID,
		Name:         create.Name,
		Backend:      create.Backend,
		Description:  create.Description,
		TokenVersion: create.TokenVersion,
		CreatedBy:    create.CreatedBy,
		CreatedAt:    create.CreatedAt,
		Status:       create.Status,
	}
	s.cacheProvisioner(provisioner)
	return provisioner, nil
}

func (s *Store) UpdateProvisioner(ctx context.Context, current *ProvisionerMessage, patch *UpdateProvisionerMessage) (*ProvisionerMessage, error) {
	sets, args := []string{}, []any{}
	if v := patch.Name; v != nil {
		sets, args = append(sets, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Description; v != nil {
		sets, args = append(sets, fmt.Sprintf("description = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.TokenVersion; v != nil {
		sets, args = append(sets, fmt.Sprintf("token_version = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Status; v != nil {
		statusBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, fmt.Sprintf("status = $%d", len(args)+1)), append(args, statusBytes)
	}
	if v := patch.Delete; v != nil {
		sets, args = append(sets, fmt.Sprintf("deleted = $%d", len(args)+1)), append(args, *v)
	}

	if len(sets) == 0 {
		return current, nil
	}

	args = append(args, current.ID)

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE provisioner
		SET `+strings.Join(sets, ", ")+`
		WHERE id = $%d
	`, len(args)),
		args...,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.provisionerIDCache.Remove(current.ID)
	s.provisionerResourceIDCache.Remove(current.ResourceID)
	provisioner, err := s.GetProvisioner(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	return provisioner, nil
}

// UpdateProvisionerStatus stamps the runtime status (connected, last_seen,
// version, ...) read back from the stream. Convenience wrapper so the stream
// handler does not need a full patch.
func (s *Store) UpdateProvisionerStatus(ctx context.Context, current *ProvisionerMessage, status *models.ProvisionerStatus) (*ProvisionerMessage, error) {
	return s.UpdateProvisioner(ctx, current, &UpdateProvisionerMessage{Status: status})
}

// DeleteProvisioner soft-deletes the provisioner row.
func (s *Store) DeleteProvisioner(ctx context.Context, resourceID string) error {
	provisioner, err := s.GetProvisionerByResourceID(ctx, resourceID)
	if err != nil {
		return err
	}
	if provisioner == nil {
		return errors.Errorf("provisioner %s not found", resourceID)
	}

	deleted := true
	if _, err := s.UpdateProvisioner(ctx, provisioner, &UpdateProvisionerMessage{Delete: &deleted}); err != nil {
		return err
	}
	return nil
}

// CountMachinesByProvisioner returns a live-machine count per provisioner id
// for the given provisioners in a single query, so ListProvisioners can
// populate machine_count without an N+1.
func (s *Store) CountMachinesByProvisioner(ctx context.Context, provisionerIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(provisionerIDs))
	if len(provisionerIDs) == 0 {
		return counts, nil
	}
	args := make([]any, 0, len(provisionerIDs))
	placeholders := make([]string, 0, len(provisionerIDs))
	for i, id := range provisionerIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT provisioner_id, COUNT(*)
		FROM machine
		WHERE provisioner_id IN (`+strings.Join(placeholders, ",")+`) AND deleted = FALSE
		GROUP BY provisioner_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	return counts, rows.Err()
}
