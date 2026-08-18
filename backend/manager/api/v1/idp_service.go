package v1

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/plugin/idp/oauth2"
	"github.com/Ranxy/laelia/backend/manager/store"
)

var (
	resourceIDMatcher = regexp.MustCompile("^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$")
)

func isValidResourceID(resourceID string) bool {
	return resourceIDMatcher.MatchString(resourceID)
}

// IdentityProviderService implements the identity provider management API.
// It is the admin-facing configuration surface for SSO: workspace admins can
// create/update/delete OAuth2 (and, once their plugins are ported, OIDC/LDAP)
// providers, and the public ListIdentityProviders feeds the login page.
type IdentityProviderService struct {
	v1connect.UnimplementedIdentityProviderServiceHandler
	store *store.Store
}

// NewIdentityProviderService creates a new IdentityProviderService.
func NewIdentityProviderService(s *store.Store) *IdentityProviderService {
	return &IdentityProviderService{store: s}
}

// Compile-time assertion that the service implements the generated handler.
var _ v1connect.IdentityProviderServiceHandler = (*IdentityProviderService)(nil)

// GetIdentityProvider gets an identity provider by name.
func (s *IdentityProviderService) GetIdentityProvider(ctx context.Context, req *connect.Request[v1pb.GetIdentityProviderRequest]) (*connect.Response[v1pb.IdentityProvider], error) {
	identityProviderMessage, err := s.getIdentityProviderMessage(ctx, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	identityProvider := convertToIdentityProvider(identityProviderMessage)
	return connect.NewResponse(identityProvider), nil
}

// ListIdentityProviders lists all configured identity providers. It is public
// (no credential) so the login page can render the enabled SSO targets.
func (s *IdentityProviderService) ListIdentityProviders(ctx context.Context, _ *connect.Request[v1pb.ListIdentityProvidersRequest]) (*connect.Response[v1pb.ListIdentityProvidersResponse], error) {
	identityProviders, err := s.store.ListIdentityProviders(ctx, &store.FindIdentityProviderMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list identity providers"))
	}

	response := &v1pb.ListIdentityProvidersResponse{}
	for _, identityProviderMessage := range identityProviders {
		response.IdentityProviders = append(response.IdentityProviders, convertToIdentityProvider(identityProviderMessage))
	}
	return connect.NewResponse(response), nil
}

// CreateIdentityProvider creates a new identity provider.
func (s *IdentityProviderService) CreateIdentityProvider(ctx context.Context, req *connect.Request[v1pb.CreateIdentityProviderRequest]) (*connect.Response[v1pb.IdentityProvider], error) {
	if req.Msg.IdentityProvider == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("identity_provider must be set"))
	}
	if !isValidResourceID(req.Msg.IdentityProviderId) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid identity provider ID %q", req.Msg.IdentityProviderId))
	}
	if req.Msg.IdentityProvider.Domain != "" && strings.ToLower(req.Msg.IdentityProvider.Domain) != req.Msg.IdentityProvider.Domain {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("domain name must use lower-case"))
	}
	if err := validIdentityProviderConfig(req.Msg.IdentityProvider.Type, req.Msg.IdentityProvider.Config); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	identityProviderMessage := &store.IdentityProviderMessage{
		ResourceID: req.Msg.IdentityProviderId,
		Title:      req.Msg.IdentityProvider.Title,
		Domain:     req.Msg.IdentityProvider.Domain,
		Type:       models.IdentityProviderType(req.Msg.IdentityProvider.Type),
		Config:     convertIdentityProviderConfigToStore(req.Msg.IdentityProvider.GetConfig()),
	}
	if req.Msg.ValidateOnly {
		identityProvider := convertToIdentityProvider(identityProviderMessage)
		return connect.NewResponse(identityProvider), nil
	}

	created, err := s.store.CreateIdentityProvider(ctx, identityProviderMessage)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("identity provider ID %q already exists", req.Msg.IdentityProviderId))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create identity provider"))
	}
	return connect.NewResponse(convertToIdentityProvider(created)), nil
}

// UpdateIdentityProvider updates an identity provider.
func (s *IdentityProviderService) UpdateIdentityProvider(ctx context.Context, req *connect.Request[v1pb.UpdateIdentityProviderRequest]) (*connect.Response[v1pb.IdentityProvider], error) {
	if req.Msg.IdentityProvider == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("identity_provider must be set"))
	}
	if req.Msg.UpdateMask == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("update_mask must be set"))
	}

	identityProviderID, err := common.GetIdentityProviderID(req.Msg.IdentityProvider.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	current, err := s.store.GetIdentityProvider(ctx, &store.FindIdentityProviderMessage{ResourceID: &identityProviderID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get identity provider"))
	}
	if current == nil {
		if req.Msg.AllowMissing {
			return s.CreateIdentityProvider(ctx, connect.NewRequest(&v1pb.CreateIdentityProviderRequest{
				IdentityProviderId: identityProviderID,
				IdentityProvider:   req.Msg.IdentityProvider,
			}))
		}
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("identity provider %q not found", req.Msg.IdentityProvider.Name))
	}

	patch := &store.UpdateIdentityProviderMessage{ResourceID: current.ResourceID}
	for _, path := range req.Msg.UpdateMask.Paths {
		switch path {
		case "title":
			patch.Title = &req.Msg.IdentityProvider.Title
		case "domain":
			if req.Msg.IdentityProvider.Domain != "" && strings.ToLower(req.Msg.IdentityProvider.Domain) != req.Msg.IdentityProvider.Domain {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("domain name must use lower-case"))
			}
			patch.Domain = &req.Msg.IdentityProvider.Domain
		case "config":
			if err := validIdentityProviderConfig(v1pb.IdentityProviderType(current.Type), req.Msg.IdentityProvider.Config); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			patch.Config = convertIdentityProviderConfigToStore(req.Msg.IdentityProvider.Config)
			// Don't overwrite secrets with an empty string (the read path masks them).
			switch current.Type {
			case models.IdentityProviderType_OAUTH2:
				if req.Msg.IdentityProvider.Config.GetOauth2Config().GetClientSecret() == "" && patch.Config.GetOauth2Config() != nil {
					patch.Config.GetOauth2Config().ClientSecret = current.Config.GetOauth2Config().GetClientSecret()
				}
			case models.IdentityProviderType_OIDC:
				if req.Msg.IdentityProvider.Config.GetOidcConfig().GetClientSecret() == "" && patch.Config.GetOidcConfig() != nil {
					patch.Config.GetOidcConfig().ClientSecret = current.Config.GetOidcConfig().GetClientSecret()
				}
			case models.IdentityProviderType_LDAP:
				if req.Msg.IdentityProvider.Config.GetLdapConfig().GetBindPassword() == "" && patch.Config.GetLdapConfig() != nil {
					patch.Config.GetLdapConfig().BindPassword = current.Config.GetLdapConfig().GetBindPassword()
				}
			default:
			}
		default:
		}
	}

	updated, err := s.store.UpdateIdentityProvider(ctx, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update identity provider"))
	}
	return connect.NewResponse(convertToIdentityProvider(updated)), nil
}

// DeleteIdentityProvider deletes an identity provider.
func (s *IdentityProviderService) DeleteIdentityProvider(ctx context.Context, req *connect.Request[v1pb.DeleteIdentityProviderRequest]) (*connect.Response[emptypb.Empty], error) {
	identityProvider, err := s.getIdentityProviderMessage(ctx, req.Msg.Name)
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteIdentityProvider(ctx, identityProvider.ResourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete identity provider"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// TestIdentityProvider tests an identity provider connection. OAuth2 is fully
// supported; OIDC and LDAP return not-implemented until their plugins are
func (s *IdentityProviderService) TestIdentityProvider(ctx context.Context, req *connect.Request[v1pb.TestIdentityProviderRequest]) (*connect.Response[v1pb.TestIdentityProviderResponse], error) {
	identityProvider := req.Msg.IdentityProvider
	if identityProvider == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("identity_provider must be set"))
	}

	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace setting"))
	}
	externalURL := setting.GetExternalUrl()
	if externalURL == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("workspace external URL is not configured"))
	}

	switch identityProvider.Type {
	case v1pb.IdentityProviderType_OAUTH2:
		// Find client secret for existing identity providers when the request
		// carries a masked/empty secret.
		if identityProvider.Config.GetOauth2Config().GetClientSecret() == "" {
			stored, err := s.getIdentityProviderMessage(ctx, identityProvider.Name)
			if err != nil {
				// Name may be empty for an uncreated provider; only fail when a
				// name was supplied but not found.
				if identityProvider.Name != "" {
					return nil, err
				}
			} else if stored != nil {
				identityProvider.Config.GetOauth2Config().ClientSecret = stored.Config.GetOauth2Config().GetClientSecret()
			}
		}

		oauth2Context := req.Msg.GetOauth2Context()
		if oauth2Context == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("missing OAuth2 context"))
		}

		oauth2IdentityProvider, err := oauth2.NewIdentityProvider(convertIdentityProviderConfigToStore(identityProvider.Config).GetOauth2Config())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create OAuth2 identity provider"))
		}
		redirectURL := fmt.Sprintf("%s/oauth/callback", externalURL)
		token, err := oauth2IdentityProvider.ExchangeToken(ctx, redirectURL, oauth2Context.Code)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to exchange access token"))
		}
		userInfo, claims, err := oauth2IdentityProvider.UserInfo(ctx, token)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "failed to get user info"))
		}

		claimsMap := make(map[string]string, len(claims))
		for key, value := range claims {
			claimsMap[key] = fmt.Sprintf("%v", value)
		}
		userInfoMap := make(map[string]string)
		if userInfo != nil {
			if userInfo.Identifier != "" {
				userInfoMap["email"] = userInfo.Identifier
			}
			if userInfo.DisplayName != "" {
				userInfoMap["title"] = userInfo.DisplayName
			}
			if userInfo.Phone != "" {
				userInfoMap["phone"] = userInfo.Phone
			}
		}
		return connect.NewResponse(&v1pb.TestIdentityProviderResponse{Claims: claimsMap, UserInfo: userInfoMap}), nil
	case v1pb.IdentityProviderType_OIDC, v1pb.IdentityProviderType_LDAP:
		return nil, connect.NewError(connect.CodeUnimplemented, errors.Errorf("identity provider type %s is not implemented yet", identityProvider.Type.String()))
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("identity provider type %s not supported", identityProvider.Type.String()))
	}
}

func (s *IdentityProviderService) getIdentityProviderMessage(ctx context.Context, name string) (*store.IdentityProviderMessage, error) {
	identityProviderID, err := common.GetIdentityProviderID(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	identityProvider, err := s.store.GetIdentityProvider(ctx, &store.FindIdentityProviderMessage{ResourceID: &identityProviderID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get identity provider"))
	}
	if identityProvider == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("identity provider %q not found", name))
	}
	return identityProvider, nil
}

func convertToIdentityProvider(identityProvider *store.IdentityProviderMessage) *v1pb.IdentityProvider {
	return &v1pb.IdentityProvider{
		Name:   fmt.Sprintf("%s%s", common.IdentityProviderNamePrefix, identityProvider.ResourceID),
		Title:  identityProvider.Title,
		Domain: identityProvider.Domain,
		Type:   v1pb.IdentityProviderType(identityProvider.Type),
		Config: convertIdentityProviderConfigFromStore(identityProvider.Config),
	}
}

func convertIdentityProviderConfigFromStore(identityProviderConfig *models.IdentityProviderConfig) *v1pb.IdentityProviderConfig {
	if v := identityProviderConfig.GetOauth2Config(); v != nil {
		fieldMapping := v1pb.FieldMapping{
			Identifier:  v.FieldMapping.GetIdentifier(),
			DisplayName: v.FieldMapping.GetDisplayName(),
			Phone:       v.FieldMapping.GetPhone(),
			Groups:      v.FieldMapping.GetGroups(),
		}
		return &v1pb.IdentityProviderConfig{
			Config: &v1pb.IdentityProviderConfig_Oauth2Config{
				Oauth2Config: &v1pb.OAuth2IdentityProviderConfig{
					AuthUrl:       v.AuthUrl,
					TokenUrl:      v.TokenUrl,
					UserInfoUrl:   v.UserInfoUrl,
					ClientId:      v.ClientId,
					ClientSecret:  "", // SECURITY: never expose the client secret
					Scopes:        v.Scopes,
					FieldMapping:  &fieldMapping,
					SkipTlsVerify: v.SkipTlsVerify,
					AuthStyle:     v1pb.OAuth2AuthStyle(v.AuthStyle),
				},
			},
		}
	} else if v := identityProviderConfig.GetOidcConfig(); v != nil {
		fieldMapping := v1pb.FieldMapping{
			Identifier:  v.FieldMapping.GetIdentifier(),
			DisplayName: v.FieldMapping.GetDisplayName(),
			Phone:       v.FieldMapping.GetPhone(),
			Groups:      v.FieldMapping.GetGroups(),
		}
		return &v1pb.IdentityProviderConfig{
			Config: &v1pb.IdentityProviderConfig_OidcConfig{
				OidcConfig: &v1pb.OIDCIdentityProviderConfig{
					Issuer:        v.Issuer,
					ClientId:      v.ClientId,
					ClientSecret:  "", // SECURITY: never expose the client secret
					FieldMapping:  &fieldMapping,
					SkipTlsVerify: v.SkipTlsVerify,
					AuthStyle:     v1pb.OAuth2AuthStyle(v.AuthStyle),
					Scopes:        v.Scopes,
				},
			},
		}
	} else if v := identityProviderConfig.GetLdapConfig(); v != nil {
		fieldMapping := v1pb.FieldMapping{
			Identifier:  v.FieldMapping.GetIdentifier(),
			DisplayName: v.FieldMapping.GetDisplayName(),
			Phone:       v.FieldMapping.GetPhone(),
			Groups:      v.FieldMapping.GetGroups(),
		}
		return &v1pb.IdentityProviderConfig{
			Config: &v1pb.IdentityProviderConfig_LdapConfig{
				LdapConfig: &v1pb.LDAPIdentityProviderConfig{
					Host:             v.Host,
					Port:             v.Port,
					SkipTlsVerify:    v.SkipTlsVerify,
					BindDn:           v.BindDn,
					BindPassword:     "", // SECURITY: never expose the bind password
					BaseDn:           v.BaseDn,
					UserFilter:       v.UserFilter,
					SecurityProtocol: v1pb.LDAPIdentityProviderConfig_SecurityProtocol(v.SecurityProtocol),
					FieldMapping:     &fieldMapping,
				},
			},
		}
	}
	return nil
}

func convertIdentityProviderConfigToStore(identityProviderConfig *v1pb.IdentityProviderConfig) *models.IdentityProviderConfig {
	if v := identityProviderConfig.GetOauth2Config(); v != nil {
		fieldMapping := models.FieldMapping{
			Identifier:  v.FieldMapping.GetIdentifier(),
			DisplayName: v.FieldMapping.GetDisplayName(),
			Phone:       v.FieldMapping.GetPhone(),
			Groups:      v.FieldMapping.GetGroups(),
		}
		return &models.IdentityProviderConfig{
			Config: &models.IdentityProviderConfig_Oauth2Config{
				Oauth2Config: &models.OAuth2IdentityProviderConfig{
					AuthUrl:       v.AuthUrl,
					TokenUrl:      v.TokenUrl,
					UserInfoUrl:   v.UserInfoUrl,
					ClientId:      v.ClientId,
					ClientSecret:  v.ClientSecret,
					Scopes:        v.Scopes,
					FieldMapping:  &fieldMapping,
					SkipTlsVerify: v.SkipTlsVerify,
					AuthStyle:     models.OAuth2AuthStyle(v.AuthStyle),
				},
			},
		}
	} else if v := identityProviderConfig.GetOidcConfig(); v != nil {
		fieldMapping := models.FieldMapping{
			Identifier:  v.FieldMapping.GetIdentifier(),
			DisplayName: v.FieldMapping.GetDisplayName(),
			Phone:       v.FieldMapping.GetPhone(),
			Groups:      v.FieldMapping.GetGroups(),
		}
		return &models.IdentityProviderConfig{
			Config: &models.IdentityProviderConfig_OidcConfig{
				OidcConfig: &models.OIDCIdentityProviderConfig{
					Issuer:        v.Issuer,
					ClientId:      v.ClientId,
					ClientSecret:  v.ClientSecret,
					FieldMapping:  &fieldMapping,
					SkipTlsVerify: v.SkipTlsVerify,
					AuthStyle:     models.OAuth2AuthStyle(v.AuthStyle),
					Scopes:        v.Scopes,
				},
			},
		}
	} else if v := identityProviderConfig.GetLdapConfig(); v != nil {
		fieldMapping := models.FieldMapping{
			Identifier:  v.FieldMapping.GetIdentifier(),
			DisplayName: v.FieldMapping.GetDisplayName(),
			Phone:       v.FieldMapping.GetPhone(),
			Groups:      v.FieldMapping.GetGroups(),
		}
		return &models.IdentityProviderConfig{
			Config: &models.IdentityProviderConfig_LdapConfig{
				LdapConfig: &models.LDAPIdentityProviderConfig{
					Host:             v.Host,
					Port:             v.Port,
					SkipTlsVerify:    v.SkipTlsVerify,
					BindDn:           v.BindDn,
					BindPassword:     v.BindPassword,
					BaseDn:           v.BaseDn,
					UserFilter:       v.UserFilter,
					SecurityProtocol: models.LDAPIdentityProviderConfig_SecurityProtocol(v.SecurityProtocol),
					FieldMapping:     &fieldMapping,
				},
			},
		}
	}
	return nil
}

// validIdentityProviderConfig validates that the config matches the declared
// provider type.
func validIdentityProviderConfig(identityProviderType v1pb.IdentityProviderType, identityProviderConfig *v1pb.IdentityProviderConfig) error {
	switch identityProviderType {
	case v1pb.IdentityProviderType_OAUTH2:
		if identityProviderConfig.GetOauth2Config() == nil {
			return errors.New("unexpected provider config value")
		}
	case v1pb.IdentityProviderType_OIDC:
		if identityProviderConfig.GetOidcConfig() == nil {
			return errors.New("unexpected provider config value")
		}
	case v1pb.IdentityProviderType_LDAP:
		if identityProviderConfig.GetLdapConfig() == nil {
			return errors.New("unexpected provider config value")
		}
	default:
		return errors.Errorf("unexpected provider type %s", identityProviderType)
	}
	return nil
}
