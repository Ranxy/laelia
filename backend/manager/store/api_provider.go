package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Ranxy/laelia/backend/common"
)

// APIProviderMessage is the storage-layer representation of a global LLM API
// provider. Entries carry their api key (plaintext-at-rest, consistent with the
// S3 secret and legacy per-agent api_key posture); the service layer masks keys
// before they cross the API and resolves them at the agent-daemon boundary.
type APIProviderMessage struct {
	ID           int
	ResourceID   string
	Name         string
	ProviderType string
	BaseURL      string
	Description  string
	CreatedBy    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Entries      []*APIProviderEntryMessage
	Members      []string
}

// APIProviderEntryMessage is one (api_key, model) entry of an ApiProvider.
type APIProviderEntryMessage struct {
	ID         int
	ProviderID int
	Label      string
	ModelName  string
	APIKey     string
	CreatedAt  time.Time
}

// GetAPIProviderByResourceID returns a provider (with entries and members) by
// resource id, or nil when not found.
func (s *Store) GetAPIProviderByResourceID(ctx context.Context, resourceID string) (*APIProviderMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	providers, err := listAPIProviderRows(ctx, tx, "resource_id = $1", []any{resourceID})
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, nil
	}
	provider := providers[0]
	if err := loadAPIProviderDetail(ctx, tx, providers); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return provider, nil
}

// ListAPIProviders returns every provider (with entries and members), ordered
// by creation time. Provider counts are small (workspace-level config), so no
// pagination is applied; callers filter by member access in the service layer.
func (s *Store) ListAPIProviders(ctx context.Context) ([]*APIProviderMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	providers, err := listAPIProviderRows(ctx, tx, "TRUE", nil)
	if err != nil {
		return nil, err
	}
	if err := loadAPIProviderDetail(ctx, tx, providers); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return providers, nil
}

// CreateAPIProvider inserts a provider together with its entries and members.
// A nil Entries/Members is treated as empty.
func (s *Store) CreateAPIProvider(ctx context.Context, create *APIProviderMessage) (*APIProviderMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	resourceID := uuid.New().String()
	var id int
	var createdAt, updatedAt time.Time
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO api_provider (resource_id, name, provider_type, base_url, description, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, resourceID, create.Name, create.ProviderType, create.BaseURL, create.Description, create.CreatedBy,
	).Scan(&id, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	provider := &APIProviderMessage{
		ID:           id,
		ResourceID:   resourceID,
		Name:         create.Name,
		ProviderType: create.ProviderType,
		BaseURL:      create.BaseURL,
		Description:  create.Description,
		CreatedBy:    create.CreatedBy,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
	entries, err := insertAPIProviderEntries(ctx, tx, id, create.Entries)
	if err != nil {
		return nil, err
	}
	provider.Entries = entries
	if err := replaceAPIProviderMembers(ctx, tx, id, create.Members); err != nil {
		return nil, err
	}
	provider.Members = append([]string(nil), create.Members...)

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return provider, nil
}

// UpdateAPIProvider replaces the provider's mutable fields plus its entries and
// members (full replace of the entry/member lists). Returns the updated
// provider.
func (s *Store) UpdateAPIProvider(ctx context.Context, current *APIProviderMessage, patch *APIProviderMessage) (*APIProviderMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE api_provider
		SET name = $1, base_url = $2, description = $3, updated_at = now()
		WHERE id = $4
	`, patch.Name, patch.BaseURL, patch.Description, current.ID); err != nil {
		return nil, err
	}

	// Full replace of entries: the service has already resolved masked api_key
	// sentinels against the stored entries before calling here.
	if _, err := tx.ExecContext(ctx, `DELETE FROM api_provider_entry WHERE provider_id = $1`, current.ID); err != nil {
		return nil, err
	}
	entries, err := insertAPIProviderEntries(ctx, tx, current.ID, patch.Entries)
	if err != nil {
		return nil, err
	}
	if err := replaceAPIProviderMembers(ctx, tx, current.ID, patch.Members); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	updated := &APIProviderMessage{
		ID:           current.ID,
		ResourceID:   current.ResourceID,
		Name:         patch.Name,
		ProviderType: current.ProviderType,
		BaseURL:      patch.BaseURL,
		Description:  patch.Description,
		CreatedBy:    current.CreatedBy,
		CreatedAt:    current.CreatedAt,
		UpdatedAt:    time.Now(),
		Entries:      entries,
		Members:      append([]string(nil), patch.Members...),
	}
	return updated, nil
}

// DeleteAPIProvider hard-deletes a provider (cascade removes its entries and
// members). Callers must ensure no agent references the provider first.
func (s *Store) DeleteAPIProvider(ctx context.Context, resourceID string) error {
	_, err := s.GetDB().ExecContext(ctx, `DELETE FROM api_provider WHERE resource_id = $1`, resourceID)
	return err
}

// CountAgentsReferencingProvider returns the number of live agents whose
// acp_config.global_provider points at the given provider resource name
// (apiProviders/{id}).
func (s *Store) CountAgentsReferencingProvider(ctx context.Context, providerResourceID string) (int, error) {
	name := common.FormatAPIProviderUID(providerResourceID)
	return s.countAgentsByConfigField(ctx, "global_provider", name)
}

// CountAgentsReferencingEntry returns the number of live agents whose
// acp_config.global_provider_entry points at the given entry resource name
// (apiProviders/{provider}/entries/{entry}).
func (s *Store) CountAgentsReferencingEntry(ctx context.Context, entryResourceName string) (int, error) {
	return s.countAgentsByConfigField(ctx, "global_provider_entry", entryResourceName)
}

func (s *Store) countAgentsByConfigField(ctx context.Context, field, value string) (int, error) {
	var count int
	// The value is built from resource names (uuid tokens), never user input, so
	// interpolating it into the JSON path string literal is safe.
	err := s.GetDB().QueryRowContext(ctx, fmt.Sprintf(`
		SELECT COUNT(*)
		FROM agent
		WHERE deleted = FALSE
			AND info->'acp_config'->>'%s' = $1
	`, field), value).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// listAPIProviderRows scans provider rows (without entries/members) matching the
// given WHERE clause and args.
func listAPIProviderRows(ctx context.Context, txn *sql.Tx, where string, args []any) ([]*APIProviderMessage, error) {
	rows, err := txn.QueryContext(ctx, `
		SELECT id, resource_id, name, provider_type, base_url, description, created_by, created_at, updated_at
		FROM api_provider
		WHERE `+where+` ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*APIProviderMessage
	for rows.Next() {
		var p APIProviderMessage
		if err := rows.Scan(
			&p.ID,
			&p.ResourceID,
			&p.Name,
			&p.ProviderType,
			&p.BaseURL,
			&p.Description,
			&p.CreatedBy,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		providers = append(providers, &p)
	}
	return providers, rows.Err()
}

// loadAPIProviderDetail bulk-loads entries and members for the given providers
// in two queries.
func loadAPIProviderDetail(ctx context.Context, txn *sql.Tx, providers []*APIProviderMessage) error {
	if len(providers) == 0 {
		return nil
	}
	ids := make([]int, 0, len(providers))
	for _, p := range providers {
		ids = append(ids, p.ID)
	}
	entries, err := listAPIProviderEntries(ctx, txn, ids)
	if err != nil {
		return err
	}
	members, err := listAPIProviderMembers(ctx, txn, ids)
	if err != nil {
		return err
	}
	for _, p := range providers {
		p.Entries = entries[p.ID]
		p.Members = members[p.ID]
	}
	return nil
}

func listAPIProviderEntries(ctx context.Context, txn *sql.Tx, providerIDs []int) (map[int][]*APIProviderEntryMessage, error) {
	byID := make(map[int][]*APIProviderEntryMessage, len(providerIDs))
	if len(providerIDs) == 0 {
		return byID, nil
	}
	args := make([]any, 0, len(providerIDs))
	placeholders := make([]string, 0, len(providerIDs))
	for i, id := range providerIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	rows, err := txn.QueryContext(ctx, `
		SELECT id, provider_id, label, model_name, api_key, created_at
		FROM api_provider_entry
		WHERE provider_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e APIProviderEntryMessage
		if err := rows.Scan(&e.ID, &e.ProviderID, &e.Label, &e.ModelName, &e.APIKey, &e.CreatedAt); err != nil {
			return nil, err
		}
		byID[e.ProviderID] = append(byID[e.ProviderID], &e)
	}
	return byID, rows.Err()
}

func listAPIProviderMembers(ctx context.Context, txn *sql.Tx, providerIDs []int) (map[int][]string, error) {
	byID := make(map[int][]string, len(providerIDs))
	if len(providerIDs) == 0 {
		return byID, nil
	}
	args := make([]any, 0, len(providerIDs))
	placeholders := make([]string, 0, len(providerIDs))
	for i, id := range providerIDs {
		args = append(args, id)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	rows, err := txn.QueryContext(ctx, `
		SELECT provider_id, member
		FROM api_provider_member
		WHERE provider_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY member ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var providerID int
		var member string
		if err := rows.Scan(&providerID, &member); err != nil {
			return nil, err
		}
		byID[providerID] = append(byID[providerID], member)
	}
	return byID, rows.Err()
}

func insertAPIProviderEntries(ctx context.Context, txn *sql.Tx, providerID int, entries []*APIProviderEntryMessage) ([]*APIProviderEntryMessage, error) {
	out := make([]*APIProviderEntryMessage, 0, len(entries))
	for _, e := range entries {
		var id int
		var createdAt time.Time
		if err := txn.QueryRowContext(ctx, `
			INSERT INTO api_provider_entry (provider_id, label, model_name, api_key)
			VALUES ($1, $2, $3, $4)
			RETURNING id, created_at
		`, providerID, e.Label, e.ModelName, e.APIKey).Scan(&id, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, &APIProviderEntryMessage{
			ID:         id,
			ProviderID: providerID,
			Label:      e.Label,
			ModelName:  e.ModelName,
			APIKey:     e.APIKey,
			CreatedAt:  createdAt,
		})
	}
	return out, nil
}

func replaceAPIProviderMembers(ctx context.Context, txn *sql.Tx, providerID int, members []string) error {
	if _, err := txn.ExecContext(ctx, `DELETE FROM api_provider_member WHERE provider_id = $1`, providerID); err != nil {
		return err
	}
	for _, member := range members {
		if _, err := txn.ExecContext(ctx, `
			INSERT INTO api_provider_member (provider_id, member)
			VALUES ($1, $2)
		`, providerID, member); err != nil {
			return err
		}
	}
	return nil
}
