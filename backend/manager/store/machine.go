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
	// ProvisionerID is the provisioner that created (and manages the workload
	// of) this machine; 0 for self-hosted machines.
	ProvisionerID int
	// Provisioning is the provisioning job state; nil for self-hosted machines
	// and machines whose provisioning row was never set.
	Provisioning *models.ProvisioningStatus
}

// GetResourceID returns the machine's resource name, used to key context-derived
// identifiers such as per-machine rate-limit buckets.
func (m *MachineMessage) GetResourceID() string {
	return m.ResourceID
}

type FindMachineMessage struct {
	ID            *int
	ResourceID    *string
	ProvisionerID *int
	ShowDeleted   bool
	Limit         *int
	Offset        *int
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
	CreatedBy          *int
	ProvisionerID      *int
	Provisioning       *models.ProvisioningStatus
}

func (s *Store) GetMachine(ctx context.Context, id int) (*MachineMessage, error) {
	if v, ok := s.machineIDCache.Get(id); ok && s.enableCache {
		return v, nil
	}

	machine, err := s.findMachine(ctx, &FindMachineMessage{ID: &id, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if machine == nil {
		return nil, nil
	}
	s.cacheMachine(machine)
	return machine, nil
}

func (s *Store) GetMachineByResourceID(ctx context.Context, resourceID string) (*MachineMessage, error) {
	if v, ok := s.machineResourceIDCache.Get(resourceID); ok && s.enableCache {
		return v, nil
	}

	machine, err := s.findMachine(ctx, &FindMachineMessage{ResourceID: &resourceID, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if machine == nil {
		return nil, nil
	}
	s.cacheMachine(machine)
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
		s.cacheMachine(machine)
	}
	return machines, nil
}

// findMachine runs a single-machine lookup via listMachineImpl in a read
// transaction and returns the match (or nil when absent). It is the
// point-query path used on a cache miss so resolving one machine does not
// trigger a full-table load.
func (s *Store) findMachine(ctx context.Context, find *FindMachineMessage) (*MachineMessage, error) {
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

	if len(machines) == 0 {
		return nil, nil
	}
	return machines[0], nil
}

// cacheMachine stores a machine in both the ID and resource-id caches.
func (s *Store) cacheMachine(machine *MachineMessage) {
	if machine == nil {
		return
	}
	s.machineIDCache.Add(machine.ID, machine)
	s.machineResourceIDCache.Add(machine.ResourceID, machine)
}

func listMachineImpl(ctx context.Context, txn *sql.Tx, find *FindMachineMessage) ([]*MachineMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("machine.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ResourceID; v != nil {
		where, args = append(where, fmt.Sprintf("machine.resource_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.ProvisionerID; v != nil {
		where, args = append(where, fmt.Sprintf("machine.provisioner_id = $%d", len(args)+1)), append(args, *v)
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
			machine.avatar_s3_key,
			machine.provisioner_id,
			machine.provisioning
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
		var provisioningBytes []byte
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
			&machineMessage.ProvisionerID,
			&provisioningBytes,
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

		if provisioningBytes != nil {
			provisioning := &models.ProvisioningStatus{}
			if err := json.Unmarshal(provisioningBytes, provisioning); err != nil {
				return nil, err
			}
			machineMessage.Provisioning = provisioning
		}

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

// CreateMachineWithToken creates a machine with the given resource id and
// mints its first refresh token in one transaction, so the device approval
// can never leave a machine row without a credential (which would strand it:
// the CLI only learns the refresh token from the approval result). The caller
// generates the resource id so the refresh token JWT can be signed with it
// before the transaction. The machine message may carry ProvisionerID and
// Provisioning for provisioned machines (the ProvisionMachine path). Returns
// the created machine.
func (s *Store) CreateMachineWithToken(ctx context.Context, resourceID string, create *MachineMessage, token *MachineTokenMessage) (*MachineMessage, error) {
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
	var provisioningBytes []byte
	if create.Provisioning != nil {
		provisioningBytes, err = json.Marshal(create.Provisioning)
		if err != nil {
			return nil, err
		}
	}

	var machineID int
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO machine (
			resource_id, name, token_version, info, status, created_by,
			provisioner_id, provisioning
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at
	`,
		resourceID,
		create.Name,
		create.TokenVersion,
		infoBytes,
		statusBytes,
		create.CreatedBy,
		create.ProvisionerID,
		provisioningBytes,
	).Scan(&machineID, &create.CreatedAt); err != nil {
		return nil, err
	}

	token.MachineID = machineID
	token.TokenFamily = resourceID
	if err := insertMachineTokenTx(ctx, tx, token); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	machine := &MachineMessage{
		ID:            machineID,
		ResourceID:    resourceID,
		Name:          create.Name,
		TokenVersion:  create.TokenVersion,
		CreatedAt:     create.CreatedAt,
		Info:          create.Info,
		Status:        create.Status,
		CreatedBy:     create.CreatedBy,
		ProvisionerID: create.ProvisionerID,
		Provisioning:  create.Provisioning,
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
	if v := patch.CreatedBy; v != nil {
		sets, args = append(sets, fmt.Sprintf("created_by = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.ProvisionerID; v != nil {
		sets, args = append(sets, fmt.Sprintf("provisioner_id = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Provisioning; v != nil {
		provisioningBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		sets, args = append(sets, fmt.Sprintf("provisioning = $%d", len(args)+1)), append(args, provisioningBytes)
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

// DeleteMachineIfNoAgents soft-deletes the machine iff it currently hosts no
// live agents, as a single atomic statement so a CreateAgent racing the delete
// cannot slip into the gap between the agent-count check and the soft-delete
// (the NOT EXISTS guard and the UPDATE evaluate together). Returns ok=false
// when the machine was not found, already deleted, or still hosts agents —
// the caller re-fetches to tell the last two apart. Unlike DeleteMachine this
// is race-free against concurrent agent creation on the same machine.
func (s *Store) DeleteMachineIfNoAgents(ctx context.Context, resourceID string) (bool, error) {
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine SET deleted = TRUE
		WHERE resource_id = $1 AND deleted = FALSE
			AND NOT EXISTS (
				SELECT 1 FROM agent
				WHERE agent.machine_id = machine.id AND agent.deleted = FALSE
			)
	`, resourceID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountAgentsByMachine returns a live-agent count per machine id for the given
// machines in a single query, so ListMachines can populate AgentCount for a
// page of machines without an N+1 (one ListAgents query per row).
func (s *Store) CountAgentsByMachine(ctx context.Context, machineIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(machineIDs))
	if len(machineIDs) == 0 {
		return counts, nil
	}
	args := make([]any, 0, len(machineIDs))
	placeholders := make([]string, 0, len(machineIDs))
	for i, id := range machineIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT machine_id, COUNT(*)
		FROM agent
		WHERE machine_id IN (`+strings.Join(placeholders, ",")+`) AND deleted = FALSE
		GROUP BY machine_id
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

// UpdateMachineProvisioning persists the provisioning job state carried on the
// machine row. The single write path for machine.provisioning, so the phase
// transitions recorded here are exactly what the reconnect replay reads back.
func (s *Store) UpdateMachineProvisioning(ctx context.Context, machineID int, provisioning *models.ProvisioningStatus) error {
	provisioningBytes, err := json.Marshal(provisioning)
	if err != nil {
		return err
	}

	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE machine SET provisioning = $2 WHERE id = $1
	`, machineID, provisioningBytes)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.Errorf("machine %d not found", machineID)
	}

	s.machineIDCache.Remove(machineID)
	return nil
}

// replayableProvisioningPhases are the non-terminal phases whose job is pushed
// again when the provisioner (re)connects: PENDING (never acked),
// PROVISIONING (acked, unfinished), DEPROVISIONING (teardown unfinished).
// Terminal rows (PROVISIONED / FAILED / DELETED) are never replayed: pods
// self-heal via their StatefulSet without provisioner involvement.
var replayableProvisioningPhases = map[models.ProvisioningPhase]bool{
	models.ProvisioningPhase_PROVISIONING_PHASE_PENDING:        true,
	models.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING:   true,
	models.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING: true,
}

// ListReplayableProvisioningMachines returns the non-deleted machines bound to
// the provisioner whose provisioning job is in a replayable phase. Bound to
// one provisioner, so the lookup is small and phases are filtered in Go (the
// jsonb column stores the enum as a number; no SQL-side phase predicate).
func (s *Store) ListReplayableProvisioningMachines(ctx context.Context, provisionerID int) ([]*MachineMessage, error) {
	machines, err := s.ListMachines(ctx, &FindMachineMessage{ProvisionerID: &provisionerID})
	if err != nil {
		return nil, err
	}

	var replayable []*MachineMessage
	for _, machine := range machines {
		if machine.Provisioning != nil && replayableProvisioningPhases[machine.Provisioning.Phase] {
			replayable = append(replayable, machine)
		}
	}
	return replayable, nil
}
