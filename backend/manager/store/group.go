package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// FindGroupMessage is the message for finding groups.
type FindGroupMessage struct {
	ID        *string
	Email     *string
	ProjectID *string
	Filter    *ListResourceFilter

	Limit  *int
	Offset *int
}

// UpdateGroupMessage is the message to update a group.
type UpdateGroupMessage struct {
	Title       *string
	Description *string
	Payload     *models.GroupPayload
}

// GroupMessage is the message for a group. ID is the stable primary key; Email
// is optional (external/SCIM groups may have no email). IAM bindings may
// reference a group as groups/{id} or, when it has one, groups/{email}.
type GroupMessage struct {
	ID          string
	Email       string
	Title       string
	Description string
	Payload     *models.GroupPayload
}

// GetGroup gets a group by email.
func (s *Store) GetGroup(ctx context.Context, email string) (*GroupMessage, error) {
	groups, err := s.ListGroups(ctx, &FindGroupMessage{Email: &email})
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, nil
	} else if len(groups) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d groups with email %+v, expect 1", len(groups), email)}
	}
	return groups[0], nil
}

// GetGroupByID gets a group by its stable id.
func (s *Store) GetGroupByID(ctx context.Context, id string) (*GroupMessage, error) {
	groups, err := s.ListGroups(ctx, &FindGroupMessage{ID: &id})
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, nil
	} else if len(groups) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d groups with id %+v, expect 1", len(groups), id)}
	}
	return groups[0], nil
}

// GetGroupByName resolves a group by its resource name "groups/{identifier}",
// where the identifier is either the group email (contains "@") or its id.
// Lookups try the matching identifier first, then fall back to the other
// (legacy SCIM sync may have stored an email as the id).
func (s *Store) GetGroupByName(ctx context.Context, name string) (*GroupMessage, error) {
	token, err := common.GetGroupEmail(name)
	if err != nil {
		return nil, err
	}
	if strings.Contains(token, "@") {
		if group, err := s.GetGroup(ctx, token); err != nil || group != nil {
			return group, err
		}
		return s.GetGroupByID(ctx, token)
	}
	if group, err := s.GetGroupByID(ctx, token); err != nil || group != nil {
		return group, err
	}
	return s.GetGroup(ctx, token)
}

// ListGroups list all groups.
func (s *Store) ListGroups(ctx context.Context, find *FindGroupMessage) ([]*GroupMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	groups, err := s.listGroupImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, group := range groups {
		s.groupCache.Add(group.ID, group)
	}
	return groups, nil
}

func (*Store) listGroupImpl(ctx context.Context, txn *sql.Tx, find *FindGroupMessage) ([]*GroupMessage, error) {
	where, args := []string{"TRUE"}, []any{}
	if filter := find.Filter; filter != nil {
		where = append(where, filter.Where)
		args = append(args, filter.Args...)
	}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.Email; v != nil {
		where, args = append(where, fmt.Sprintf("email = $%d", len(args)+1)), append(args, *v)
	}

	var with, join string
	if v := find.ProjectID; v != nil {
		// *v is user-controlled (CEL `project == "projects/{x}"`) and must never be
		// interpolated into the SQL text. PostgreSQL positional placeholders ($N)
		// map to args by index, not by textual position, so a placeholder in the
		// WITH clause can safely reference an arg appended after the WHERE-clause
		// args. The resource_type/type literals are enum constants, not user input.
		placeholder := len(args) + 1
		args = append(args, "projects/"+*v)
		with = fmt.Sprintf(`WITH all_members AS (
			SELECT
				jsonb_array_elements_text(jsonb_array_elements(policy.payload->'bindings')->'members') AS member,
				jsonb_array_elements(policy.payload->'bindings')->>'role' AS role
			FROM policy
			WHERE ((resource_type = '%s' AND resource = $%d) OR resource_type = '%s') AND type = '%s'
		),
		project_members AS (
			SELECT ARRAY_AGG(member) AS members FROM all_members WHERE role NOT LIKE 'roles/workspace%%'
		)`, models.Policy_PROJECT.String(), placeholder, models.Policy_WORKSPACE.String(), models.Policy_IAM.String())
		join = fmt.Sprintf(`INNER JOIN project_members ON (
			CONCAT('groups/', user_group.id) = ANY(project_members.members)
			OR (user_group.email IS NOT NULL AND CONCAT('groups/', user_group.email) = ANY(project_members.members))
			OR '%s' = ANY(project_members.members)
		)`, common.AllUsers)
	}

	query := with + `
	SELECT
		user_group.id,
		user_group.email,
		user_group.name,
		user_group.description,
		user_group.payload
	FROM user_group ` + join + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY name, email`
	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}

	var groups []*GroupMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var group GroupMessage
		var email sql.NullString
		var payload []byte
		if err := rows.Scan(
			&group.ID,
			&email,
			&group.Title,
			&group.Description,
			&payload,
		); err != nil {
			return nil, err
		}
		if email.Valid {
			group.Email = email.String
		}
		groupPayload := models.GroupPayload{}
		if err := common.ProtojsonUnmarshaler.Unmarshal(payload, &groupPayload); err != nil {
			return nil, err
		}
		group.Payload = &groupPayload
		groups = append(groups, &group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

// CreateGroup creates a group. ID may be empty (a UUID is generated); Email may
// be empty for groups without an email.
func (s *Store) CreateGroup(ctx context.Context, create *GroupMessage) (*GroupMessage, error) {
	if create.Payload == nil {
		create.Payload = &models.GroupPayload{}
	}
	payloadBytes, err := protojson.Marshal(create.Payload)
	if err != nil {
		return nil, err
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin tx")
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO user_group (id, email, name, description, payload)
		VALUES (COALESCE(NULLIF($1, ''), gen_random_uuid()::text), NULLIF($2, ''), $3, $4, $5)
		RETURNING id
	`, create.ID, create.Email, create.Title, create.Description, payloadBytes).Scan(&create.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit")
	}

	s.groupCache.Add(create.ID, create)
	return create, nil
}

// UpdateGroup updates a group by its stable id.
func (s *Store) UpdateGroup(ctx context.Context, id string, patch *UpdateGroupMessage) (*GroupMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to begin transaction")
	}

	set, args := []string{}, []any{}
	if v := patch.Title; v != nil {
		set, args = append(set, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Description; v != nil {
		set, args = append(set, fmt.Sprintf("description = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Payload; v != nil {
		payload, err := protojson.Marshal(v)
		if err != nil {
			return nil, err
		}
		set, args = append(set, fmt.Sprintf("payload = $%d", len(args)+1)), append(args, payload)
	}
	if len(set) == 0 {
		return nil, errors.New("no fields to update")
	}
	args = append(args, id)

	var group GroupMessage
	var email sql.NullString
	var payload []byte
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE user_group
		SET %s
		WHERE id = $%d
		RETURNING
			id,
			email,
			name,
			description,
			payload
		`, strings.Join(set, ", "), len(set)+1), args...).Scan(
		&group.ID,
		&email,
		&group.Title,
		&group.Description,
		&payload,
	); err != nil {
		return nil, err
	}
	if email.Valid {
		group.Email = email.String
	}
	groupPayload := models.GroupPayload{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, &groupPayload); err != nil {
		return nil, err
	}
	group.Payload = &groupPayload

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit transaction")
	}

	s.groupCache.Add(group.ID, &group)
	return &group, nil
}

// DeleteGroup deletes a group by its stable id.
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_group WHERE id = $1`, id); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transaction")
	}

	s.groupCache.Remove(id)
	return nil
}
