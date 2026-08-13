package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

var systemBotUser = &UserMessage{
	ID:     common.SystemBotID,
	Handle: common.SystemBotHandle,
	Name:   "SYSTEM_BOT",
	Email:  "SYSTEM_BOT@example.com",
	Type:   models.PrincipalType_SYSTEM_BOT,
}

// FindUserMessage is the message for finding users.
type FindUserMessage struct {
	ID          *int
	Email       *string
	Handle      *string
	ShowDeleted bool
	// ExcludeSystemBot hides the internal SYSTEM_BOT account (id=1). Only the
	// user-service API sets it (unless the caller opts in); store-internal
	// lookups (agent-DM owner resolution, resolveUserName) keep the default
	// false so they can still read the system bot.
	ExcludeSystemBot bool
	Type             *models.PrincipalType
	Limit            *int
	Offset           *int
	Filter           *ListResourceFilter
	ProjectID        *string
}

// UpdateUserMessage is the message to update a user.
type UpdateUserMessage struct {
	Email           *string
	Name            *string
	PasswordHash    *string
	Delete          *bool
	Profile         *models.UserProfile
	Phone           *string
	Description     *string
	ChatPreferences *models.ChatPreferences
	// EmailVerifiedAt marks the account as verified. Email verification is the
	// self-service signup path; an SSO login also vouches for the address and
	// sets it. Nil leaves the value unchanged.
	EmailVerifiedAt *time.Time
}

// UserMessage is the message for an user.
type UserMessage struct {
	ID int
	// Handle is the user's human-readable, unique mention id ("ran-user-1"),
	// generated at creation and immutable thereafter. The {user} segment of the
	// users/{handle} resource name and the value typed after "@" to mention/DM.
	Handle string
	// Email must be lower case.
	Email         string
	Name          string
	Type          models.PrincipalType
	PasswordHash  string
	MemberDeleted bool
	Profile       *models.UserProfile
	// Phone conforms E.164 format.
	Phone string
	// output only
	CreatedAt time.Time
	// Groups are the full group resource names the user belongs to
	// ("groups/{email}" when the group has an email, else "groups/{id}").
	Groups []string
	// Description is the user-authored self-description surfaced in channel/thread
	// rosters so agents and other users can perceive who this user is.
	Description string
	// AvatarS3Key is the S3 object key of the user's uploaded avatar image, empty
	// when the user has not uploaded one (the frontend renders a pixel identicon).
	AvatarS3Key string
	// ChatPreferences holds per-user chat composer preferences. A nil pointer
	// means "unset" (the column is NULL): the API layer surfaces the default
	// (enter_to_send = true) so the historic behavior is preserved until the
	// user explicitly customizes it.
	ChatPreferences *models.ChatPreferences
	// EmailVerifiedAt marks the account as verified. It is set when the user
	// confirms the address via the signup verification email link, or when an
	// SSO login vouches for the address. Nil means the account cannot sign in
	// with a password yet.
	EmailVerifiedAt *time.Time
}

// GetResourceID returns the stable per-user resource name used to key
// context-derived identifiers such as per-user rate-limit buckets.
func (m *UserMessage) GetResourceID() string {
	return common.FormatUserHandle(m.Handle)
}

type UserStat struct {
	Type    models.PrincipalType
	Deleted bool
	Count   int
}

// queryRower abstracts *sql.DB and *sql.Tx for handle reservation.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// GetSystemBotUser gets the system bot.
func (s *Store) GetSystemBotUser(ctx context.Context) *UserMessage {
	user, err := s.GetUserByID(ctx, common.SystemBotID)
	if err != nil {
		slog.Error("failed to find system bot", slog.Int("id", common.SystemBotID), log.WithError(err))
		return systemBotUser
	}
	if user == nil {
		return systemBotUser
	}
	return user
}

// cacheActiveUser stores a user in both the ID and email caches only when it is
// not soft-deleted. The LRU must never serve a deleted user: a soft-deleted
// email frees up for reuse (the idx_principal_unique_email index is partial on
// deleted=FALSE), and callers must see MemberDeleted=true for deleted users,
// which requires a fresh DB read rather than a stale cached active copy.
func (s *Store) cacheActiveUser(user *UserMessage) {
	if user == nil || user.MemberDeleted || user.Handle == "" {
		return
	}
	s.userIDCache.Add(user.ID, user)
	s.userEmailCache.Add(user.Email, user)
	s.userHandleCache.Add(user.Handle, user)
}

// invalidateUserCache evicts a user from both caches by its previous id/email,
// used on delete/restore/email-change so the next lookup re-reads from the DB.
func (s *Store) invalidateUserCache(id int, email string) {
	if cached, ok := s.userIDCache.Get(id); ok {
		s.userHandleCache.Remove(cached.Handle)
	}
	s.userIDCache.Remove(id)
	s.userEmailCache.Remove(email)
}

// findUser runs a single-user lookup via listUserImpl in a read transaction and
// returns the match (or nil when absent). It is the point-query path used on a
// cache miss so that resolving one user does not trigger a full-table load.
// ShowDeleted is forwarded so deleted users are still resolvable, with
// MemberDeleted set, without being cached.
func (s *Store) findUser(ctx context.Context, find *FindUserMessage) (*UserMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	users, err := listUserImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if len(users) == 0 {
		return nil, nil
	}
	return users[0], nil
}

// GetUserByID gets the user by ID. A cache hit returns the active cached copy;
// a miss falls back to a point query (not a full-table load), which resolves
// soft-deleted users with MemberDeleted=true but does not cache them.
func (s *Store) GetUserByID(ctx context.Context, id int) (*UserMessage, error) {
	if v, ok := s.userIDCache.Get(id); ok && s.enableCache {
		return v, nil
	}

	user, err := s.findUser(ctx, &FindUserMessage{ID: &id, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	s.cacheActiveUser(user)
	return user, nil
}

// GetUserByEmail gets the user by email. A cache hit returns the active cached
// copy; a miss falls back to a point query (not a full-table load), which
// resolves soft-deleted users with MemberDeleted=true but does not cache them.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*UserMessage, error) {
	if v, ok := s.userEmailCache.Get(email); ok && s.enableCache {
		return v, nil
	}

	user, err := s.findUser(ctx, &FindUserMessage{Email: &email, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	s.cacheActiveUser(user)
	return user, nil
}

// GetUserByHandle gets the user by handle ("ran-user-1"). A cache hit returns
// the active cached copy; a miss falls back to a point query (not a full-table
// load), which resolves soft-deleted users with MemberDeleted=true but does not
// cache them.
func (s *Store) GetUserByHandle(ctx context.Context, handle string) (*UserMessage, error) {
	if v, ok := s.userHandleCache.Get(handle); ok && s.enableCache {
		return v, nil
	}

	user, err := s.findUser(ctx, &FindUserMessage{Handle: &handle, ShowDeleted: true})
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	s.cacheActiveUser(user)
	return user, nil
}

// GetUserByIdentifier resolves a users/{identifier} token — a handle when the
// token contains no '@', an email otherwise (the users/{email} SCIM lookup
// alias). Returns nil when no principal carries that identifier.
func (s *Store) GetUserByIdentifier(ctx context.Context, identifier string) (*UserMessage, error) {
	if strings.Contains(identifier, "@") {
		return s.GetUserByEmail(ctx, identifier)
	}
	return s.GetUserByHandle(ctx, identifier)
}

func (s *Store) StatUsers(ctx context.Context) ([]*UserStat, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
	SELECT
		COUNT(*),
		type,
		deleted
	FROM principal
	GROUP BY type, deleted`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []*UserStat

	for rows.Next() {
		var stat UserStat
		var typeString string
		if err := rows.Scan(
			&stat.Count,
			&typeString,
			&stat.Deleted,
		); err != nil {
			return nil, err
		}
		if typeValue, ok := models.PrincipalType_value[typeString]; ok {
			stat.Type = models.PrincipalType(typeValue)
		} else {
			return nil, errors.Errorf("invalid principal type string: %s", typeString)
		}
		stats = append(stats, &stat)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to scan rows")
	}

	return stats, nil
}

// ListUsers list users.
func (s *Store) ListUsers(ctx context.Context, find *FindUserMessage) ([]*UserMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	users, err := listUserImpl(ctx, tx, find)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for _, user := range users {
		s.cacheActiveUser(user)
	}
	return users, nil
}

// buildListUsersQuery assembles the ListUsers SQL statement and its positional
// parameters from the find message. It is split out from listUserImpl so the
// parameterization of user-controlled values (notably the project filter) can
// be unit-tested without a database: every user-controlled value must appear in
// args, never interpolated into the query text.
func buildListUsersQuery(find *FindUserMessage) (string, []any) {
	where, args := []string{"TRUE"}, []any{}
	if filter := find.Filter; filter != nil {
		where = append(where, filter.Where)
		args = append(args, filter.Args...)
	}
	if v := find.ID; v != nil {
		where, args = append(where, fmt.Sprintf("principal.id = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.Handle; v != nil {
		where, args = append(where, fmt.Sprintf("principal.handle = $%d", len(args)+1)), append(args, *v)
	}
	if v := find.Email; v != nil {
		if *v == common.AllUsers {
			where, args = append(where, fmt.Sprintf("principal.email = $%d", len(args)+1)), append(args, *v)
		} else {
			where, args = append(where, fmt.Sprintf("principal.email = $%d", len(args)+1)), append(args, strings.ToLower(*v))
		}
	}
	if v := find.Type; v != nil {
		where, args = append(where, fmt.Sprintf("principal.type = $%d", len(args)+1)), append(args, v.String())
	}
	// The internal SYSTEM_BOT account is hidden unless the caller explicitly
	// opts in; even a user_type filter naming SYSTEM_BOT is overridden so the
	// exclusion is unconditional.
	if find.ExcludeSystemBot {
		where, args = append(where, fmt.Sprintf("principal.type != $%d", len(args)+1)), append(args, models.PrincipalType_SYSTEM_BOT.String())
	}
	if !find.ShowDeleted {
		where, args = append(where, fmt.Sprintf("principal.deleted = $%d", len(args)+1)), append(args, false)
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
		join = fmt.Sprintf(`INNER JOIN project_members ON (CONCAT('users/', principal.id) = ANY(project_members.members) OR '%s' = ANY(project_members.members))`, common.AllUsers)
	}

	// Join the user_group table to find groups for each user.
	// The user will be stored in the user_group.payload.members.member field, the member is in the "users/{handle}" format
	if strings.HasPrefix(with, "WITH") {
		with += ","
	} else {
		with = "WITH"
	}
	query := with + ` user_groups AS (
		SELECT
			principal.id AS user_id,
			COALESCE(ARRAY_AGG(
				CASE WHEN user_group.email IS NOT NULL
					THEN 'groups/' || user_group.email
					ELSE 'groups/' || user_group.id
				END
				ORDER BY user_group.email
			) FILTER (WHERE user_group.id IS NOT NULL), '{}') AS groups
		FROM principal
		LEFT JOIN user_group ON EXISTS (
			SELECT 1 FROM jsonb_array_elements(user_group.payload->'members') AS m
			WHERE m->>'member' = CONCAT('users/', principal.id)
		)
		GROUP BY principal.id
	)
	SELECT
		principal.id AS user_id,
		principal.deleted,
		principal.email,
		principal.name,
		principal.handle,
		principal.type,
		principal.password_hash,
		principal.phone,
		principal.profile,
		principal.created_at,
		user_groups.groups,
		principal.description,
		principal.avatar_s3_key,
		principal.chat_preferences,
		principal.email_verified_at
	FROM principal
	INNER JOIN user_groups ON principal.id = user_groups.user_id
	` + join + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY type DESC, created_at ASC`

	if v := find.Limit; v != nil {
		query += fmt.Sprintf(" LIMIT %d", *v)
	}
	if v := find.Offset; v != nil {
		query += fmt.Sprintf(" OFFSET %d", *v)
	}
	return query, args
}

func listUserImpl(ctx context.Context, txn *sql.Tx, find *FindUserMessage) ([]*UserMessage, error) {
	query, args := buildListUsersQuery(find)

	var userMessages []*UserMessage
	rows, err := txn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userMessage UserMessage
		var profileBytes []byte
		var chatPrefBytes []byte
		var typeString string
		var groups pq.StringArray
		var emailVerifiedAt sql.NullTime
		if err := rows.Scan(
			&userMessage.ID,
			&userMessage.MemberDeleted,
			&userMessage.Email,
			&userMessage.Name,
			&userMessage.Handle,
			&typeString,
			&userMessage.PasswordHash,
			&userMessage.Phone,
			&profileBytes,
			&userMessage.CreatedAt,
			&groups,
			&userMessage.Description,
			&userMessage.AvatarS3Key,
			&chatPrefBytes,
			&emailVerifiedAt,
		); err != nil {
			return nil, err
		}
		if emailVerifiedAt.Valid {
			userMessage.EmailVerifiedAt = &emailVerifiedAt.Time
		}
		userMessage.Groups = []string(groups)
		if typeValue, ok := models.PrincipalType_value[typeString]; ok {
			userMessage.Type = models.PrincipalType(typeValue)
		} else {
			return nil, errors.Errorf("invalid user type string: %s", typeString)
		}

		profile := models.UserProfile{}
		if err := json.Unmarshal(profileBytes, &profile); err != nil {
			return nil, err
		}
		userMessage.Profile = &profile

		if len(chatPrefBytes) > 0 {
			chatPrefs := &models.ChatPreferences{}
			if err := json.Unmarshal(chatPrefBytes, chatPrefs); err != nil {
				return nil, err
			}
			userMessage.ChatPreferences = chatPrefs
		}

		userMessages = append(userMessages, &userMessage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return userMessages, nil
}

// CreateUser creates an user with a freshly generated, immutable handle
// ("ran-user-1"): the slugified display name plus a per-slug counter. A
// concurrent creation of a same-slug user is detected via the unique handle
// index and retried with the next free number. The SYSTEM_BOT principal is
// the sole exception and always gets the reserved "system-bot" handle.
func (s *Store) CreateUser(ctx context.Context, create *UserMessage) (*UserMessage, error) {
	// Double check the passing-in emails.
	// We use lower-case for emails.
	if create.Email != strings.ToLower(create.Email) {
		return nil, errors.Errorf("emails must be lower-case when they are passed into store")
	}

	if create.Profile == nil {
		create.Profile = &models.UserProfile{}
	}
	profileBytes, err := json.Marshal(create.Profile)
	if err != nil {
		return nil, err
	}

	slug := common.SlugifyHandle(create.Name)
	if slug == "" {
		slug = "user"
	}

	for attempt := 0; attempt < 32; attempt++ {
		handle := common.SystemBotHandle
		if create.Type != models.PrincipalType_SYSTEM_BOT {
			handle, err = s.reserveHandle(ctx, s.GetDB(), "principal", "handle", slug, common.HandleKindUser)
			if err != nil {
				return nil, err
			}
		}

		tx, err := s.GetDB().BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}

		var userID int
		err = tx.QueryRowContext(ctx, `
			INSERT INTO principal (
				handle, email, name, type, password_hash, phone, profile, description, email_verified_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (handle) DO NOTHING
			RETURNING id, created_at
		`,
			handle,
			create.Email,
			create.Name,
			create.Type.String(),
			create.PasswordHash,
			create.Phone,
			profileBytes,
			create.Description,
			create.EmailVerifiedAt,
		).Scan(&userID, &create.CreatedAt)
		if err == sql.ErrNoRows {
			// A concurrent creation claimed the same handle; roll back and retry
			// with the next free number.
			if rbErr := tx.Rollback(); rbErr != nil {
				return nil, errors.Wrap(rbErr, "failed to rollback handle collision")
			}
			continue
		}
		if err != nil {
			_ = tx.Rollback()
			if isUniqueViolation(err) {
				return nil, errors.Errorf("user with email %q already exists", create.Email)
			}
			return nil, err
		}

		if err := tx.Commit(); err != nil {
			return nil, err
		}

		user := &UserMessage{
			ID:              userID,
			Handle:          handle,
			Email:           create.Email,
			Name:            create.Name,
			Type:            create.Type,
			PasswordHash:    create.PasswordHash,
			Phone:           create.Phone,
			CreatedAt:       create.CreatedAt,
			Profile:         create.Profile,
			Description:     create.Description,
			EmailVerifiedAt: create.EmailVerifiedAt,
		}
		s.cacheActiveUser(user)
		return user, nil
	}
	return nil, errors.New("failed to allocate a unique user handle after 32 attempts")
}

// reserveHandle returns the next free "slug-kind-N" handle for the given
// table/column by counting existing handles with the same slug prefix, then
// verifying the candidate is still free. The count and the verification run in
// the caller's transaction; a concurrent creation that lands between them is
// caught by the unique index at INSERT time and retried by the caller.
func (*Store) reserveHandle(ctx context.Context, q queryRower, table, column, slug, kind string) (string, error) {
	prefix := likeEscape(slug) + "-" + kind + "-%"
	for i := 0; i < 32; i++ {
		var n int
		if err := q.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s WHERE %s LIKE $1 ESCAPE '\\'", table, column), prefix).Scan(&n); err != nil {
			return "", err
		}
		handle := common.FormatHandle(slug, kind, n+1)
		var one int
		err := q.QueryRowContext(ctx, fmt.Sprintf("SELECT 1 FROM %s WHERE %s = $1", table, column), handle).Scan(&one)
		switch {
		case err == sql.ErrNoRows:
			return handle, nil
		case err != nil:
			return "", err
		}
	}
	return "", errors.Errorf("failed to reserve %s handle for slug %q after 32 attempts", kind, slug)
}

// likeEscape escapes LIKE wildcards so a slug is matched literally.
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// UpdateUser updates a user.
func (s *Store) UpdateUser(ctx context.Context, currentUser *UserMessage, patch *UpdateUserMessage) (*UserMessage, error) {
	if currentUser.ID == common.SystemBotID {
		return nil, errors.Errorf("cannot update system bot")
	}

	principalSet, principalArgs := []string{}, []any{}
	if v := patch.Delete; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("deleted = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.EmailVerifiedAt; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("email_verified_at = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.Email; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("email = $%d", len(principalArgs)+1)), append(principalArgs, strings.ToLower(*v))
	}
	if v := patch.Name; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("name = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.PasswordHash; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("password_hash = $%d", len(principalArgs)+1)), append(principalArgs, *v)
		if patch.Profile == nil {
			patch.Profile = currentUser.Profile
			patch.Profile.LastChangePasswordTime = timestamppb.New(time.Now())
		}
	}
	if v := patch.Phone; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("phone = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.Profile; v != nil {
		profileBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("profile = $%d", len(principalArgs)+1)), append(principalArgs, profileBytes)
	}
	if v := patch.Description; v != nil {
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("description = $%d", len(principalArgs)+1)), append(principalArgs, *v)
	}
	if v := patch.ChatPreferences; v != nil {
		chatPrefsBytes, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		principalSet, principalArgs = append(principalSet, fmt.Sprintf("chat_preferences = $%d", len(principalArgs)+1)), append(principalArgs, chatPrefsBytes)
	}
	principalArgs = append(principalArgs, currentUser.ID)

	if len(principalSet) == 0 {
		return currentUser, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE principal
		SET `+strings.Join(principalSet, ", ")+`
		WHERE id = $%d
	`, len(principalArgs)),
		principalArgs...,
	); err != nil {
		if isUniqueViolation(err) {
			if patch.Email != nil {
				return nil, errors.Errorf("user with email %q already exists", strings.ToLower(*patch.Email))
			}
			return nil, errors.Errorf("user already exists")
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.invalidateUserCache(currentUser.ID, currentUser.Email)
	user, err := s.GetUserByID(ctx, currentUser.ID)
	if err != nil {
		return nil, err
	}

	s.cacheActiveUser(user)
	return user, nil
}

// UpdateUserAvatarS3Key sets the user's avatar S3 object key. Pass an empty
// key to clear the avatar. It invalidates the user cache so callers see the
// change immediately.
func (s *Store) UpdateUserAvatarS3Key(ctx context.Context, uid int, key string) error {
	if _, err := s.GetDB().ExecContext(ctx, `
		UPDATE principal
		SET avatar_s3_key = $1
		WHERE id = $2`, key, uid); err != nil {
		return errors.Wrap(err, "failed to update user avatar s3 key")
	}
	if cached, ok := s.userIDCache.Get(uid); ok {
		s.invalidateUserCache(uid, cached.Email)
	} else {
		s.userIDCache.Remove(uid)
	}
	return nil
}
