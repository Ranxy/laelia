# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [store/agent.proto](#store_agent-proto)
    - [AgentACPConfig](#laelia-store-AgentACPConfig)
    - [AgentCapability](#laelia-store-AgentCapability)
    - [AgentInfo](#laelia-store-AgentInfo)
    - [AgentInfo.LabelsEntry](#laelia-store-AgentInfo-LabelsEntry)
    - [AgentStatus](#laelia-store-AgentStatus)
  
    - [AgentStatus.ConnectionState](#laelia-store-AgentStatus-ConnectionState)
    - [AgentTokenState](#laelia-store-AgentTokenState)
    - [AgentTokenType](#laelia-store-AgentTokenType)
  
- [store/common.proto](#store_common-proto)
    - [PageToken](#laelia-store-PageToken)
    - [Position](#laelia-store-Position)
    - [Range](#laelia-store-Range)
  
- [store/group.proto](#store_group-proto)
    - [GroupMember](#laelia-store-GroupMember)
    - [GroupPayload](#laelia-store-GroupPayload)
  
    - [GroupMember.Role](#laelia-store-GroupMember-Role)
  
- [store/idp.proto](#store_idp-proto)
    - [FieldMapping](#laelia-store-FieldMapping)
    - [IdentityProviderConfig](#laelia-store-IdentityProviderConfig)
    - [IdentityProviderUserInfo](#laelia-store-IdentityProviderUserInfo)
    - [LDAPIdentityProviderConfig](#laelia-store-LDAPIdentityProviderConfig)
    - [OAuth2IdentityProviderConfig](#laelia-store-OAuth2IdentityProviderConfig)
    - [OIDCIdentityProviderConfig](#laelia-store-OIDCIdentityProviderConfig)
  
    - [IdentityProviderType](#laelia-store-IdentityProviderType)
    - [LDAPIdentityProviderConfig.SecurityProtocol](#laelia-store-LDAPIdentityProviderConfig-SecurityProtocol)
    - [OAuth2AuthStyle](#laelia-store-OAuth2AuthStyle)
  
- [store/policy.proto](#store_policy-proto)
    - [Binding](#laelia-store-Binding)
    - [EnvironmentTierPolicy](#laelia-store-EnvironmentTierPolicy)
    - [IamPolicy](#laelia-store-IamPolicy)
    - [Policy](#laelia-store-Policy)
    - [TagPolicy](#laelia-store-TagPolicy)
    - [TagPolicy.TagsEntry](#laelia-store-TagPolicy-TagsEntry)
  
    - [EnvironmentTierPolicy.EnvironmentTier](#laelia-store-EnvironmentTierPolicy-EnvironmentTier)
    - [Policy.Resource](#laelia-store-Policy-Resource)
    - [Policy.Type](#laelia-store-Policy-Type)
  
- [store/role.proto](#store_role-proto)
    - [RolePermissions](#laelia-store-RolePermissions)
  
- [store/setting.proto](#store_setting-proto)
    - [AgentSecuritySetting](#laelia-store-AgentSecuritySetting)
    - [EnvironmentSetting](#laelia-store-EnvironmentSetting)
    - [EnvironmentSetting.Environment](#laelia-store-EnvironmentSetting-Environment)
    - [EnvironmentSetting.Environment.TagsEntry](#laelia-store-EnvironmentSetting-Environment-TagsEntry)
    - [PasswordRestrictionSetting](#laelia-store-PasswordRestrictionSetting)
    - [WorkspaceProfileSetting](#laelia-store-WorkspaceProfileSetting)
  
    - [IPValidationPolicy](#laelia-store-IPValidationPolicy)
    - [SettingName](#laelia-store-SettingName)
  
- [store/user.proto](#store_user-proto)
    - [UserProfile](#laelia-store-UserProfile)
  
    - [PrincipalType](#laelia-store-PrincipalType)
  
- [Scalar Value Types](#scalar-value-types)



<a name="store_agent-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/agent.proto



<a name="laelia-store-AgentACPConfig"></a>

### AgentACPConfig



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| executable | [string](#string) |  |  |
| args | [string](#string) | repeated |  |
| allow_env | [string](#string) | repeated |  |






<a name="laelia-store-AgentCapability"></a>

### AgentCapability



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| supports_acp | [bool](#bool) |  |  |
| max_timeout_seconds | [int32](#int32) |  |  |
| supports_diff | [bool](#bool) |  |  |
| supports_raw_events | [bool](#bool) |  |  |
| supports_tool_traces | [bool](#bool) |  |  |
| max_event_count | [int32](#int32) |  |  |
| max_output_bytes | [int64](#int64) |  |  |
| supports_autonomous_decision | [bool](#bool) |  |  |






<a name="laelia-store-AgentInfo"></a>

### AgentInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_type | [string](#string) |  |  |
| hostname | [string](#string) |  |  |
| os | [string](#string) |  |  |
| arch | [string](#string) |  |  |
| ip | [string](#string) |  |  |
| version | [string](#string) |  |  |
| labels | [AgentInfo.LabelsEntry](#laelia-store-AgentInfo-LabelsEntry) | repeated |  |
| capability | [AgentCapability](#laelia-store-AgentCapability) |  |  |
| acp_config | [AgentACPConfig](#laelia-store-AgentACPConfig) |  |  |






<a name="laelia-store-AgentInfo-LabelsEntry"></a>

### AgentInfo.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-store-AgentStatus"></a>

### AgentStatus



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| state | [AgentStatus.ConnectionState](#laelia-store-AgentStatus-ConnectionState) |  |  |
| last_heartbeat_at | [int64](#int64) |  |  |
| connected_at | [int64](#int64) |  |  |
| error_message | [string](#string) |  |  |
| active_session_id | [string](#string) |  |  |





 


<a name="laelia-store-AgentStatus-ConnectionState"></a>

### AgentStatus.ConnectionState


| Name | Number | Description |
| ---- | ------ | ----------- |
| CONNECTION_STATE_UNSPECIFIED | 0 |  |
| ONLINE | 1 |  |
| OFFLINE | 2 |  |
| ERROR | 3 |  |
| KICKED | 4 |  |



<a name="laelia-store-AgentTokenState"></a>

### AgentTokenState


| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNSPECIFIED | 0 |  |
| ACTIVE | 1 |  |
| CONSUMED | 2 |  |
| REVOKED | 3 |  |



<a name="laelia-store-AgentTokenType"></a>

### AgentTokenType


| Name | Number | Description |
| ---- | ------ | ----------- |
| TOKEN_TYPE_UNSPECIFIED | 0 |  |
| BOOTSTRAP | 1 |  |
| ACCESS | 2 |  |
| REFRESH | 3 |  |


 

 

 



<a name="store_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/common.proto



<a name="laelia-store-PageToken"></a>

### PageToken
Used internally for obfuscating the page token.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| limit | [int32](#int32) |  |  |
| offset | [int32](#int32) |  |  |






<a name="laelia-store-Position"></a>

### Position
Position in a text expressed as zero-based line and zero-based column byte
offset.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| line | [int32](#int32) |  | Line position in a text (zero-based). |
| column | [int32](#int32) |  | Column position in a text (zero-based), equivalent to byte offset. |






<a name="laelia-store-Range"></a>

### Range



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| start | [int32](#int32) |  |  |
| end | [int32](#int32) |  |  |





 

 

 

 



<a name="store_group-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/group.proto



<a name="laelia-store-GroupMember"></a>

### GroupMember



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member | [string](#string) |  | Member is the principal who belong to this group.

Format: users/{userUID}. |
| role | [GroupMember.Role](#laelia-store-GroupMember-Role) |  |  |






<a name="laelia-store-GroupPayload"></a>

### GroupPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| members | [GroupMember](#laelia-store-GroupMember) | repeated |  |
| source | [string](#string) |  | source means where the group comes from. For now we support Entra ID SCIM sync, so the source could be Entra ID. |





 


<a name="laelia-store-GroupMember-Role"></a>

### GroupMember.Role


| Name | Number | Description |
| ---- | ------ | ----------- |
| ROLE_UNSPECIFIED | 0 |  |
| OWNER | 1 |  |
| MEMBER | 2 |  |


 

 

 



<a name="store_idp-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/idp.proto



<a name="laelia-store-FieldMapping"></a>

### FieldMapping
FieldMapping saves the field names from user info API of identity provider.
As we save all raw json string of user info response data into `principal.idp_user_info`,
we can extract the relevant data based with `FieldMapping`.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identifier | [string](#string) |  | Identifier is the field name of the unique identifier in 3rd-party idp user info. Required. |
| display_name | [string](#string) |  | DisplayName is the field name of display name in 3rd-party idp user info. Optional. |
| phone | [string](#string) |  | Phone is the field name of primary phone in 3rd-party idp user info. Optional. |
| groups | [string](#string) |  | Groups is the field name of groups in 3rd-party idp user info. Optional. Mainly used for OIDC: https://developer.okta.com/docs/guides/customize-tokens-groups-claim/main/ |






<a name="laelia-store-IdentityProviderConfig"></a>

### IdentityProviderConfig



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| oauth2_config | [OAuth2IdentityProviderConfig](#laelia-store-OAuth2IdentityProviderConfig) |  |  |
| oidc_config | [OIDCIdentityProviderConfig](#laelia-store-OIDCIdentityProviderConfig) |  |  |
| ldap_config | [LDAPIdentityProviderConfig](#laelia-store-LDAPIdentityProviderConfig) |  |  |






<a name="laelia-store-IdentityProviderUserInfo"></a>

### IdentityProviderUserInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| identifier | [string](#string) |  | Identifier is the value of the unique identifier in 3rd-party idp user info. |
| display_name | [string](#string) |  | DisplayName is the value of display name in 3rd-party idp user info. |
| phone | [string](#string) |  | Phone is the value of primary phone in 3rd-party idp user info. |
| groups | [string](#string) | repeated | Groups is the value of groups in 3rd-party idp user info. Mainly used for OIDC: https://developer.okta.com/docs/guides/customize-tokens-groups-claim/main/ |
| has_groups | [bool](#bool) |  |  |






<a name="laelia-store-LDAPIdentityProviderConfig"></a>

### LDAPIdentityProviderConfig
LDAPIdentityProviderConfig is the structure for LDAP identity provider config.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| host | [string](#string) |  | Host is the hostname or IP address of the LDAP server, e.g. &#34;ldap.example.com&#34;. |
| port | [int32](#int32) |  | Port is the port number of the LDAP server, e.g. 389. When not set, the default port of the corresponding security protocol will be used, i.e. 389 for StartTLS and 636 for LDAPS. |
| skip_tls_verify | [bool](#bool) |  | SkipTLSVerify controls whether to skip TLS certificate verification. |
| bind_dn | [string](#string) |  | BindDN is the DN of the user to bind as a service account to perform search requests. |
| bind_password | [string](#string) |  | BindPassword is the password of the user to bind as a service account. |
| base_dn | [string](#string) |  | BaseDN is the base DN to search for users, e.g. &#34;ou=users,dc=example,dc=com&#34;. |
| user_filter | [string](#string) |  | UserFilter is the filter to search for users, e.g. &#34;(uid=%s)&#34;. |
| security_protocol | [LDAPIdentityProviderConfig.SecurityProtocol](#laelia-store-LDAPIdentityProviderConfig-SecurityProtocol) |  | SecurityProtocol is the security protocol to be used for establishing connections with the LDAP server. |
| field_mapping | [FieldMapping](#laelia-store-FieldMapping) |  | FieldMapping is the mapping of the user attributes returned by the LDAP server. |






<a name="laelia-store-OAuth2IdentityProviderConfig"></a>

### OAuth2IdentityProviderConfig
OAuth2IdentityProviderConfig is the structure for OAuth2 identity provider config.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| auth_url | [string](#string) |  |  |
| token_url | [string](#string) |  |  |
| user_info_url | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| client_secret | [string](#string) |  |  |
| scopes | [string](#string) | repeated |  |
| field_mapping | [FieldMapping](#laelia-store-FieldMapping) |  |  |
| skip_tls_verify | [bool](#bool) |  |  |
| auth_style | [OAuth2AuthStyle](#laelia-store-OAuth2AuthStyle) |  |  |






<a name="laelia-store-OIDCIdentityProviderConfig"></a>

### OIDCIdentityProviderConfig
OIDCIdentityProviderConfig is the structure for OIDC identity provider config.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| issuer | [string](#string) |  |  |
| client_id | [string](#string) |  |  |
| client_secret | [string](#string) |  |  |
| field_mapping | [FieldMapping](#laelia-store-FieldMapping) |  |  |
| skip_tls_verify | [bool](#bool) |  |  |
| auth_style | [OAuth2AuthStyle](#laelia-store-OAuth2AuthStyle) |  |  |
| scopes | [string](#string) | repeated |  |





 


<a name="laelia-store-IdentityProviderType"></a>

### IdentityProviderType


| Name | Number | Description |
| ---- | ------ | ----------- |
| IDENTITY_PROVIDER_TYPE_UNSPECIFIED | 0 |  |
| OAUTH2 | 1 |  |
| OIDC | 2 |  |
| LDAP | 3 |  |



<a name="laelia-store-LDAPIdentityProviderConfig-SecurityProtocol"></a>

### LDAPIdentityProviderConfig.SecurityProtocol


| Name | Number | Description |
| ---- | ------ | ----------- |
| SECURITY_PROTOCOL_UNSPECIFIED | 0 |  |
| START_TLS | 1 | StartTLS is the security protocol that starts with an unencrypted connection and then upgrades to TLS. |
| LDAPS | 2 | LDAPS is the security protocol that uses TLS from the beginning. |



<a name="laelia-store-OAuth2AuthStyle"></a>

### OAuth2AuthStyle


| Name | Number | Description |
| ---- | ------ | ----------- |
| OAUTH2_AUTH_STYLE_UNSPECIFIED | 0 |  |
| IN_PARAMS | 1 | IN_PARAMS sends the &#34;client_id&#34; and &#34;client_secret&#34; in the POST body as application/x-www-form-urlencoded parameters. |
| IN_HEADER | 2 | IN_HEADER sends the client_id and client_password using HTTP Basic Authorization. This is an optional style described in the OAuth2 RFC 6749 section 2.3.1. |


 

 

 



<a name="store_policy-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/policy.proto



<a name="laelia-store-Binding"></a>

### Binding



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [string](#string) |  | The role that is assigned to the members. Format: roles/{role} |
| members | [string](#string) | repeated | Specifies the principals requesting access for a resource. For users, the member should be: users/{userUID} For groups, the member should be: groups/{email} |
| condition | [google.type.Expr](#google-type-Expr) |  | The condition that is associated with this binding. If the condition evaluates to true, then this binding applies to the current request. If the condition evaluates to false, then this binding does not apply to the current request. However, a different role binding might grant the same role to one or more of the principals in this binding. |






<a name="laelia-store-EnvironmentTierPolicy"></a>

### EnvironmentTierPolicy
EnvironmentTierPolicy is the tier of an environment.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| environment_tier | [EnvironmentTierPolicy.EnvironmentTier](#laelia-store-EnvironmentTierPolicy-EnvironmentTier) |  |  |
| color | [string](#string) |  |  |






<a name="laelia-store-IamPolicy"></a>

### IamPolicy



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| bindings | [Binding](#laelia-store-Binding) | repeated | Collection of binding. A binding binds one or more members or groups to a single role. |






<a name="laelia-store-Policy"></a>

### Policy







<a name="laelia-store-TagPolicy"></a>

### TagPolicy



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tags | [TagPolicy.TagsEntry](#laelia-store-TagPolicy-TagsEntry) | repeated | tags is the key - value map for resources. for example, the environment resource can have the sql review config tag, like &#34;ll.tag.review_config&#34;: &#34;reviewConfigs/{review config resource id}&#34; |






<a name="laelia-store-TagPolicy-TagsEntry"></a>

### TagPolicy.TagsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |





 


<a name="laelia-store-EnvironmentTierPolicy-EnvironmentTier"></a>

### EnvironmentTierPolicy.EnvironmentTier


| Name | Number | Description |
| ---- | ------ | ----------- |
| ENVIRONMENT_TIER_UNSPECIFIED | 0 |  |
| PROTECTED | 1 |  |
| UNPROTECTED | 2 |  |



<a name="laelia-store-Policy-Resource"></a>

### Policy.Resource


| Name | Number | Description |
| ---- | ------ | ----------- |
| RESOURCE_UNSPECIFIED | 0 |  |
| WORKSPACE | 1 |  |
| ENVIRONMENT | 2 |  |
| PROJECT | 3 |  |



<a name="laelia-store-Policy-Type"></a>

### Policy.Type


| Name | Number | Description |
| ---- | ------ | ----------- |
| TYPE_UNSPECIFIED | 0 |  |
| IAM | 1 |  |
| TAG | 2 |  |


 

 

 



<a name="store_role-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/role.proto



<a name="laelia-store-RolePermissions"></a>

### RolePermissions



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| permissions | [string](#string) | repeated |  |





 

 

 

 



<a name="store_setting-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/setting.proto



<a name="laelia-store-AgentSecuritySetting"></a>

### AgentSecuritySetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| heartbeat_interval_seconds | [int32](#int32) |  | Heartbeat interval in seconds. |
| offline_threshold_seconds | [int32](#int32) |  | Offline threshold in seconds. |
| bootstrap_token_duration | [google.protobuf.Duration](#google-protobuf-Duration) |  | Bootstrap token duration. |
| access_token_duration | [google.protobuf.Duration](#google-protobuf-Duration) |  | Access token duration. |
| refresh_token_duration | [google.protobuf.Duration](#google-protobuf-Duration) |  | Refresh token duration. |
| max_concurrent_sessions | [int32](#int32) |  | Max concurrent sessions per agent (default: 1). |
| ip_validation_policy | [IPValidationPolicy](#laelia-store-IPValidationPolicy) |  | IP validation policy. |
| heartbeat_rate_limit_per_minute | [int32](#int32) |  | Heartbeat rate limit per minute per agent. |
| connect_rate_limit_per_minute | [int32](#int32) |  | Connect rate limit per minute per IP. |






<a name="laelia-store-EnvironmentSetting"></a>

### EnvironmentSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| environments | [EnvironmentSetting.Environment](#laelia-store-EnvironmentSetting-Environment) | repeated |  |






<a name="laelia-store-EnvironmentSetting-Environment"></a>

### EnvironmentSetting.Environment



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| title | [string](#string) |  |  |
| tags | [EnvironmentSetting.Environment.TagsEntry](#laelia-store-EnvironmentSetting-Environment-TagsEntry) | repeated |  |
| color | [string](#string) |  |  |






<a name="laelia-store-EnvironmentSetting-Environment-TagsEntry"></a>

### EnvironmentSetting.Environment.TagsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-store-PasswordRestrictionSetting"></a>

### PasswordRestrictionSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| min_length | [int32](#int32) |  | min_length is the minimum length for password, should no less than 8. |
| require_number | [bool](#bool) |  | require_number requires the password must contains at least one number. |
| require_letter | [bool](#bool) |  | require_letter requires the password must contains at least one letter, regardless of upper case or lower case |
| require_uppercase_letter | [bool](#bool) |  | require_uppercase_letter requires the password must contains at least one upper case letter. |
| require_special_character | [bool](#bool) |  | require_uppercase_letter requires the password must contains at least one special character. |
| require_reset_password_for_first_login | [bool](#bool) |  | require_reset_password_for_first_login requires users to reset their password after the 1st login. |
| password_rotation | [google.protobuf.Duration](#google-protobuf-Duration) |  | password_rotation requires users to reset their password after the duration. |






<a name="laelia-store-WorkspaceProfileSetting"></a>

### WorkspaceProfileSetting



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| external_url | [string](#string) |  | The external URL is used for sso authentication callback. |
| disallow_signup | [bool](#bool) |  | Disallow self-service signup, users can only be invited by the owner. |
| require_2fa | [bool](#bool) |  | Require 2FA for all users. |
| token_duration | [google.protobuf.Duration](#google-protobuf-Duration) |  | The duration for token. |
| maximum_role_expiration | [google.protobuf.Duration](#google-protobuf-Duration) |  | The max duration for role expired. |
| domains | [string](#string) | repeated | The workspace domain, e.g. example.com. |
| enforce_identity_domain | [bool](#bool) |  | Only user and group from the domains can be created and login. |
| disallow_password_signin | [bool](#bool) |  | Whether to disallow password signin. (Except workspace admins) |
| enable_metric_collection | [bool](#bool) |  | Whether to enable metric collection for the workspace. |





 


<a name="laelia-store-IPValidationPolicy"></a>

### IPValidationPolicy
IP validation policy for agent connections.

| Name | Number | Description |
| ---- | ------ | ----------- |
| IP_VALIDATION_POLICY_UNSPECIFIED | 0 |  |
| IP_VALIDATION_OFF | 1 |  |
| IP_VALIDATION_WARN | 2 |  |
| IP_VALIDATION_STRICT | 3 |  |



<a name="laelia-store-SettingName"></a>

### SettingName


| Name | Number | Description |
| ---- | ------ | ----------- |
| SETTING_NAME_UNSPECIFIED | 0 |  |
| AUTH_SECRET | 1 |  |
| BRANDING_LOGO | 2 |  |
| WORKSPACE_ID | 3 |  |
| WORKSPACE_PROFILE | 4 |  |
| WORKSPACE_APPROVAL | 5 |  |
| WORKSPACE_EXTERNAL_APPROVAL | 6 |  |
| PASSWORD_RESTRICTION | 7 |  |
| ENVIRONMENT | 8 |  |
| AGENT_SECURITY | 9 |  |


 

 

 



<a name="store_user-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## store/user.proto



<a name="laelia-store-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| source | [string](#string) |  | source means where the user comes from. For now we support Entra ID SCIM sync, so the source could be Entra ID. |





 


<a name="laelia-store-PrincipalType"></a>

### PrincipalType
PrincipalType is the type of a principal.

| Name | Number | Description |
| ---- | ------ | ----------- |
| PRINCIPAL_TYPE_UNSPECIFIED | 0 |  |
| END_USER | 1 | END_USER represents the human being. |
| SERVICE_ACCOUNT | 2 | SERVICE_ACCOUNT represents the external service calling OpenAPI. |
| SYSTEM_BOT | 3 | SYSTEM_BOT represents the internal system bot performing operations. |


 

 

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

