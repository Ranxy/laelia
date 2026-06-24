# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [v1/annotation.proto](#v1_annotation-proto)
    - [AuthMethod](#laelia-v1-AuthMethod)
  
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
    - [File-level Extensions](#v1_annotation-proto-extensions)
  
- [v1/common.proto](#v1_common-proto)
    - [Position](#laelia-v1-Position)
    - [Range](#laelia-v1-Range)
  
    - [RiskLevel](#laelia-v1-RiskLevel)
    - [State](#laelia-v1-State)
  
- [v1/agent.proto](#v1_agent-proto)
    - [Agent](#laelia-v1-Agent)
    - [Agent.LabelsEntry](#laelia-v1-Agent-LabelsEntry)
    - [AgentCapability](#laelia-v1-AgentCapability)
    - [AgentDisconnectRequest](#laelia-v1-AgentDisconnectRequest)
    - [AgentHeartbeatRequest](#laelia-v1-AgentHeartbeatRequest)
    - [AgentHeartbeatResponse](#laelia-v1-AgentHeartbeatResponse)
    - [AgentInfo](#laelia-v1-AgentInfo)
    - [AgentInfo.LabelsEntry](#laelia-v1-AgentInfo-LabelsEntry)
    - [AgentMetrics](#laelia-v1-AgentMetrics)
    - [AgentSession](#laelia-v1-AgentSession)
    - [AgentStatus](#laelia-v1-AgentStatus)
    - [ConnectAgentRequest](#laelia-v1-ConnectAgentRequest)
    - [ConnectAgentResponse](#laelia-v1-ConnectAgentResponse)
    - [CreateAgentRequest](#laelia-v1-CreateAgentRequest)
    - [CreateAgentResponse](#laelia-v1-CreateAgentResponse)
    - [DeleteAgentRequest](#laelia-v1-DeleteAgentRequest)
    - [ForceDisconnectAgentRequest](#laelia-v1-ForceDisconnectAgentRequest)
    - [GetAgentRequest](#laelia-v1-GetAgentRequest)
    - [HelloRequest](#laelia-v1-HelloRequest)
    - [HelloResponse](#laelia-v1-HelloResponse)
    - [ListAgentSessionsRequest](#laelia-v1-ListAgentSessionsRequest)
    - [ListAgentSessionsResponse](#laelia-v1-ListAgentSessionsResponse)
    - [ListAgentsRequest](#laelia-v1-ListAgentsRequest)
    - [ListAgentsResponse](#laelia-v1-ListAgentsResponse)
    - [PendingCommandHint](#laelia-v1-PendingCommandHint)
    - [PendingCommandHint.EnvEntry](#laelia-v1-PendingCommandHint-EnvEntry)
    - [RefreshAgentTokenRequest](#laelia-v1-RefreshAgentTokenRequest)
    - [RefreshAgentTokenResponse](#laelia-v1-RefreshAgentTokenResponse)
    - [RevokeAgentTokenRequest](#laelia-v1-RevokeAgentTokenRequest)
    - [RevokeAgentTokenResponse](#laelia-v1-RevokeAgentTokenResponse)
    - [RotateAgentTokenRequest](#laelia-v1-RotateAgentTokenRequest)
    - [RotateAgentTokenResponse](#laelia-v1-RotateAgentTokenResponse)
    - [UpdateAgentACPConfigRequest](#laelia-v1-UpdateAgentACPConfigRequest)
  
    - [AgentStatus.ConnectionState](#laelia-v1-AgentStatus-ConnectionState)
  
    - [AgentService](#laelia-v1-AgentService)
  
- [v1/user_service.proto](#v1_user_service-proto)
    - [BatchGetUsersRequest](#laelia-v1-BatchGetUsersRequest)
    - [BatchGetUsersResponse](#laelia-v1-BatchGetUsersResponse)
    - [CreateUserRequest](#laelia-v1-CreateUserRequest)
    - [DeleteUserRequest](#laelia-v1-DeleteUserRequest)
    - [GetUserRequest](#laelia-v1-GetUserRequest)
    - [ListUsersRequest](#laelia-v1-ListUsersRequest)
    - [ListUsersResponse](#laelia-v1-ListUsersResponse)
    - [UndeleteUserRequest](#laelia-v1-UndeleteUserRequest)
    - [UpdateUserRequest](#laelia-v1-UpdateUserRequest)
    - [User](#laelia-v1-User)
    - [UserProfile](#laelia-v1-UserProfile)
  
    - [UserType](#laelia-v1-UserType)
  
    - [UserService](#laelia-v1-UserService)
  
- [v1/auth_service.proto](#v1_auth_service-proto)
    - [IdentityProviderContext](#laelia-v1-IdentityProviderContext)
    - [LoginRequest](#laelia-v1-LoginRequest)
    - [LoginResponse](#laelia-v1-LoginResponse)
    - [LogoutRequest](#laelia-v1-LogoutRequest)
    - [OAuth2IdentityProviderContext](#laelia-v1-OAuth2IdentityProviderContext)
  
    - [AuthService](#laelia-v1-AuthService)
  
- [v1/command.proto](#v1_command-proto)
    - [ActionResponse](#laelia-v1-ActionResponse)
    - [AddChannelMemberRequest](#laelia-v1-AddChannelMemberRequest)
    - [AgentActivity](#laelia-v1-AgentActivity)
    - [AgentReady](#laelia-v1-AgentReady)
    - [AgentStreamMessage](#laelia-v1-AgentStreamMessage)
    - [CancelCommandRequest](#laelia-v1-CancelCommandRequest)
    - [CancelMessage](#laelia-v1-CancelMessage)
    - [ChannelMember](#laelia-v1-ChannelMember)
    - [ChatHistoryEntry](#laelia-v1-ChatHistoryEntry)
    - [ChatMessage](#laelia-v1-ChatMessage)
    - [Command](#laelia-v1-Command)
    - [Command.EnvEntry](#laelia-v1-Command-EnvEntry)
    - [CommandEvent](#laelia-v1-CommandEvent)
    - [CommandOutput](#laelia-v1-CommandOutput)
    - [CommandProgress](#laelia-v1-CommandProgress)
    - [CommandRequest](#laelia-v1-CommandRequest)
    - [CommandRequest.EnvEntry](#laelia-v1-CommandRequest-EnvEntry)
    - [CommandResult](#laelia-v1-CommandResult)
    - [Conversation](#laelia-v1-Conversation)
    - [CreateChannelRequest](#laelia-v1-CreateChannelRequest)
    - [DeleteChannelRequest](#laelia-v1-DeleteChannelRequest)
    - [DiffEmittedPayload](#laelia-v1-DiffEmittedPayload)
    - [FetchConversationActivityRequest](#laelia-v1-FetchConversationActivityRequest)
    - [FetchConversationActivityResponse](#laelia-v1-FetchConversationActivityResponse)
    - [FinalSummaryPayload](#laelia-v1-FinalSummaryPayload)
    - [GetChannelRequest](#laelia-v1-GetChannelRequest)
    - [GetCommandContextRequest](#laelia-v1-GetCommandContextRequest)
    - [GetCommandContextResponse](#laelia-v1-GetCommandContextResponse)
    - [GetCommandRequest](#laelia-v1-GetCommandRequest)
    - [GetOrCreateConversationRequest](#laelia-v1-GetOrCreateConversationRequest)
    - [GetOrCreateConversationResponse](#laelia-v1-GetOrCreateConversationResponse)
    - [LifecyclePayload](#laelia-v1-LifecyclePayload)
    - [ListChannelMembersRequest](#laelia-v1-ListChannelMembersRequest)
    - [ListChannelMembersResponse](#laelia-v1-ListChannelMembersResponse)
    - [ListChannelsRequest](#laelia-v1-ListChannelsRequest)
    - [ListChannelsResponse](#laelia-v1-ListChannelsResponse)
    - [ListCommandsRequest](#laelia-v1-ListCommandsRequest)
    - [ListCommandsResponse](#laelia-v1-ListCommandsResponse)
    - [ListConversationMessagesRequest](#laelia-v1-ListConversationMessagesRequest)
    - [ListConversationMessagesResponse](#laelia-v1-ListConversationMessagesResponse)
    - [ManagerStreamMessage](#laelia-v1-ManagerStreamMessage)
    - [MessageSnapshot](#laelia-v1-MessageSnapshot)
    - [NewMessagesAvailable](#laelia-v1-NewMessagesAvailable)
    - [PermissionDecidedPayload](#laelia-v1-PermissionDecidedPayload)
    - [PermissionDecision](#laelia-v1-PermissionDecision)
    - [PermissionOptionPayload](#laelia-v1-PermissionOptionPayload)
    - [PermissionRequestedPayload](#laelia-v1-PermissionRequestedPayload)
    - [PermissionTimedOutPayload](#laelia-v1-PermissionTimedOutPayload)
    - [Ping](#laelia-v1-Ping)
    - [Pong](#laelia-v1-Pong)
    - [PostMessageRequest](#laelia-v1-PostMessageRequest)
    - [PostMessageResponse](#laelia-v1-PostMessageResponse)
    - [PullMessages](#laelia-v1-PullMessages)
    - [RawAcpPayload](#laelia-v1-RawAcpPayload)
    - [RemoveChannelMemberRequest](#laelia-v1-RemoveChannelMemberRequest)
    - [ResolveHeldAction](#laelia-v1-ResolveHeldAction)
    - [RespondPermissionRequest](#laelia-v1-RespondPermissionRequest)
    - [SearchChatHistoryRequest](#laelia-v1-SearchChatHistoryRequest)
    - [SearchChatHistoryResponse](#laelia-v1-SearchChatHistoryResponse)
    - [SendMessageRequest](#laelia-v1-SendMessageRequest)
    - [SubmitAction](#laelia-v1-SubmitAction)
    - [SubmitAction.EnvEntry](#laelia-v1-SubmitAction-EnvEntry)
    - [TextDeltaPayload](#laelia-v1-TextDeltaPayload)
    - [ToolCallFinishedPayload](#laelia-v1-ToolCallFinishedPayload)
    - [ToolCallStartedPayload](#laelia-v1-ToolCallStartedPayload)
    - [UpdateChannelRequest](#laelia-v1-UpdateChannelRequest)
    - [WarningPayload](#laelia-v1-WarningPayload)
    - [WatchCommandEventsRequest](#laelia-v1-WatchCommandEventsRequest)
    - [WatchCommandRequest](#laelia-v1-WatchCommandRequest)
  
    - [ActionResolution](#laelia-v1-ActionResolution)
    - [CommandEventType](#laelia-v1-CommandEventType)
    - [CommandOutput.StreamType](#laelia-v1-CommandOutput-StreamType)
    - [CommandStatus](#laelia-v1-CommandStatus)
    - [SenderType](#laelia-v1-SenderType)
  
    - [AgentStreamService](#laelia-v1-AgentStreamService)
    - [CommandService](#laelia-v1-CommandService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="v1_annotation-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/annotation.proto


 


<a name="laelia-v1-AuthMethod"></a>

### AuthMethod


| Name | Number | Description |
| ---- | ------ | ----------- |
| AUTH_METHOD_UNSPECIFIED | 0 |  |
| IAM | 1 | IAM uses the standard IAM authorization check on the organizational resources. |
| CUSTOM | 2 | Custom authorization method. |


 


<a name="v1_annotation-proto-extensions"></a>

### File-level Extensions
| Extension | Type | Base | Number | Description |
| --------- | ---- | ---- | ------ | ----------- |
| allow_without_credential | bool | .google.protobuf.MethodOptions | 100000 |  |
| audit | bool | .google.protobuf.MethodOptions | 100003 |  |
| auth_method | AuthMethod | .google.protobuf.MethodOptions | 100002 |  |
| permission | string | .google.protobuf.MethodOptions | 100001 |  |

 

 



<a name="v1_common-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/common.proto



<a name="laelia-v1-Position"></a>

### Position
Position in a text expressed as zero-based line and zero-based column byte
offset.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| line | [int32](#int32) |  | Line position in a text (zero-based). |
| column | [int32](#int32) |  | Column position in a text (zero-based), equivalent to byte offset. |






<a name="laelia-v1-Range"></a>

### Range



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| start | [int32](#int32) |  |  |
| end | [int32](#int32) |  |  |





 


<a name="laelia-v1-RiskLevel"></a>

### RiskLevel
RiskLevel is the risk level.

| Name | Number | Description |
| ---- | ------ | ----------- |
| RISK_LEVEL_UNSPECIFIED | 0 |  |
| LOW | 1 |  |
| MODERATE | 2 |  |
| HIGH | 3 |  |



<a name="laelia-v1-State"></a>

### State


| Name | Number | Description |
| ---- | ------ | ----------- |
| STATE_UNSPECIFIED | 0 |  |
| ACTIVE | 1 |  |
| DELETED | 2 |  |


 

 

 



<a name="v1_agent-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/agent.proto



<a name="laelia-v1-Agent"></a>

### Agent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| state | [State](#laelia-v1-State) |  |  |
| title | [string](#string) |  |  |
| info | [AgentInfo](#laelia-v1-AgentInfo) |  |  |
| status | [AgentStatus](#laelia-v1-AgentStatus) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| labels | [Agent.LabelsEntry](#laelia-v1-Agent-LabelsEntry) | repeated |  |
| last_token_rotated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| token_version | [int32](#int32) |  |  |






<a name="laelia-v1-Agent-LabelsEntry"></a>

### Agent.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-AgentCapability"></a>

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






<a name="laelia-v1-AgentDisconnectRequest"></a>

### AgentDisconnectRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| reason | [string](#string) |  | &#34;shutdown&#34;, &#34;upgrade&#34; etc. |






<a name="laelia-v1-AgentHeartbeatRequest"></a>

### AgentHeartbeatRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| previous_nonce | [string](#string) |  | nonce from previous response (replay protection) |
| metrics | [AgentMetrics](#laelia-v1-AgentMetrics) |  | optional agent metrics |






<a name="laelia-v1-AgentHeartbeatResponse"></a>

### AgentHeartbeatResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_nonce | [string](#string) |  | nonce for next request |
| next_heartbeat_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | expected next heartbeat time |
| access_token | [string](#string) |  | new access token (only if expiring soon) |
| access_token_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| command_stream_required | [bool](#bool) |  | Fallback channel: when bidi command stream is unavailable |
| pending_command_hint | [PendingCommandHint](#laelia-v1-PendingCommandHint) |  |  |






<a name="laelia-v1-AgentInfo"></a>

### AgentInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_type | [string](#string) |  |  |
| hostname | [string](#string) |  |  |
| os | [string](#string) |  |  |
| arch | [string](#string) |  |  |
| ip | [string](#string) |  |  |
| version | [string](#string) |  |  |
| labels | [AgentInfo.LabelsEntry](#laelia-v1-AgentInfo-LabelsEntry) | repeated |  |
| capability | [AgentCapability](#laelia-v1-AgentCapability) |  |  |
| acp_config_yaml | [string](#string) |  |  |






<a name="laelia-v1-AgentInfo-LabelsEntry"></a>

### AgentInfo.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-AgentMetrics"></a>

### AgentMetrics



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| cpu_percent | [double](#double) |  |  |
| memory_used_bytes | [uint64](#uint64) |  |  |
| memory_total_bytes | [uint64](#uint64) |  |  |
| disk_used_bytes | [uint64](#uint64) |  |  |
| disk_total_bytes | [uint64](#uint64) |  |  |
| uptime_seconds | [uint32](#uint32) |  |  |
| goroutine_count | [uint32](#uint32) |  |  |






<a name="laelia-v1-AgentSession"></a>

### AgentSession



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| agent_name | [string](#string) |  |  |
| source_ip | [string](#string) |  |  |
| agent_version | [string](#string) |  |  |
| fingerprint | [string](#string) |  |  |
| connected_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_heartbeat_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| disconnected_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| disconnect_reason | [string](#string) |  |  |
| state | [AgentStatus.ConnectionState](#laelia-v1-AgentStatus-ConnectionState) |  |  |






<a name="laelia-v1-AgentStatus"></a>

### AgentStatus



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| state | [AgentStatus.ConnectionState](#laelia-v1-AgentStatus-ConnectionState) |  |  |
| last_heartbeat_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| connected_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| error_message | [string](#string) |  |  |
| active_session_id | [string](#string) |  | current active session ID |






<a name="laelia-v1-ConnectAgentRequest"></a>

### ConnectAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| bootstrap_token | [string](#string) |  | first connection or after refresh failure |
| info | [AgentInfo](#laelia-v1-AgentInfo) |  |  |
| fingerprint | [string](#string) |  | client-generated connection fingerprint (hostname:os:arch) |






<a name="laelia-v1-ConnectAgentResponse"></a>

### ConnectAgentResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| access_token | [string](#string) |  | 15-minute validity |
| refresh_token | [string](#string) |  | 24-hour validity, single-use rotation |
| session_id | [string](#string) |  | session identifier |
| next_nonce | [string](#string) |  | server-signed nonce for next heartbeat |
| access_token_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| initial_status | [AgentStatus](#laelia-v1-AgentStatus) |  |  |
| acp_config_yaml | [string](#string) |  | server-provided ACP YAML config (empty when agent uses local file) |






<a name="laelia-v1-CreateAgentRequest"></a>

### CreateAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [Agent](#laelia-v1-Agent) |  |  |






<a name="laelia-v1-CreateAgentResponse"></a>

### CreateAgentResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [Agent](#laelia-v1-Agent) |  |  |
| bootstrap_token | [string](#string) |  | 7-day validity, reusable until rotated/revoked |






<a name="laelia-v1-DeleteAgentRequest"></a>

### DeleteAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-ForceDisconnectAgentRequest"></a>

### ForceDisconnectAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="laelia-v1-GetAgentRequest"></a>

### GetAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-HelloRequest"></a>

### HelloRequest







<a name="laelia-v1-HelloResponse"></a>

### HelloResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| current_time | [int64](#int64) |  |  |
| server_version | [string](#string) |  |  |






<a name="laelia-v1-ListAgentSessionsRequest"></a>

### ListAgentSessionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| include_terminated | [bool](#bool) |  | default: only active sessions |






<a name="laelia-v1-ListAgentSessionsResponse"></a>

### ListAgentSessionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| sessions | [AgentSession](#laelia-v1-AgentSession) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListAgentsRequest"></a>

### ListAgentsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| show_deleted | [bool](#bool) |  |  |






<a name="laelia-v1-ListAgentsResponse"></a>

### ListAgentsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agents | [Agent](#laelia-v1-Agent) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-PendingCommandHint"></a>

### PendingCommandHint



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| command | [string](#string) |  |  |
| env | [PendingCommandHint.EnvEntry](#laelia-v1-PendingCommandHint-EnvEntry) | repeated |  |
| working_dir | [string](#string) |  |  |
| timeout_seconds | [int32](#int32) |  |  |






<a name="laelia-v1-PendingCommandHint-EnvEntry"></a>

### PendingCommandHint.EnvEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-RefreshAgentTokenRequest"></a>

### RefreshAgentTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| refresh_token | [string](#string) |  |  |
| fingerprint | [string](#string) |  | verify connection fingerprint |






<a name="laelia-v1-RefreshAgentTokenResponse"></a>

### RefreshAgentTokenResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| access_token | [string](#string) |  |  |
| refresh_token | [string](#string) |  | new refresh token (rotation) |
| access_token_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-RevokeAgentTokenRequest"></a>

### RevokeAgentTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="laelia-v1-RevokeAgentTokenResponse"></a>

### RevokeAgentTokenResponse







<a name="laelia-v1-RotateAgentTokenRequest"></a>

### RotateAgentTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  | audit purpose |






<a name="laelia-v1-RotateAgentTokenResponse"></a>

### RotateAgentTokenResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| bootstrap_token | [string](#string) |  | new bootstrap token |






<a name="laelia-v1-UpdateAgentACPConfigRequest"></a>

### UpdateAgentACPConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| acp_config_yaml | [string](#string) |  |  |





 


<a name="laelia-v1-AgentStatus-ConnectionState"></a>

### AgentStatus.ConnectionState


| Name | Number | Description |
| ---- | ------ | ----------- |
| CONNECTION_STATE_UNSPECIFIED | 0 |  |
| ONLINE | 1 |  |
| OFFLINE | 2 |  |
| ERROR | 3 |  |
| KICKED | 4 | evicted by a new connection |


 

 


<a name="laelia-v1-AgentService"></a>

### AgentService
========== Management APIs (IAM auth, admin only) ==========

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| CreateAgent | [CreateAgentRequest](#laelia-v1-CreateAgentRequest) | [CreateAgentResponse](#laelia-v1-CreateAgentResponse) |  |
| ListAgents | [ListAgentsRequest](#laelia-v1-ListAgentsRequest) | [ListAgentsResponse](#laelia-v1-ListAgentsResponse) |  |
| GetAgent | [GetAgentRequest](#laelia-v1-GetAgentRequest) | [Agent](#laelia-v1-Agent) |  |
| DeleteAgent | [DeleteAgentRequest](#laelia-v1-DeleteAgentRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| RotateAgentToken | [RotateAgentTokenRequest](#laelia-v1-RotateAgentTokenRequest) | [RotateAgentTokenResponse](#laelia-v1-RotateAgentTokenResponse) | Token rotation: generate a new bootstrap token, old token invalid after grace period |
| RevokeAgentToken | [RevokeAgentTokenRequest](#laelia-v1-RevokeAgentTokenRequest) | [RevokeAgentTokenResponse](#laelia-v1-RevokeAgentTokenResponse) | Token revocation: revoke all tokens for the agent |
| ForceDisconnectAgent | [ForceDisconnectAgentRequest](#laelia-v1-ForceDisconnectAgentRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Admin force disconnects an agent connection |
| ListAgentSessions | [ListAgentSessionsRequest](#laelia-v1-ListAgentSessionsRequest) | [ListAgentSessionsResponse](#laelia-v1-ListAgentSessionsResponse) | List agent sessions |
| UpdateAgentACPConfig | [UpdateAgentACPConfigRequest](#laelia-v1-UpdateAgentACPConfigRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Update agent ACP config YAML (admin only) |
| ConnectAgent | [ConnectAgentRequest](#laelia-v1-ConnectAgentRequest) | [ConnectAgentResponse](#laelia-v1-ConnectAgentResponse) | Agent initial connection using bootstrap token |
| AgentHeartbeat | [AgentHeartbeatRequest](#laelia-v1-AgentHeartbeatRequest) | [AgentHeartbeatResponse](#laelia-v1-AgentHeartbeatResponse) | Agent heartbeat |
| AgentDisconnect | [AgentDisconnectRequest](#laelia-v1-AgentDisconnectRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Agent graceful disconnect |
| RefreshAgentToken | [RefreshAgentTokenRequest](#laelia-v1-RefreshAgentTokenRequest) | [RefreshAgentTokenResponse](#laelia-v1-RefreshAgentTokenResponse) | Agent refreshes access token |
| Hello | [HelloRequest](#laelia-v1-HelloRequest) | [HelloResponse](#laelia-v1-HelloResponse) | Health check (no auth required) |

 



<a name="v1_user_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/user_service.proto



<a name="laelia-v1-BatchGetUsersRequest"></a>

### BatchGetUsersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| names | [string](#string) | repeated | The user names to retrieve. Format: users/{user uid or user email} |






<a name="laelia-v1-BatchGetUsersResponse"></a>

### BatchGetUsersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| users | [User](#laelia-v1-User) | repeated | The users from the specified request. |






<a name="laelia-v1-CreateUserRequest"></a>

### CreateUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [User](#laelia-v1-User) |  | The user to create. |






<a name="laelia-v1-DeleteUserRequest"></a>

### DeleteUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user to delete. Format: users/{user} |






<a name="laelia-v1-GetUserRequest"></a>

### GetUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user to retrieve. Format: users/{user uid or user email} |






<a name="laelia-v1-ListUsersRequest"></a>

### ListUsersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of users to return. The service may return fewer than this value. If unspecified, at most 10 users will be returned. The maximum value is 1000; values above 1000 will be coerced to 1000. |
| page_token | [string](#string) |  | A page token, received from a previous `ListUsers` call. Provide this to retrieve the subsequent page.

When paginating, all other parameters provided to `ListUsers` must match the call that provided the page token. |
| show_deleted | [bool](#bool) |  | Show deleted users if specified. |
| filter | [string](#string) |  | Filter is used to filter users returned in the list. The syntax and semantics of CEL are documented at https://github.com/google/cel-spec

Supported filter: - name: the user name, support &#34;==&#34; and &#34;.matches()&#34; operator. - email: the user email, support &#34;==&#34; and &#34;.matches()&#34; operator. - user_type: the type, check UserType enum for values, support &#34;==&#34;, &#34;in [xx]&#34;, &#34;!(in [xx])&#34; operator. - state: check State enum for values, support &#34;==&#34; operator. - project: the project full name in &#34;projects/{id}&#34; format, support &#34;==&#34; operator.

For example: name == &#34;ed&#34; name.matches(&#34;ed&#34;) email == &#34;ed@example.com&#34; email.matches(&#34;ed&#34;) user_type == &#34;SERVICE_ACCOUNT&#34; user_type in [&#34;SERVICE_ACCOUNT&#34;, &#34;USER&#34;] !(user_type in [&#34;SERVICE_ACCOUNT&#34;, &#34;USER&#34;]) state == &#34;DELETED&#34; project == &#34;projects/sample-project&#34; You can combine filter conditions like: name.matches(&#34;ed&#34;) &amp;&amp; project == &#34;projects/sample-project&#34; (name == &#34;ed&#34; || email == &#34;ed@example.com&#34;) &amp;&amp; project == &#34;projects/sample-project&#34; |






<a name="laelia-v1-ListUsersResponse"></a>

### ListUsersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| users | [User](#laelia-v1-User) | repeated | The users from the specified request. |
| next_page_token | [string](#string) |  | A token, which can be sent as `page_token` to retrieve the next page. If this field is omitted, there are no subsequent pages. |






<a name="laelia-v1-UndeleteUserRequest"></a>

### UndeleteUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the deleted user. Format: users/{user} |






<a name="laelia-v1-UpdateUserRequest"></a>

### UpdateUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [User](#laelia-v1-User) |  | The user to update.

The user&#39;s `name` field is used to identify the user to update. Format: users/{user} |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update. |
| allow_missing | [bool](#bool) |  | If set to true, and the user is not found, a new user will be created. In this situation, `update_mask` is ignored. |






<a name="laelia-v1-User"></a>

### User



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user. Format: users/{user}. {user} is a system-generated unique ID. |
| state | [State](#laelia-v1-State) |  |  |
| email | [string](#string) |  |  |
| title | [string](#string) |  |  |
| user_type | [UserType](#laelia-v1-UserType) |  |  |
| password | [string](#string) |  |  |
| service_key | [string](#string) |  |  |
| recovery_codes | [string](#string) | repeated | The recovery_codes is the temporary recovery codes using in two phase verification. |
| phone | [string](#string) |  | Should be a valid E.164 compliant phone number. Could be empty. |
| profile | [UserProfile](#laelia-v1-UserProfile) |  |  |
| groups | [string](#string) | repeated | The groups for the user. Format: groups/{email} |






<a name="laelia-v1-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| source | [string](#string) |  | source means where the user comes from. For now we support Entra ID SCIM sync, so the source could be Entra ID. |





 


<a name="laelia-v1-UserType"></a>

### UserType


| Name | Number | Description |
| ---- | ------ | ----------- |
| USER_TYPE_UNSPECIFIED | 0 |  |
| USER | 1 |  |
| SERVICE_ACCOUNT | 2 |  |
| SYSTEM_BOT | 3 |  |


 

 


<a name="laelia-v1-UserService"></a>

### UserService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetUser | [GetUserRequest](#laelia-v1-GetUserRequest) | [User](#laelia-v1-User) | Get the user. Any authenticated user can get the user. |
| BatchGetUsers | [BatchGetUsersRequest](#laelia-v1-BatchGetUsersRequest) | [BatchGetUsersResponse](#laelia-v1-BatchGetUsersResponse) | Get the users in batch. Any authenticated user can batch get users. |
| GetCurrentUser | [.google.protobuf.Empty](#google-protobuf-Empty) | [User](#laelia-v1-User) | Get the current authenticated user. Permissions required: None |
| ListUsers | [ListUsersRequest](#laelia-v1-ListUsersRequest) | [ListUsersResponse](#laelia-v1-ListUsersResponse) | List all users. Any authenticated user can list users. |
| CreateUser | [CreateUserRequest](#laelia-v1-CreateUserRequest) | [User](#laelia-v1-User) | Create a user. |
| UpdateUser | [UpdateUserRequest](#laelia-v1-UpdateUserRequest) | [User](#laelia-v1-User) | Only the user itself and the user with permission on the workspace can update the user. |
| DeleteUser | [DeleteUserRequest](#laelia-v1-DeleteUserRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Only the user with permission on the workspace can delete the user. The last remaining workspace admin cannot be deleted. |
| UndeleteUser | [UndeleteUserRequest](#laelia-v1-UndeleteUserRequest) | [User](#laelia-v1-User) | Only the user with permission on the workspace can undelete the user. |

 



<a name="v1_auth_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/auth_service.proto



<a name="laelia-v1-IdentityProviderContext"></a>

### IdentityProviderContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| oauth2_context | [OAuth2IdentityProviderContext](#laelia-v1-OAuth2IdentityProviderContext) |  |  |






<a name="laelia-v1-LoginRequest"></a>

### LoginRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| email | [string](#string) |  |  |
| password | [string](#string) |  |  |
| web | [bool](#bool) |  | If web is set, we will set access token, refresh token, and user to the cookie. |
| idp_name | [string](#string) |  | The name of the identity provider. Format: idps/{idp} |
| idp_context | [IdentityProviderContext](#laelia-v1-IdentityProviderContext) |  | The idp_context is using to get the user information from identity provider. |






<a name="laelia-v1-LoginResponse"></a>

### LoginResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| token | [string](#string) |  |  |
| require_reset_password | [bool](#bool) |  |  |
| user | [User](#laelia-v1-User) |  | The user of successful login. |






<a name="laelia-v1-LogoutRequest"></a>

### LogoutRequest







<a name="laelia-v1-OAuth2IdentityProviderContext"></a>

### OAuth2IdentityProviderContext



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| code | [string](#string) |  |  |





 

 

 


<a name="laelia-v1-AuthService"></a>

### AuthService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| Login | [LoginRequest](#laelia-v1-LoginRequest) | [LoginResponse](#laelia-v1-LoginResponse) | Permissions required: None |
| Logout | [LogoutRequest](#laelia-v1-LogoutRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Permissions required: None |

 



<a name="v1_command-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/command.proto



<a name="laelia-v1-ActionResponse"></a>

### ActionResponse
ActionResponse is the manager&#39;s reply to SubmitAction. When committed=true
the action was accepted and a command was created; when committed=false the
action is held pending agent resolution via ResolveHeldAction.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action_id | [string](#string) |  |  |
| committed | [bool](#bool) |  |  |
| command_id | [string](#string) |  |  |
| current_version | [int64](#int64) |  |  |
| new_messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |






<a name="laelia-v1-AddChannelMemberRequest"></a>

### AddChannelMemberRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| member_type | [int32](#int32) |  |  |
| member_id | [string](#string) |  |  |






<a name="laelia-v1-AgentActivity"></a>

### AgentActivity



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_id | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| status | [string](#string) |  |  |
| tool_name | [string](#string) |  |  |






<a name="laelia-v1-AgentReady"></a>

### AgentReady



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| last_command_id | [string](#string) |  |  |
| last_ack_seq | [int32](#int32) |  |  |
| last_event_seq | [int32](#int32) |  |  |






<a name="laelia-v1-AgentStreamMessage"></a>

### AgentStreamMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_ready | [AgentReady](#laelia-v1-AgentReady) |  |  |
| pull_messages | [PullMessages](#laelia-v1-PullMessages) |  |  |
| submit_action | [SubmitAction](#laelia-v1-SubmitAction) |  |  |
| resolve_held_action | [ResolveHeldAction](#laelia-v1-ResolveHeldAction) |  |  |
| progress | [CommandProgress](#laelia-v1-CommandProgress) |  |  |
| result | [CommandResult](#laelia-v1-CommandResult) |  |  |
| event | [CommandEvent](#laelia-v1-CommandEvent) |  |  |
| ping | [Ping](#laelia-v1-Ping) |  |  |






<a name="laelia-v1-CancelCommandRequest"></a>

### CancelCommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="laelia-v1-CancelMessage"></a>

### CancelMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |






<a name="laelia-v1-ChannelMember"></a>

### ChannelMember



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member_type | [int32](#int32) |  |  |
| member_id | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| member_role | [int32](#int32) |  |  |
| joined_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-ChatHistoryEntry"></a>

### ChatHistoryEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message_id | [string](#string) |  |  |
| command_id | [string](#string) |  |  |
| role | [string](#string) |  |  |
| content | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-ChatMessage"></a>

### ChatMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| conversation | [string](#string) |  |  |
| principal_name | [string](#string) |  |  |
| role | [int32](#int32) |  |  |
| content | [string](#string) |  |  |
| command_id | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| sender_name | [string](#string) |  |  |
| sender_type | [SenderType](#laelia-v1-SenderType) |  |  |
| room_version | [int64](#int64) |  | room_version is the conversation.version at the time this message was created. Agents use it together with PullMessages.after_version to track their cursor into the conversation. |






<a name="laelia-v1-Command"></a>

### Command



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| agent | [string](#string) |  |  |
| principal_id | [string](#string) |  |  |
| principal_name | [string](#string) |  |  |
| command | [string](#string) |  |  |
| status | [CommandStatus](#laelia-v1-CommandStatus) |  |  |
| exit_code | [int32](#int32) |  |  |
| duration_ms | [int64](#int64) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| started_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| completed_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| error_message | [string](#string) |  |  |
| env | [Command.EnvEntry](#laelia-v1-Command-EnvEntry) | repeated |  |
| working_dir | [string](#string) |  |  |
| instruction | [string](#string) |  |  |
| profile | [string](#string) |  |  |
| final_summary | [string](#string) |  |  |
| result | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| allow_diff | [bool](#bool) |  |  |
| conversation_id | [string](#string) |  |  |






<a name="laelia-v1-Command-EnvEntry"></a>

### Command.EnvEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-CommandEvent"></a>

### CommandEvent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| seq_no | [int32](#int32) |  |  |
| type | [CommandEventType](#laelia-v1-CommandEventType) |  |  |
| summary | [string](#string) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| lifecycle | [LifecyclePayload](#laelia-v1-LifecyclePayload) |  |  |
| text_delta | [TextDeltaPayload](#laelia-v1-TextDeltaPayload) |  |  |
| tool_call_started | [ToolCallStartedPayload](#laelia-v1-ToolCallStartedPayload) |  |  |
| tool_call_finished | [ToolCallFinishedPayload](#laelia-v1-ToolCallFinishedPayload) |  |  |
| diff_emitted | [DiffEmittedPayload](#laelia-v1-DiffEmittedPayload) |  |  |
| warning | [WarningPayload](#laelia-v1-WarningPayload) |  |  |
| raw_acp | [RawAcpPayload](#laelia-v1-RawAcpPayload) |  |  |
| final_summary | [FinalSummaryPayload](#laelia-v1-FinalSummaryPayload) |  |  |
| permission_requested | [PermissionRequestedPayload](#laelia-v1-PermissionRequestedPayload) |  |  |
| permission_timed_out | [PermissionTimedOutPayload](#laelia-v1-PermissionTimedOutPayload) |  |  |
| permission_decided | [PermissionDecidedPayload](#laelia-v1-PermissionDecidedPayload) |  |  |






<a name="laelia-v1-CommandOutput"></a>

### CommandOutput



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| type | [CommandOutput.StreamType](#laelia-v1-CommandOutput-StreamType) |  |  |
| content | [string](#string) |  |  |
| seq_no | [int32](#int32) |  |  |
| timestamp | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-CommandProgress"></a>

### CommandProgress



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| type | [CommandOutput.StreamType](#laelia-v1-CommandOutput-StreamType) |  |  |
| content | [string](#string) |  |  |
| seq_no | [int32](#int32) |  |  |






<a name="laelia-v1-CommandRequest"></a>

### CommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| instruction | [string](#string) |  |  |
| profile | [string](#string) |  |  |
| env | [CommandRequest.EnvEntry](#laelia-v1-CommandRequest-EnvEntry) | repeated |  |
| working_dir | [string](#string) |  |  |
| timeout_seconds | [int32](#int32) |  |  |
| allow_diff | [bool](#bool) |  |  |
| principal_id | [string](#string) |  |  |
| conversation_id | [string](#string) |  |  |
| reply_to_message_id | [string](#string) |  |  |
| agent_display_name | [string](#string) |  |  |






<a name="laelia-v1-CommandRequest-EnvEntry"></a>

### CommandRequest.EnvEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-CommandResult"></a>

### CommandResult



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| exit_code | [int32](#int32) |  |  |
| duration_ms | [int64](#int64) |  |  |
| error_message | [string](#string) |  |  |
| last_seq_no | [int32](#int32) |  |  |
| final_summary | [string](#string) |  |  |
| result | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |






<a name="laelia-v1-Conversation"></a>

### Conversation



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| title | [string](#string) |  |  |
| type | [int32](#int32) |  |  |
| member_count | [int32](#int32) |  |  |
| owner_id | [string](#string) |  |  |
| owner_name | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-CreateChannelRequest"></a>

### CreateChannelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  |  |






<a name="laelia-v1-DeleteChannelRequest"></a>

### DeleteChannelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-DiffEmittedPayload"></a>

### DiffEmittedPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| path | [string](#string) |  |  |
| old_text | [string](#string) |  |  |
| new_text | [string](#string) |  |  |






<a name="laelia-v1-FetchConversationActivityRequest"></a>

### FetchConversationActivityRequest
FetchConversationActivity returns the execution status of each agent member
in a conversation. It is polled by the frontend to show real-time agent
status in the channel header.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






<a name="laelia-v1-FetchConversationActivityResponse"></a>

### FetchConversationActivityResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| activities | [AgentActivity](#laelia-v1-AgentActivity) | repeated |  |






<a name="laelia-v1-FinalSummaryPayload"></a>

### FinalSummaryPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stop_reason | [string](#string) |  |  |
| session_id | [string](#string) |  |  |






<a name="laelia-v1-GetChannelRequest"></a>

### GetChannelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-GetCommandContextRequest"></a>

### GetCommandContextRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-GetCommandContextResponse"></a>

### GetCommandContextResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command | [Command](#laelia-v1-Command) |  |  |
| outputs | [CommandOutput](#laelia-v1-CommandOutput) | repeated |  |
| events | [CommandEvent](#laelia-v1-CommandEvent) | repeated |  |






<a name="laelia-v1-GetCommandRequest"></a>

### GetCommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-GetOrCreateConversationRequest"></a>

### GetOrCreateConversationRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [string](#string) |  |  |






<a name="laelia-v1-GetOrCreateConversationResponse"></a>

### GetOrCreateConversationResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-LifecyclePayload"></a>

### LifecyclePayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| executor_kind | [string](#string) |  |  |
| profile | [string](#string) |  |  |






<a name="laelia-v1-ListChannelMembersRequest"></a>

### ListChannelMembersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






<a name="laelia-v1-ListChannelMembersResponse"></a>

### ListChannelMembersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| members | [ChannelMember](#laelia-v1-ChannelMember) | repeated |  |






<a name="laelia-v1-ListChannelsRequest"></a>

### ListChannelsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListChannelsResponse"></a>

### ListChannelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| channels | [Conversation](#laelia-v1-Conversation) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListCommandsRequest"></a>

### ListCommandsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| status | [CommandStatus](#laelia-v1-CommandStatus) |  |  |






<a name="laelia-v1-ListCommandsResponse"></a>

### ListCommandsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| commands | [Command](#laelia-v1-Command) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListConversationMessagesRequest"></a>

### ListConversationMessagesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| after_version | [int64](#int64) |  |  |






<a name="laelia-v1-ListConversationMessagesResponse"></a>

### ListConversationMessagesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |
| next_page_token | [string](#string) |  |  |
| current_version | [int64](#int64) |  |  |






<a name="laelia-v1-ManagerStreamMessage"></a>

### ManagerStreamMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message_snapshot | [MessageSnapshot](#laelia-v1-MessageSnapshot) |  |  |
| action_response | [ActionResponse](#laelia-v1-ActionResponse) |  |  |
| command_request | [CommandRequest](#laelia-v1-CommandRequest) |  |  |
| new_messages | [NewMessagesAvailable](#laelia-v1-NewMessagesAvailable) |  |  |
| cancel | [CancelMessage](#laelia-v1-CancelMessage) |  |  |
| pong | [Pong](#laelia-v1-Pong) |  |  |
| permission_decision | [PermissionDecision](#laelia-v1-PermissionDecision) |  |  |






<a name="laelia-v1-MessageSnapshot"></a>

### MessageSnapshot
MessageSnapshot is the Manager reply to PullMessages. It returns the newly
available messages and the conversation&#39;s current version so the agent can
record it as the base_version for any subsequent action.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |
| current_version | [int64](#int64) |  |  |






<a name="laelia-v1-NewMessagesAvailable"></a>

### NewMessagesAvailable
NewMessagesAvailable is pushed from Manager to agent over the bidi stream to
notify that a conversation the agent is connected to has produced new
messages. The agent should follow up with a PullMessages request.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation_ids | [string](#string) | repeated |  |
| versions | [int64](#int64) | repeated |  |






<a name="laelia-v1-PermissionDecidedPayload"></a>

### PermissionDecidedPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tool_call_id | [string](#string) |  |  |
| kind | [string](#string) |  |  |
| option_id | [string](#string) |  |  |
| option_kind | [string](#string) |  |  |






<a name="laelia-v1-PermissionDecision"></a>

### PermissionDecision



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| option_id | [string](#string) |  |  |






<a name="laelia-v1-PermissionOptionPayload"></a>

### PermissionOptionPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| option_id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| kind | [string](#string) |  |  |






<a name="laelia-v1-PermissionRequestedPayload"></a>

### PermissionRequestedPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tool_call_id | [string](#string) |  |  |
| kind | [string](#string) |  |  |
| title | [string](#string) |  |  |
| options | [PermissionOptionPayload](#laelia-v1-PermissionOptionPayload) | repeated |  |
| expires_at | [int64](#int64) |  |  |






<a name="laelia-v1-PermissionTimedOutPayload"></a>

### PermissionTimedOutPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tool_call_id | [string](#string) |  |  |
| kind | [string](#string) |  |  |






<a name="laelia-v1-Ping"></a>

### Ping



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| seq | [int64](#int64) |  |  |
| sent_at | [int64](#int64) |  |  |






<a name="laelia-v1-Pong"></a>

### Pong



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| seq | [int64](#int64) |  |  |
| server_time | [int64](#int64) |  |  |






<a name="laelia-v1-PostMessageRequest"></a>

### PostMessageRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| content | [string](#string) |  |  |
| base_version | [int64](#int64) |  |  |
| command_id | [string](#string) |  |  |






<a name="laelia-v1-PostMessageResponse"></a>

### PostMessageResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| committed | [bool](#bool) |  |  |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  |  |
| current_version | [int64](#int64) |  |  |
| new_messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |
| conflict_description | [string](#string) |  |  |






<a name="laelia-v1-PullMessages"></a>

### PullMessages
PullMessages is sent by an agent over the AgentChannel bidi stream to fetch
chat messages with room_version greater than after_version for a single
conversation. The agent tracks its own cursor per conversation and supplies
it explicitly so that reconnection / crash recovery is self-describing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation_id | [string](#string) |  |  |
| after_version | [int64](#int64) |  |  |






<a name="laelia-v1-RawAcpPayload"></a>

### RawAcpPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |






<a name="laelia-v1-RemoveChannelMemberRequest"></a>

### RemoveChannelMemberRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| member_id | [string](#string) |  |  |
| member_type | [int32](#int32) |  |  |






<a name="laelia-v1-ResolveHeldAction"></a>

### ResolveHeldAction
ResolveHeldAction is sent by the agent to resolve a held action (one where
ActionResponse.committed was false).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action_id | [string](#string) |  |  |
| resolution | [ActionResolution](#laelia-v1-ActionResolution) |  |  |






<a name="laelia-v1-RespondPermissionRequest"></a>

### RespondPermissionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| option_id | [string](#string) |  |  |






<a name="laelia-v1-SearchChatHistoryRequest"></a>

### SearchChatHistoryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [string](#string) |  |  |
| query | [string](#string) |  |  |
| since | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| until | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| limit | [int32](#int32) |  |  |
| conversation | [string](#string) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-SearchChatHistoryResponse"></a>

### SearchChatHistoryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entries | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-SendMessageRequest"></a>

### SendMessageRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| content | [string](#string) |  |  |






<a name="laelia-v1-SubmitAction"></a>

### SubmitAction
SubmitAction is the Phase 2 execution trigger. It replaces the Phase 1
manager-driven dispatch: after PullMessages the agent autonomously decides
whether to act and sends SubmitAction. The manager performs a Held Draft
check comparing base_version against the conversation&#39;s current version.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation_id | [string](#string) |  |  |
| reply_to_message_id | [string](#string) |  |  |
| base_version | [int64](#int64) |  |  |
| instruction | [string](#string) |  |  |
| profile | [string](#string) |  |  |
| env | [SubmitAction.EnvEntry](#laelia-v1-SubmitAction-EnvEntry) | repeated |  |
| working_dir | [string](#string) |  |  |
| timeout_seconds | [int32](#int32) |  |  |
| allow_diff | [bool](#bool) |  |  |






<a name="laelia-v1-SubmitAction-EnvEntry"></a>

### SubmitAction.EnvEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-TextDeltaPayload"></a>

### TextDeltaPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream_type | [string](#string) |  |  |
| content | [string](#string) |  |  |






<a name="laelia-v1-ToolCallFinishedPayload"></a>

### ToolCallFinishedPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| status | [string](#string) |  |  |
| raw_output | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |






<a name="laelia-v1-ToolCallStartedPayload"></a>

### ToolCallStartedPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  |  |
| raw_input | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |






<a name="laelia-v1-UpdateChannelRequest"></a>

### UpdateChannelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |






<a name="laelia-v1-WarningPayload"></a>

### WarningPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  |  |






<a name="laelia-v1-WatchCommandEventsRequest"></a>

### WatchCommandEventsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| after_seq_no | [int32](#int32) |  |  |






<a name="laelia-v1-WatchCommandRequest"></a>

### WatchCommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| after_seq_no | [int32](#int32) |  |  |





 


<a name="laelia-v1-ActionResolution"></a>

### ActionResolution
ActionResolution is the agent&#39;s decision when a SubmitAction is held due
to a version mismatch (new messages arrived between PullMessages and
SubmitAction).

| Name | Number | Description |
| ---- | ------ | ----------- |
| ACTION_RESOLUTION_UNSPECIFIED | 0 |  |
| REVISE | 1 |  |
| SEND_AS_IS | 2 |  |
| DISCARD | 3 |  |
| FORCE_SEND | 4 |  |



<a name="laelia-v1-CommandEventType"></a>

### CommandEventType


| Name | Number | Description |
| ---- | ------ | ----------- |
| COMMAND_EVENT_TYPE_UNSPECIFIED | 0 |  |
| LIFECYCLE | 1 |  |
| TEXT_DELTA | 2 |  |
| TOOL_CALL_STARTED | 3 |  |
| TOOL_CALL_FINISHED | 4 |  |
| DIFF_EMITTED | 5 |  |
| WARNING | 6 |  |
| RAW_ACP | 7 |  |
| FINAL_SUMMARY | 8 |  |
| PERMISSION_REQUESTED | 9 |  |
| PERMISSION_TIMED_OUT | 10 |  |
| PERMISSION_DECIDED | 11 |  |



<a name="laelia-v1-CommandOutput-StreamType"></a>

### CommandOutput.StreamType


| Name | Number | Description |
| ---- | ------ | ----------- |
| STREAM_TYPE_UNSPECIFIED | 0 |  |
| STDOUT | 1 |  |
| STDERR | 2 |  |
| SYSTEM | 3 |  |



<a name="laelia-v1-CommandStatus"></a>

### CommandStatus


| Name | Number | Description |
| ---- | ------ | ----------- |
| COMMAND_STATUS_UNSPECIFIED | 0 |  |
| PENDING | 1 |  |
| RUNNING | 2 |  |
| COMPLETED | 3 |  |
| FAILED | 4 |  |
| CANCELLED | 5 |  |
| TIMEOUT | 6 |  |



<a name="laelia-v1-SenderType"></a>

### SenderType
SenderType distinguishes who authored a chat message. It replaces the
deprecated CommandSource enum and covers programmatic (SYSTEM) senders that
CommandSource could not express inside chat conversations. Values are
prefixed because UserType already occupies the unprefixed USER/SYSTEM_BOT
names (protobuf C&#43;&#43; scoping rules forbid sibling enums from sharing value
names).

| Name | Number | Description |
| ---- | ------ | ----------- |
| SENDER_TYPE_UNSPECIFIED | 0 |  |
| SENDER_TYPE_USER | 1 |  |
| SENDER_TYPE_AGENT | 2 |  |
| SENDER_TYPE_SYSTEM | 3 |  |


 

 


<a name="laelia-v1-AgentStreamService"></a>

### AgentStreamService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| AgentChannel | [AgentStreamMessage](#laelia-v1-AgentStreamMessage) stream | [ManagerStreamMessage](#laelia-v1-ManagerStreamMessage) stream |  |


<a name="laelia-v1-CommandService"></a>

### CommandService


| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListCommands | [ListCommandsRequest](#laelia-v1-ListCommandsRequest) | [ListCommandsResponse](#laelia-v1-ListCommandsResponse) |  |
| GetCommand | [GetCommandRequest](#laelia-v1-GetCommandRequest) | [Command](#laelia-v1-Command) |  |
| CancelCommand | [CancelCommandRequest](#laelia-v1-CancelCommandRequest) | [Command](#laelia-v1-Command) |  |
| WatchCommand | [WatchCommandRequest](#laelia-v1-WatchCommandRequest) | [CommandOutput](#laelia-v1-CommandOutput) stream |  |
| WatchCommandEvents | [WatchCommandEventsRequest](#laelia-v1-WatchCommandEventsRequest) | [CommandEvent](#laelia-v1-CommandEvent) stream |  |
| RespondPermission | [RespondPermissionRequest](#laelia-v1-RespondPermissionRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| SearchChatHistory | [SearchChatHistoryRequest](#laelia-v1-SearchChatHistoryRequest) | [SearchChatHistoryResponse](#laelia-v1-SearchChatHistoryResponse) |  |
| GetCommandContext | [GetCommandContextRequest](#laelia-v1-GetCommandContextRequest) | [GetCommandContextResponse](#laelia-v1-GetCommandContextResponse) |  |
| GetOrCreateConversation | [GetOrCreateConversationRequest](#laelia-v1-GetOrCreateConversationRequest) | [GetOrCreateConversationResponse](#laelia-v1-GetOrCreateConversationResponse) |  |
| ListConversationMessages | [ListConversationMessagesRequest](#laelia-v1-ListConversationMessagesRequest) | [ListConversationMessagesResponse](#laelia-v1-ListConversationMessagesResponse) |  |
| CreateChannel | [CreateChannelRequest](#laelia-v1-CreateChannelRequest) | [Conversation](#laelia-v1-Conversation) |  |
| ListChannels | [ListChannelsRequest](#laelia-v1-ListChannelsRequest) | [ListChannelsResponse](#laelia-v1-ListChannelsResponse) |  |
| GetChannel | [GetChannelRequest](#laelia-v1-GetChannelRequest) | [Conversation](#laelia-v1-Conversation) |  |
| UpdateChannel | [UpdateChannelRequest](#laelia-v1-UpdateChannelRequest) | [Conversation](#laelia-v1-Conversation) |  |
| DeleteChannel | [DeleteChannelRequest](#laelia-v1-DeleteChannelRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| AddChannelMember | [AddChannelMemberRequest](#laelia-v1-AddChannelMemberRequest) | [ChannelMember](#laelia-v1-ChannelMember) |  |
| RemoveChannelMember | [RemoveChannelMemberRequest](#laelia-v1-RemoveChannelMemberRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| ListChannelMembers | [ListChannelMembersRequest](#laelia-v1-ListChannelMembersRequest) | [ListChannelMembersResponse](#laelia-v1-ListChannelMembersResponse) |  |
| SendMessage | [SendMessageRequest](#laelia-v1-SendMessageRequest) | [ChatMessage](#laelia-v1-ChatMessage) |  |
| PostMessage | [PostMessageRequest](#laelia-v1-PostMessageRequest) | [PostMessageResponse](#laelia-v1-PostMessageResponse) |  |
| FetchConversationActivity | [FetchConversationActivityRequest](#laelia-v1-FetchConversationActivityRequest) | [FetchConversationActivityResponse](#laelia-v1-FetchConversationActivityResponse) |  |

 



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

