package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// RoleMessage is the message for roles. A role is a named bundle of permissions.
// Predefined roles are defined in Go (predefined_roles.go) and never stored in
// the DB; custom roles live in the role table. Both are resolved identically by
// the IAM manager during CheckPermission.
type RoleMessage struct {
	ResourceID  string
	Name        string
	Description string
	Permissions map[permission.Permission]bool
	Predefined  bool
}

// FindRoleMessage is the message for finding roles.
type FindRoleMessage struct {
	ResourceID *string
}

// UpdateRoleMessage is the message for updating roles.
type UpdateRoleMessage struct {
	ResourceID string

	Name        *string
	Description *string
	Permissions *map[permission.Permission]bool
}

// CreateRole creates a new custom role.
func (s *Store) CreateRole(ctx context.Context, create *RoleMessage) (*RoleMessage, error) {
	p := &models.RolePermissions{}
	for k := range create.Permissions {
		p.Permissions = append(p.Permissions, k)
	}
	permissionBytes, err := protojson.Marshal(p)
	if err != nil {
		return nil, err
	}

	if _, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO role (resource_id, name, description, permissions)
		VALUES ($1, $2, $3, $4)
	`,
		create.ResourceID,
		create.Name,
		create.Description,
		permissionBytes,
	); err != nil {
		return nil, err
	}
	s.rolesCache.Add(create.ResourceID, create)
	return create, nil
}

// GetRole returns a role by ID with strong/consistent reads (no cache). Custom
// roles are read from the DB; predefined roles are served from
// GetPredefinedRole without touching the DB.
func (s *Store) GetRole(ctx context.Context, find *FindRoleMessage) (*RoleMessage, error) {
	roles, err := s.ListRoles(ctx, find)
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return nil, nil
	}
	if len(roles) > 1 {
		return nil, &common.Error{Code: common.Conflict, Err: errors.Errorf("found %d roles with filter %+v, expect 1", len(roles), find)}
	}
	role := roles[0]
	s.rolesCache.Add(role.ResourceID, role)
	return role, nil
}

// GetRoleSnapshot returns a role by ID with snapshot reads (with cache). Trades
// consistency for performance. Predefined roles resolve in-memory first.
func (s *Store) GetRoleSnapshot(ctx context.Context, resourceID string) (*RoleMessage, error) {
	if role := GetPredefinedRole(resourceID); role != nil {
		return role, nil
	}
	if v, ok := s.rolesCache.Get(resourceID); ok && s.enableCache {
		return v, nil
	}
	return s.GetRole(ctx, &FindRoleMessage{ResourceID: &resourceID})
}

// ListRoles returns a list of roles. When ResourceID is set, predefined roles are
// consulted first; when listing all, predefined roles are appended to the DB
// result.
func (s *Store) ListRoles(ctx context.Context, find *FindRoleMessage) ([]*RoleMessage, error) {
	if v := find.ResourceID; v != nil {
		if role := GetPredefinedRole(*v); role != nil {
			return []*RoleMessage{role}, nil
		}
	}

	query := "SELECT resource_id, name, description, permissions FROM role"
	args := []any{}
	if v := find.ResourceID; v != nil {
		query += " WHERE resource_id = $1"
		args = append(args, *v)
	}

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []*RoleMessage
	for rows.Next() {
		role := &RoleMessage{Permissions: map[permission.Permission]bool{}}
		var permissionBytes []byte
		if err := rows.Scan(&role.ResourceID, &role.Name, &role.Description, &permissionBytes); err != nil {
			return nil, err
		}
		var rolePermissions models.RolePermissions
		if err := common.ProtojsonUnmarshaler.Unmarshal(permissionBytes, &rolePermissions); err != nil {
			return nil, err
		}
		for _, v := range rolePermissions.Permissions {
			role.Permissions[v] = true
		}
		s.rolesCache.Add(role.ResourceID, role)
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if find.ResourceID == nil {
		roles = append(roles, PredefinedRoles...)
	}
	return roles, nil
}

// UpdateRole updates an existing custom role.
func (s *Store) UpdateRole(ctx context.Context, patch *UpdateRoleMessage) (*RoleMessage, error) {
	set := []string{}
	args := []any{}
	if v := patch.Name; v != nil {
		set, args = append(set, fmt.Sprintf("name = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Description; v != nil {
		set, args = append(set, fmt.Sprintf("description = $%d", len(args)+1)), append(args, *v)
	}
	if v := patch.Permissions; v != nil {
		p := &models.RolePermissions{}
		for k := range *v {
			p.Permissions = append(p.Permissions, k)
		}
		permissionBytes, err := protojson.Marshal(p)
		if err != nil {
			return nil, err
		}
		set, args = append(set, fmt.Sprintf("permissions = $%d", len(args)+1)), append(args, permissionBytes)
	}
	if len(set) == 0 {
		return nil, errors.New("no fields to update")
	}
	args = append(args, patch.ResourceID)

	role := &RoleMessage{ResourceID: patch.ResourceID, Permissions: map[permission.Permission]bool{}}
	var permissionBytes []byte
	if err := s.GetDB().QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE role
		SET %s
		WHERE resource_id = $%d
		RETURNING name, description, permissions
	`, strings.Join(set, ", "), len(args)),
		args...,
	).Scan(&role.Name, &role.Description, &permissionBytes); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	s.rolesCache.Remove(patch.ResourceID)
	var rolePermissions models.RolePermissions
	if err := common.ProtojsonUnmarshaler.Unmarshal(permissionBytes, &rolePermissions); err != nil {
		return nil, err
	}
	for _, v := range rolePermissions.Permissions {
		role.Permissions[v] = true
	}
	s.rolesCache.Add(role.ResourceID, role)
	return role, nil
}

// DeleteRole deletes an existing custom role.
func (s *Store) DeleteRole(ctx context.Context, resourceID string) error {
	if _, err := s.GetDB().ExecContext(ctx, `DELETE FROM role WHERE resource_id = $1`, resourceID); err != nil {
		return err
	}
	s.rolesCache.Remove(resourceID)
	return nil
}
