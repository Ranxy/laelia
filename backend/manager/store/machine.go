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

var machineDeleteTrue = true

// MachineMessage is the storage-layer representation of a machine (the
// long-lived agent-application process a user runs once on a host). Mirrors
// AgentMessage.
type MachineMessage struct {
	ID                 int
	ResourceID         string
	Name               string
	TokenVersion       int
	CreatedAt          time.Time
	Deleted            bool
	Info               *models.MachineInfo
	Status             *models.MachineStatus
	LastTokenRotatedAt time.Time
	// CreatedBy is the principal id of the user who created the machine.
	CreatedBy int
	// AvatarS3Key is the S3 object key of the machine's uploaded avatar image,
	// empty when the machine has not uploaded one.
	AvatarS3Key string
}

// GetResourceID returns the machine's resource name, used to key context-derived
// identifiers such as per-machine rate-limit buckets.
func (m *MachineMessage) GetResourceID() string {
	return m.ResourceID
}

type FindMachineMessage struct {
	ID          *int
	ResourceID  *string
	ShowDeleted bool
	Limit       *int
	Offset      *int
}

type UpdateMachineMessage struct {
	ResourceID         *string
	Name               *string
	Info               *models.MachineInfo
	Status             *models.MachineStatus
	TokenVersion       *int
	LastTokenRotatedAt *time.Time
	Delete             *bool
	AvatarS3Key        *string
}

func (s *Store) GetMachine(ctx context.Context, id int) (*MachineMessage, error) {
	if v, ok := s.machineIDCache.Get(id); ok && s.enableCache {
		return v, nil
	}

	if err := s.listAndCacheAllMachines(ctx); err != nil {
		return nil, err
	}

	machine, _ := s.machineIDCache.Get(id)
	return machine, nil
}

func (s *Store) GetMachineByResourceID(ctx context.Context, resourceID string) (*MachineMessage, error) {
	if v, ok := s.machineResourceIDCache.Get(resourceID); ok && s.enableCache {
		return v, nil
	}

	if err := s.listAndCacheAllMachines(ctx); err != nil {
		return nil, err
	}

	machine, _ := s.machineResourceIDCache.Get(resourceID)
	return machine, nil
}

func (s *Store) ListMachines(ctx context.Context, find *FindMachineMessage) ([]*MachineMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	machines, err := listMachineImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, machine := range machines {
		s.machineIDCache.Add(machine.ID, machine)
		s.machineResourceIDCache.Add(machine.ResourceID, machine)
	}
	return machines, nil
}

func (s *Store) listAndCacheAllMachines(ctx context.Context) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	machines, err := listMachineImpl(ctx, tx, &FindMachineMessage{ShowDeleted: true})
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	for _, machine := range machines {
		s.machineIDCache.Add(machine.ID, machine)
		s.machineResourceIDCache.Add(machine.ResourceID, machine)
	}
	return nil
}

func listMachineImpl(ctx context.Context, txn *sql.Tx, find *FindMachineMessage) ([]*MachineMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("machine.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("machine.resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("machine.deleted = $%d", len(args)+1)), append(args, false)
	}

	query := `SELECT
			machine.id,
			machine.resource_id,
			machine.name,
			machine.token_version,
			machine.created_at,
			machine.deleted,
			machine.info,
			machine.status,
			machine.last_token_rotated_at,
			machine.created_by,
			machine.avatar_s3_key
		FROM machine
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY machine.created_at ASC`

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var machineMessages []*MachineMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var machineMessage MachineMessage
		var infoBytes []byte
		var statusBytes []byte
		var lastTokenRotatedAt sql.NullTime
		if err := rows.Scan(
			&machineMessage.ID,
			&machineMessage.ResourceID,
			&machineMessage.Name,
			&machineMessage.TokenVersion,
			&machineMessage.CreatedAt,
			&machineMessage.Deleted,
			&infoBytes,
			&statusBytes,
			&lastTokenRotatedAt,
			&machineMessage.CreatedBy,
			&machineMessage.AvatarS3Key,
		); err != nil {
			return nil, err
		}
		if lastTokenRotatedAt.Valid {
			machineMessage.LastTokenRotatedAt = lastTokenRotatedAt.Time
		}

		info := &models.MachineInfo{}
		if err := json.Unmarshal(infoBytes, info); err != nil {
			return nil, err
		}
		machineMessage.Info = info

		status := &models.MachineStatus{}
		if err := json.Unmarshal(statusBytes, status); err != nil {
			return nil, err
		}
		machineMessage.Status = status

		machineMessages = append(machineMessages, &machineMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return machineMessages, nil
}

func (s *Store) CreateMachine(ctx context.Context, create *MachineMessage) (*MachineMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if create.Info == nil {
		create.Info = &models.MachineInfo{}
	}
	infoBytes, err := json.Marshal(create.Info)
	if err != nil {
		return nil, err
	}

	if create.Status == nil {
		create.Status = &models.MachineStatus{}
	}
	statusBytes, err := json.Marshal(create.Status)
	if err != nil {
		return nil, err
	}

	resourceID := uuid.New().String()

	var machineID int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO machine (
			resource_id, name, token_version, info, status, created_by
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`,
		resourceID,
		create.Name,
		create.TokenVersion,
		infoBytes,
		statusBytes,
		create.CreatedBy,
	).Scan(&machineID, &create.CreatedAt); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	machine := &MachineMessage{
		ID:           machineID,
		ResourceID:   resourceID,
		Name:         create.Name,
		TokenVersion: create.TokenVersion,
		CreatedAt:    create.CreatedAt,
		Info:         create.Info,
		Status:       create.Status,
		CreatedBy:    create.CreatedBy,
	}
	s.machineIDCache.Add(machine.ID, machine)
	s.machineResourceIDCache.Add(machine.ResourceID, machine)
	return machine, nil
}

func (s *Store) UpdateMachine(ctx context.Context, current *MachineMessage, patch *UpdateMachineMessage) (*MachineMessage, error) {
	sets, args := []string{}, []any{}
	if v := patch.ResourceID; v != nil {
		sets, args = append(sets, fmt.Sprintf("resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Name; v != nil {
		sets, args = append(sets, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Info; v != nil {
		infoBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, fmt.Sprintf("info = $%d", len(args)+1)), append(args, infoBytes)
	}
	if v := patch.Status; v != nil {
		statusBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, fmt.Sprintf("status = $%d", len(args)+1)), append(args, statusBytes)
	}
	if v := patch.TokenVersion; v != nil {
		sets, args = append(sets, fmt.Sprintf("token_version = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.LastTokenRotatedAt; v != nil {
		sets, args = append(sets, fmt.Sprintf("last_token_rotated_at = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Delete; v != nil {
		sets, args = append(sets, fmt.Sprintf("deleted = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.AvatarS3Key; v != nil {
		sets, args = append(sets, fmt.Sprintf("avatar_s3_key = $%d", len(args)+1)), append(args, *v)
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
		UPDATE machine
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

	s.machineIDCache.Remove(current.ID)
	s.machineResourceIDCache.Remove(current.ResourceID)
	machine, err := s.GetMachine(ctx, current.ID)
	if err != nil {
		return nil, err
	}

	s.machineIDCache.Add(machine.ID, machine)
	s.machineResourceIDCache.Add(machine.ResourceID, machine)
	return machine, nil
}

func (s *Store) DeleteMachine(ctx context.Context, resourceID string) error {
	machine, err := s.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return err
	}
	if machine == nil {
		return errors.Errorf("machine %s not found", resourceID)
	}

	if _, err := s.UpdateMachine(ctx, machine, &UpdateMachineMessage{Delete: &machineDeleteTrue}); err != nil {
		return err
	}
	return nil
}
