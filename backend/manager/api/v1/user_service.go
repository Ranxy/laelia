package v1

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celoperators "github.com/google/cel-go/common/operators"
	celoverloads "github.com/google/cel-go/common/overloads"
	"github.com/pkg/errors"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/mailer"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/Ranxy/laelia/backend/manager/utils"
)

// UserService implements the user service.
type UserService struct {
	v1connect.UnimplementedUserServiceHandler
	store    *store.Store
	profile  *config.Profile
	stateCfg *state.State
	iam      *iam.Manager
	s3client *s3client.Client
	mailer   *mailer.Sender
}

// NewUserService creates a new UserService.
func NewUserService(store *store.Store, profile *config.Profile, stateCfg *state.State, iamManager *iam.Manager, s3clientManager *s3client.Client, mailerSender *mailer.Sender) *UserService {
	return &UserService{
		store:    store,
		profile:  profile,
		stateCfg: stateCfg,
		iam:      iamManager,
		s3client: s3clientManager,
		mailer:   mailerSender,
	}
}

// GetUser gets a user.
func (s *UserService) GetUser(ctx context.Context, request *connect.Request[v1pb.GetUserRequest]) (*connect.Response[v1pb.User], error) {
	identifier, err := common.GetUserHandle(request.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", identifier))
	}
	// The internal SYSTEM_BOT account is only reachable through ListUsers with
	// include_system_bot set (the settings user directory); direct lookups are
	// hidden so no other surface can resolve or display it.
	if user.Type == storepb.PrincipalType_SYSTEM_BOT {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", identifier))
	}
	callerID, isAdmin, err := callerPhoneVisibility(ctx, s.store)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(convertToUser(user, !isAdmin && user.ID != callerID)), nil
}

// BatchGetUsers get users in batch.
func (s *UserService) BatchGetUsers(ctx context.Context, request *connect.Request[v1pb.BatchGetUsersRequest]) (*connect.Response[v1pb.BatchGetUsersResponse], error) {
	response := &v1pb.BatchGetUsersResponse{}
	for _, name := range request.Msg.Names {
		user, err := s.GetUser(ctx, connect.NewRequest(&v1pb.GetUserRequest{Name: name}))
		if err != nil {
			return nil, err
		}
		response.Users = append(response.Users, user.Msg)
	}
	return connect.NewResponse(response), nil
}

// GetCurrentUser gets the current authenticated user.
func (s *UserService) GetCurrentUser(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.User], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("authenticated user not found"))
	}
	out := convertToUser(user, false)
	// Expose the caller's effective workspace-scope permission set so the
	// frontend can gate actions (laelia.users.update, laelia.agents.create, the
	// review perms, etc.) without re-deriving roles client-side. workspace_admin
	// is kept as a computed shim during the transition. A lookup failure is
	// logged but does not fail the request; the fields default to empty/false.
	perms, err := s.iam.EffectiveWorkspacePermissions(ctx, user)
	if err != nil {
		slog.Error("failed to resolve workspace permissions", log.WithError(err), slog.String("user", user.Email))
	} else {
		out.Permissions = perms
	}
	isAdmin, err := isUserWorkspaceAdmin(ctx, s.store, user)
	if err != nil {
		slog.Error("failed to resolve workspace admin", log.WithError(err), slog.String("user", user.Email))
	}
	out.WorkspaceAdmin = isAdmin
	out.DebugMode = s.profile.RuntimeDebug.Load()
	return connect.NewResponse(out), nil
}

// ListUsers lists all users.
func (s *UserService) ListUsers(ctx context.Context, request *connect.Request[v1pb.ListUsersRequest]) (*connect.Response[v1pb.ListUsersResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   request.Msg.PageToken,
		limit:   int(request.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindUserMessage{
		Limit:            &limitPlusOne,
		Offset:           &offset.offset,
		ShowDeleted:      request.Msg.ShowDeleted,
		ExcludeSystemBot: !request.Msg.IncludeSystemBot,
	}
	if err := parseListUserFilter(find, request.Msg.Filter); err != nil {
		return nil, err
	}
	if v := find.ProjectID; v != nil {
		_, ok := GetUserFromContext(ctx)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("user not found"))
		}
		// TODO check permission
	}

	users, err := s.store.ListUsers(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list user, error: %v", err))
	}

	callerID, isAdmin, err := callerPhoneVisibility(ctx, s.store)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	nextPageToken := ""
	if len(users) == limitPlusOne {
		users = users[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to marshal next page token, error: %v", err))
		}
	}

	response := &v1pb.ListUsersResponse{
		NextPageToken: nextPageToken,
	}
	for _, user := range users {
		response.Users = append(response.Users, convertToUser(user, !isAdmin && user.ID != callerID))
	}
	return connect.NewResponse(response), nil
}

func parseListUserFilter(find *store.FindUserMessage, filter string) error {
	if filter == "" {
		return nil
	}
	e, err := cel.NewEnv()
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Errorf("failed to create cel env"))
	}
	ast, iss := e.Parse(filter)
	if iss != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failed to parse filter %v, error: %v", filter, iss.String()))
	}

	var getFilter func(expr celast.Expr) (string, error)
	var positionalArgs []any

	parseToSQL := func(variable, value any) (string, error) {
		switch variable {
		case "email":
			positionalArgs = append(positionalArgs, value.(string))
			return fmt.Sprintf("principal.email = $%d", len(positionalArgs)), nil
		case "name":
			positionalArgs = append(positionalArgs, value.(string))
			return fmt.Sprintf("principal.name = $%d", len(positionalArgs)), nil
		case "user_type":
			v1UserType, ok := v1pb.UserType_value[value.(string)]
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user type filter %q", value))
			}
			principalType, err := convertToPrincipalType(v1pb.UserType(v1UserType))
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failed to parse the user type %q with error: %v", v1UserType, err))
			}
			positionalArgs = append(positionalArgs, principalType)
			return fmt.Sprintf("principal.type = $%d", len(positionalArgs)), nil
		case "state":
			v1State, ok := v1pb.State_value[value.(string)]
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid state filter %q", value))
			}
			positionalArgs = append(positionalArgs, v1pb.State(v1State) == v1pb.State_DELETED)
			return fmt.Sprintf("principal.deleted = $%d", len(positionalArgs)), nil
		case "project":
			projectID, err := common.GetProjectID(value.(string))
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid project filter %q", value))
			}
			find.ProjectID = &projectID
			return "TRUE", nil
		default:
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupport variable %q", variable))
		}
	}

	parseToUserTypeSQL := func(expr celast.Expr, relation string) (string, error) {
		variable, value := getVariableAndValueFromExpr(expr)
		if variable != "user_type" {
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`only "user_type" support "user_type in [xx]"/"!(user_type in [xx])" operator`))
		}

		rawTypeList, ok := value.([]any)
		if !ok {
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user_type value %q", value))
		}
		if len(rawTypeList) == 0 {
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("empty user_type filter"))
		}
		userTypeList := []string{}
		for _, rawType := range rawTypeList {
			v1UserType, ok := v1pb.UserType_value[rawType.(string)]
			if !ok {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user type filter %q", rawType))
			}
			principalType, err := convertToPrincipalType(v1pb.UserType(v1UserType))
			if err != nil {
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("failed to parse the user type %q with error: %v", v1UserType, err))
			}
			positionalArgs = append(positionalArgs, principalType)
			userTypeList = append(userTypeList, fmt.Sprintf("$%d", len(positionalArgs)))
		}

		return fmt.Sprintf("principal.type %s (%s)", relation, strings.Join(userTypeList, ",")), nil
	}

	getFilter = func(expr celast.Expr) (string, error) {
		switch expr.Kind() {
		case celast.CallKind:
			functionName := expr.AsCall().FunctionName()
			switch functionName {
			case celoperators.LogicalOr:
				return getSubConditionFromExpr(expr, getFilter, "OR")
			case celoperators.LogicalAnd:
				return getSubConditionFromExpr(expr, getFilter, "AND")
			case celoperators.Equals:
				variable, value := getVariableAndValueFromExpr(expr)
				return parseToSQL(variable, value)
			case celoverloads.Matches:
				variable := expr.AsCall().Target().AsIdent()
				args := expr.AsCall().Args()
				if len(args) != 1 {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`invalid args for %q`, variable))
				}
				value := args[0].AsLiteral().Value()
				if variable != "name" && variable != "email" {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`only "name" and "email" support %q operator, but found %q`, celoverloads.Matches, variable))
				}
				strValue, ok := value.(string)
				if !ok {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("expect string, got %T, hint: filter literals should be string", value))
				}
				positionalArgs = append(positionalArgs, "%"+strings.ToLower(strValue)+"%")
				return fmt.Sprintf("LOWER(principal.%s) LIKE $%d", variable, len(positionalArgs)), nil
			case celoperators.In:
				return parseToUserTypeSQL(expr, "IN")
			case celoperators.LogicalNot:
				args := expr.AsCall().Args()
				if len(args) != 1 {
					return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf(`only support !(user_type in ["{type1}", "{type2}"]) format`))
				}
				return parseToUserTypeSQL(args[0], "NOT IN")
			default:
				return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unexpected function %v", functionName))
			}
		default:
			return "", connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unexpected expr kind %v", expr.Kind()))
		}
	}

	where, err := getFilter(ast.NativeRep().Expr())
	if err != nil {
		return err
	}

	find.Filter = &store.ListResourceFilter{
		Args:  positionalArgs,
		Where: "(" + where + ")",
	}
	return nil
}

// CreateUser creates a user.
func (s *UserService) CreateUser(ctx context.Context, request *connect.Request[v1pb.CreateUserRequest]) (*connect.Response[v1pb.User], error) {
	if err := s.userCountGuard(ctx); err != nil {
		return nil, err
	}

	if request.Msg.User == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user must be set"))
	}
	if request.Msg.User.Email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("email must be set"))
	}
	if request.Msg.User.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user title must be set"))
	}

	principalType, err := convertToPrincipalType(request.Msg.User.UserType)
	if err != nil {
		return nil, err
	}
	if request.Msg.User.UserType != v1pb.UserType_SERVICE_ACCOUNT && request.Msg.User.UserType != v1pb.UserType_USER {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("support user and service account only"))
	}

	// Service accounts are privileged programmatic identities: the response
	// hands out their generated access key, which authenticates directly via
	// Login. Anonymous self-service signup must therefore stay limited to USER;
	// only a caller holding the workspace-scope laelia.users.create permission
	// (workspace admins) may create a service account.
	if principalType == storepb.PrincipalType_SERVICE_ACCOUNT {
		if err := authorizeServiceAccountCreation(ctx, s.iam); err != nil {
			return nil, err
		}
	}

	count, err := s.store.CountUsers(ctx, storepb.PrincipalType_END_USER)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to count users, error: %v", err))
	}
	firstEndUser := count == 0

	// Self-service signup is the anonymous CreateUser path. When the workspace
	// disallows signup, only callers holding laelia.users.create (workspace
	// admins) may create users. The very first end user is exempt so a fresh
	// workspace can always bootstrap its first admin.
	//
	// Email verification (when enabled) applies only to anonymous
	// self-service signup: admin-created users, service accounts, the first
	// bootstrap user, and SSO-created users are always treated as verified.
	// If verification is required but no SMTP server is configured, signup is
	// rejected so an account is never created that the user cannot activate.
	var workspaceSetting *storepb.WorkspaceProfileSetting
	requireVerification := false
	if principalType == storepb.PrincipalType_END_USER && !firstEndUser {
		setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace general setting"))
		}
		workspaceSetting = setting
		if setting.DisallowSignup {
			caller, ok := GetUserFromContext(ctx)
			if !ok || caller == nil {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("signup is disallowed, please contact the workspace administrator"))
			}
			allowed, err := canCreateUser(ctx, s.iam, caller)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
			}
			if !allowed {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("signup is disallowed, only workspace admins can create users"))
			}
		}
		if caller, ok := GetUserFromContext(ctx); !ok || caller == nil {
			requireVerification = store.RequireEmailVerification(setting)
			if requireVerification {
				configured, err := s.mailer.Configured(ctx)
				if err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check SMTP configuration"))
				}
				if !configured {
					return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("the workspace requires email verification but the mail service is not configured, please contact the administrator"))
				}
			}
		}
	}

	if request.Msg.User.Phone != "" {
		if err := common.ValidatePhone(request.Msg.User.Phone); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid phone %q, error: %v", request.Msg.User.Phone, err))
		}
	}

	if err := validateEmailWithDomains(ctx, s.store, request.Msg.User.Email, principalType == storepb.PrincipalType_SERVICE_ACCOUNT); err != nil {
		return nil, err
	}
	existingUser, err := s.store.GetUserByEmail(ctx, request.Msg.User.Email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find user by email, error: %v", err))
	}
	if existingUser != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("email %s is already existed", request.Msg.User.Email))
	}

	password := request.Msg.User.Password
	if request.Msg.User.UserType == v1pb.UserType_SERVICE_ACCOUNT {
		pwd, err := common.RandomString(20)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access key for service account"))
		}
		password = fmt.Sprintf("%s%s", common.ServiceAccountAccessKeyPrefix, pwd)
	} else {
		if password != "" {
			if err := s.validatePassword(ctx, password); err != nil {
				return nil, err
			}
		} else {
			pwd, err := common.RandomString(20)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate random password for service account"))
			}
			password = pwd
		}
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate password hash, error: %v", err))
	}
	userMessage := &store.UserMessage{
		Email:        request.Msg.User.Email,
		Name:         request.Msg.User.Title,
		Phone:        request.Msg.User.Phone,
		Type:         principalType,
		PasswordHash: string(passwordHash),
		Description:  request.Msg.User.Description,
	}
	if !requireVerification {
		verifiedAt := time.Now()
		userMessage.EmailVerifiedAt = &verifiedAt
	}

	user, err := s.store.CreateUser(ctx, userMessage)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create user, error: %v", err))
	}

	if firstEndUser {
		// The first end user should be workspace admin.
		updateRole := &store.PatchIamPolicyMessage{
			Member: common.FormatUserHandle(user.Handle),
			Roles:  []string{common.FormatRole(common.WorkspaceAdmin)},
		}
		if _, err := s.store.PatchWorkspaceIamPolicy(ctx, updateRole); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	if requireVerification {
		// The account is created unverified; the signup completes only after
		// the user clicks the link in the email. A send failure is logged but
		// does not fail the request — the user can request a resend from the
		// signup page.
		baseURL := workspaceSetting.GetExternalUrl()
		if baseURL == "" {
			baseURL = request.Header().Get("Origin")
		}
		if err := issueVerificationEmail(ctx, s.mailer, s.store, user, baseURL); err != nil {
			slog.Error("failed to send signup verification email", log.WithError(err), slog.String("user", user.Email))
		}
	}

	// isFirstUser := user.ID == common.PrincipalIDForFirstUser
	// s.metricReporter.Report(ctx, &metric.Metric{
	// 	Name:  metricapi.PrincipalRegistrationMetricName,
	// 	Value: 1,
	// 	Labels: map[string]any{
	// 		"email": user.Email,
	// 		"name":  user.Name,
	// 		"phone": user.Phone,
	// 		// We only send lark notification for the first principal registration.
	// 		// false means do not notify upfront. Later the notification will be triggered by the scheduler.
	// 		"lark_notified": !isFirstUser,
	// 	},
	// })

	userResponse := convertToUser(user, false)
	if request.Msg.User.UserType == v1pb.UserType_SERVICE_ACCOUNT {
		userResponse.ServiceKey = password
	}
	return connect.NewResponse(userResponse), nil
}

func (s *UserService) validatePassword(ctx context.Context, password string) error {
	passwordRestriction, err := s.store.GetPasswordRestrictionSetting(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Errorf("failed to get password restriction with error: %v", err))
	}
	if len(password) < int(passwordRestriction.MinLength) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password length should no less than %v characters", passwordRestriction.MinLength))
	}
	if passwordRestriction.RequireNumber && !regexp.MustCompile("[0-9]+").MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 number"))
	}
	if passwordRestriction.RequireLetter && !regexp.MustCompile("[a-zA-Z]+").MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 lower case letter"))
	}
	if passwordRestriction.RequireUppercaseLetter && !regexp.MustCompile("[A-Z]+").MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 upper case letter"))
	}
	if passwordRestriction.RequireSpecialCharacter && !regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]+`).MatchString(password) {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password must contains at least 1 special character"))
	}
	return nil
}

// UpdateUser updates a user.
func (s *UserService) UpdateUser(ctx context.Context, request *connect.Request[v1pb.UpdateUserRequest]) (*connect.Response[v1pb.User], error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("failed to get caller user"))
	}
	if request.Msg.User == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user must be set"))
	}
	if request.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("update_mask must be set"))
	}

	identifier, err := common.GetUserHandle(request.Msg.User.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		if request.Msg.AllowMissing {
			allowed, err := canCreateUser(ctx, s.iam, caller)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
			}
			if !allowed {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", permission.UsersCreate))
			}
			return s.CreateUser(ctx, connect.NewRequest(&v1pb.CreateUserRequest{
				User: request.Msg.User,
			}))
		}
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", identifier))
	}
	if user.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q has been deleted", identifier))
	}
	if isReservedUserID(user.ID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("built-in user %q cannot be modified", user.Handle))
	}

	allowed, err := canUpdateUser(ctx, s.iam, caller, user)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !allowed {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", permission.UsersUpdate))
	}

	var passwordPatch *string
	patch := &store.UpdateUserMessage{}
	for _, path := range request.Msg.UpdateMask.Paths {
		switch path {
		case "email":
			if err := validateEmailWithDomains(ctx, s.store, request.Msg.User.Email, user.Type == storepb.PrincipalType_SERVICE_ACCOUNT); err != nil {
				return nil, err
			}
			existedUser, err := s.store.GetUserByEmail(ctx, request.Msg.User.Email)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find user list, error: %v", err))
			}
			if existedUser != nil && existedUser.ID != user.ID {
				return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("email %s is already existed", request.Msg.User.Email))
			}
			patch.Email = &request.Msg.User.Email
		case "title":
			patch.Name = &request.Msg.User.Title
		case "password":
			if user.Type != storepb.PrincipalType_END_USER {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password can be mutated for end users only"))
			}
			if err := s.validatePassword(ctx, request.Msg.User.Password); err != nil {
				return nil, err
			}
			passwordPatch = &request.Msg.User.Password
		case "service_key":
			if user.Type != storepb.PrincipalType_SERVICE_ACCOUNT {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("service key can be mutated for service accounts only"))
			}
			val, err := common.RandomString(20)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access key for service account"))
			}
			password := fmt.Sprintf("%s%s", common.ServiceAccountAccessKeyPrefix, val)
			passwordPatch = &password
		case "phone":
			if request.Msg.User.Phone != "" {
				if err := common.ValidatePhone(request.Msg.User.Phone); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid phone number %q, error: %v", request.Msg.User.Phone, err))
				}
			}
			patch.Phone = &request.Msg.User.Phone
		case "description":
			patch.Description = &request.Msg.User.Description
		case "chat_preferences":
			if v := request.Msg.User.ChatPreferences; v != nil {
				if err := validatePreferredLanguage(v.PreferredLanguage); err != nil {
					return nil, err
				}
				// Copy both fields so saving one never wipes the other (the
				// frontend always submits the full current prefs).
				patch.ChatPreferences = &storepb.ChatPreferences{
					EnterToSend:       v.EnterToSend,
					PreferredLanguage: storepb.PreferredLanguage(v.PreferredLanguage),
				}
			}
		default:
		}
	}
	if passwordPatch != nil {
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(*passwordPatch)); err == nil {
			// return bad request if the passwords match
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("password cannot be the same"))
		}

		passwordHash, err := bcrypt.GenerateFromPassword([]byte((*passwordPatch)), bcrypt.DefaultCost)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate password hash, error: %v", err))
		}
		passwordHashStr := string(passwordHash)
		patch.PasswordHash = &passwordHashStr
	}

	user, err = s.store.UpdateUser(ctx, user, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update user, error: %v", err))
	}

	userResponse := convertToUser(user, false)
	if request.Msg.User.UserType == v1pb.UserType_SERVICE_ACCOUNT && passwordPatch != nil {
		userResponse.ServiceKey = *passwordPatch
	}
	return connect.NewResponse(userResponse), nil
}

// DeleteUser deletes a user.
func (s *UserService) DeleteUser(ctx context.Context, request *connect.Request[v1pb.DeleteUserRequest]) (*connect.Response[emptypb.Empty], error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("failed to get caller user"))
	}

	allowed, err := canDeleteUser(ctx, s.iam, caller)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !allowed {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", permission.UsersDelete))
	}
	identifier, err := common.GetUserHandle(request.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", identifier))
	}
	if user.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q has been deleted", identifier))
	}
	if caller != nil && caller.ID == user.ID {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("cannot delete your own account"))
	}
	if isReservedUserID(user.ID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("built-in user %q cannot be deleted", user.Handle))
	}

	// Check if there is still workspace admin if the current user is deleted.
	policy, err := s.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, err
	}
	hasExtraWorkspaceAdmin, err := hasActiveWorkspaceAdmin(ctx, s.store, policy.Policy, user.ID)
	if err != nil {
		return nil, err
	}
	if !hasExtraWorkspaceAdmin {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("workspace must have at least one admin"))
	}

	if _, err := s.store.UpdateUser(ctx, user, &store.UpdateUserMessage{Delete: &deletePatch}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UndeleteUser undeletes a user.
func (s *UserService) UndeleteUser(ctx context.Context, request *connect.Request[v1pb.UndeleteUserRequest]) (*connect.Response[v1pb.User], error) {
	if err := s.userCountGuard(ctx); err != nil {
		return nil, err
	}

	caller, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("failed to get caller user"))
	}
	allowed, err := canDeleteUser(ctx, s.iam, caller)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !allowed {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("permission %q denied", permission.UsersDelete))
	}
	identifier, err := common.GetUserHandle(request.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	user, err := s.store.GetUserByIdentifier(ctx, identifier)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get user, error: %v", err))
	}
	if user == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("user %q not found", identifier))
	}
	if !user.MemberDeleted {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("user %q is already active", user.Handle))
	}
	if isReservedUserID(user.ID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("built-in user %q cannot be modified", user.Handle))
	}

	user, err = s.store.UpdateUser(ctx, user, &store.UpdateUserMessage{Delete: &undeletePatch})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(convertToUser(user, false)), nil
}

// maskPhoneNumber returns a masked form of a phone number (e.g. "138****8000"),
// keeping the first 3 and last 4 digits. Shorter numbers keep only the last 4
// digits; empty stays empty.
func maskPhoneNumber(phone string) string {
	if phone == "" {
		return ""
	}
	r := []rune(phone)
	n := len(r)
	if n <= 4 {
		return strings.Repeat("*", n)
	}
	head := 3
	if n < 8 {
		head = 0
	}
	masked := append([]rune(nil), r[:head]...)
	for i := head; i < n-4; i++ {
		masked = append(masked, '*')
	}
	masked = append(masked, r[n-4:]...)
	return string(masked)
}

// callerPhoneVisibility resolves the caller's user id and workspace-admin
// status — the two conditions that allow seeing a full phone number. A nil
// caller (agent or unauthenticated) yields zero values.
func callerPhoneVisibility(ctx context.Context, stores *store.Store) (callerID int, isAdmin bool, err error) {
	caller, ok := GetUserFromContext(ctx)
	if !ok || caller == nil {
		return 0, false, nil
	}
	admin, err := isUserWorkspaceAdmin(ctx, stores, caller)
	if err != nil {
		return 0, false, err
	}
	return caller.ID, admin, nil
}

func convertToV1UserType(userType storepb.PrincipalType) v1pb.UserType {
	switch userType {
	case storepb.PrincipalType_END_USER:
		return v1pb.UserType_USER
	case storepb.PrincipalType_SYSTEM_BOT:
		return v1pb.UserType_SYSTEM_BOT
	case storepb.PrincipalType_SERVICE_ACCOUNT:
		return v1pb.UserType_SERVICE_ACCOUNT
	default:
		return v1pb.UserType_USER_TYPE_UNSPECIFIED
	}
}

func convertToUser(user *store.UserMessage, maskPhone bool) *v1pb.User {
	convertedUser := &v1pb.User{
		Name:        common.FormatUserHandle(user.Handle),
		Handle:      user.Handle,
		State:       convertDeletedToState(user.MemberDeleted),
		Email:       user.Email,
		Phone:       user.Phone,
		Title:       user.Name,
		Description: user.Description,
		UserType:    convertToV1UserType(user.Type),
		Profile: &v1pb.UserProfile{
			LastLoginTime:          user.Profile.LastLoginTime,
			LastChangePasswordTime: user.Profile.LastChangePasswordTime,
			Source:                 user.Profile.Source,
		},
	}
	// Phone is PII: only the target user themself and workspace admins see the
	// full number; everyone else (including agents) gets a masked form.
	if maskPhone {
		convertedUser.Phone = maskPhoneNumber(user.Phone)
	}
	if user.AvatarS3Key != "" {
		convertedUser.Avatar = common.FormatUserAvatar(user.Handle)
	}

	// ChatPreferences defaults to enter_to_send = true (the historic behavior)
	// when the user has never customized it (nil = NULL column). PreferredLanguage
	// stays UNSPECIFIED (the zero value) in that case.
	enterToSend := true
	var preferredLanguage v1pb.PreferredLanguage
	if user.ChatPreferences != nil {
		enterToSend = user.ChatPreferences.EnterToSend
		preferredLanguage = v1pb.PreferredLanguage(user.ChatPreferences.PreferredLanguage)
	}
	convertedUser.ChatPreferences = &v1pb.ChatPreferences{
		EnterToSend:       enterToSend,
		PreferredLanguage: preferredLanguage,
	}

	// Groups already carries full group resource names ("groups/{email}" or
	// "groups/{id}") from the store.
	convertedUser.Groups = user.Groups

	return convertedUser
}

// validatePreferredLanguage rejects a PreferredLanguage value outside the known
// set. proto3 enums arrive as raw ints on the wire, so an out-of-range value
// would otherwise be silently persisted into the chat_preferences jsonb column.
func validatePreferredLanguage(lang v1pb.PreferredLanguage) error {
	switch lang {
	case v1pb.PreferredLanguage_PREFERRED_LANGUAGE_UNSPECIFIED,
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_ZH_CN,
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_EN_US,
		v1pb.PreferredLanguage_PREFERRED_LANGUAGE_JA_JP:
		return nil
	}
	return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid preferred_language %d", lang))
}

func convertToPrincipalType(userType v1pb.UserType) (storepb.PrincipalType, error) {
	var t storepb.PrincipalType
	switch userType {
	case v1pb.UserType_USER:
		t = storepb.PrincipalType_END_USER
	case v1pb.UserType_SYSTEM_BOT:
		t = storepb.PrincipalType_SYSTEM_BOT
	case v1pb.UserType_SERVICE_ACCOUNT:
		t = storepb.PrincipalType_SERVICE_ACCOUNT
	default:
		return t, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid user type %s", userType))
	}
	return t, nil
}

func validateEmailWithDomains(ctx context.Context, stores *store.Store, email string, isServiceAccount bool) error {
	setting, err := stores.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Errorf("failed to find workspace setting, error: %v", err))
	}

	var allowedDomains []string
	if setting.EnforceIdentityDomain {
		allowedDomains = setting.Domains
	}

	// Check if the email is valid.
	if err := validateEmail(email); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid email: %v", err.Error()))
	}
	// Domain restrictions are not applied to service account.
	if isServiceAccount {
		return nil
	}
	// Enforce domain restrictions.
	if len(allowedDomains) > 0 {
		ok := false
		for _, v := range allowedDomains {
			if strings.HasSuffix(email, fmt.Sprintf("@%s", v)) {
				ok = true
				break
			}
		}
		if !ok {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("email %q does not belong to domains %v", email, allowedDomains))
		}
	}
	return nil
}

func validateEmail(email string) error {
	if email != strings.ToLower(email) {
		return errors.New("email should be lowercase")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return err
	}
	return nil
}

func extractDomain(input string) string {
	pattern := `[a-zA-Z0-9-]+(\.[a-zA-Z0-9-]+)+`
	regExp, err := regexp.Compile(pattern)
	if err != nil {
		// WHen the pattern is invalid, we just return the input.
		return input
	}

	match := regExp.FindString(input)
	domainParts := strings.Split(match, ".")
	// If the domain has at least 3 parts, we will remove the first part.
	if len(domainParts) >= 3 {
		match = strings.Join(domainParts[1:], ".")
	}
	return match
}

func (*UserService) userCountGuard(_ context.Context) error {
	return nil
}

func isUserWorkspaceAdmin(ctx context.Context, stores *store.Store, user *store.UserMessage) (bool, error) {
	workspacePolicy, err := stores.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return false, err
	}
	roles := utils.GetUserFormattedRolesMap(ctx, stores, user, workspacePolicy.Policy)
	return roles[common.FormatRole(common.WorkspaceAdmin)], nil
}

// isReservedUserID reports whether the id belongs to a built-in account in the
// reserved range (id < PrincipalIDForFirstUser, e.g. the seeded system bot at
// id=1). Reserved accounts cannot be modified, deleted, or restored. Real users
// are always assigned ids >= PrincipalIDForFirstUser (see migration/latest.sql,
// `ALTER SEQUENCE principal_id_seq RESTART WITH 101`).
func isReservedUserID(userID int) bool {
	return userID < common.PrincipalIDForFirstUser
}
