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
    - [AgentACPConfig](#laelia-v1-AgentACPConfig)
    - [AgentACPConfig.CustomEnvEntry](#laelia-v1-AgentACPConfig-CustomEnvEntry)
    - [AgentCapability](#laelia-v1-AgentCapability)
    - [AgentDisconnectRequest](#laelia-v1-AgentDisconnectRequest)
    - [AgentHeartbeatRequest](#laelia-v1-AgentHeartbeatRequest)
    - [AgentHeartbeatResponse](#laelia-v1-AgentHeartbeatResponse)
    - [AgentInfo](#laelia-v1-AgentInfo)
    - [AgentInfo.LabelsEntry](#laelia-v1-AgentInfo-LabelsEntry)
    - [AgentMetrics](#laelia-v1-AgentMetrics)
    - [AgentModelOption](#laelia-v1-AgentModelOption)
    - [AgentProviderInfo](#laelia-v1-AgentProviderInfo)
    - [AgentSession](#laelia-v1-AgentSession)
    - [AgentStatus](#laelia-v1-AgentStatus)
    - [AgentSummary](#laelia-v1-AgentSummary)
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
    - [RefreshAgentProvidersRequest](#laelia-v1-RefreshAgentProvidersRequest)
    - [RefreshAgentProvidersResponse](#laelia-v1-RefreshAgentProvidersResponse)
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
    - [AckProcessedVersionRequest](#laelia-v1-AckProcessedVersionRequest)
    - [AckProcessedVersionResponse](#laelia-v1-AckProcessedVersionResponse)
    - [AddChannelMemberRequest](#laelia-v1-AddChannelMemberRequest)
    - [AgentActivity](#laelia-v1-AgentActivity)
    - [AgentReady](#laelia-v1-AgentReady)
    - [AgentStreamMessage](#laelia-v1-AgentStreamMessage)
    - [Attachment](#laelia-v1-Attachment)
    - [BeginSession](#laelia-v1-BeginSession)
    - [BeginSessionResponse](#laelia-v1-BeginSessionResponse)
    - [CancelCommandRequest](#laelia-v1-CancelCommandRequest)
    - [CancelMessage](#laelia-v1-CancelMessage)
    - [CancelReminderRequest](#laelia-v1-CancelReminderRequest)
    - [CancelReminderResponse](#laelia-v1-CancelReminderResponse)
    - [ChannelMember](#laelia-v1-ChannelMember)
    - [ChannelThread](#laelia-v1-ChannelThread)
    - [ChannelUpdate](#laelia-v1-ChannelUpdate)
    - [ChatHistoryEntry](#laelia-v1-ChatHistoryEntry)
    - [ChatMessage](#laelia-v1-ChatMessage)
    - [ClaimTaskRequest](#laelia-v1-ClaimTaskRequest)
    - [ClaimTaskResponse](#laelia-v1-ClaimTaskResponse)
    - [Command](#laelia-v1-Command)
    - [Command.EnvEntry](#laelia-v1-Command-EnvEntry)
    - [CommandEvent](#laelia-v1-CommandEvent)
    - [CommandOutput](#laelia-v1-CommandOutput)
    - [CommandProgress](#laelia-v1-CommandProgress)
    - [CommandRequest](#laelia-v1-CommandRequest)
    - [CommandRequest.EnvEntry](#laelia-v1-CommandRequest-EnvEntry)
    - [CommandResult](#laelia-v1-CommandResult)
    - [CompleteReminderRequest](#laelia-v1-CompleteReminderRequest)
    - [CompleteReminderResponse](#laelia-v1-CompleteReminderResponse)
    - [Conversation](#laelia-v1-Conversation)
    - [ConvertMessageToReminderRequest](#laelia-v1-ConvertMessageToReminderRequest)
    - [ConvertMessageToReminderResponse](#laelia-v1-ConvertMessageToReminderResponse)
    - [ConvertMessageToTaskRequest](#laelia-v1-ConvertMessageToTaskRequest)
    - [ConvertMessageToTaskResponse](#laelia-v1-ConvertMessageToTaskResponse)
    - [CreateChannelRequest](#laelia-v1-CreateChannelRequest)
    - [CreateTaskRequest](#laelia-v1-CreateTaskRequest)
    - [CreateTaskResponse](#laelia-v1-CreateTaskResponse)
    - [DeleteChannelRequest](#laelia-v1-DeleteChannelRequest)
    - [DiffEmittedPayload](#laelia-v1-DiffEmittedPayload)
    - [DiscoverProviders](#laelia-v1-DiscoverProviders)
    - [DownloadFileRequest](#laelia-v1-DownloadFileRequest)
    - [DownloadFileResponse](#laelia-v1-DownloadFileResponse)
    - [FailReminderRequest](#laelia-v1-FailReminderRequest)
    - [FailReminderResponse](#laelia-v1-FailReminderResponse)
    - [FetchConversationActivityRequest](#laelia-v1-FetchConversationActivityRequest)
    - [FetchConversationActivityResponse](#laelia-v1-FetchConversationActivityResponse)
    - [File](#laelia-v1-File)
    - [FinalSummaryPayload](#laelia-v1-FinalSummaryPayload)
    - [GetChannelRequest](#laelia-v1-GetChannelRequest)
    - [GetCommandContextRequest](#laelia-v1-GetCommandContextRequest)
    - [GetCommandContextResponse](#laelia-v1-GetCommandContextResponse)
    - [GetCommandRequest](#laelia-v1-GetCommandRequest)
    - [GetOrCreateAgentDMRequest](#laelia-v1-GetOrCreateAgentDMRequest)
    - [GetOrCreateAgentDMResponse](#laelia-v1-GetOrCreateAgentDMResponse)
    - [GetOrCreateConversationRequest](#laelia-v1-GetOrCreateConversationRequest)
    - [GetOrCreateConversationResponse](#laelia-v1-GetOrCreateConversationResponse)
    - [GetOrCreateUserDMRequest](#laelia-v1-GetOrCreateUserDMRequest)
    - [GetOrCreateUserDMResponse](#laelia-v1-GetOrCreateUserDMResponse)
    - [GetReminderRequest](#laelia-v1-GetReminderRequest)
    - [GetReminderResponse](#laelia-v1-GetReminderResponse)
    - [LeaveChannelRequest](#laelia-v1-LeaveChannelRequest)
    - [LifecyclePayload](#laelia-v1-LifecyclePayload)
    - [ListChannelMembersRequest](#laelia-v1-ListChannelMembersRequest)
    - [ListChannelMembersResponse](#laelia-v1-ListChannelMembersResponse)
    - [ListChannelThreadsRequest](#laelia-v1-ListChannelThreadsRequest)
    - [ListChannelThreadsResponse](#laelia-v1-ListChannelThreadsResponse)
    - [ListChannelUpdatesRequest](#laelia-v1-ListChannelUpdatesRequest)
    - [ListChannelUpdatesResponse](#laelia-v1-ListChannelUpdatesResponse)
    - [ListChannelsForAgentRequest](#laelia-v1-ListChannelsForAgentRequest)
    - [ListChannelsForAgentResponse](#laelia-v1-ListChannelsForAgentResponse)
    - [ListChannelsRequest](#laelia-v1-ListChannelsRequest)
    - [ListChannelsResponse](#laelia-v1-ListChannelsResponse)
    - [ListCommandsRequest](#laelia-v1-ListCommandsRequest)
    - [ListCommandsResponse](#laelia-v1-ListCommandsResponse)
    - [ListConversationMessagesRequest](#laelia-v1-ListConversationMessagesRequest)
    - [ListConversationMessagesResponse](#laelia-v1-ListConversationMessagesResponse)
    - [ListDueRemindersRequest](#laelia-v1-ListDueRemindersRequest)
    - [ListDueRemindersResponse](#laelia-v1-ListDueRemindersResponse)
    - [ListFilesRequest](#laelia-v1-ListFilesRequest)
    - [ListFilesResponse](#laelia-v1-ListFilesResponse)
    - [ListPeerAgentsRequest](#laelia-v1-ListPeerAgentsRequest)
    - [ListPeerAgentsResponse](#laelia-v1-ListPeerAgentsResponse)
    - [ListRemindersRequest](#laelia-v1-ListRemindersRequest)
    - [ListRemindersResponse](#laelia-v1-ListRemindersResponse)
    - [ListTasksRequest](#laelia-v1-ListTasksRequest)
    - [ListTasksResponse](#laelia-v1-ListTasksResponse)
    - [ListThreadMessagesRequest](#laelia-v1-ListThreadMessagesRequest)
    - [ListThreadMessagesResponse](#laelia-v1-ListThreadMessagesResponse)
    - [ListThreadParticipantsRequest](#laelia-v1-ListThreadParticipantsRequest)
    - [ListThreadParticipantsResponse](#laelia-v1-ListThreadParticipantsResponse)
    - [ListThreadUpdatesRequest](#laelia-v1-ListThreadUpdatesRequest)
    - [ListThreadUpdatesResponse](#laelia-v1-ListThreadUpdatesResponse)
    - [ManagerStreamMessage](#laelia-v1-ManagerStreamMessage)
    - [MarkConversationReadRequest](#laelia-v1-MarkConversationReadRequest)
    - [MarkConversationReadResponse](#laelia-v1-MarkConversationReadResponse)
    - [Mention](#laelia-v1-Mention)
    - [NewMessagesAvailable](#laelia-v1-NewMessagesAvailable)
    - [PeerAgent](#laelia-v1-PeerAgent)
    - [PermissionDecidedPayload](#laelia-v1-PermissionDecidedPayload)
    - [PermissionDecision](#laelia-v1-PermissionDecision)
    - [PermissionOptionPayload](#laelia-v1-PermissionOptionPayload)
    - [PermissionRequestedPayload](#laelia-v1-PermissionRequestedPayload)
    - [PermissionTimedOutPayload](#laelia-v1-PermissionTimedOutPayload)
    - [Ping](#laelia-v1-Ping)
    - [Pong](#laelia-v1-Pong)
    - [PostMessageRequest](#laelia-v1-PostMessageRequest)
    - [PostMessageResponse](#laelia-v1-PostMessageResponse)
    - [ProvidersDiscovered](#laelia-v1-ProvidersDiscovered)
    - [RawAcpPayload](#laelia-v1-RawAcpPayload)
    - [Reminder](#laelia-v1-Reminder)
    - [RemoveChannelMemberRequest](#laelia-v1-RemoveChannelMemberRequest)
    - [ResolveChannelByTitleRequest](#laelia-v1-ResolveChannelByTitleRequest)
    - [ResolveChannelByTitleResponse](#laelia-v1-ResolveChannelByTitleResponse)
    - [RespondPermissionRequest](#laelia-v1-RespondPermissionRequest)
    - [SearchChatHistoryRequest](#laelia-v1-SearchChatHistoryRequest)
    - [SearchChatHistoryResponse](#laelia-v1-SearchChatHistoryResponse)
    - [SendMessageRequest](#laelia-v1-SendMessageRequest)
    - [TaskInfo](#laelia-v1-TaskInfo)
    - [TextDeltaPayload](#laelia-v1-TextDeltaPayload)
    - [ThreadUpdate](#laelia-v1-ThreadUpdate)
    - [ToolCallFinishedPayload](#laelia-v1-ToolCallFinishedPayload)
    - [ToolCallStartedPayload](#laelia-v1-ToolCallStartedPayload)
    - [TransferChannelOwnershipRequest](#laelia-v1-TransferChannelOwnershipRequest)
    - [TransferChannelOwnershipResponse](#laelia-v1-TransferChannelOwnershipResponse)
    - [UnclaimTaskRequest](#laelia-v1-UnclaimTaskRequest)
    - [UnclaimTaskResponse](#laelia-v1-UnclaimTaskResponse)
    - [UpdateChannelMemberRoleRequest](#laelia-v1-UpdateChannelMemberRoleRequest)
    - [UpdateChannelRequest](#laelia-v1-UpdateChannelRequest)
    - [UpdateReminderRequest](#laelia-v1-UpdateReminderRequest)
    - [UpdateReminderResponse](#laelia-v1-UpdateReminderResponse)
    - [UpdateTaskStatusRequest](#laelia-v1-UpdateTaskStatusRequest)
    - [UpdateTaskStatusResponse](#laelia-v1-UpdateTaskStatusResponse)
    - [UploadFileRequest](#laelia-v1-UploadFileRequest)
    - [WarningPayload](#laelia-v1-WarningPayload)
    - [WatchCommandEventsRequest](#laelia-v1-WatchCommandEventsRequest)
    - [WatchCommandRequest](#laelia-v1-WatchCommandRequest)
  
    - [CommandEventType](#laelia-v1-CommandEventType)
    - [CommandOutput.StreamType](#laelia-v1-CommandOutput-StreamType)
    - [CommandStatus](#laelia-v1-CommandStatus)
    - [ReminderStatus](#laelia-v1-ReminderStatus)
    - [SenderType](#laelia-v1-SenderType)
    - [TaskStatus](#laelia-v1-TaskStatus)
  
    - [AgentStreamService](#laelia-v1-AgentStreamService)
    - [CommandService](#laelia-v1-CommandService)
  
- [v1/iam_service.proto](#v1_iam_service-proto)
    - [GetAgentIamPolicyRequest](#laelia-v1-GetAgentIamPolicyRequest)
    - [GetWorkspaceIamPolicyRequest](#laelia-v1-GetWorkspaceIamPolicyRequest)
    - [IamPolicyView](#laelia-v1-IamPolicyView)
    - [SetAgentIamPolicyRequest](#laelia-v1-SetAgentIamPolicyRequest)
    - [SetWorkspaceIamPolicyRequest](#laelia-v1-SetWorkspaceIamPolicyRequest)
  
    - [IamService](#laelia-v1-IamService)
  
- [v1/role_service.proto](#v1_role_service-proto)
    - [CreateRoleRequest](#laelia-v1-CreateRoleRequest)
    - [DeleteRoleRequest](#laelia-v1-DeleteRoleRequest)
    - [GetRoleRequest](#laelia-v1-GetRoleRequest)
    - [ListRolesRequest](#laelia-v1-ListRolesRequest)
    - [ListRolesResponse](#laelia-v1-ListRolesResponse)
    - [Role](#laelia-v1-Role)
    - [UpdateRoleRequest](#laelia-v1-UpdateRoleRequest)
  
    - [RoleService](#laelia-v1-RoleService)
  
- [v1/setting.proto](#v1_setting-proto)
    - [GetDebugConfigRequest](#laelia-v1-GetDebugConfigRequest)
    - [GetDebugConfigResponse](#laelia-v1-GetDebugConfigResponse)
    - [GetS3ConfigRequest](#laelia-v1-GetS3ConfigRequest)
    - [GetS3ConfigResponse](#laelia-v1-GetS3ConfigResponse)
    - [UpdateDebugConfigRequest](#laelia-v1-UpdateDebugConfigRequest)
    - [UpdateDebugConfigResponse](#laelia-v1-UpdateDebugConfigResponse)
    - [UpdateS3ConfigRequest](#laelia-v1-UpdateS3ConfigRequest)
    - [UpdateS3ConfigResponse](#laelia-v1-UpdateS3ConfigResponse)
  
    - [SettingService](#laelia-v1-SettingService)
  
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
| created_by | [string](#string) |  | Creator&#39;s user resource name (users/{id}); empty for legacy agents with no recorded creator. Only the creator or a workspace admin may modify the agent. |
| can_edit | [bool](#bool) |  | can_edit reports whether the current caller may modify this agent (laelia.agents.edit): true for the creator (via the agentEditor IAM binding) and for workspace admins (via the all-permissions union), false otherwise. Populated per caller by GetAgent/ListAgents; not set on agent-daemon paths. |






<a name="laelia-v1-Agent-LabelsEntry"></a>

### Agent.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-AgentACPConfig"></a>

### AgentACPConfig
User-configurable ACP settings. Everything else (working dir, capabilities,
permissions) is derived from a built-in template, not set by the admin.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| executable | [string](#string) |  | command to run, e.g. &#34;npx&#34;. Only used when provider is &#34;custom&#34; or empty. |
| args | [string](#string) | repeated | args passed to executable, e.g. [&#34;-y&#34;, &#34;@agentclientprotocol/claude-agent-acp@latest&#34;] |
| allow_env | [string](#string) | repeated | env var names the child process may inherit |
| provider | [string](#string) |  | selected LLM agent provider id, e.g. &#34;opencode&#34;, &#34;claude-code&#34;, &#34;custom&#34; |
| model | [string](#string) |  | selected model valueId, matching an option advertised by the provider in NewSession ConfigOptions |
| custom_env | [AgentACPConfig.CustomEnvEntry](#laelia-v1-AgentACPConfig-CustomEnvEntry) | repeated | user-defined key-value env vars overlaid (and overriding) the inherited allow_env set |
| persona_prompt | [string](#string) |  | admin-authored self-awareness prompt: personality, chat style, focus area. Empty = not loaded. |






<a name="laelia-v1-AgentACPConfig-CustomEnvEntry"></a>

### AgentACPConfig.CustomEnvEntry



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
| available_providers | [AgentProviderInfo](#laelia-v1-AgentProviderInfo) | repeated | LLM agent providers auto-discovered by the agent daemon (agent-owned, not overwritten by the server) |
| acp_config | [AgentACPConfig](#laelia-v1-AgentACPConfig) |  |  |






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






<a name="laelia-v1-AgentModelOption"></a>

### AgentModelOption
AgentModelOption is one model selectable via the ACP session config option
round trip. Value is the valueId the client sends to SetSessionConfigOption.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| value | [string](#string) |  |  |
| name | [string](#string) |  |  |
| description | [string](#string) |  |  |






<a name="laelia-v1-AgentProviderInfo"></a>

### AgentProviderInfo
AgentProviderInfo describes one LLM agent provider detected on the agent
daemon&#39;s host. Reported via ConnectAgent and refreshed on demand. The server
treats this as agent-owned: it preserves it across ConnectAgent/Update flows
the same way it preserves acp_config.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provider_id | [string](#string) |  | e.g. &#34;opencode&#34;, &#34;claude-code&#34; |
| display_name | [string](#string) |  |  |
| version | [string](#string) |  |  |
| executable_path | [string](#string) |  |  |
| models | [AgentModelOption](#laelia-v1-AgentModelOption) | repeated | empty when the provider does not advertise a model config option |
| supports_model_config_option | [bool](#bool) |  | whether probing observed a category==&#34;model&#34; config option |
| detected_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






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






<a name="laelia-v1-AgentSummary"></a>

### AgentSummary
AgentSummary is the lightweight list-view projection of an Agent returned by
ListAgents. It carries only the fields list/header views need: identity,
lifecycle state, connection status, and the provider/executable signal that
agentLifecycle() reads to classify an agent as ready/pending/offline. The
full Agent (available_providers, the rest of acp_config, capability, host
info, token fields, created_by, can_edit) is returned only by GetAgent, so
the two RPCs don&#39;t overlap. can_edit is intentionally omitted: resolving it
per row would N&#43;1 the IAM policy lookup for non-admin callers, and the list
view does not gate affordances on it (delete is enforced server-side).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| state | [State](#laelia-v1-State) |  |  |
| title | [string](#string) |  |  |
| status | [AgentStatus](#laelia-v1-AgentStatus) |  |  |
| provider | [string](#string) |  | provider/executable mirror acp_config.provider/executable on the full Agent, surfaced top-level so list consumers don&#39;t pull in AgentInfo. |
| executable | [string](#string) |  |  |






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
| acp_config | [AgentACPConfig](#laelia-v1-AgentACPConfig) |  | server-provided structured ACP config |






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
| agents | [AgentSummary](#laelia-v1-AgentSummary) | repeated |  |
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






<a name="laelia-v1-RefreshAgentProvidersRequest"></a>

### RefreshAgentProvidersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-RefreshAgentProvidersResponse"></a>

### RefreshAgentProvidersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| providers | [AgentProviderInfo](#laelia-v1-AgentProviderInfo) | repeated |  |






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
| acp_config | [AgentACPConfig](#laelia-v1-AgentACPConfig) |  |  |





 


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
| RefreshAgentProviders | [RefreshAgentProvidersRequest](#laelia-v1-RefreshAgentProvidersRequest) | [RefreshAgentProvidersResponse](#laelia-v1-RefreshAgentProvidersResponse) | Ask the agent daemon to re-probe its host for installed LLM agent providers and their models. Returns the freshly discovered provider list (also persisted into agent.info.available_providers). Admin only. |
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
| workspace_admin | [bool](#bool) |  | workspace_admin is true when the user holds the roles/workspaceAdmin role. Only populated for the current caller (GetCurrentUser). Retained as a computed shim during the IAM transition; prefer `permissions` for gating. |
| description | [string](#string) |  | description is a short, user-authored self-description surfaced to agents and other users so they know who this user is and what they focus on, e.g. &#34;Backend engineer, focused on agent building&#34; or &#34;UI/UX expert, reviews come to me&#34;. Editable via UpdateUser with update_mask &#34;description&#34;. |
| permissions | [string](#string) | repeated | permissions is the caller&#39;s effective workspace-scope permission set (roles/workspaceMember baseline ∪ the permissions of every workspace role the user holds), populated only by GetCurrentUser. The frontend gates workspace actions on this (e.g. laelia.users.update). Per-resource permissions (conversations.read/send/manage, agents.edit) are resolved per resource and surfaced on the resource, not here. |
| debug_mode | [bool](#bool) |  | debug_mode is true when RuntimeDebug is enabled for the workspace. Populated only by GetCurrentUser so the frontend can gate debug-only UI without calling the admin-gated SettingService.GetDebugConfig. |






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



<a name="laelia-v1-AckProcessedVersionRequest"></a>

### AckProcessedVersionRequest
AckProcessedVersion advances the agent&#39;s durable per-channel cursor to
processed_version, marking the channel as processed up to that room_version so
that subsequent ListChannelUpdates no longer report it. command_id, when
supplied, links the current session&#39;s command to this conversation so the
frontend can associate execution events with the channel.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| processed_version | [int64](#int64) |  |  |
| command_id | [string](#string) |  |  |






<a name="laelia-v1-AckProcessedVersionResponse"></a>

### AckProcessedVersionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| processed_version | [int64](#int64) |  |  |






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
| begin_session | [BeginSession](#laelia-v1-BeginSession) |  |  |
| progress | [CommandProgress](#laelia-v1-CommandProgress) |  |  |
| result | [CommandResult](#laelia-v1-CommandResult) |  |  |
| event | [CommandEvent](#laelia-v1-CommandEvent) |  |  |
| ping | [Ping](#laelia-v1-Ping) |  |  |
| providers_discovered | [ProvidersDiscovered](#laelia-v1-ProvidersDiscovered) |  | response to ManagerStreamMessage.discover_providers |






<a name="laelia-v1-Attachment"></a>

### Attachment
Attachment references a file stored in S3 that is attached to a chat message.
The id is the file row uuid and doubles as the download key (/v1/files/{id}).

The anchor fields below are set only when this attachment represents a
comment anchoring a span of a file (e.g. a markdown section) rather than a
whole-file upload. They are caller-supplied (the file row is not their
source of truth) and left empty for ordinary whole-file attachments.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| name | [string](#string) |  |  |
| mime_type | [string](#string) |  |  |
| size_bytes | [int64](#int64) |  |  |
| section_anchor | [string](#string) |  | section_anchor is the human-readable anchor of the commented section, e.g. &#34;§ 2.1 Server (server/)&#34;. |
| section_id | [string](#string) |  | section_id is the stable DOM id of the section heading within the file, used to jump back to the section from a comment. |
| quoted_text | [string](#string) |  | quoted_text is the exact text the commenter selected in the file. |






<a name="laelia-v1-BeginSession"></a>

### BeginSession
BeginSession is sent by an agent to ask the Manager to start a new
autonomous processing session. The Manager checks the agent&#39;s per-channel
cursors: if no conversation has room_version greater than the agent&#39;s cursor,
it replies BeginSessionResponse{idle=true} and the agent stays idle;
otherwise it creates a RUNNING command and replies with its command_id, which
the agent uses to anchor its execution events and link any posted replies.






<a name="laelia-v1-BeginSessionResponse"></a>

### BeginSessionResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| idle | [bool](#bool) |  |  |
| agent_display_name | [string](#string) |  | agent_display_name is the posting agent&#39;s human-readable name, sourced from the manager (the source of truth for agent identity). The agent client injects it into its system prompt so it knows who it is and can recognize its own messages and @mentions of itself. |






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






<a name="laelia-v1-CancelReminderRequest"></a>

### CancelReminderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-CancelReminderResponse"></a>

### CancelReminderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminder | [Reminder](#laelia-v1-Reminder) |  |  |






<a name="laelia-v1-ChannelMember"></a>

### ChannelMember



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member_type | [int32](#int32) |  |  |
| member_id | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| member_role | [int32](#int32) |  |  |
| joined_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| description | [string](#string) |  | description is the member&#39;s self-description: for users it is User.description, for agents it is the agent&#39;s full persona_prompt (from AgentACPConfig). Surfaced inline in the roster so an agent can perceive who is in a channel/thread — and each co-agent&#39;s persona — in a single lookup, and decide whom to address. |






<a name="laelia-v1-ChannelThread"></a>

### ChannelThread
ChannelThread is a summary of one active thread (a root with ≥1 reply) in a
conversation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| root_message | [string](#string) |  | root_message is the id (bare UUID) of the thread&#39;s root message; matches ChatMessage.name so the client can map it back to a root row. |
| reply_count | [int32](#int32) |  | reply_count is the total number of replies in the thread (always ≥1). |
| latest_reply_version | [int64](#int64) |  | latest_reply_version is the maximum room_version among the thread&#39;s replies. |
| latest_reply_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | latest_reply_at is the created_at of the most recent reply. |






<a name="laelia-v1-ChannelUpdate"></a>

### ChannelUpdate
ChannelUpdate describes one conversation that has unread messages for the
agent. new_message_count is the number of chat_message rows with
room_version greater than the agent&#39;s processed_version for that channel.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| current_version | [int64](#int64) |  |  |
| processed_version | [int64](#int64) |  |  |
| new_message_count | [int32](#int32) |  |  |






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
| room_version | [int64](#int64) |  | room_version is the conversation.version at the time this message was created. Agents use it together with their per-channel cursor (managed via the ListChannelUpdates / AckProcessedVersion RPCs) to track progress into the conversation. |
| mentions | [Mention](#laelia-v1-Mention) | repeated |  |
| is_own | [bool](#bool) |  | is_own is true when this message was sent by the calling agent itself. It is caller-relative (computed by the manager from the authenticated agent vs the message&#39;s sender_agent_id) so an agent can recognize its own past messages as context-only and avoid replying to itself. |
| attachments | [Attachment](#laelia-v1-Attachment) | repeated |  |
| thread_root | [string](#string) |  | thread_root is the resource name of the root message of the thread this message belongs to (&#34;conversations/{c}/messages/{m}&#34;). Empty for a normal channel message (i.e. a root message itself, or a message outside any thread). Replies in a thread carry the root message&#39;s name here. |
| thread_reply_count | [int32](#int32) |  | thread_reply_count is the number of replies in the thread rooted at this message. Only meaningful for root messages (thread_root empty); the frontend uses it to render the reply-count badge on the root message in the main channel list. Always 0 for thread replies. |
| task | [TaskInfo](#laelia-v1-TaskInfo) |  | task is set when this message is a task (a row exists in the task table for this message id). Populated by ListConversationMessages / ListThreadMessages for root messages; absent for non-task messages and thread replies. |
| agent_id | [string](#string) |  | agent_id is the agent resource ID (&#34;agents/{id}&#34;) that owns the command referenced by command_id. Populated when the sender is an agent so the frontend can construct command-detail URLs. |






<a name="laelia-v1-ClaimTaskRequest"></a>

### ClaimTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | message is the resource name of the task&#39;s root message (&#34;conversations/{c}/messages/{m}&#34;). |






<a name="laelia-v1-ClaimTaskResponse"></a>

### ClaimTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  | message is the task message after the claim, with task populated (status now IN_PROGRESS, assignee set to the caller). When the claim failed because another agent already owns the task or it is not in TODO, the RPC returns FAILED_PRECONDITION instead and this response is not sent. |






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






<a name="laelia-v1-CompleteReminderRequest"></a>

### CompleteReminderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the reminder resource name (&#34;reminders/{message_id}&#34;). Only the owning agent may call this, and only when the reminder is DUE. |
| result | [string](#string) |  | result is the agent&#39;s completion report, posted as a single system message in the reminder&#39;s thread. The backend posts it atomically with the status update so it never appears twice. |






<a name="laelia-v1-CompleteReminderResponse"></a>

### CompleteReminderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminder | [Reminder](#laelia-v1-Reminder) |  |  |






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
| unread_count | [int32](#int32) |  | unread_count is the number of chat_message rows with room_version beyond the requesting user&#39;s read cursor for this conversation. Populated by ListChannels; 0 (or unset) when the user is caught up. |
| address | [string](#string) |  | address is the name-based display address for this conversation, the form agents write and read: &#34;#&lt;title&gt;&#34; for a channel (type 2), &#34;dm:@&lt;peer&gt;&#34; for a direct message (type 1 peer is the user, type 3 peer is the other agent). Empty when the address is not applicable. Populated by the single builder convertToV1Conversation so every emit site renders the same form. |






<a name="laelia-v1-ConvertMessageToReminderRequest"></a>

### ConvertMessageToReminderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | message is the resource name of the trigger message (&#34;conversations/{c}/messages/{m}&#34;). Must be a root message in the conversation (thread_root empty) and not already a reminder. The calling agent claims the reminder at creation (assignee = caller). |
| task_content | [string](#string) |  | task_content is the agent&#39;s structured summary of the scheduled work. |
| fire_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | fire_at is the first fire time. Required. |
| cron_expr | [string](#string) |  | cron_expr, when non-empty, makes the reminder recurring (5-field cron in tz). Empty = one-shot. |
| tz | [string](#string) |  | tz is the IANA timezone for cron_expr. Defaults to &#34;UTC&#34; when empty. |






<a name="laelia-v1-ConvertMessageToReminderResponse"></a>

### ConvertMessageToReminderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminder | [Reminder](#laelia-v1-Reminder) |  |  |






<a name="laelia-v1-ConvertMessageToTaskRequest"></a>

### ConvertMessageToTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | message is the resource name of the top-level message to convert (&#34;conversations/{c}/messages/{m}&#34;). Must be a root message in the conversation (thread_root empty) and not already a task. |






<a name="laelia-v1-ConvertMessageToTaskResponse"></a>

### ConvertMessageToTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  | message is the converted message, with task populated. A separate system notification row is also inserted into the conversation flow. |






<a name="laelia-v1-CreateChannelRequest"></a>

### CreateChannelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  |  |






<a name="laelia-v1-CreateTaskRequest"></a>

### CreateTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| content | [string](#string) |  |  |
| mentions | [Mention](#laelia-v1-Mention) | repeated |  |
| attachments | [Attachment](#laelia-v1-Attachment) | repeated |  |






<a name="laelia-v1-CreateTaskResponse"></a>

### CreateTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  | message is the newly posted task message, with task populated. The task is created unassigned (status TODO); the posting agent does NOT auto-claim it — call ClaimTask afterwards to own it. |






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






<a name="laelia-v1-DiscoverProviders"></a>

### DiscoverProviders
DiscoverProviders asks the agent daemon to re-probe its host for installed
LLM agent providers and their models. The daemon replies with
AgentStreamMessage.providers_discovered.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | correlation id for the pending unary RefreshAgentProviders call |






<a name="laelia-v1-DownloadFileRequest"></a>

### DownloadFileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |






<a name="laelia-v1-DownloadFileResponse"></a>

### DownloadFileResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| file | [File](#laelia-v1-File) |  |  |
| data | [bytes](#bytes) |  |  |






<a name="laelia-v1-FailReminderRequest"></a>

### FailReminderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| error | [string](#string) |  | error is the failure reason, posted as a system message in the thread. |






<a name="laelia-v1-FailReminderResponse"></a>

### FailReminderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminder | [Reminder](#laelia-v1-Reminder) |  |  |






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






<a name="laelia-v1-File"></a>

### File
File is the persisted metadata for an S3-backed object.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| conversation | [string](#string) |  |  |
| uploader_principal_id | [string](#string) |  |  |
| original_name | [string](#string) |  |  |
| mime_type | [string](#string) |  |  |
| size_bytes | [int64](#int64) |  |  |
| s3_key | [string](#string) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






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






<a name="laelia-v1-GetOrCreateAgentDMRequest"></a>

### GetOrCreateAgentDMRequest
GetOrCreateAgentDM opens (or reuses) the type-3 agent-to-agent direct
conversation between the calling agent and the named peer agent. Agent-
callable. Self-address is rejected. The peer is resolved by agent resource
name (&#34;agents/&lt;id&gt;&#34;); the pair is canonicalized by the store.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| peer_agent | [string](#string) |  |  |






<a name="laelia-v1-GetOrCreateAgentDMResponse"></a>

### GetOrCreateAgentDMResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |






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






<a name="laelia-v1-GetOrCreateUserDMRequest"></a>

### GetOrCreateUserDMRequest
GetOrCreateUserDM opens (or reuses) the type-1 direct conversation between the
calling agent and the named end user. Agent-callable. The peer is resolved by
principal display name; an ambiguous (non-unique) or unknown name fails. This
is the agent-callable twin of the user-only GetOrCreateConversation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| peer_user_name | [string](#string) |  |  |






<a name="laelia-v1-GetOrCreateUserDMResponse"></a>

### GetOrCreateUserDMResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |






<a name="laelia-v1-GetReminderRequest"></a>

### GetReminderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the reminder resource name (&#34;reminders/{message_id}&#34;). |






<a name="laelia-v1-GetReminderResponse"></a>

### GetReminderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminder | [Reminder](#laelia-v1-Reminder) |  |  |






<a name="laelia-v1-LeaveChannelRequest"></a>

### LeaveChannelRequest
LeaveChannelRequest names the channel the caller is leaving. The caller is
resolved from the auth context; no member_id is carried.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






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






<a name="laelia-v1-ListChannelThreadsRequest"></a>

### ListChannelThreadsRequest
ListChannelThreadsRequest summarizes every active thread in a conversation:
each thread&#39;s root message, total reply count, and the latest reply&#39;s
room_version / created_at. The channel page polls this to keep the root
messages&#39; reply-count badges fresh (including replies that arrive while the
thread panel is closed, e.g. an async agent reply) without fetching the whole
message list — thread replies are excluded from ListConversationMessages, so
the message watcher alone cannot observe a changed reply count.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  | The conversation whose active threads to summarize: &#34;conversations/{id}&#34;. |






<a name="laelia-v1-ListChannelThreadsResponse"></a>

### ListChannelThreadsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| threads | [ChannelThread](#laelia-v1-ChannelThread) | repeated |  |






<a name="laelia-v1-ListChannelUpdatesRequest"></a>

### ListChannelUpdatesRequest
ListChannelUpdates returns, for the authenticated agent, every conversation
it is a member of whose current room_version is greater than the agent&#39;s
stored per-channel cursor — i.e. the channels that have unread messages. It
is the agent&#39;s &#34;what is worth my context&#34; discovery and drives the autonomous
drain loop. The agent identity is resolved from the auth context.






<a name="laelia-v1-ListChannelUpdatesResponse"></a>

### ListChannelUpdatesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| updates | [ChannelUpdate](#laelia-v1-ChannelUpdate) | repeated |  |






<a name="laelia-v1-ListChannelsForAgentRequest"></a>

### ListChannelsForAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListChannelsForAgentResponse"></a>

### ListChannelsForAgentResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| channels | [Conversation](#laelia-v1-Conversation) | repeated |  |
| next_page_token | [string](#string) |  |  |






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
| before_version | [int64](#int64) |  |  |






<a name="laelia-v1-ListConversationMessagesResponse"></a>

### ListConversationMessagesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |
| next_page_token | [string](#string) |  |  |
| current_version | [int64](#int64) |  |  |






<a name="laelia-v1-ListDueRemindersRequest"></a>

### ListDueRemindersRequest







<a name="laelia-v1-ListDueRemindersResponse"></a>

### ListDueRemindersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminders | [Reminder](#laelia-v1-Reminder) | repeated | reminders are the DUE reminders owned by the calling agent, ordered by fire_at ascending. The agent drain loop calls this each session to pick up fired reminders and process them. |






<a name="laelia-v1-ListFilesRequest"></a>

### ListFilesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






<a name="laelia-v1-ListFilesResponse"></a>

### ListFilesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| files | [File](#laelia-v1-File) | repeated |  |






<a name="laelia-v1-ListPeerAgentsRequest"></a>

### ListPeerAgentsRequest
ListPeerAgents returns every other agent (the caller excluded) with the
fields an agent needs to decide whom to address: display name, persona, and
connection state. Agent-callable. Powers the &#34;agent list&#34; discovery tool.






<a name="laelia-v1-ListPeerAgentsResponse"></a>

### ListPeerAgentsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agents | [PeerAgent](#laelia-v1-PeerAgent) | repeated |  |






<a name="laelia-v1-ListRemindersRequest"></a>

### ListRemindersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [string](#string) |  | agent, when set, restricts the result to reminders owned by the given agent (&#34;agents/{id}&#34;). The agent-page Reminders tab filters by the viewed agent. |
| conversation | [string](#string) |  | conversation, when set, restricts the result to reminders in that conversation. |
| status_filter | [ReminderStatus](#laelia-v1-ReminderStatus) | repeated | status_filter, when non-empty, restricts the result to the given statuses. |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListRemindersResponse"></a>

### ListRemindersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminders | [Reminder](#laelia-v1-Reminder) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListTasksRequest"></a>

### ListTasksRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| status_filter | [TaskStatus](#laelia-v1-TaskStatus) | repeated | status_filter, when non-empty, restricts the result to the given statuses. Empty returns tasks in every status. |






<a name="laelia-v1-ListTasksResponse"></a>

### ListTasksResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tasks | [ChatMessage](#laelia-v1-ChatMessage) | repeated | tasks are the channel&#39;s task root messages, each with task populated, ordered by task_number ascending. |






<a name="laelia-v1-ListThreadMessagesRequest"></a>

### ListThreadMessagesRequest
ListThreadMessagesRequest reads one thread: the root message followed by its
replies, in room_version order. The root message is included as the first
element so a reader has the thread context. The cursor model mirrors
ListConversationMessages: after_version returns replies with room_version
greater than it (chronological tail), before_version returns a page before a
pivot, and the default returns the latest N replies.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| thread_root | [string](#string) |  | thread_root is the resource name of the root message (&#34;conversations/{c}/messages/{m}&#34;). |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| after_version | [int64](#int64) |  |  |
| before_version | [int64](#int64) |  |  |






<a name="laelia-v1-ListThreadMessagesResponse"></a>

### ListThreadMessagesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated | messages is [root, ...replies] in room_version order. |
| next_page_token | [string](#string) |  |  |
| current_version | [int64](#int64) |  |  |






<a name="laelia-v1-ListThreadParticipantsRequest"></a>

### ListThreadParticipantsRequest
ListThreadParticipantsRequest lists the distinct senders (users and agents) that
have posted in a thread (the root message and its replies). Participants are derived
from message senders, not from a membership table, so this reflects who actually
took part in the thread. The caller must be a member of the conversation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| thread_root | [string](#string) |  | thread_root is the resource name of the thread&#39;s root message (&#34;conversations/{c}/messages/{m}&#34;). |






<a name="laelia-v1-ListThreadParticipantsResponse"></a>

### ListThreadParticipantsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| members | [ChannelMember](#laelia-v1-ChannelMember) | repeated | members is the distinct senders in the thread. member_role is not meaningful for thread participation and is left 0. |






<a name="laelia-v1-ListThreadUpdatesRequest"></a>

### ListThreadUpdatesRequest
ListThreadUpdatesRequest returns, for the authenticated agent, every thread
the agent is subscribed to (via @mention or having replied) that has replies
with room_version beyond the agent&#39;s per-channel cursor for that
conversation. It is the agent&#39;s thread inbox and is run after message check in
the drain loop, before acking the conversation cursor.






<a name="laelia-v1-ListThreadUpdatesResponse"></a>

### ListThreadUpdatesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| updates | [ThreadUpdate](#laelia-v1-ThreadUpdate) | repeated |  |






<a name="laelia-v1-ManagerStreamMessage"></a>

### ManagerStreamMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| new_messages | [NewMessagesAvailable](#laelia-v1-NewMessagesAvailable) |  |  |
| begin_session_response | [BeginSessionResponse](#laelia-v1-BeginSessionResponse) |  |  |
| cancel | [CancelMessage](#laelia-v1-CancelMessage) |  |  |
| pong | [Pong](#laelia-v1-Pong) |  |  |
| permission_decision | [PermissionDecision](#laelia-v1-PermissionDecision) |  |  |
| discover_providers | [DiscoverProviders](#laelia-v1-DiscoverProviders) |  | ask the agent daemon to re-probe installed LLM agent providers |






<a name="laelia-v1-MarkConversationReadRequest"></a>

### MarkConversationReadRequest
MarkConversationRead advances the requesting user&#39;s per-conversation read
cursor to the conversation&#39;s current room_version, clearing the user-facing
unread badge for that conversation. read_version in the response is the
resulting cursor value, so the frontend can set its local state to the exact
server value.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






<a name="laelia-v1-MarkConversationReadResponse"></a>

### MarkConversationReadResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| read_version | [int64](#int64) |  |  |






<a name="laelia-v1-Mention"></a>

### Mention



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| type | [string](#string) |  |  |
| id | [string](#string) |  |  |
| name | [string](#string) |  |  |






<a name="laelia-v1-NewMessagesAvailable"></a>

### NewMessagesAvailable
NewMessagesAvailable is a best-effort wake signal pushed from the Manager to
an agent over the bidi stream whenever a conversation the agent is a member
of produces a new message (from any sender: user, agent, or system). It is
NOT the source of truth: the agent&#39;s durable per-channel cursor is. If a wake
is missed (agent offline), the agent rediscovers pending work on reconnect by
calling ListChannelUpdates, which compares conversation.version to the cursor.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation_ids | [string](#string) | repeated |  |
| versions | [int64](#int64) | repeated |  |
| thread_root_message_id | [string](#string) |  | thread_root_message_id, when non-empty, indicates the wake is for a new reply in a thread the agent is subscribed to (the value is the thread&#39;s root message resource name). It is a hint so the agent can go straight to thread check/read; the agent still relies on ListThreadUpdates as the source of truth. Empty means a normal channel-message wake. |






<a name="laelia-v1-PeerAgent"></a>

### PeerAgent
PeerAgent is a roster entry for the calling agent: the name, display name,
persona, and connection state of one peer agent. Returned by ListPeerAgents,
which excludes the caller.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| display_name | [string](#string) |  |  |
| persona_prompt | [string](#string) |  |  |
| connection_state | [AgentStatus.ConnectionState](#laelia-v1-AgentStatus-ConnectionState) |  |  |






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
| attachments | [Attachment](#laelia-v1-Attachment) | repeated |  |
| thread_root | [string](#string) |  | thread_root, when set, makes this agent reply a message in the thread rooted at the given message name. Empty posts a normal channel message. |






<a name="laelia-v1-PostMessageResponse"></a>

### PostMessageResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| committed | [bool](#bool) |  |  |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  |  |
| current_version | [int64](#int64) |  |  |
| new_messages | [ChatMessage](#laelia-v1-ChatMessage) | repeated |  |
| conflict_description | [string](#string) |  |  |






<a name="laelia-v1-ProvidersDiscovered"></a>

### ProvidersDiscovered
ProvidersDiscovered carries the freshly discovered provider list back to the
manager, which persists it into agent.info.available_providers and hands it
to the pending RefreshAgentProviders caller.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  |  |
| providers | [AgentProviderInfo](#laelia-v1-AgentProviderInfo) | repeated |  |






<a name="laelia-v1-RawAcpPayload"></a>

### RawAcpPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |






<a name="laelia-v1-Reminder"></a>

### Reminder
Reminder is the scheduled-task metadata attached to a root chat_message. The
chat_message (root) remains the source of truth for the trigger content; this
row carries the schedule, assignee, and lifecycle state. The resource name is
&#34;reminders/{message_id}&#34; — the reminder&#39;s identity is its trigger message.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| conversation | [string](#string) |  | conversation is the conversation the trigger message belongs to (&#34;conversations/{id}&#34;). |
| message | [string](#string) |  | message is the trigger message (&#34;conversations/{c}/messages/{m}&#34;); it is also the thread root for the reminder&#39;s discussion. |
| assignee_agent | [string](#string) |  | assignee_agent is the owning agent (&#34;agents/{id}&#34;); the agent that claimed the reminder at creation. |
| assignee_name | [string](#string) |  |  |
| task_content | [string](#string) |  | task_content is the agent&#39;s structured summary of the work to perform on each fire. |
| fire_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | fire_at is the next fire time. For one-shot reminders (cron_expr empty) it is the single trigger time; for recurring reminders it is the next cron fire, recomputed after each completion/miss. |
| cron_expr | [string](#string) |  | cron_expr, when non-empty, makes the reminder recurring. A standard 5-field cron (min hour dom month dow) interpreted in tz. Empty = one-shot. |
| tz | [string](#string) |  | tz is the IANA timezone name used to interpret cron_expr (e.g. &#34;Asia/Shanghai&#34;). &#34;UTC&#34; by default. |
| status | [ReminderStatus](#laelia-v1-ReminderStatus) |  |  |
| retry_count | [int32](#int32) |  | retry_count is the number of delivery attempts since the last fire for an offline agent (0 when the agent was reached on the first try). |
| next_retry_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_attempt_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_fired_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_completed_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| result | [string](#string) |  | result is the agent&#39;s completion/failure report from the most recent fire. |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-RemoveChannelMemberRequest"></a>

### RemoveChannelMemberRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| member_id | [string](#string) |  |  |
| member_type | [int32](#int32) |  |  |






<a name="laelia-v1-ResolveChannelByTitleRequest"></a>

### ResolveChannelByTitleRequest
ResolveChannelByTitle looks up the unique channel (type 2) with the given
title. Agent-callable (no auth_method annotation; identity from
GetAgentFromContext). Returns NOT_FOUND when no such channel exists; it never
creates one. Powers the &#34;#&lt;title&gt;&#34; address resolver.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| title | [string](#string) |  |  |






<a name="laelia-v1-ResolveChannelByTitleResponse"></a>

### ResolveChannelByTitleResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |






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
| mentions | [Mention](#laelia-v1-Mention) | repeated |  |
| attachments | [Attachment](#laelia-v1-Attachment) | repeated |  |
| thread_root | [string](#string) |  | thread_root, when set, makes this message a reply in the thread rooted at the given message name (&#34;conversations/{c}/messages/{m}&#34;). Empty posts a normal channel message. |
| as_task | [bool](#bool) |  | as_task, when true, creates this message as a task: a task row is inserted in the same transaction with a per-conversation task number and status TODO. Only valid for top-level messages (thread_root must be empty). |






<a name="laelia-v1-TaskInfo"></a>

### TaskInfo
TaskInfo is the task metadata attached to a ChatMessage that is a task. It is
a read-only join output populated by ListConversationMessages /
ListThreadMessages for root messages; absent on non-task messages and on
thread replies. The chat_message itself remains the source of truth for
content/sender/room_version.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| task_number | [int32](#int32) |  | task_number is the per-conversation sequence number shown as &#34;[task #N]&#34;. |
| status | [TaskStatus](#laelia-v1-TaskStatus) |  |  |
| assignee_name | [string](#string) |  | assignee_name is the assigned agent&#39;s display name, empty when unassigned. |
| assignee_resource_id | [string](#string) |  | assignee_resource_id is the assigned agent&#39;s resource id (&#34;agents/&lt;id&gt;&#34;), empty when unassigned. |






<a name="laelia-v1-TextDeltaPayload"></a>

### TextDeltaPayload



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| stream_type | [string](#string) |  |  |
| content | [string](#string) |  |  |






<a name="laelia-v1-ThreadUpdate"></a>

### ThreadUpdate
ThreadUpdate describes one subscribed thread with unread replies.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| thread_root | [string](#string) |  | thread_root is the resource name of the thread&#39;s root message. |
| latest_version | [int64](#int64) |  | latest_version is the maximum room_version among the thread&#39;s replies (the version the agent should read up to before acking). |
| new_reply_count | [int32](#int32) |  | new_reply_count is the number of replies with room_version greater than the agent&#39;s processed_version for this conversation. |






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






<a name="laelia-v1-TransferChannelOwnershipRequest"></a>

### TransferChannelOwnershipRequest
TransferChannelOwnershipRequest names the channel and the member who will
become the new owner. The new owner must already be a member.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| member_type | [int32](#int32) |  |  |
| member_id | [string](#string) |  |  |






<a name="laelia-v1-TransferChannelOwnershipResponse"></a>

### TransferChannelOwnershipResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |






<a name="laelia-v1-UnclaimTaskRequest"></a>

### UnclaimTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  |  |






<a name="laelia-v1-UnclaimTaskResponse"></a>

### UnclaimTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  |  |






<a name="laelia-v1-UpdateChannelMemberRoleRequest"></a>

### UpdateChannelMemberRoleRequest
UpdateChannelMemberRoleRequest sets a member&#39;s chat role. target_role is the
conversation_member role value: 2 = Member, 3 = Admin. Owner (1) is not
settable here — ownership only moves via TransferChannelOwnership.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| member_id | [string](#string) |  |  |
| member_type | [int32](#int32) |  |  |
| target_role | [int32](#int32) |  |  |






<a name="laelia-v1-UpdateChannelRequest"></a>

### UpdateChannelRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |






<a name="laelia-v1-UpdateReminderRequest"></a>

### UpdateReminderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is the reminder resource name (&#34;reminders/{message_id}&#34;). |
| fire_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | fire_at, cron_expr, tz, task_content are the editable fields. At least one must be set. Editing a DUE or MISSED reminder resets it to PENDING with the new schedule. COMPLETED/CANCELLED/FAILED reminders cannot be edited. |
| cron_expr | [string](#string) |  |  |
| tz | [string](#string) |  |  |
| task_content | [string](#string) |  |  |






<a name="laelia-v1-UpdateReminderResponse"></a>

### UpdateReminderResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reminder | [Reminder](#laelia-v1-Reminder) |  |  |






<a name="laelia-v1-UpdateTaskStatusRequest"></a>

### UpdateTaskStatusRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  |  |
| status | [TaskStatus](#laelia-v1-TaskStatus) |  | status is the target status. Allowed transitions (enforced server-side): IN_PROGRESS -&gt; IN_REVIEW (the assignee marks the task ready for human review) and IN_REVIEW -&gt; DONE (the assignee marks the task done after detecting the human&#39;s approval in the task&#39;s thread). TODO -&gt; IN_PROGRESS is performed by ClaimTask, not this RPC. |






<a name="laelia-v1-UpdateTaskStatusResponse"></a>

### UpdateTaskStatusResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  |  |






<a name="laelia-v1-UploadFileRequest"></a>

### UploadFileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| original_name | [string](#string) |  |  |
| mime_type | [string](#string) |  |  |
| data | [bytes](#bytes) |  |  |






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



<a name="laelia-v1-ReminderStatus"></a>

### ReminderStatus
ReminderStatus is the lifecycle state of a reminder. Values prefixed to
satisfy protobuf&#39;s C&#43;&#43; scoping rules (sibling enums cannot share value
names), matching TaskStatus/SenderType.

| Name | Number | Description |
| ---- | ------ | ----------- |
| REMINDER_STATUS_UNSPECIFIED | 0 |  |
| REMINDER_STATUS_PENDING | 1 |  |
| REMINDER_STATUS_DUE | 2 |  |
| REMINDER_STATUS_COMPLETED | 3 |  |
| REMINDER_STATUS_CANCELLED | 4 |  |
| REMINDER_STATUS_MISSED | 5 |  |
| REMINDER_STATUS_FAILED | 6 |  |



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



<a name="laelia-v1-TaskStatus"></a>

### TaskStatus
TaskStatus is the lifecycle state of a task message. A task is a top-level
channel/DM message with task metadata; its thread is the discussion/approval
channel. Values prefixed to satisfy protobuf&#39;s C&#43;&#43; scoping rules (sibling
enums cannot share value names), matching SenderType/CommandStatus.

| Name | Number | Description |
| ---- | ------ | ----------- |
| TASK_STATUS_UNSPECIFIED | 0 |  |
| TASK_STATUS_TODO | 1 |  |
| TASK_STATUS_IN_PROGRESS | 2 |  |
| TASK_STATUS_IN_REVIEW | 3 |  |
| TASK_STATUS_DONE | 4 |  |


 

 


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
| ResolveChannelByTitle | [ResolveChannelByTitleRequest](#laelia-v1-ResolveChannelByTitleRequest) | [ResolveChannelByTitleResponse](#laelia-v1-ResolveChannelByTitleResponse) | ResolveChannelByTitle looks up the unique channel (type 2) with the given title, returning NOT_FOUND when absent (it never creates one). Agent- callable: no auth_method annotation, identity from GetAgentFromContext. Powers the &#34;#&lt;title&gt;&#34; address resolver. |
| GetOrCreateUserDM | [GetOrCreateUserDMRequest](#laelia-v1-GetOrCreateUserDMRequest) | [GetOrCreateUserDMResponse](#laelia-v1-GetOrCreateUserDMResponse) | GetOrCreateUserDM opens (or reuses) the type-1 DM between the calling agent and a named end user. Agent-callable. The peer is resolved by principal display name; ambiguous or unknown names fail. Agent-callable twin of the user-only GetOrCreateConversation. Powers the &#34;dm:@&lt;user&gt;&#34; address resolver. |
| GetOrCreateAgentDM | [GetOrCreateAgentDMRequest](#laelia-v1-GetOrCreateAgentDMRequest) | [GetOrCreateAgentDMResponse](#laelia-v1-GetOrCreateAgentDMResponse) | GetOrCreateAgentDM opens (or reuses) the type-3 agent-to-agent DM between the calling agent and a peer agent. Agent-callable. Self-address is rejected. The peer is resolved by agent resource name (&#34;agents/&lt;id&gt;&#34;); the pair is canonicalized by the store. Powers the &#34;dm:@&lt;agent&gt;&#34; address resolver. |
| ListPeerAgents | [ListPeerAgentsRequest](#laelia-v1-ListPeerAgentsRequest) | [ListPeerAgentsResponse](#laelia-v1-ListPeerAgentsResponse) | ListPeerAgents returns every other agent (the caller excluded) with the display name, persona, and connection state an agent needs to decide whom to address. Agent-callable. Powers the &#34;agent list&#34; discovery tool. |
| ListConversationMessages | [ListConversationMessagesRequest](#laelia-v1-ListConversationMessagesRequest) | [ListConversationMessagesResponse](#laelia-v1-ListConversationMessagesResponse) |  |
| ListThreadMessages | [ListThreadMessagesRequest](#laelia-v1-ListThreadMessagesRequest) | [ListThreadMessagesResponse](#laelia-v1-ListThreadMessagesResponse) |  |
| ListChannelThreads | [ListChannelThreadsRequest](#laelia-v1-ListChannelThreadsRequest) | [ListChannelThreadsResponse](#laelia-v1-ListChannelThreadsResponse) |  |
| CreateChannel | [CreateChannelRequest](#laelia-v1-CreateChannelRequest) | [Conversation](#laelia-v1-Conversation) |  |
| ListChannels | [ListChannelsRequest](#laelia-v1-ListChannelsRequest) | [ListChannelsResponse](#laelia-v1-ListChannelsResponse) |  |
| ListChannelsForAgent | [ListChannelsForAgentRequest](#laelia-v1-ListChannelsForAgentRequest) | [ListChannelsForAgentResponse](#laelia-v1-ListChannelsForAgentResponse) | ListChannelsForAgent returns every conversation the given agent is a member of (both direct DMs with users and multi-user channels), used by the agent detail page&#39;s &#34;Chat&#34; tab. Admin-scoped: gated by laelia.agents.get. |
| GetChannel | [GetChannelRequest](#laelia-v1-GetChannelRequest) | [Conversation](#laelia-v1-Conversation) |  |
| UpdateChannel | [UpdateChannelRequest](#laelia-v1-UpdateChannelRequest) | [Conversation](#laelia-v1-Conversation) |  |
| DeleteChannel | [DeleteChannelRequest](#laelia-v1-DeleteChannelRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| AddChannelMember | [AddChannelMemberRequest](#laelia-v1-AddChannelMemberRequest) | [ChannelMember](#laelia-v1-ChannelMember) |  |
| RemoveChannelMember | [RemoveChannelMemberRequest](#laelia-v1-RemoveChannelMemberRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| TransferChannelOwnership | [TransferChannelOwnershipRequest](#laelia-v1-TransferChannelOwnershipRequest) | [TransferChannelOwnershipResponse](#laelia-v1-TransferChannelOwnershipResponse) | TransferChannelOwnership hands channel ownership from the calling owner to another member: the target is promoted to Owner and the caller demoted to Member, atomically. The interceptor gates the call with conversations.manage (Admin&#43;Owner); the handler additionally enforces that the caller is the current Owner. Only channels (type 2) support ownership transfer. |
| UpdateChannelMemberRole | [UpdateChannelMemberRoleRequest](#laelia-v1-UpdateChannelMemberRoleRequest) | [ChannelMember](#laelia-v1-ChannelMember) | UpdateChannelMemberRole grants or revokes channel admin: the target member&#39;s role is set to the requested role (Member or Admin). The interceptor gates with conversations.manage (Admin&#43;Owner); the handler enforces that the caller is the Owner and the target role is Member or Admin (never Owner — ownership only moves via TransferChannelOwnership). |
| LeaveChannel | [LeaveChannelRequest](#laelia-v1-LeaveChannelRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | LeaveChannel removes the calling member from a channel. The interceptor gates with conversations.read (any member); the handler rejects the current Owner — an owner must transfer ownership or delete the channel first to avoid orphaning it. Only channels (type 2) support leaving. |
| ListChannelMembers | [ListChannelMembersRequest](#laelia-v1-ListChannelMembersRequest) | [ListChannelMembersResponse](#laelia-v1-ListChannelMembersResponse) |  |
| ListThreadParticipants | [ListThreadParticipantsRequest](#laelia-v1-ListThreadParticipantsRequest) | [ListThreadParticipantsResponse](#laelia-v1-ListThreadParticipantsResponse) | ListThreadParticipants lists the distinct senders (users and agents) that posted in a thread. Intended for the agent daemon. The caller must be a member of the conversation. |
| SendMessage | [SendMessageRequest](#laelia-v1-SendMessageRequest) | [ChatMessage](#laelia-v1-ChatMessage) |  |
| PostMessage | [PostMessageRequest](#laelia-v1-PostMessageRequest) | [PostMessageResponse](#laelia-v1-PostMessageResponse) |  |
| ConvertMessageToTask | [ConvertMessageToTaskRequest](#laelia-v1-ConvertMessageToTaskRequest) | [ConvertMessageToTaskResponse](#laelia-v1-ConvertMessageToTaskResponse) | ConvertMessageToTask turns an existing top-level message into a task by attaching task metadata (number, status=TODO, no assignee). Any channel member (user or agent) may convert. Emits a system notification row. |
| ListTasks | [ListTasksRequest](#laelia-v1-ListTasksRequest) | [ListTasksResponse](#laelia-v1-ListTasksResponse) | ListTasks returns the task board for a conversation: every task (root message with task metadata) in the channel, optionally filtered by status. |
| CreateTask | [CreateTaskRequest](#laelia-v1-CreateTaskRequest) | [CreateTaskResponse](#laelia-v1-CreateTaskResponse) | CreateTask posts a new top-level task message in a channel (used by agents to break work into subtasks for others to claim). The new task is created unassigned (status TODO); the posting agent does NOT auto-claim it. Emits a system notification row and wakes other agent members. |
| ClaimTask | [ClaimTaskRequest](#laelia-v1-ClaimTaskRequest) | [ClaimTaskResponse](#laelia-v1-ClaimTaskResponse) | ClaimTask atomically transitions a TODO task to IN_PROGRESS and assigns it to the calling agent, subscribing the agent to the task&#39;s thread so approval replies wake it. Returns FAILED_PRECONDITION if the task is already claimed or not in TODO. Emits a system notification row. |
| UnclaimTask | [UnclaimTaskRequest](#laelia-v1-UnclaimTaskRequest) | [UnclaimTaskResponse](#laelia-v1-UnclaimTaskResponse) | UnclaimTask releases the calling agent&#39;s claim on a task it owns, setting it back to TODO so another agent may claim it. Not allowed on DONE (terminal). Emits a system notification row. |
| UpdateTaskStatus | [UpdateTaskStatusRequest](#laelia-v1-UpdateTaskStatusRequest) | [UpdateTaskStatusResponse](#laelia-v1-UpdateTaskStatusResponse) | UpdateTaskStatus advances a task&#39;s status. IN_PROGRESS -&gt; IN_REVIEW marks the assignee&#39;s work ready for human review; IN_REVIEW -&gt; DONE marks it complete (the assignee should call this only after detecting the human&#39;s approval in the task&#39;s thread). Only the assignee may call this. Emits a system notification row. |
| ConvertMessageToReminder | [ConvertMessageToReminderRequest](#laelia-v1-ConvertMessageToReminderRequest) | [ConvertMessageToReminderResponse](#laelia-v1-ConvertMessageToReminderResponse) | ConvertMessageToReminder turns an existing top-level message into a scheduled reminder owned by the calling agent (atomic create&#43;claim). The message must be a root in the conversation and not already a reminder. The agent is subscribed to the reminder&#39;s thread so discussion replies wake it. |
| ListReminders | [ListRemindersRequest](#laelia-v1-ListRemindersRequest) | [ListRemindersResponse](#laelia-v1-ListRemindersResponse) | ListReminders returns reminders, optionally filtered by owning agent, conversation, and status. Used by the agent-page Reminders tab (user) and the agent CLI (self-list). |
| GetReminder | [GetReminderRequest](#laelia-v1-GetReminderRequest) | [GetReminderResponse](#laelia-v1-GetReminderResponse) | GetReminder returns a single reminder by its resource name. |
| UpdateReminder | [UpdateReminderRequest](#laelia-v1-UpdateReminderRequest) | [UpdateReminderResponse](#laelia-v1-UpdateReminderResponse) | UpdateReminder edits the schedule (fire_at/cron_expr/tz) or task_content of a reminder. The caller is the owning agent or a workspace admin. Editing a DUE or MISSED reminder resets it to PENDING with the new schedule. |
| CancelReminder | [CancelReminderRequest](#laelia-v1-CancelReminderRequest) | [CancelReminderResponse](#laelia-v1-CancelReminderResponse) | CancelReminder cancels a reminder. The caller is the owning agent or a workspace admin. A cancelled reminder is terminal. |
| CompleteReminder | [CompleteReminderRequest](#laelia-v1-CompleteReminderRequest) | [CompleteReminderResponse](#laelia-v1-CompleteReminderResponse) | CompleteReminder marks a DUE reminder completed and atomically posts the result as a single system message in the reminder&#39;s thread. Only the owning agent may call this. Recurring reminders reschedule to the next cron fire. |
| FailReminder | [FailReminderRequest](#laelia-v1-FailReminderRequest) | [FailReminderResponse](#laelia-v1-FailReminderResponse) | FailReminder marks a DUE reminder failed with the given error and posts it as a system thread message. Recurring reminders reschedule. Only the owning agent may call this. |
| ListDueReminders | [ListDueRemindersRequest](#laelia-v1-ListDueRemindersRequest) | [ListDueRemindersResponse](#laelia-v1-ListDueRemindersResponse) | ListDueReminders returns the DUE reminders owned by the calling agent, for the autonomous drain loop to pick up fired work. Agent identity is resolved from the auth context. |
| ListChannelUpdates | [ListChannelUpdatesRequest](#laelia-v1-ListChannelUpdatesRequest) | [ListChannelUpdatesResponse](#laelia-v1-ListChannelUpdatesResponse) |  |
| ListThreadUpdates | [ListThreadUpdatesRequest](#laelia-v1-ListThreadUpdatesRequest) | [ListThreadUpdatesResponse](#laelia-v1-ListThreadUpdatesResponse) |  |
| AckProcessedVersion | [AckProcessedVersionRequest](#laelia-v1-AckProcessedVersionRequest) | [AckProcessedVersionResponse](#laelia-v1-AckProcessedVersionResponse) |  |
| FetchConversationActivity | [FetchConversationActivityRequest](#laelia-v1-FetchConversationActivityRequest) | [FetchConversationActivityResponse](#laelia-v1-FetchConversationActivityResponse) |  |
| MarkConversationRead | [MarkConversationReadRequest](#laelia-v1-MarkConversationReadRequest) | [MarkConversationReadResponse](#laelia-v1-MarkConversationReadResponse) |  |
| UploadFile | [UploadFileRequest](#laelia-v1-UploadFileRequest) | [File](#laelia-v1-File) | UploadFile stores data in S3 and persists a file row. Intended for the agent daemon (browser uploads go through the Echo multipart route). No google.api.http annotation: the agent reaches it via Connect-JSON over the CommandServiceClient, and avoiding a /v1/files/{id} gateway entry keeps it from colliding with the browser download route. |
| DownloadFile | [DownloadFileRequest](#laelia-v1-DownloadFileRequest) | [DownloadFileResponse](#laelia-v1-DownloadFileResponse) | DownloadFile fetches a file&#39;s bytes from S3. The caller must be a member of the file&#39;s conversation. Used by the agent daemon; browser downloads go through the Echo route. |
| ListFiles | [ListFilesRequest](#laelia-v1-ListFilesRequest) | [ListFilesResponse](#laelia-v1-ListFilesResponse) | ListFiles returns the files attached to a conversation. The caller must be a member. |

 



<a name="v1_iam_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/iam_service.proto



<a name="laelia-v1-GetAgentIamPolicyRequest"></a>

### GetAgentIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The agent resource name, in the form `agents/{agent}`. |






<a name="laelia-v1-GetWorkspaceIamPolicyRequest"></a>

### GetWorkspaceIamPolicyRequest







<a name="laelia-v1-IamPolicyView"></a>

### IamPolicyView
IamPolicyView is an IAM policy together with its etag. The etag is returned
by Get and must be supplied on Set for optimistic concurrency: a Set whose
etag does not match the stored policy&#39;s etag is rejected with
connect.CodeAborted so the caller can re-fetch and retry.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [laelia.store.IamPolicy](#laelia-store-IamPolicy) |  |  |
| etag | [string](#string) |  |  |






<a name="laelia-v1-SetAgentIamPolicyRequest"></a>

### SetAgentIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The agent resource name, in the form `agents/{agent}`. |
| policy | [laelia.store.IamPolicy](#laelia-store-IamPolicy) |  |  |
| etag | [string](#string) |  |  |






<a name="laelia-v1-SetWorkspaceIamPolicyRequest"></a>

### SetWorkspaceIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [laelia.store.IamPolicy](#laelia-store-IamPolicy) |  |  |
| etag | [string](#string) |  |  |





 

 

 


<a name="laelia-v1-IamService"></a>

### IamService
IamService exposes the workspace and per-agent IAM policies for management.
Get reads the full policy; Set replaces it whole, guarded by an etag. Each
RPC is gated by the IAM interceptor with laelia.iam.getPolicy / setPolicy.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetWorkspaceIamPolicy | [GetWorkspaceIamPolicyRequest](#laelia-v1-GetWorkspaceIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Get the workspace IAM policy. |
| SetWorkspaceIamPolicy | [SetWorkspaceIamPolicyRequest](#laelia-v1-SetWorkspaceIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Set the workspace IAM policy (full replace, etag-guarded). |
| GetAgentIamPolicy | [GetAgentIamPolicyRequest](#laelia-v1-GetAgentIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Get the IAM policy attached to an agent. |
| SetAgentIamPolicy | [SetAgentIamPolicyRequest](#laelia-v1-SetAgentIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Set the IAM policy attached to an agent (full replace, etag-guarded). |

 



<a name="v1_role_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/role_service.proto



<a name="laelia-v1-CreateRoleRequest"></a>

### CreateRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [Role](#laelia-v1-Role) |  |  |






<a name="laelia-v1-DeleteRoleRequest"></a>

### DeleteRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-GetRoleRequest"></a>

### GetRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the role, in the form `roles/{role}`. |






<a name="laelia-v1-ListRolesRequest"></a>

### ListRolesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListRolesResponse"></a>

### ListRolesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| roles | [Role](#laelia-v1-Role) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-Role"></a>

### Role
Role is a named bundle of permissions. Predefined roles (workspaceAdmin,
workspaceMember, conversationMember/Admin/Owner, agentEditor,
agentDMReviewer, oversightReviewer) are defined in Go and never stored in
the DB; custom roles live in the role table. Both resolve identically in the
IAM engine. Predefined roles are read-only over this API.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the role, in the form `roles/{resource_id}`. |
| title | [string](#string) |  | Human-readable title. |
| description | [string](#string) |  | Longer description of what the role grants. |
| permissions | [string](#string) | repeated | Permissions bundled into the role, each a `laelia.&lt;resource&gt;.&lt;verb&gt;` string from the permission catalog. |
| predefined | [bool](#bool) |  | Output only. Whether the role is predefined (defined in Go, read-only). |






<a name="laelia-v1-UpdateRoleRequest"></a>

### UpdateRoleRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| role | [Role](#laelia-v1-Role) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 

 

 


<a name="laelia-v1-RoleService"></a>

### RoleService
RoleService manages custom roles. Predefined roles are read-only over this
API: create/update/delete refuse a resource id that collides with a
predefined role. Each RPC is gated by the IAM interceptor with the
laelia.roles.* permissions.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetRole | [GetRoleRequest](#laelia-v1-GetRoleRequest) | [Role](#laelia-v1-Role) | Get a role. |
| ListRoles | [ListRolesRequest](#laelia-v1-ListRolesRequest) | [ListRolesResponse](#laelia-v1-ListRolesResponse) | List all roles (predefined and custom). |
| CreateRole | [CreateRoleRequest](#laelia-v1-CreateRoleRequest) | [Role](#laelia-v1-Role) | Create a custom role. |
| UpdateRole | [UpdateRoleRequest](#laelia-v1-UpdateRoleRequest) | [Role](#laelia-v1-Role) | Update a custom role. |
| DeleteRole | [DeleteRoleRequest](#laelia-v1-DeleteRoleRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete a custom role. |

 



<a name="v1_setting-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/setting.proto



<a name="laelia-v1-GetDebugConfigRequest"></a>

### GetDebugConfigRequest







<a name="laelia-v1-GetDebugConfigResponse"></a>

### GetDebugConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  |  |






<a name="laelia-v1-GetS3ConfigRequest"></a>

### GetS3ConfigRequest







<a name="laelia-v1-GetS3ConfigResponse"></a>

### GetS3ConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.S3ConfigSetting](#laelia-store-S3ConfigSetting) |  |  |






<a name="laelia-v1-UpdateDebugConfigRequest"></a>

### UpdateDebugConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  |  |






<a name="laelia-v1-UpdateDebugConfigResponse"></a>

### UpdateDebugConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  |  |






<a name="laelia-v1-UpdateS3ConfigRequest"></a>

### UpdateS3ConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.S3ConfigSetting](#laelia-store-S3ConfigSetting) |  |  |






<a name="laelia-v1-UpdateS3ConfigResponse"></a>

### UpdateS3ConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.S3ConfigSetting](#laelia-store-S3ConfigSetting) |  |  |





 

 

 


<a name="laelia-v1-SettingService"></a>

### SettingService
SettingService exposes workspace-level configuration. It is admin-only; the
handlers enforce workspace admin membership and return
connect.CodePermissionDenied otherwise. The S3 secret_key is masked on read;
an update carrying a masked secret preserves the stored value.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetS3Config | [GetS3ConfigRequest](#laelia-v1-GetS3ConfigRequest) | [GetS3ConfigResponse](#laelia-v1-GetS3ConfigResponse) |  |
| UpdateS3Config | [UpdateS3ConfigRequest](#laelia-v1-UpdateS3ConfigRequest) | [UpdateS3ConfigResponse](#laelia-v1-UpdateS3ConfigResponse) |  |
| GetDebugConfig | [GetDebugConfigRequest](#laelia-v1-GetDebugConfigRequest) | [GetDebugConfigResponse](#laelia-v1-GetDebugConfigResponse) |  |
| UpdateDebugConfig | [UpdateDebugConfigRequest](#laelia-v1-UpdateDebugConfigRequest) | [UpdateDebugConfigResponse](#laelia-v1-UpdateDebugConfigResponse) |  |

 



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

