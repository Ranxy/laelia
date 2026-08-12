package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// FindSettingMessage is the message for finding setting.
type FindSettingMessage struct {
	Name *models.SettingName
}

// SetSettingMessage is the message for updating setting.
type SetSettingMessage struct {
	Name  models.SettingName
	Value string
}

// SettingMessage is the message of setting.
type SettingMessage struct {
	Name  models.SettingName
	Value string
}

// settingPayloadDefaults maps every typed setting to a factory producing its
// default payload, used when the row is missing. Raw-string settings
// (AUTH_SECRET, WORKSPACE_ID, BRANDING_LOGO) are intentionally absent: their
// values are opaque strings, not JSON payloads.
var settingPayloadDefaults = map[models.SettingName]func() proto.Message{
	models.SettingName_S3_CONFIG: func() proto.Message {
		return &models.S3ConfigSetting{UseSsl: true}
	},
	models.SettingName_LLM_AGENT_CONFIG: func() proto.Message {
		return &models.LlmAgentConfigSetting{AllowUserSelfProvidedKeys: true}
	},
	models.SettingName_USER_MCP_CONFIG: func() proto.Message {
		return &models.UserMcpConfigSetting{AllowUserMcpServers: true}
	},
	models.SettingName_PASSWORD_RESTRICTION: func() proto.Message {
		return &models.PasswordRestrictionSetting{MinLength: 8}
	},
	models.SettingName_WORKSPACE_PROFILE: func() proto.Message {
		return &models.WorkspaceProfileSetting{}
	},
	models.SettingName_WEB_PUSH_CONFIG: func() proto.Message {
		return &models.WebPushSetting{}
	},
	models.SettingName_SMTP_CONFIG: func() proto.Message {
		return &models.SMTPSetting{}
	},
}

// GetSettingValue reads a typed setting payload by name. A missing row yields
// the registered default; an invalid stored value is an error. Names without a
// registered payload type (raw-string settings) are rejected.
func (s *Store) GetSettingValue(ctx context.Context, name models.SettingName) (proto.Message, error) {
	defaults, ok := settingPayloadDefaults[name]
	if !ok {
		return nil, errors.Errorf("no typed payload registered for setting %v", name)
	}
	return getSettingValue(ctx, s, name, defaults)
}

// UpsertSettingValue stores a typed setting payload by name.
func (s *Store) UpsertSettingValue(ctx context.Context, name models.SettingName, value proto.Message) error {
	_, err := upsertSettingValue(ctx, s, name, value)
	return err
}

// UpdateSettingValueAtomic reads the stored payload, lets apply compute the next
// value from it (a missing row yields the registered default), and writes the
// result back -- all in one transaction with a row lock so concurrent updates
// to the same setting serialize instead of losing fields. The cache is
// refreshed on success; apply must not call back into the store (it runs
// inside the transaction).
func (s *Store) UpdateSettingValueAtomic(ctx context.Context, name models.SettingName, apply func(current proto.Message) (proto.Message, error)) (proto.Message, error) {
	defaults, ok := settingPayloadDefaults[name]
	if !ok {
		return nil, errors.Errorf("no typed payload registered for setting %v", name)
	}
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	// Ensure the row exists, then lock it so concurrent updates serialize.
	// A rolled-back apply also rolls back the placeholder row.
	if _, err := tx.ExecContext(ctx, `INSERT INTO setting (name, value) VALUES ($1, '{}') ON CONFLICT (name) DO NOTHING`, name.String()); err != nil {
		return nil, err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM setting WHERE name = $1 FOR UPDATE`, name.String()).Scan(&raw); err != nil {
		return nil, err
	}
	current := defaults()
	if err := json.Unmarshal([]byte(raw), current); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal setting %v", name)
	}
	next, err := apply(current)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal setting %v", name)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE setting SET value = $2 WHERE name = $1`, name.String(), string(payload)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	s.settingCache.Add(name, &SettingMessage{Name: name, Value: string(payload)})
	return next, nil
}

// getSettingValue reads the raw setting row and unmarshals it into a payload
// created by defaults; a missing row yields defaults() as-is.
func getSettingValue[T proto.Message](ctx context.Context, s *Store, name models.SettingName, defaults func() T) (T, error) {
	setting, err := s.GetSettingV2(ctx, name)
	if err != nil {
		return *new(T), errors.Wrapf(err, "failed to get setting %v", name)
	}
	return unmarshalSettingValue(setting, defaults)
}

// unmarshalSettingValue decodes a raw setting row into a payload created by
// defaults; a nil row yields defaults() as-is.
func unmarshalSettingValue[T proto.Message](setting *SettingMessage, defaults func() T) (T, error) {
	out := defaults()
	if setting == nil {
		return out, nil
	}
	if err := json.Unmarshal([]byte(setting.Value), out); err != nil {
		return *new(T), errors.Wrapf(err, "failed to unmarshal setting %v", setting.Name)
	}
	return out, nil
}

// upsertSettingValue marshals a typed payload and upserts the setting row.
func upsertSettingValue[T proto.Message](ctx context.Context, s *Store, name models.SettingName, value T) (T, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return *new(T), errors.Wrapf(err, "failed to marshal setting %v", name)
	}
	if _, err := s.UpsertSettingV2(ctx, &SetSettingMessage{Name: name, Value: string(payload)}); err != nil {
		return *new(T), errors.Wrapf(err, "failed to upsert setting %v", name)
	}
	return value, nil
}

// getSettingPayload reads the typed payload for name and asserts it to T.
func getSettingPayload[T proto.Message](ctx context.Context, s *Store, name models.SettingName) (T, error) {
	value, err := s.GetSettingValue(ctx, name)
	if err != nil {
		return *new(T), err
	}
	out, ok := value.(T)
	if !ok {
		return *new(T), errors.Errorf("unexpected payload type %T for setting %v", value, name)
	}
	return out, nil
}

// GetPasswordRestrictionSetting returns the password restriction payload.
func (s *Store) GetPasswordRestrictionSetting(ctx context.Context) (*models.PasswordRestrictionSetting, error) {
	return getSettingPayload[*models.PasswordRestrictionSetting](ctx, s, models.SettingName_PASSWORD_RESTRICTION)
}

// UpsertPasswordRestrictionSetting stores the password restriction payload.
func (s *Store) UpsertPasswordRestrictionSetting(ctx context.Context, setting *models.PasswordRestrictionSetting) error {
	_, err := upsertSettingValue(ctx, s, models.SettingName_PASSWORD_RESTRICTION, setting)
	return err
}

// GetWorkspaceGeneralSetting returns the workspace general setting payload.
func (s *Store) GetWorkspaceGeneralSetting(ctx context.Context) (*models.WorkspaceProfileSetting, error) {
	return getSettingPayload[*models.WorkspaceProfileSetting](ctx, s, models.SettingName_WORKSPACE_PROFILE)
}

// UpsertWorkspaceGeneralSetting stores the workspace general setting payload.
func (s *Store) UpsertWorkspaceGeneralSetting(ctx context.Context, setting *models.WorkspaceProfileSetting) error {
	_, err := upsertSettingValue(ctx, s, models.SettingName_WORKSPACE_PROFILE, setting)
	return err
}

// GetWorkspaceID finds the workspace id in setting ll.workspace.id.
func (s *Store) GetWorkspaceID(ctx context.Context) (string, error) {
	setting, err := s.GetSettingV2(ctx, models.SettingName_WORKSPACE_ID)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get setting %v", models.SettingName_WORKSPACE_ID)
	}
	if setting == nil {
		return "", errors.Errorf("cannot find setting %v", models.SettingName_WORKSPACE_ID)
	}
	return setting.Value, nil
}

// GetSettingV2 returns the setting by name.
func (s *Store) GetSettingV2(ctx context.Context, name models.SettingName) (*SettingMessage, error) {
	if v, ok := s.settingCache.Get(name); ok && s.enableCache {
		return v, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	settings, err := listSettingV2Impl(ctx, tx, &FindSettingMessage{
		Name: &name,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list setting")
	}
	if len(settings) == 0 {
		return nil, nil
	}
	if len(settings) > 1 {
		return nil, errors.Errorf("found multiple settings: %v", name)
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	return settings[0], nil
}

// ListSettingV2 returns a list of settings.
func (s *Store) ListSettingV2(ctx context.Context, find *FindSettingMessage) ([]*SettingMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()
	settings, err := listSettingV2Impl(ctx, tx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list setting")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	for _, setting := range settings {
		s.settingCache.Add(setting.Name, setting)
	}
	return settings, nil
}

func (s *Store) GetSecret(ctx context.Context) (string, error) {
	if s.Secret != "" {
		return s.Secret, nil
	}
	setting, err := s.GetSettingV2(ctx, models.SettingName_AUTH_SECRET)
	if err != nil {
		return "", err
	}
	if setting == nil {
		return "", errors.New("auth secret not found")
	}
	s.Secret = setting.Value
	return setting.Value, nil
}

// UpsertSettingV2 upserts the setting by name.
func (s *Store) UpsertSettingV2(ctx context.Context, update *SetSettingMessage) (*SettingMessage, error) {
	fields := []string{"name", "value"}
	updateFields := []string{"value = EXCLUDED.value"}
	valuePlaceholders, args := []string{"$1", "$2"}, []any{update.Name.String(), update.Value}

	query := `INSERT INTO setting (` + strings.Join(fields, ", ") + `) 
		VALUES (` + strings.Join(valuePlaceholders, ", ") + `) 
		ON CONFLICT (name) DO UPDATE SET ` + strings.Join(updateFields, ", ") + `
		RETURNING name, value`

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	var setting SettingMessage
	var nameString string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(
		&nameString,
		&setting.Value,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, &common.Error{Code: common.NotFound, Err: errors.Errorf("setting not found: %s", update.Name)}
		}
		return nil, err
	}
	value, ok := models.SettingName_value[nameString]
	if !ok {
		return nil, errors.Errorf("invalid setting name string: %s", nameString)
	}
	setting.Name = models.SettingName(value)

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}
	s.settingCache.Add(setting.Name, &setting)
	return &setting, nil
}

// CreateSettingIfNotExistV2 creates a new setting only if the named setting doesn't exist.
func (s *Store) CreateSettingIfNotExistV2(ctx context.Context, create *SettingMessage) (*SettingMessage, bool, error) {
	if v, ok := s.settingCache.Get(create.Name); ok && s.enableCache {
		return v, false, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()
	settings, err := listSettingV2Impl(ctx, tx, &FindSettingMessage{Name: &create.Name})
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to list settings")
	}
	if len(settings) > 1 {
		return nil, false, errors.Errorf("found settings for setting name: %v", create.Name)
	}
	if len(settings) == 1 {
		// Don't create setting if the named setting already exists.
		return settings[0], false, nil
	}

	fields := []string{"name", "value"}
	valuesPlaceholders, args := []string{"$1", "$2"}, []any{create.Name.String(), create.Value}

	query := `INSERT INTO setting (` + strings.Join(fields, ",") + `)
		VALUES (` + strings.Join(valuesPlaceholders, ",") + `)
		RETURNING name, value`
	var setting SettingMessage
	var nameString string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(
		&nameString,
		&setting.Value,
	); err != nil {
		return nil, false, err
	}
	value, ok := models.SettingName_value[nameString]
	if !ok {
		return nil, false, errors.Errorf("invalid setting name string: %s", nameString)
	}
	setting.Name = models.SettingName(value)

	if err := tx.Commit(); err != nil {
		return nil, false, errors.Wrap(err, "failed to commit transaction")
	}
	s.settingCache.Add(setting.Name, &setting)
	return &setting, true, nil
}

// DeleteSettingV2 deletes a setting by the name.
func (s *Store) DeleteSettingV2(ctx context.Context, name models.SettingName) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM setting WHERE name = $1`, name.String()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	s.settingCache.Remove(name)
	return nil
}

func listSettingV2Impl(ctx context.Context, txn *sql.Tx, find *FindSettingMessage) ([]*SettingMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if v := find.Name; v != nil {
		where, args = append(where, fmt.Sprintf("name = $%d", len(args)+1)), append(args, v.String())
	}
	rows, err := txn.QueryContext(ctx, `
		SELECT
			name,
			value
		FROM setting
		WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settingMessages []*SettingMessage
	for rows.Next() {
		var settingMessage SettingMessage
		var nameString string
		if err := rows.Scan(
			&nameString,
			&settingMessage.Value,
		); err != nil {
			return nil, err
		}
		value, ok := models.SettingName_value[nameString]
		if !ok {
			return nil, errors.Errorf("invalid setting name string: %s", nameString)
		}
		settingMessage.Name = models.SettingName(value)
		settingMessages = append(settingMessages, &settingMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return settingMessages, nil
}

// RequireEmailVerification reports whether self-service signup must verify the
// email address by clicking a link before the account can sign in. It only
// takes effect when signup itself is enabled (disallow_signup false); a nil
// (unset) value means enabled, which is the default.
func RequireEmailVerification(setting *models.WorkspaceProfileSetting) bool {
	return setting == nil || setting.RequireEmailVerification == nil || *setting.RequireEmailVerification
}

// GetSMTPSetting returns the workspace SMTP payload. An empty Host means the
// mail service is not configured.
func (s *Store) GetSMTPSetting(ctx context.Context) (*models.SMTPSetting, error) {
	return getSettingPayload[*models.SMTPSetting](ctx, s, models.SettingName_SMTP_CONFIG)
}

// UpsertSMTPSetting stores the workspace SMTP payload.
func (s *Store) UpsertSMTPSetting(ctx context.Context, cfg *models.SMTPSetting) error {
	_, err := upsertSettingValue(ctx, s, models.SettingName_SMTP_CONFIG, cfg)
	return err
}
