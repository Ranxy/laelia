package v1

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/plugin/idp/oauth2"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/manager/api/auth"

	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

var (
	invalidUserOrPasswordError = connect.NewError(connect.CodeUnauthenticated, errors.Errorf("the email or password is not valid"))
)

// AuthService implements the auth service.
type AuthService struct {
	v1connect.UnimplementedAuthServiceHandler
	store    *store.Store
	secret   string
	profile  *config.Profile
	stateCfg *state.State
}

// NewAuthService creates a new AuthService.
func NewAuthService(store *store.Store, secret string, profile *config.Profile, stateCfg *state.State) *AuthService {
	return &AuthService{
		store:    store,
		secret:   secret,
		profile:  profile,
		stateCfg: stateCfg,
	}
}

// Login is the auth login method including SSO.
func (s *AuthService) Login(ctx context.Context, req *connect.Request[v1pb.LoginRequest]) (*connect.Response[v1pb.LoginResponse], error) {
	request := req.Msg
	var loginUser *store.UserMessage
	loginViaIDP := request.GetIdpName() != ""

	response := &v1pb.LoginResponse{}
	resp := connect.NewResponse(response)
	var err error
	if loginViaIDP {
		loginUser, err = s.getOrCreateUserWithIDP(ctx, request)
		if err != nil {
			return nil, err
		}
	} else {
		loginUser, err = s.getAndVerifyUser(ctx, request)
		if err != nil {
			return nil, err
		}
		// Reset password restriction only works for end user with email & password login.
		response.RequireResetPassword = s.needResetPassword(ctx, loginUser)
	}

	if loginUser.MemberDeleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("user has been deactivated by administrators"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to find workspace setting, error"))
	}
	isWorkspaceAdmin, err := isUserWorkspaceAdmin(ctx, s.store, loginUser)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to check user roles, error"))
	}
	if !isWorkspaceAdmin && loginUser.Type == models.PrincipalType_END_USER {
		if setting.DisallowPasswordSignin && !loginViaIDP {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("password signin is disallowed"))
		}

		// Check domain restriction for end users.
		if err := validateEmailWithDomains(ctx, s.store, loginUser.Email, false); err != nil {
			return nil, err
		}
	}

	tokenDuration := auth.GetTokenDuration(ctx, s.store)

	switch loginUser.Type {
	case models.PrincipalType_END_USER:
		token, err := auth.GenerateAccessToken(loginUser.Name, loginUser.ID, s.profile.Mode, s.secret, tokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate API access token"))
		}
		response.Token = token
	case models.PrincipalType_SERVICE_ACCOUNT:
		token, err := auth.GenerateAPIToken(loginUser.Name, loginUser.ID, s.profile.Mode, s.secret)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate API access token"))
		}
		response.Token = token
	default:
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("user type %s cannot login", loginUser.Type))
	}

	if request.Web {
		// Only allow end users to use web login, not service accounts.
		if loginUser.Type != models.PrincipalType_END_USER {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("only users can use web login"))
		}

		origin := req.Header().Get("Origin")
		if origin == "" {
			origin = req.Header().Get("grpcgateway-origin")
		}

		cookie := auth.GetTokenCookie(ctx, s.store, origin, response.Token)
		resp.Header().Add("Set-Cookie", cookie.String())
	}

	if _, err := s.store.UpdateUser(ctx, loginUser, &store.UpdateUserMessage{
		Profile: &models.UserProfile{
			LastLoginTime:          timestamppb.Now(),
			LastChangePasswordTime: loginUser.Profile.GetLastChangePasswordTime(),
		},
	}); err != nil {
		slog.Error("failed to update user profile", log.WithError(err), slog.String("user", loginUser.Email))
	}

	response.User = convertToUser(loginUser)

	// s.metricReporter.Report(ctx, &metric.Metric{
	// 	Name:  metricapi.PrincipalLoginMetricName,
	// 	Value: 1,
	// 	Labels: map[string]any{
	// 		"email": loginUser.Email,
	// 	},
	// })

	return resp, nil
}

func (s *AuthService) needResetPassword(ctx context.Context, user *store.UserMessage) bool {
	// Reset password restriction only works for end user with email & password login.
	if user.Type != models.PrincipalType_END_USER {
		return false
	}

	passwordRestriction, err := s.store.GetPasswordRestrictionSetting(ctx)
	if err != nil {
		slog.Error("failed to get password restriction", log.WithError(err))
		return false
	}

	if user.Profile.LastLoginTime == nil {
		if !passwordRestriction.RequireResetPasswordForFirstLogin {
			return false
		}
		count, err := s.store.CountUsers(ctx, models.PrincipalType_END_USER)
		if err != nil {
			slog.Error("failed to count end users", log.WithError(err))
			return false
		}
		// The 1st workspace admin login don't need to reset the password
		return count > 1
	}

	if passwordRestriction.PasswordRotation != nil {
		lastChangePasswordTime := user.CreatedAt
		if user.Profile.LastChangePasswordTime != nil {
			lastChangePasswordTime = user.Profile.LastChangePasswordTime.AsTime()
		}
		if lastChangePasswordTime.Add(passwordRestriction.PasswordRotation.AsDuration()).Before(time.Now()) {
			return true
		}
	}

	return false
}

// Logout is the auth logout method.
func (s *AuthService) Logout(ctx context.Context, req *connect.Request[v1pb.LogoutRequest]) (*connect.Response[emptypb.Empty], error) {
	accessTokenStr, err := auth.GetTokenFromHeaders(req.Header())
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	s.stateCfg.TokenExpireCache.Add(accessTokenStr, true)

	resp := connect.NewResponse(&emptypb.Empty{})

	origin := req.Header().Get("Origin")
	if origin == "" {
		origin = req.Header().Get("grpcgateway-origin")
	}
	cookie := auth.GetTokenCookie(ctx, s.store, origin, "")
	resp.Header().Add("Set-Cookie", cookie.String())
	return resp, nil
}

func (s *AuthService) getAndVerifyUser(ctx context.Context, request *v1pb.LoginRequest) (*store.UserMessage, error) {
	user, err := s.store.GetUserByEmail(ctx, request.Email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get user by email %q", request.Email))
	}
	if user == nil {
		return nil, invalidUserOrPasswordError
	}
	// Compare the stored hashed password, with the hashed version of the password that was received.
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)); err != nil {
		// If the two passwords don't match, return a 401 status.
		return nil, invalidUserOrPasswordError
	}
	return user, nil
}

func (s *AuthService) getOrCreateUserWithIDP(ctx context.Context, request *v1pb.LoginRequest) (*store.UserMessage, error) {
	idpID, err := common.GetIdentityProviderID(request.IdpName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "failed to get identity provider ID"))
	}
	idp, err := s.store.GetIdentityProvider(ctx, &store.FindIdentityProviderMessage{
		ResourceID: &idpID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get identity provider"))
	}
	if idp == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("identity provider not found"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get workspace setting"))
	}

	var userInfo *models.IdentityProviderUserInfo
	switch idp.Type {
	case models.IdentityProviderType_OAUTH2:
		oauth2Context := request.IdpContext.GetOauth2Context()
		if oauth2Context == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("missing OAuth2 context"))
		}
		oauth2IdentityProvider, err := oauth2.NewIdentityProvider(idp.Config.GetOauth2Config())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create new OAuth2 identity provider"))
		}
		redirectURL := fmt.Sprintf("%s/oauth/callback", setting.ExternalUrl)
		token, err := oauth2IdentityProvider.ExchangeToken(ctx, redirectURL, oauth2Context.Code)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to exchange token"))
		}
		userInfo, _, err = oauth2IdentityProvider.UserInfo(ctx, token)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get user info"))
		}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("identity provider type %s not supported", idp.Type.String()))
	}
	if userInfo == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("failed to get user info from identity provider %q", idp.Title))
	}
	if userInfo.Identifier == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("missing identifier in user info from identity provider %q", idp.Title))
	}
	// The userinfo's email comes from identity provider, it has to be converted to lower-case.
	email := strings.ToLower(userInfo.Identifier)
	if err := validateEmail(email); err != nil {
		// If the email is invalid, we will try to use the domain and identifier to construct the email.
		domain := extractDomain(idp.Domain)
		if domain != "" {
			email = strings.ToLower(fmt.Sprintf("%s@%s", email, domain))
		} else {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid email %q", userInfo.Identifier))
		}
	}
	// If the email is still invalid, we will return an error.
	if err := validateEmailWithDomains(ctx, s.store, email, false); err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list users by email %s", email))
	}
	if user != nil {
		if user.MemberDeleted {
			if err := s.userCountGuard(ctx); err != nil {
				return nil, err
			}
			// Undelete the user when login via SSO.
			user, err = s.store.UpdateUser(ctx, user, &store.UpdateUserMessage{Delete: &undeletePatch})
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to undelete user"))
			}
		}
		if userInfo.HasGroups {
			// Sync user groups with the identity provider.
			// The userInfo.Groups is the groups that the user belongs to in the identity provider.
			if err := s.syncUserGroups(ctx, user, userInfo.Groups); err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync user groups"))
			}
		}
		return user, nil
	}

	// Create new user from identity provider.
	password, err := common.RandomString(20)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate random password"))
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate password hash"))
	}
	if err := s.userCountGuard(ctx); err != nil {
		return nil, err
	}
	newUser, err := s.store.CreateUser(ctx, &store.UserMessage{
		Name:         userInfo.DisplayName,
		Email:        email,
		Phone:        userInfo.Phone,
		Type:         models.PrincipalType_END_USER,
		PasswordHash: string(passwordHash),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to create user, error"))
	}
	if userInfo.HasGroups {
		// Sync user groups with the identity provider.
		// The userInfo.Groups is the groups that the user belongs to in the identity provider.
		if err := s.syncUserGroups(ctx, newUser, userInfo.Groups); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to sync user groups"))
		}
	}
	return newUser, nil
}

func (*AuthService) userCountGuard(_ context.Context) error {
	return nil
}

// syncUserGroups syncs the user groups with the given groups.
// The given groups are the groups that the user belongs to in the identity provider.
// Supported groups format: ["group1", "group2", ...], ["dev.example.com", ...]
func (s *AuthService) syncUserGroups(ctx context.Context, user *store.UserMessage, groups []string) error {
	groupMessageList, err := s.store.ListGroups(ctx, &store.FindGroupMessage{})
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to list groups"))
	}

	for _, groupMessage := range groupMessageList {
		var isMember bool
		for _, group := range groups {
			if groupMessage.Email == group || groupMessage.Title == group {
				isMember = true
				break
			}
		}
		var isGroupMember bool
		for _, member := range groupMessage.Payload.Members {
			if member.Member == common.FormatUserUID(user.ID) {
				isGroupMember = true
				break
			}
		}
		if isMember != isGroupMember {
			if isMember {
				// Add the user to the group.
				groupMessage.Payload.Members = append(groupMessage.Payload.Members, &models.GroupMember{
					Role:   models.GroupMember_MEMBER,
					Member: common.FormatUserUID(user.ID),
				})
			} else {
				// Remove the user from the group.
				groupMessage.Payload.Members = slices.DeleteFunc(groupMessage.Payload.Members, func(member *models.GroupMember) bool {
					return member.Member == common.FormatUserUID(user.ID)
				})
			}
			if _, err := s.store.UpdateGroup(ctx, groupMessage.Email, &store.UpdateGroupMessage{
				Payload: groupMessage.Payload,
			}); err != nil {
				return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to update group %q", groupMessage.Email))
			}
		}
	}

	return nil
}
