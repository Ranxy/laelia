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
    - [PermissionDeniedDetail](#laelia-v1-PermissionDeniedDetail)
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
    - [DeleteAgentAvatarRequest](#laelia-v1-DeleteAgentAvatarRequest)
    - [DeleteAgentRequest](#laelia-v1-DeleteAgentRequest)
    - [DownloadAgentAvatarRequest](#laelia-v1-DownloadAgentAvatarRequest)
    - [DownloadAgentAvatarResponse](#laelia-v1-DownloadAgentAvatarResponse)
    - [ForceDisconnectAgentRequest](#laelia-v1-ForceDisconnectAgentRequest)
    - [GetAgentRequest](#laelia-v1-GetAgentRequest)
    - [HelloRequest](#laelia-v1-HelloRequest)
    - [HelloResponse](#laelia-v1-HelloResponse)
    - [ListAgentSessionsRequest](#laelia-v1-ListAgentSessionsRequest)
    - [ListAgentSessionsResponse](#laelia-v1-ListAgentSessionsResponse)
    - [ListAgentWorkspaceRequest](#laelia-v1-ListAgentWorkspaceRequest)
    - [ListAgentWorkspaceResponse](#laelia-v1-ListAgentWorkspaceResponse)
    - [ListAgentsRequest](#laelia-v1-ListAgentsRequest)
    - [ListAgentsResponse](#laelia-v1-ListAgentsResponse)
    - [ListPiModelsRequest](#laelia-v1-ListPiModelsRequest)
    - [ListPiModelsResponse](#laelia-v1-ListPiModelsResponse)
    - [PendingCommandHint](#laelia-v1-PendingCommandHint)
    - [PendingCommandHint.EnvEntry](#laelia-v1-PendingCommandHint-EnvEntry)
    - [PiModel](#laelia-v1-PiModel)
    - [ReadAgentWorkspaceFileRequest](#laelia-v1-ReadAgentWorkspaceFileRequest)
    - [ReadAgentWorkspaceFileResponse](#laelia-v1-ReadAgentWorkspaceFileResponse)
    - [RefreshAgentProvidersRequest](#laelia-v1-RefreshAgentProvidersRequest)
    - [RefreshAgentProvidersResponse](#laelia-v1-RefreshAgentProvidersResponse)
    - [RefreshAgentTokenRequest](#laelia-v1-RefreshAgentTokenRequest)
    - [RefreshAgentTokenResponse](#laelia-v1-RefreshAgentTokenResponse)
    - [RevokeAgentTokenRequest](#laelia-v1-RevokeAgentTokenRequest)
    - [RevokeAgentTokenResponse](#laelia-v1-RevokeAgentTokenResponse)
    - [RotateAgentTokenRequest](#laelia-v1-RotateAgentTokenRequest)
    - [RotateAgentTokenResponse](#laelia-v1-RotateAgentTokenResponse)
    - [TransferAgentOwnershipRequest](#laelia-v1-TransferAgentOwnershipRequest)
    - [TransferAgentOwnershipResponse](#laelia-v1-TransferAgentOwnershipResponse)
    - [UpdateAgentACPConfigRequest](#laelia-v1-UpdateAgentACPConfigRequest)
    - [UpdateAgentMcpConfigRequest](#laelia-v1-UpdateAgentMcpConfigRequest)
    - [UpdateAgentRequest](#laelia-v1-UpdateAgentRequest)
    - [UploadAgentAvatarRequest](#laelia-v1-UploadAgentAvatarRequest)
    - [WorkspaceEntry](#laelia-v1-WorkspaceEntry)
    - [WorkspaceReadResponse](#laelia-v1-WorkspaceReadResponse)
  
    - [AgentStatus.ConnectionState](#laelia-v1-AgentStatus-ConnectionState)
  
    - [AgentService](#laelia-v1-AgentService)
  
- [v1/api_provider_service.proto](#v1_api_provider_service-proto)
    - [ApiProvider](#laelia-v1-ApiProvider)
    - [ApiProviderChange](#laelia-v1-ApiProviderChange)
    - [ApiProviderEntry](#laelia-v1-ApiProviderEntry)
    - [CreateApiProviderRequest](#laelia-v1-CreateApiProviderRequest)
    - [DeleteApiProviderRequest](#laelia-v1-DeleteApiProviderRequest)
    - [GetApiProviderRequest](#laelia-v1-GetApiProviderRequest)
    - [ListApiProviderModelsRequest](#laelia-v1-ListApiProviderModelsRequest)
    - [ListApiProviderModelsResponse](#laelia-v1-ListApiProviderModelsResponse)
    - [ListApiProvidersRequest](#laelia-v1-ListApiProvidersRequest)
    - [ListApiProvidersResponse](#laelia-v1-ListApiProvidersResponse)
    - [UpdateApiProviderRequest](#laelia-v1-UpdateApiProviderRequest)
  
    - [ApiProviderService](#laelia-v1-ApiProviderService)
  
- [v1/audit_log_service.proto](#v1_audit_log_service-proto)
    - [AuditLog](#laelia-v1-AuditLog)
    - [ExportAuditLogsRequest](#laelia-v1-ExportAuditLogsRequest)
    - [ExportAuditLogsResponse](#laelia-v1-ExportAuditLogsResponse)
    - [SearchAuditLogsRequest](#laelia-v1-SearchAuditLogsRequest)
    - [SearchAuditLogsResponse](#laelia-v1-SearchAuditLogsResponse)
  
    - [AuditLogService](#laelia-v1-AuditLogService)
  
- [v1/user_service.proto](#v1_user_service-proto)
    - [BatchGetUsersRequest](#laelia-v1-BatchGetUsersRequest)
    - [BatchGetUsersResponse](#laelia-v1-BatchGetUsersResponse)
    - [ChatPreferences](#laelia-v1-ChatPreferences)
    - [CreateUserRequest](#laelia-v1-CreateUserRequest)
    - [DeleteAvatarRequest](#laelia-v1-DeleteAvatarRequest)
    - [DeleteUserRequest](#laelia-v1-DeleteUserRequest)
    - [DownloadAvatarRequest](#laelia-v1-DownloadAvatarRequest)
    - [DownloadAvatarResponse](#laelia-v1-DownloadAvatarResponse)
    - [GetUserRequest](#laelia-v1-GetUserRequest)
    - [ListUsersRequest](#laelia-v1-ListUsersRequest)
    - [ListUsersResponse](#laelia-v1-ListUsersResponse)
    - [UndeleteUserRequest](#laelia-v1-UndeleteUserRequest)
    - [UpdateUserRequest](#laelia-v1-UpdateUserRequest)
    - [UploadAvatarRequest](#laelia-v1-UploadAvatarRequest)
    - [User](#laelia-v1-User)
    - [UserProfile](#laelia-v1-UserProfile)
  
    - [PreferredLanguage](#laelia-v1-PreferredLanguage)
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
    - [AccessibleChannel](#laelia-v1-AccessibleChannel)
    - [AckProcessedVersionRequest](#laelia-v1-AckProcessedVersionRequest)
    - [AckProcessedVersionResponse](#laelia-v1-AckProcessedVersionResponse)
    - [Activity](#laelia-v1-Activity)
    - [AddChannelMemberInput](#laelia-v1-AddChannelMemberInput)
    - [AddChannelMemberRequest](#laelia-v1-AddChannelMemberRequest)
    - [AddChannelMemberResponse](#laelia-v1-AddChannelMemberResponse)
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
    - [CloseTaskRequest](#laelia-v1-CloseTaskRequest)
    - [CloseTaskResponse](#laelia-v1-CloseTaskResponse)
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
    - [ContextCompactionPayload](#laelia-v1-ContextCompactionPayload)
    - [ContextUsagePayload](#laelia-v1-ContextUsagePayload)
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
    - [GetOrCreateUserUserDMRequest](#laelia-v1-GetOrCreateUserUserDMRequest)
    - [GetOrCreateUserUserDMResponse](#laelia-v1-GetOrCreateUserUserDMResponse)
    - [GetReminderRequest](#laelia-v1-GetReminderRequest)
    - [GetReminderResponse](#laelia-v1-GetReminderResponse)
    - [JoinChannelRequest](#laelia-v1-JoinChannelRequest)
    - [JoinChannelResponse](#laelia-v1-JoinChannelResponse)
    - [LeaveChannelRequest](#laelia-v1-LeaveChannelRequest)
    - [LifecyclePayload](#laelia-v1-LifecyclePayload)
    - [ListAccessibleChannelsRequest](#laelia-v1-ListAccessibleChannelsRequest)
    - [ListAccessibleChannelsResponse](#laelia-v1-ListAccessibleChannelsResponse)
    - [ListActivitiesRequest](#laelia-v1-ListActivitiesRequest)
    - [ListActivitiesResponse](#laelia-v1-ListActivitiesResponse)
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
    - [ListTaskCountsRequest](#laelia-v1-ListTaskCountsRequest)
    - [ListTaskCountsResponse](#laelia-v1-ListTaskCountsResponse)
    - [ListTasksRequest](#laelia-v1-ListTasksRequest)
    - [ListTasksResponse](#laelia-v1-ListTasksResponse)
    - [ListThreadMessagesRequest](#laelia-v1-ListThreadMessagesRequest)
    - [ListThreadMessagesResponse](#laelia-v1-ListThreadMessagesResponse)
    - [ListThreadParticipantsRequest](#laelia-v1-ListThreadParticipantsRequest)
    - [ListThreadParticipantsResponse](#laelia-v1-ListThreadParticipantsResponse)
    - [ListThreadUpdatesRequest](#laelia-v1-ListThreadUpdatesRequest)
    - [ListThreadUpdatesResponse](#laelia-v1-ListThreadUpdatesResponse)
    - [ManagerStreamMessage](#laelia-v1-ManagerStreamMessage)
    - [MarkActivityDoneRequest](#laelia-v1-MarkActivityDoneRequest)
    - [MarkActivityDoneResponse](#laelia-v1-MarkActivityDoneResponse)
    - [MarkConversationReadRequest](#laelia-v1-MarkConversationReadRequest)
    - [MarkConversationReadResponse](#laelia-v1-MarkConversationReadResponse)
    - [Mention](#laelia-v1-Mention)
    - [NewMessagesAvailable](#laelia-v1-NewMessagesAvailable)
    - [PeerAgent](#laelia-v1-PeerAgent)
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
    - [SearchChatHistoryRequest](#laelia-v1-SearchChatHistoryRequest)
    - [SearchChatHistoryResponse](#laelia-v1-SearchChatHistoryResponse)
    - [SendMessageRequest](#laelia-v1-SendMessageRequest)
    - [SetConversationPinnedRequest](#laelia-v1-SetConversationPinnedRequest)
    - [SetConversationPinnedResponse](#laelia-v1-SetConversationPinnedResponse)
    - [SteerCommandRequest](#laelia-v1-SteerCommandRequest)
    - [SteerMessage](#laelia-v1-SteerMessage)
    - [TaskInfo](#laelia-v1-TaskInfo)
    - [TextDeltaPayload](#laelia-v1-TextDeltaPayload)
    - [ThreadUpdate](#laelia-v1-ThreadUpdate)
    - [TokenUsagePayload](#laelia-v1-TokenUsagePayload)
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
    - [WorkspaceListRequest](#laelia-v1-WorkspaceListRequest)
    - [WorkspaceListResponse](#laelia-v1-WorkspaceListResponse)
    - [WorkspaceReadRequest](#laelia-v1-WorkspaceReadRequest)
  
    - [ActivityCategory](#laelia-v1-ActivityCategory)
    - [ActivityState](#laelia-v1-ActivityState)
    - [CommandEventType](#laelia-v1-CommandEventType)
    - [CommandOutput.StreamType](#laelia-v1-CommandOutput-StreamType)
    - [CommandStatus](#laelia-v1-CommandStatus)
    - [ReminderStatus](#laelia-v1-ReminderStatus)
    - [SenderType](#laelia-v1-SenderType)
    - [TaskStatus](#laelia-v1-TaskStatus)
  
    - [AgentStreamService](#laelia-v1-AgentStreamService)
    - [CommandService](#laelia-v1-CommandService)
  
- [v1/group_service.proto](#v1_group_service-proto)
    - [BatchGetGroupsRequest](#laelia-v1-BatchGetGroupsRequest)
    - [BatchGetGroupsResponse](#laelia-v1-BatchGetGroupsResponse)
    - [CreateGroupRequest](#laelia-v1-CreateGroupRequest)
    - [DeleteGroupRequest](#laelia-v1-DeleteGroupRequest)
    - [GetGroupRequest](#laelia-v1-GetGroupRequest)
    - [Group](#laelia-v1-Group)
    - [GroupMember](#laelia-v1-GroupMember)
    - [GroupReference](#laelia-v1-GroupReference)
    - [GroupReferences](#laelia-v1-GroupReferences)
    - [ListGroupsRequest](#laelia-v1-ListGroupsRequest)
    - [ListGroupsResponse](#laelia-v1-ListGroupsResponse)
    - [UpdateGroupRequest](#laelia-v1-UpdateGroupRequest)
  
    - [GroupMemberRole](#laelia-v1-GroupMemberRole)
  
    - [GroupService](#laelia-v1-GroupService)
  
- [v1/iam_service.proto](#v1_iam_service-proto)
    - [BindingDelta](#laelia-v1-BindingDelta)
    - [GetAgentIamPolicyRequest](#laelia-v1-GetAgentIamPolicyRequest)
    - [GetMachineIamPolicyRequest](#laelia-v1-GetMachineIamPolicyRequest)
    - [GetWorkspaceIamPolicyRequest](#laelia-v1-GetWorkspaceIamPolicyRequest)
    - [IamPolicyChange](#laelia-v1-IamPolicyChange)
    - [IamPolicyView](#laelia-v1-IamPolicyView)
    - [PolicyDelta](#laelia-v1-PolicyDelta)
    - [SetAgentIamPolicyRequest](#laelia-v1-SetAgentIamPolicyRequest)
    - [SetMachineIamPolicyRequest](#laelia-v1-SetMachineIamPolicyRequest)
    - [SetWorkspaceIamPolicyRequest](#laelia-v1-SetWorkspaceIamPolicyRequest)
  
    - [BindingDelta.Action](#laelia-v1-BindingDelta-Action)
  
    - [IamService](#laelia-v1-IamService)
  
- [v1/machine.proto](#v1_machine-proto)
    - [AgentAssignment](#laelia-v1-AgentAssignment)
    - [AgentConfigUpdate](#laelia-v1-AgentConfigUpdate)
    - [ConnectMachineRequest](#laelia-v1-ConnectMachineRequest)
    - [ConnectMachineResponse](#laelia-v1-ConnectMachineResponse)
    - [CreateMachineRequest](#laelia-v1-CreateMachineRequest)
    - [CreateMachineResponse](#laelia-v1-CreateMachineResponse)
    - [DeleteMachineRequest](#laelia-v1-DeleteMachineRequest)
    - [ForceDisconnectMachineRequest](#laelia-v1-ForceDisconnectMachineRequest)
    - [GetMachineRequest](#laelia-v1-GetMachineRequest)
    - [ListMachineAgentsRequest](#laelia-v1-ListMachineAgentsRequest)
    - [ListMachineAgentsResponse](#laelia-v1-ListMachineAgentsResponse)
    - [ListMachineWorkspacesRequest](#laelia-v1-ListMachineWorkspacesRequest)
    - [ListMachineWorkspacesResponse](#laelia-v1-ListMachineWorkspacesResponse)
    - [ListMachinesRequest](#laelia-v1-ListMachinesRequest)
    - [ListMachinesResponse](#laelia-v1-ListMachinesResponse)
    - [Machine](#laelia-v1-Machine)
    - [Machine.LabelsEntry](#laelia-v1-Machine-LabelsEntry)
    - [MachineDisconnectNotice](#laelia-v1-MachineDisconnectNotice)
    - [MachineDisconnectRequest](#laelia-v1-MachineDisconnectRequest)
    - [MachineHeartbeatRequest](#laelia-v1-MachineHeartbeatRequest)
    - [MachineHeartbeatResponse](#laelia-v1-MachineHeartbeatResponse)
    - [MachineInfo](#laelia-v1-MachineInfo)
    - [MachineInfo.LabelsEntry](#laelia-v1-MachineInfo-LabelsEntry)
    - [MachineReady](#laelia-v1-MachineReady)
    - [MachineStatus](#laelia-v1-MachineStatus)
    - [MachineStreamMessage](#laelia-v1-MachineStreamMessage)
    - [MachineSummary](#laelia-v1-MachineSummary)
    - [MachineWorkspaceScanRequest](#laelia-v1-MachineWorkspaceScanRequest)
    - [MachineWorkspaceScanResponse](#laelia-v1-MachineWorkspaceScanResponse)
    - [MachineWorkspaceSummary](#laelia-v1-MachineWorkspaceSummary)
    - [ManagerMachineStreamMessage](#laelia-v1-ManagerMachineStreamMessage)
    - [RefreshMachineProvidersRequest](#laelia-v1-RefreshMachineProvidersRequest)
    - [RefreshMachineProvidersResponse](#laelia-v1-RefreshMachineProvidersResponse)
    - [RefreshMachineTokenRequest](#laelia-v1-RefreshMachineTokenRequest)
    - [RefreshMachineTokenResponse](#laelia-v1-RefreshMachineTokenResponse)
    - [ReloadAgentAssignment](#laelia-v1-ReloadAgentAssignment)
    - [RemoveAgent](#laelia-v1-RemoveAgent)
    - [RevokeMachineTokenRequest](#laelia-v1-RevokeMachineTokenRequest)
    - [RevokeMachineTokenResponse](#laelia-v1-RevokeMachineTokenResponse)
    - [RotateMachineTokenRequest](#laelia-v1-RotateMachineTokenRequest)
    - [RotateMachineTokenResponse](#laelia-v1-RotateMachineTokenResponse)
  
    - [MachineStatus.ConnectionState](#laelia-v1-MachineStatus-ConnectionState)
  
    - [MachineService](#laelia-v1-MachineService)
    - [MachineStreamService](#laelia-v1-MachineStreamService)
  
- [v1/mcp.proto](#v1_mcp-proto)
    - [CallMcpToolRequest](#laelia-v1-CallMcpToolRequest)
    - [CallMcpToolResponse](#laelia-v1-CallMcpToolResponse)
    - [CreateMcpServerRequest](#laelia-v1-CreateMcpServerRequest)
    - [DeleteMcpServerRequest](#laelia-v1-DeleteMcpServerRequest)
    - [GetMcpCatalogRequest](#laelia-v1-GetMcpCatalogRequest)
    - [GetMcpCatalogResponse](#laelia-v1-GetMcpCatalogResponse)
    - [GetMcpServerRequest](#laelia-v1-GetMcpServerRequest)
    - [ListMcpServersRequest](#laelia-v1-ListMcpServersRequest)
    - [ListMcpServersResponse](#laelia-v1-ListMcpServersResponse)
    - [McpContentBlock](#laelia-v1-McpContentBlock)
    - [McpHeader](#laelia-v1-McpHeader)
    - [McpHttpTransport](#laelia-v1-McpHttpTransport)
    - [McpImageContent](#laelia-v1-McpImageContent)
    - [McpServer](#laelia-v1-McpServer)
    - [McpServerChange](#laelia-v1-McpServerChange)
    - [McpSseTransport](#laelia-v1-McpSseTransport)
    - [McpTextContent](#laelia-v1-McpTextContent)
    - [McpTool](#laelia-v1-McpTool)
    - [UpdateMcpServerRequest](#laelia-v1-UpdateMcpServerRequest)
  
    - [McpServerScope](#laelia-v1-McpServerScope)
  
    - [McpGatewayService](#laelia-v1-McpGatewayService)
    - [McpServerService](#laelia-v1-McpServerService)
  
- [v1/notification.proto](#v1_notification-proto)
    - [CreatePushSubscriptionRequest](#laelia-v1-CreatePushSubscriptionRequest)
    - [DeletePushSubscriptionRequest](#laelia-v1-DeletePushSubscriptionRequest)
    - [GetPushConfigRequest](#laelia-v1-GetPushConfigRequest)
    - [GetPushConfigResponse](#laelia-v1-GetPushConfigResponse)
    - [ListPushSubscriptionsRequest](#laelia-v1-ListPushSubscriptionsRequest)
    - [ListPushSubscriptionsResponse](#laelia-v1-ListPushSubscriptionsResponse)
    - [PushSubscription](#laelia-v1-PushSubscription)
    - [UpdatePushConfigRequest](#laelia-v1-UpdatePushConfigRequest)
    - [UpdatePushConfigResponse](#laelia-v1-UpdatePushConfigResponse)
  
    - [NotificationService](#laelia-v1-NotificationService)
  
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
    - [GetLlmAgentConfigRequest](#laelia-v1-GetLlmAgentConfigRequest)
    - [GetLlmAgentConfigResponse](#laelia-v1-GetLlmAgentConfigResponse)
    - [GetS3ConfigRequest](#laelia-v1-GetS3ConfigRequest)
    - [GetS3ConfigResponse](#laelia-v1-GetS3ConfigResponse)
    - [GetSetupStatusRequest](#laelia-v1-GetSetupStatusRequest)
    - [GetSetupStatusResponse](#laelia-v1-GetSetupStatusResponse)
    - [GetUserMcpConfigRequest](#laelia-v1-GetUserMcpConfigRequest)
    - [GetUserMcpConfigResponse](#laelia-v1-GetUserMcpConfigResponse)
    - [SetupItem](#laelia-v1-SetupItem)
    - [UpdateDebugConfigRequest](#laelia-v1-UpdateDebugConfigRequest)
    - [UpdateDebugConfigResponse](#laelia-v1-UpdateDebugConfigResponse)
    - [UpdateLlmAgentConfigRequest](#laelia-v1-UpdateLlmAgentConfigRequest)
    - [UpdateLlmAgentConfigResponse](#laelia-v1-UpdateLlmAgentConfigResponse)
    - [UpdateS3ConfigRequest](#laelia-v1-UpdateS3ConfigRequest)
    - [UpdateS3ConfigResponse](#laelia-v1-UpdateS3ConfigResponse)
    - [UpdateUserMcpConfigRequest](#laelia-v1-UpdateUserMcpConfigRequest)
    - [UpdateUserMcpConfigResponse](#laelia-v1-UpdateUserMcpConfigResponse)
  
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



<a name="laelia-v1-PermissionDeniedDetail"></a>

### PermissionDeniedDetail
PermissionDeniedDetail describes why an IAM-gated RPC was denied: the
required permission and, when the request carries a recognizable resource,
the resource that failed the check. It is attached to PermissionDenied
errors by the IAM interceptor so clients can render what access is missing.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| method | [string](#string) |  | The RPC method that was denied, e.g. &#34;/laelia.v1.IamService/SetWorkspaceIamPolicy&#34;. |
| required_permissions | [string](#string) | repeated | The permission the caller lacked, e.g. &#34;laelia.iam.setPolicy&#34;. |
| resources | [string](#string) | repeated | The resources the permission was checked against (resource names such as &#34;agents/{agent}&#34;). Empty for workspace-scoped checks. |






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
| created_by | [string](#string) |  | Creator&#39;s user resource name (users/{id}); empty for legacy agents with no recorded creator. Display-only: it never authorizes anything (the agent&#39;s owner does). Immutable after creation. |
| can_edit | [bool](#bool) |  | can_edit reports whether the current caller may modify this agent: true for the agent&#39;s owner and for workspace admins, false otherwise. Populated per caller by GetAgent/ListAgents; not set on agent-daemon paths. |
| avatar | [string](#string) |  | avatar is the resource name of the agent&#39;s uploaded avatar image, or empty when the agent has not uploaded one (frontend renders a deterministic pixel identicon seeded by the agent id). Format: agents/{agent}/avatar. |
| machine | [string](#string) |  | machine is the resource name of the machine this agent is bound to (machines/{machine}). Required on CreateAgent (the parent machine the agent runs on) and immutable thereafter; an agent runs on exactly one machine, and the machine app picks it up via the MachineChannel control stream. |
| allow_add_to_channel | [bool](#bool) |  | allow_add_to_channel controls whether other users may add this agent to a channel. Default false: only the agent&#39;s owner or a workspace admin may add it. When true, the normal channel-side rule (conversations.manage = channel owner/admin) applies. |
| owner | [string](#string) |  | Owner&#39;s user resource name (users/{id}); empty for legacy agents with no recorded owner. The owner is the human responsible for the agent: only the owner or a workspace admin may modify it, the owner may transfer ownership to another user (TransferAgentOwnership), and non-owners&#39; high-risk requests to the agent require the owner&#39;s approval (see the agent prompt&#39;s Ownership &amp; Safety section). Defaults to the creator on CreateAgent and is backfilled from created_by for existing agents. |
| owner_name | [string](#string) |  | Owner&#39;s display name — the name the agent writes `dm:@&lt;owner_name&gt;` to when requesting approval for a high-risk operation. Empty for legacy agents. |
| follow_owner_permissions | [bool](#bool) |  | follow_owner_permissions grants this agent read access to every channel (and DM) its owner can read, without requiring the agent to be added as a member. The agent can read and proactively join such channels; posting still requires explicit membership. Default true: the agent acts within its owner&#39;s channel visibility. |
| mcp_servers | [string](#string) | repeated | mcp_servers is the set of MCP server resource names (mcpServers/{id}) enabled on this agent. The manager resolves them into a tool catalog when the agent starts; the machine never receives transport configuration. |
| can_manage_channel_members | [bool](#bool) |  | can_manage_channel_members grants this agent the ability to add/remove members in a channel where its owner is a channel Admin or Owner. This is separate from follow_owner_permissions (which controls read visibility): the agent acts on its owner&#39;s behalf for member management only — it never inherits the owner&#39;s other manage powers (rename, delete, transfer, roles). Default true. |






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
| provider | [string](#string) |  | selected LLM agent provider id, e.g. &#34;opencode&#34;, &#34;claude-code&#34;, &#34;custom&#34;, &#34;builtin-pi&#34; |
| model | [string](#string) |  | selected model valueId, matching an option advertised by the provider in NewSession ConfigOptions |
| custom_env | [AgentACPConfig.CustomEnvEntry](#laelia-v1-AgentACPConfig-CustomEnvEntry) | repeated | user-defined key-value env vars overlaid (and overriding) the inherited allow_env set |
| persona_prompt | [string](#string) |  | admin-authored self-awareness prompt: personality, chat style, focus area. Empty = not loaded. |
| api_provider | [string](#string) |  | api_provider is the LLM API provider for the built-in pi runtime (&#34;deepseek&#34; or &#34;openrouter&#34; in phase 1). Only meaningful when provider == &#34;builtin-pi&#34;; ignored by ACP runtimes. |
| api_key | [string](#string) |  | api_key is the plaintext LLM API key for the api_provider. Only meaningful when provider == &#34;builtin-pi&#34;; ignored by ACP runtimes. Stored in the agent info JSONB with the same plaintext-at-rest posture as custom_env. When global_provider is set, api_key is ignored: the key is resolved server-side from the global provider&#39;s entry and never stored in (nor returned with) the agent. |
| global_provider | [string](#string) |  | global_provider is the resource name of the global API provider this builtin-pi agent uses (&#34;apiProviders/{id}&#34;). Only meaningful when provider == &#34;builtin-pi&#34;. When set, the stored config carries the provider/entry references instead of an inline api_provider/api_key; the server resolves the concrete api_provider/api_key/model at the daemon boundary. |
| global_provider_entry | [string](#string) |  | global_provider_entry is the resource name of the (key, model) entry within the global provider, in the form &#34;apiProviders/{id}/entries/{entry}&#34;. Only meaningful when provider == &#34;builtin-pi&#34; and global_provider is set. |
| protocol | [string](#string) |  | protocol declares the ACP protocol generation the provider speaks: empty (inferred from the provider type), &#34;acp-v1&#34; (session protocol) or &#34;acp-v2&#34; (thread protocol). Only meaningful for a &#34;custom&#34; provider: a built-in provider&#39;s protocol is determined by its implementation. |






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
| supports_pi | [bool](#bool) |  | supports_pi is true for agents backed by the built-in non-ACP pi runtime. The dispatcher&#39;s BeginSession gate accepts either supports_acp or supports_pi so a pi agent can run drain sessions without an ACP executor. |






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
per row would N&#43;1 the IAM policy lookup for non-admin callers. can_delete is
the cheap subset the list view gates its delete affordance on: the agent&#39;s
owner or a workspace-scope holder of laelia.agents.edit (per-agent policy
bindings are not consulted, so a custom role bound on the agent may still
delete server-side while the list hides the button).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| state | [State](#laelia-v1-State) |  |  |
| title | [string](#string) |  |  |
| status | [AgentStatus](#laelia-v1-AgentStatus) |  |  |
| provider | [string](#string) |  | provider/executable mirror acp_config.provider/executable on the full Agent, surfaced top-level so list consumers don&#39;t pull in AgentInfo. |
| executable | [string](#string) |  |  |
| machine | [string](#string) |  | machine is the resource name of the machine this agent is bound to (machines/{machine}). |
| created_by | [string](#string) |  | created_by is the creator&#39;s user resource name (users/{id}); empty for legacy agents with no recorded creator. Display-only — grouping and authorization use owner. Surfaced on the summary so list consumers can show the creator without an N&#43;1 of GetAgent. |
| allow_add_to_channel | [bool](#bool) |  | allow_add_to_channel mirrors Agent.allow_add_to_channel so list consumers (e.g. the channel member picker) can hide agents the current caller may not add. |
| owner | [string](#string) |  | owner is the owner&#39;s user resource name (users/{id}); empty for legacy agents with no recorded owner. Surfaced on the summary so list consumers (e.g. the Members page&#39;s per-user &#34;Owned Agents&#34; view and the channel member picker) can group agents by owner without an N&#43;1 of GetAgent. |
| follow_owner_permissions | [bool](#bool) |  | follow_owner_permissions mirrors Agent.follow_owner_permissions so list consumers can show whether the agent follows its owner&#39;s channel access. |
| can_manage_channel_members | [bool](#bool) |  | can_manage_channel_members mirrors Agent.can_manage_channel_members so list consumers can show whether the agent may manage members on its owner&#39;s behalf. |
| can_delete | [bool](#bool) |  | can_delete reports whether the current caller may delete this agent: its owner or a workspace-scope holder of laelia.agents.edit. |






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






<a name="laelia-v1-DeleteAgentAvatarRequest"></a>

### DeleteAgentAvatarRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-DeleteAgentRequest"></a>

### DeleteAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-DownloadAgentAvatarRequest"></a>

### DownloadAgentAvatarRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-DownloadAgentAvatarResponse"></a>

### DownloadAgentAvatarResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  |  |
| mime_type | [string](#string) |  |  |
| etag | [string](#string) |  |  |






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






<a name="laelia-v1-ListAgentWorkspaceRequest"></a>

### ListAgentWorkspaceRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| dir_path | [string](#string) |  | relative to the workspace root; empty = root |
| include_hidden | [bool](#bool) |  |  |






<a name="laelia-v1-ListAgentWorkspaceResponse"></a>

### ListAgentWorkspaceResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| entries | [WorkspaceEntry](#laelia-v1-WorkspaceEntry) | repeated |  |






<a name="laelia-v1-ListAgentsRequest"></a>

### ListAgentsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| show_deleted | [bool](#bool) |  |  |
| parent | [string](#string) |  | parent, when set to a machine resource name (machines/{machine}), filters the list to agents bound to that machine. Empty lists all agents. |






<a name="laelia-v1-ListAgentsResponse"></a>

### ListAgentsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agents | [AgentSummary](#laelia-v1-AgentSummary) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListPiModelsRequest"></a>

### ListPiModelsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_provider | [string](#string) |  | LLM API provider id (&#34;deepseek&#34; | &#34;openrouter&#34;). See AgentACPConfig.api_provider. |
| api_key | [string](#string) |  | Plaintext LLM API key. Required for deepseek; ignored (public endpoint) for openrouter. |






<a name="laelia-v1-ListPiModelsResponse"></a>

### ListPiModelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| models | [PiModel](#laelia-v1-PiModel) | repeated |  |






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






<a name="laelia-v1-PiModel"></a>

### PiModel
PiModel is one model id returned by the LLM API provider&#39;s model-listing API.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  | model id, the value passed to pi --model |
| name | [string](#string) |  | optional display name (deepseek echoes id; openrouter has a human-readable name) |






<a name="laelia-v1-ReadAgentWorkspaceFileRequest"></a>

### ReadAgentWorkspaceFileRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| path | [string](#string) |  | relative to the workspace root |






<a name="laelia-v1-ReadAgentWorkspaceFileResponse"></a>

### ReadAgentWorkspaceFileResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| file | [WorkspaceReadResponse](#laelia-v1-WorkspaceReadResponse) |  |  |






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






<a name="laelia-v1-TransferAgentOwnershipRequest"></a>

### TransferAgentOwnershipRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| new_owner | [string](#string) |  | New owner&#39;s user resource name (users/{id}). |
| reason | [string](#string) |  | audit purpose |






<a name="laelia-v1-TransferAgentOwnershipResponse"></a>

### TransferAgentOwnershipResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [Agent](#laelia-v1-Agent) |  |  |






<a name="laelia-v1-UpdateAgentACPConfigRequest"></a>

### UpdateAgentACPConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| acp_config | [AgentACPConfig](#laelia-v1-AgentACPConfig) |  |  |






<a name="laelia-v1-UpdateAgentMcpConfigRequest"></a>

### UpdateAgentMcpConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| mcp_servers | [string](#string) | repeated | mcp_servers is the full replacement set of enabled MCP server resource names (mcpServers/{id}). |






<a name="laelia-v1-UpdateAgentRequest"></a>

### UpdateAgentRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent | [Agent](#laelia-v1-Agent) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |






<a name="laelia-v1-UploadAgentAvatarRequest"></a>

### UploadAgentAvatarRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| data | [bytes](#bytes) |  |  |
| mime_type | [string](#string) |  |  |






<a name="laelia-v1-WorkspaceEntry"></a>

### WorkspaceEntry
WorkspaceEntry is one file/directory node of a lazily loaded workspace tree.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| path | [string](#string) |  | relative to the workspace root |
| is_directory | [bool](#bool) |  |  |
| size | [int64](#int64) |  | file bytes; 0 for directories |
| modified_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| is_hidden | [bool](#bool) |  | dotfile |






<a name="laelia-v1-WorkspaceReadResponse"></a>

### WorkspaceReadResponse
WorkspaceReadResponse carries a previewed file (or an error) from the machine
app back to the manager. A non-empty error means the file cannot be
previewed; the unary RPC still succeeds so the frontend can show the reason.
Shared by the per-agent workspace stream (command.proto) and the unary
ReadAgentWorkspaceFile RPC.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  |  |
| content | [string](#string) |  | text: utf-8; image: base64; otherwise empty |
| binary | [bool](#bool) |  | true for images and other binary files |
| size | [int64](#int64) |  |  |
| mime_type | [string](#string) |  | set for images |
| encoding | [string](#string) |  | &#34;utf-8&#34; / &#34;base64&#34; / &#34;&#34; |
| error | [string](#string) |  | preview-disabled reason (sensitive file, too large, missing) |





 


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
| CreateAgent | [CreateAgentRequest](#laelia-v1-CreateAgentRequest) | [CreateAgentResponse](#laelia-v1-CreateAgentResponse) | CreateAgent is handler-gated (no permission annotation): the machine&#39;s creator, a workspace admin, or a principal bound to roles/machineAgentCreator on the machine&#39;s IAM policy may create agents on it. The machine-scoped check is enforced by the handler via laelia.machines.createAgent against the machine&#39;s IAM policy. |
| ListAgents | [ListAgentsRequest](#laelia-v1-ListAgentsRequest) | [ListAgentsResponse](#laelia-v1-ListAgentsResponse) |  |
| GetAgent | [GetAgentRequest](#laelia-v1-GetAgentRequest) | [Agent](#laelia-v1-Agent) |  |
| UpdateAgent | [UpdateAgentRequest](#laelia-v1-UpdateAgentRequest) | [Agent](#laelia-v1-Agent) | UpdateAgent patches a single mutable agent field. Only allow_add_to_channel is supported initially (any other update_mask path is rejected). Authorized in the handler for the agent&#39;s owner or a workspace admin; the IAM interceptor&#39;s agents.edit is admin-only, so this RPC carries no permission annotation and is handler-gated. |
| TransferAgentOwnership | [TransferAgentOwnershipRequest](#laelia-v1-TransferAgentOwnershipRequest) | [TransferAgentOwnershipResponse](#laelia-v1-TransferAgentOwnershipResponse) | TransferAgentOwnership reassigns the agent&#39;s owner to another user. The new owner takes effect immediately and unilaterally (no acceptance required); the previous owner loses owner authority at once. Authorized in the handler for the agent&#39;s current owner or a workspace admin; like UpdateAgent this RPC carries no permission annotation (agents.edit is admin-only) and is handler-gated via canEditAgent. |
| DeleteAgent | [DeleteAgentRequest](#laelia-v1-DeleteAgentRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | DeleteAgent soft-deletes an agent. Authorized in the handler for the agent&#39;s owner or a holder of laelia.agents.edit on the agent; no permission annotation so the owner short-circuit can run. |
| RotateAgentToken | [RotateAgentTokenRequest](#laelia-v1-RotateAgentTokenRequest) | [RotateAgentTokenResponse](#laelia-v1-RotateAgentTokenResponse) | Token rotation: generate a new bootstrap token, old token invalid after grace period. Authorized in the handler for the agent&#39;s owner or a holder of laelia.agents.edit on the agent; no permission annotation so the owner short-circuit can run. |
| RevokeAgentToken | [RevokeAgentTokenRequest](#laelia-v1-RevokeAgentTokenRequest) | [RevokeAgentTokenResponse](#laelia-v1-RevokeAgentTokenResponse) | Token revocation: revoke all tokens for the agent. Authorized in the handler for the agent&#39;s owner or a holder of laelia.agents.edit on the agent; no permission annotation so the owner short-circuit can run. |
| ForceDisconnectAgent | [ForceDisconnectAgentRequest](#laelia-v1-ForceDisconnectAgentRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Force-disconnects an agent connection. Authorized in the handler for the agent&#39;s owner or a holder of laelia.agents.edit on the agent; no permission annotation so the owner short-circuit can run. |
| ListAgentSessions | [ListAgentSessionsRequest](#laelia-v1-ListAgentSessionsRequest) | [ListAgentSessionsResponse](#laelia-v1-ListAgentSessionsResponse) | List agent sessions |
| UpdateAgentACPConfig | [UpdateAgentACPConfigRequest](#laelia-v1-UpdateAgentACPConfigRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Update the agent&#39;s ACP config. Handler-gated (no permission annotation): the agent&#39;s owner or a workspace admin may update it. Setting a legacy inline api_provider/api_key additionally requires laelia.agents.edit (only workspace admin today); owners without it must use a global provider. |
| UpdateAgentMcpConfig | [UpdateAgentMcpConfigRequest](#laelia-v1-UpdateAgentMcpConfigRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | UpdateAgentMcpConfig replaces the MCP servers enabled on an agent. Only servers the caller may use (members of the server&#39;s user/group list, or workspace admin) are accepted. Handler-gated like UpdateAgentACPConfig: the agent&#39;s owner or a workspace admin may update it. |
| RefreshAgentProviders | [RefreshAgentProvidersRequest](#laelia-v1-RefreshAgentProvidersRequest) | [RefreshAgentProvidersResponse](#laelia-v1-RefreshAgentProvidersResponse) | Ask the agent daemon to re-probe its host for installed LLM agent providers and their models. Returns the freshly discovered provider list (also persisted into agent.info.available_providers). Authorized in the handler for the agent&#39;s owner or a holder of laelia.agents.edit on the agent; no permission annotation so the owner short-circuit can run. |
| ListAgentWorkspace | [ListAgentWorkspaceRequest](#laelia-v1-ListAgentWorkspaceRequest) | [ListAgentWorkspaceResponse](#laelia-v1-ListAgentWorkspaceResponse) | ListAgentWorkspace lists one directory level of an agent&#39;s workspace on its machine (~/.laelia/&lt;machineID&gt;/&lt;agentID&gt;/), lazily loading the tree level by level. Workspace content is sensitive: authorized in the handler for the agent&#39;s owner or a workspace admin (canEditAgent); like UpdateAgent this RPC carries no permission annotation (agents.edit is admin-only) and is handler-gated. |
| ReadAgentWorkspaceFile | [ReadAgentWorkspaceFileRequest](#laelia-v1-ReadAgentWorkspaceFileRequest) | [ReadAgentWorkspaceFileResponse](#laelia-v1-ReadAgentWorkspaceFileResponse) | ReadAgentWorkspaceFile reads a single workspace file for text/image preview. Same handler-gated authorization as ListAgentWorkspace (owner or workspace admin). Sensitive files (secret/credential/token patterns) are rejected by the machine app and surface as a per-file error, not a transport error. |
| ListPiModels | [ListPiModelsRequest](#laelia-v1-ListPiModelsRequest) | [ListPiModelsResponse](#laelia-v1-ListPiModelsResponse) | List the models a built-in pi agent&#39;s LLM API provider exposes. The manager proxies the provider&#39;s model-listing HTTP API (DeepSeek `GET /models` with the caller&#39;s api_key; OpenRouter `GET /models`, public) so the model list is fetched dynamically rather than hardcoded. Not agent-scoped: the add-agent form calls it before the agent exists. Admin (agents.edit) only. |
| ConnectAgent | [ConnectAgentRequest](#laelia-v1-ConnectAgentRequest) | [ConnectAgentResponse](#laelia-v1-ConnectAgentResponse) | Agent initial connection using bootstrap token |
| AgentHeartbeat | [AgentHeartbeatRequest](#laelia-v1-AgentHeartbeatRequest) | [AgentHeartbeatResponse](#laelia-v1-AgentHeartbeatResponse) | Agent heartbeat |
| AgentDisconnect | [AgentDisconnectRequest](#laelia-v1-AgentDisconnectRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Agent graceful disconnect |
| RefreshAgentToken | [RefreshAgentTokenRequest](#laelia-v1-RefreshAgentTokenRequest) | [RefreshAgentTokenResponse](#laelia-v1-RefreshAgentTokenResponse) | Agent refreshes access token |
| UploadAgentAvatar | [UploadAgentAvatarRequest](#laelia-v1-UploadAgentAvatarRequest) | [Agent](#laelia-v1-Agent) | UploadAgentAvatar replaces an agent&#39;s avatar image. Requires laelia.agents.edit on the agent; the caller must be the agent&#39;s creator or a workspace admin. Bytes travel over Connect-JSON. |
| DownloadAgentAvatar | [DownloadAgentAvatarRequest](#laelia-v1-DownloadAgentAvatarRequest) | [DownloadAgentAvatarResponse](#laelia-v1-DownloadAgentAvatarResponse) | DownloadAgentAvatar fetches an agent&#39;s avatar image bytes. Any authenticated user may download any agent&#39;s avatar (workspace-internal profile image). |
| DeleteAgentAvatar | [DeleteAgentAvatarRequest](#laelia-v1-DeleteAgentAvatarRequest) | [Agent](#laelia-v1-Agent) | DeleteAgentAvatar clears an agent&#39;s avatar, reverting to the pixel default. Requires laelia.agents.edit on the agent. |
| Hello | [HelloRequest](#laelia-v1-HelloRequest) | [HelloResponse](#laelia-v1-HelloResponse) | Health check (no auth required) |

 



<a name="v1_api_provider_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/api_provider_service.proto



<a name="laelia-v1-ApiProvider"></a>

### ApiProvider
ApiProvider is a named global LLM API provider (e.g. &#34;DeepSeek&#34;). A provider
bundles a set of (api_key, model) entries plus the users/groups allowed to
use them. The API never returns the api keys: entries expose has_api_key and
a masked form (&#34;****&#34;&#43;last4) so the key stays out of the renderer.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the provider, in the form `apiProviders/{id}`. |
| provider_type | [string](#string) |  | The LLM API provider type, e.g. &#34;deepseek&#34; or &#34;openrouter&#34; (the phase-1 pi runtime support set). |
| title | [string](#string) |  | Human-readable title. |
| base_url | [string](#string) |  | API base URL. Empty means the provider type&#39;s default. |
| description | [string](#string) |  | Longer description of the provider. |
| entries | [ApiProviderEntry](#laelia-v1-ApiProviderEntry) | repeated | The (api_key, model) entries of this provider. |
| members | [string](#string) | repeated | Users or groups allowed to use this provider, in IAM member format: `users/{uid}`, `groups/{email}`, `groups/{id}`, or `allUsers`. Access is checked when an agent references the provider (write-time snapshot). |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| created_by | [string](#string) |  | Creator&#39;s user resource name (users/{id}). Display-only. |






<a name="laelia-v1-ApiProviderChange"></a>

### ApiProviderChange
ApiProviderChange is the audit payload recorded for a successful
CreateApiProvider/UpdateApiProvider. It carries only the provider resource
name and the entry names added/removed — never the api keys.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provider | [string](#string) |  |  |
| entries_added | [string](#string) | repeated |  |
| entries_removed | [string](#string) | repeated |  |






<a name="laelia-v1-ApiProviderEntry"></a>

### ApiProviderEntry
ApiProviderEntry is one (api_key, model) entry of an ApiProvider. The api key
is stored server-side and never returned; has_api_key/masked_api_key are the
only read surface.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The entry&#39;s resource name, in the form `apiProviders/{provider}/entries/{entry}`. |
| label | [string](#string) |  | Optional display label to distinguish entries sharing a model. |
| model | [string](#string) |  | The model name this entry exposes (passed to the pi runtime as --model). |
| has_api_key | [bool](#bool) |  | Whether an api key is stored. Output only. |
| masked_api_key | [string](#string) |  | Masked form of the stored key (&#34;****&#34;&#43;last4), for display only. Output only. |
| api_key | [string](#string) |  | Input only. On write: empty or a &#34;****&#34;-prefixed value means &#34;keep the existing key&#34;; any other value replaces it. Never echoed on read. |






<a name="laelia-v1-CreateApiProviderRequest"></a>

### CreateApiProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_provider | [ApiProvider](#laelia-v1-ApiProvider) |  |  |






<a name="laelia-v1-DeleteApiProviderRequest"></a>

### DeleteApiProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-GetApiProviderRequest"></a>

### GetApiProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-ListApiProviderModelsRequest"></a>

### ListApiProviderModelsRequest
ListApiProviderModelsRequest fetches the model list a provider type exposes.
The api_key is a bearer credential: it is never logged and never echoed in
errors.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| provider_type | [string](#string) |  |  |
| api_key | [string](#string) |  |  |
| base_url | [string](#string) |  | Optional base URL override; when empty the provider type&#39;s default is used. |






<a name="laelia-v1-ListApiProviderModelsResponse"></a>

### ListApiProviderModelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| models | [PiModel](#laelia-v1-PiModel) | repeated |  |






<a name="laelia-v1-ListApiProvidersRequest"></a>

### ListApiProvidersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListApiProvidersResponse"></a>

### ListApiProvidersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_providers | [ApiProvider](#laelia-v1-ApiProvider) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-UpdateApiProviderRequest"></a>

### UpdateApiProviderRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| api_provider | [ApiProvider](#laelia-v1-ApiProvider) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 

 

 


<a name="laelia-v1-ApiProviderService"></a>

### ApiProviderService
ApiProviderService manages global LLM API providers. Management RPCs
(create/update/delete) are gated by the IAM interceptor with the
laelia.apiProviders.* permissions (held by workspaceAdmin or an authorized
custom role). ListApiProviders is handler-gated instead: it returns only the
providers the caller may use, so the agent create/edit form can list them
without a management permission.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetAPIProvider | [GetApiProviderRequest](#laelia-v1-GetApiProviderRequest) | [ApiProvider](#laelia-v1-ApiProvider) |  |
| ListAPIProviders | [ListApiProvidersRequest](#laelia-v1-ListApiProvidersRequest) | [ListApiProvidersResponse](#laelia-v1-ListApiProvidersResponse) |  |
| CreateAPIProvider | [CreateApiProviderRequest](#laelia-v1-CreateApiProviderRequest) | [ApiProvider](#laelia-v1-ApiProvider) |  |
| UpdateAPIProvider | [UpdateApiProviderRequest](#laelia-v1-UpdateApiProviderRequest) | [ApiProvider](#laelia-v1-ApiProvider) |  |
| DeleteAPIProvider | [DeleteApiProviderRequest](#laelia-v1-DeleteApiProviderRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| ListAPIProviderModels | [ListApiProviderModelsRequest](#laelia-v1-ListApiProviderModelsRequest) | [ListApiProviderModelsResponse](#laelia-v1-ListApiProviderModelsResponse) | List the models a provider type exposes. The manager proxies the provider&#39;s model-listing HTTP API (DeepSeek GET /models with the caller&#39;s api_key; OpenRouter GET /models, public). Admin (laelia.apiProviders.update) only. |

 



<a name="v1_audit_log_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/audit_log_service.proto



<a name="laelia-v1-AuditLog"></a>

### AuditLog
AuditLog is one audited API call: who did what, on which resource, with the
structured change payload (e.g. IAM binding deltas) when the method records
one. Audit logs are written by the audit interceptor for methods annotated
with audit=true.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The audit log resource name, in the form &#34;auditLogs/{id}&#34;. |
| method | [string](#string) |  | The RPC method, e.g. &#34;/laelia.v1.IamService/SetWorkspaceIamPolicy&#34;. |
| actor_type | [string](#string) |  | The caller type: &#34;user&#34;, &#34;agent&#34;, or &#34;unknown&#34;. |
| actor_id | [string](#string) |  | The caller identifier: user email or agent resource id. |
| source_ip | [string](#string) |  | The caller&#39;s source IP. |
| status | [string](#string) |  | The outcome status: &#34;ok&#34; or a connect code string. |
| error | [string](#string) |  | The error message when the call failed. |
| resource | [string](#string) |  | The target resource of the call, e.g. &#34;agents/{rid}&#34; or &#34;workspaces/-&#34;. |
| payload | [string](#string) |  | The structured change payload as JSON (e.g. IAM binding deltas), empty when the method records none. |
| create_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | When the call happened. |






<a name="laelia-v1-ExportAuditLogsRequest"></a>

### ExportAuditLogsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| filter | [string](#string) |  | Same filter syntax as SearchAuditLogsRequest.filter. |
| order_by | [string](#string) |  | Order by create_time; only &#34;create_time desc&#34; (default) and &#34;create_time asc&#34; are supported. |
| limit | [int32](#int32) |  | The maximum number of rows to export. Defaults to 10000, capped at 100000. |






<a name="laelia-v1-ExportAuditLogsResponse"></a>

### ExportAuditLogsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [string](#string) |  | The audit logs as CSV: name, method, actor_type, actor_id, source_ip, status, error, resource, payload, create_time. |






<a name="laelia-v1-SearchAuditLogsRequest"></a>

### SearchAuditLogsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  | The maximum number of logs to return. The service may return fewer than this value. If unspecified, at most 100 logs will be returned. |
| page_token | [string](#string) |  | A page token, received from a previous SearchAuditLogs call. |
| filter | [string](#string) |  | Filter is used to filter audit logs. Supported fields (equality): - method: the RPC method, e.g. &#34;/laelia.v1.IamService/SetWorkspaceIamPolicy&#34;. - actor: the caller identifier (email or agent resource id). - resource: the target resource name. - status: the outcome status (&#34;ok&#34; or a connect code string). Example: method = &#34;/laelia.v1.IamService/SetWorkspaceIamPolicy&#34; |
| order_by | [string](#string) |  | Order by create_time; only &#34;create_time desc&#34; (default) and &#34;create_time asc&#34; are supported. |






<a name="laelia-v1-SearchAuditLogsResponse"></a>

### SearchAuditLogsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| audit_logs | [AuditLog](#laelia-v1-AuditLog) | repeated |  |
| next_page_token | [string](#string) |  |  |





 

 

 


<a name="laelia-v1-AuditLogService"></a>

### AuditLogService
AuditLogService exposes the structured audit trail. Search and export are
admin-tier (laelia.auditLogs.search / laelia.auditLogs.export).

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| SearchAuditLogs | [SearchAuditLogsRequest](#laelia-v1-SearchAuditLogsRequest) | [SearchAuditLogsResponse](#laelia-v1-SearchAuditLogsResponse) | Search audit logs with filtering and pagination. |
| ExportAuditLogs | [ExportAuditLogsRequest](#laelia-v1-ExportAuditLogsRequest) | [ExportAuditLogsResponse](#laelia-v1-ExportAuditLogsResponse) | Export audit logs as CSV. |

 



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






<a name="laelia-v1-ChatPreferences"></a>

### ChatPreferences
ChatPreferences holds per-user chat preferences. Only the user themselves
sees the effect; stored per principal so it follows the account across
devices/browsers. Surfaced to agents (via ChannelMember.preferred_language)
so an agent can converse with the user in their preferred language.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enter_to_send | [bool](#bool) |  | enter_to_send is true when pressing Enter sends the message and Shift&#43;Enter inserts a newline (the historic default). When false the keybinding is inverted: Enter inserts a newline and Shift&#43;Enter sends. |
| preferred_language | [PreferredLanguage](#laelia-v1-PreferredLanguage) |  | preferred_language is the user&#39;s preferred language for agent-initiated conversation. Agents read it and reply in that language when chatting with the user (e.g. in a DM or channel). UNSPECIFIED means the user has not set one; the agent then chooses the most appropriate language. |






<a name="laelia-v1-CreateUserRequest"></a>

### CreateUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| user | [User](#laelia-v1-User) |  | The user to create. |






<a name="laelia-v1-DeleteAvatarRequest"></a>

### DeleteAvatarRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The avatar resource name. Format: users/{user}/avatar. Must be the caller&#39;s own. |






<a name="laelia-v1-DeleteUserRequest"></a>

### DeleteUserRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The name of the user to delete. Format: users/{user} |






<a name="laelia-v1-DownloadAvatarRequest"></a>

### DownloadAvatarRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The avatar resource name. Format: users/{user}/avatar. |






<a name="laelia-v1-DownloadAvatarResponse"></a>

### DownloadAvatarResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  |  |
| mime_type | [string](#string) |  |  |
| etag | [string](#string) |  | etag is a content hash of data, usable as a cache-busting token. |






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
| include_system_bot | [bool](#bool) |  | Include the internal SYSTEM_BOT account in the results. Defaults to false: the system bot is hidden from every caller except the settings user directory, which opts in explicitly. |






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






<a name="laelia-v1-UploadAvatarRequest"></a>

### UploadAvatarRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [bytes](#bytes) |  | The avatar image bytes (png/jpeg/webp/gif). Clients should resize before uploading; the server enforces a hard byte cap regardless. |
| mime_type | [string](#string) |  | The mime type of data, e.g. &#34;image/png&#34;. |






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
| groups | [string](#string) | repeated | The groups for the user. Format: groups/{identifier}, where the identifier is the group email when the group has one, otherwise its id. |
| workspace_admin | [bool](#bool) |  | workspace_admin is true when the user holds the roles/workspaceAdmin role. Only populated for the current caller (GetCurrentUser). Retained as a computed shim during the IAM transition; prefer `permissions` for gating. |
| description | [string](#string) |  | description is a short, user-authored self-description surfaced to agents and other users so they know who this user is and what they focus on, e.g. &#34;Backend engineer, focused on agent building&#34; or &#34;UI/UX expert, reviews come to me&#34;. Editable via UpdateUser with update_mask &#34;description&#34;. |
| permissions | [string](#string) | repeated | permissions is the caller&#39;s effective workspace-scope permission set (roles/workspaceMember baseline ∪ the permissions of every workspace role the user holds), populated only by GetCurrentUser. The frontend gates workspace actions on this (e.g. laelia.users.update). Per-resource permissions (conversations.read/send/manage, agents.edit) are resolved per resource and surfaced on the resource, not here. |
| debug_mode | [bool](#bool) |  | debug_mode is true when RuntimeDebug is enabled for the workspace. Populated only by GetCurrentUser so the frontend can gate debug-only UI without calling the admin-gated SettingService.GetDebugConfig. |
| avatar | [string](#string) |  | avatar is the resource name of the user&#39;s uploaded avatar image, or empty when the user has not uploaded one (in which case the frontend renders a deterministic pixel identicon seeded by the user id). Format: users/{user}/avatar. |
| chat_preferences | [ChatPreferences](#laelia-v1-ChatPreferences) |  | chat_preferences holds per-user chat composer preferences. Editable via UpdateUser with update_mask &#34;chat_preferences&#34;. When unset (the user has never customized it) the server returns the default {enter_to_send = true} so the historic behavior is preserved. |






<a name="laelia-v1-UserProfile"></a>

### UserProfile



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| last_login_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| last_change_password_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| source | [string](#string) |  | source means where the user comes from. For now we support Entra ID SCIM sync, so the source could be Entra ID. |





 


<a name="laelia-v1-PreferredLanguage"></a>

### PreferredLanguage
PreferredLanguage is a user&#39;s preferred language for interacting with agents.
It is distinct from the frontend&#39;s UI locale: it is a server-stored
preference that agents can perceive and honor when conversing.

| Name | Number | Description |
| ---- | ------ | ----------- |
| PREFERRED_LANGUAGE_UNSPECIFIED | 0 |  |
| PREFERRED_LANGUAGE_ZH_CN | 1 |  |
| PREFERRED_LANGUAGE_EN_US | 2 |  |
| PREFERRED_LANGUAGE_JA_JP | 3 |  |



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
| UploadAvatar | [UploadAvatarRequest](#laelia-v1-UploadAvatarRequest) | [User](#laelia-v1-User) | UploadAvatar replaces the current user&#39;s avatar image. Self only: the caller must be the user named by the resource. The image bytes are re-encoded/stored server-side; clients should resize before uploading. Bytes travel over Connect-JSON like CommandService.UploadFile. |
| DownloadAvatar | [DownloadAvatarRequest](#laelia-v1-DownloadAvatarRequest) | [DownloadAvatarResponse](#laelia-v1-DownloadAvatarResponse) | DownloadAvatar fetches a user&#39;s avatar image bytes. Any authenticated user can download any user&#39;s avatar (workspace-internal profile image). |
| DeleteAvatar | [DeleteAvatarRequest](#laelia-v1-DeleteAvatarRequest) | [User](#laelia-v1-User) | DeleteAvatar clears the current user&#39;s avatar, reverting to the pixel default. Self only. |

 



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



<a name="laelia-v1-AccessibleChannel"></a>

### AccessibleChannel
AccessibleChannel wraps a conversation the agent can read, with is_member
reporting whether the agent has actually joined it (only joined conversations
accept posts and appear in the agent&#39;s ListChannelUpdates inbox).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| channel | [Conversation](#laelia-v1-Conversation) |  |  |
| is_member | [bool](#bool) |  |  |






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






<a name="laelia-v1-Activity"></a>

### Activity
Activity is one item in a user&#39;s per-user activity feed. Each item corresponds
to a single chat_message relevant to the user, tagged with the category(ies)
that made it relevant. The message itself is the source of truth for
content/sender; this row carries the per-user read/done state and category
flags. The resource name is &#34;users/{user}/activities/{message}&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| conversation | [string](#string) |  | conversation is the conversation the message belongs to. |
| message | [string](#string) |  | message is the chat message (&#34;conversations/{c}/messages/{m}&#34;). |
| thread_root | [string](#string) |  | thread_root, when non-empty, is the root message of the thread the message belongs to. Empty for a top-level message. |
| categories | [int32](#int32) |  | categories is the OR-ed set of ActivityCategory flags that made this message relevant to the user. |
| state | [ActivityState](#laelia-v1-ActivityState) |  | state is the user-facing lifecycle (UNREAD/READ/DONE). Read is derived from the user_channel_cursor; DONE is an explicit action. |
| room_version | [int64](#int64) |  | room_version is the message&#39;s room_version (for ordering and read-cursor comparison). |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| read_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| done_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| summary | [string](#string) |  | summary is a short preview of the message content for the left-list. |
| sender_name | [string](#string) |  | sender_name is the display name of the message sender. |
| sender_type | [SenderType](#laelia-v1-SenderType) |  | sender_type is the SenderType of the message (USER/AGENT/SYSTEM). |






<a name="laelia-v1-AddChannelMemberInput"></a>

### AddChannelMemberInput



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member_type | [int32](#int32) |  |  |
| member_id | [string](#string) |  |  |
| expire_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  | expire_time, when set, makes this a temporary member: the conversation policy binding carries a `request.time &lt; timestamp(&#34;...&#34;)` condition and the caller&#39;s access expires automatically at the given instant. Must be in the future. |
| group | [string](#string) |  | group, when set, adds every current member of the group to the channel as real user members (a snapshot: later group membership changes do not sync). Mutually exclusive with member_type/member_id. Group members already in the channel are skipped, so re-adding a group is idempotent. |






<a name="laelia-v1-AddChannelMemberRequest"></a>

### AddChannelMemberRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| members | [AddChannelMemberInput](#laelia-v1-AddChannelMemberInput) | repeated |  |






<a name="laelia-v1-AddChannelMemberResponse"></a>

### AddChannelMemberResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| members | [ChannelMember](#laelia-v1-ChannelMember) | repeated |  |






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
| agent_name | [string](#string) |  | agent_name declares which agent (agents/{agent}) this AgentChannel runs. The manager validates the authenticated machine owns this agent. Set by the machine app&#39;s per-agent runner; required on the first message. |






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
| workspace_list_response | [WorkspaceListResponse](#laelia-v1-WorkspaceListResponse) |  | response to ManagerStreamMessage.workspace_list_request |
| workspace_read_response | [WorkspaceReadResponse](#laelia-v1-WorkspaceReadResponse) |  | response to ManagerStreamMessage.workspace_read_request |






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
| owner_display_name | [string](#string) |  | owner_display_name is the agent&#39;s owner&#39;s display name, sourced from the manager (the source of truth for ownership). The agent client injects it into its system prompt (the Ownership &amp; Safety section) so the agent knows whom to DM for approval of high-risk requests from non-owners. Empty for legacy agents with no recorded owner. |






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
| avatar | [string](#string) |  | avatar is the member&#39;s avatar resource name (users/{user}/avatar or agents/{agent}/avatar) when the member has uploaded one, empty otherwise (in which case the frontend renders a deterministic pixel identicon). Surfaced inline so the frontend can render roster avatars without a per-member lookup. |
| preferred_language | [PreferredLanguage](#laelia-v1-PreferredLanguage) |  | preferred_language is the member&#39;s preferred language when the member is a user (from User.chat_preferences), UNSPECIFIED otherwise. Surfaced so an agent can perceive whom it is talking to and converse in that language. |






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
| principal_id | [string](#string) |  | principal_id is the decimal id of the principal that authored this message (the chat_message.principal_id row). For a user message it is the sending user&#39;s principal id (matching the {user} segment of the &#34;users/{user}&#34; resource name); for an agent message it is the conversation owner&#39;s principal id; for a system message it is the system bot&#39;s id. The frontend uses it to tell the current user&#39;s own messages apart from other users&#39; messages in shared channels (sender_name alone is a display name and can collide across users). |






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






<a name="laelia-v1-CloseTaskRequest"></a>

### CloseTaskRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [string](#string) |  | message is the resource name of the task&#39;s root message (&#34;conversations/{c}/messages/{m}&#34;). |






<a name="laelia-v1-CloseTaskResponse"></a>

### CloseTaskResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| message | [ChatMessage](#laelia-v1-ChatMessage) |  | message is the task message after the close, with task populated (status now DONE, completed_at set). Closing an already-DONE task is idempotent: the current state is returned as-is. |






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
| context_compaction | [ContextCompactionPayload](#laelia-v1-ContextCompactionPayload) |  | 18-20 were permission_requested/timed_out/decided; permissions are now auto-granted by the runtime, so the payloads are gone. |
| context_usage | [ContextUsagePayload](#laelia-v1-ContextUsagePayload) |  |  |
| token_usage | [TokenUsagePayload](#laelia-v1-TokenUsagePayload) |  |  |






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






<a name="laelia-v1-ContextCompactionPayload"></a>

### ContextCompactionPayload
ContextCompactionPayload describes a context-window compaction observed on
the agent runtime. inferred is true when the compaction was detected
indirectly (e.g. a usage drop) rather than reported by the agent.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reason | [string](#string) |  |  |
| inferred | [bool](#bool) |  |  |






<a name="laelia-v1-ContextUsagePayload"></a>

### ContextUsagePayload
ContextUsagePayload is a point-in-time snapshot of the session context
window. usage_ratio is used/size.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| size | [int64](#int64) |  |  |
| used | [int64](#int64) |  |  |
| usage_ratio | [double](#double) |  |  |






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
| address | [string](#string) |  | address is the name-based display address for this conversation, the form agents write and read: &#34;#&lt;title&gt;&#34; for a channel (type 2), &#34;dm:@&lt;peer&gt;&#34; for a direct message (type 1 peer is the user, type 3 peer is the other agent, type 4 peer is the other user). Empty when the address is not applicable. Populated by the single builder convertToV1Conversation so every emit site renders the same form. |
| read_version | [int64](#int64) |  | read_version is the requesting user&#39;s per-conversation read cursor (user_channel_cursor.read_version) — the room_version of the last message the user has read. Populated by GetChannel for a user viewer so the Activity detail embed can scroll to the first unread message (the user&#39;s last-read position) instead of the latest message. 0 when the caller is not a user or has no cursor row (treated as caught-up). |
| peer | [string](#string) |  | peer is the DM peer&#39;s resource name from the viewer&#39;s perspective (&#34;users/&lt;id&gt;&#34; for a user peer, &#34;agents/&lt;id&gt;&#34; for an agent peer). Empty for channels (type 2) and when no peer can be resolved. Lets list viewers fetch the peer&#39;s avatar without an extra member lookup. |
| pinned | [bool](#bool) |  | pinned is the requesting user&#39;s per-conversation pin state (conversation_member.pinned). Pinned channels/DMs stay at the top of the left-rail list regardless of last message time. Per-user: each viewer has their own pins. Populated by ListChannels and GetChannel for a user viewer. |






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






<a name="laelia-v1-GetOrCreateUserUserDMRequest"></a>

### GetOrCreateUserUserDMRequest
GetOrCreateUserUserDM opens (or reuses) the type-4 direct conversation
between the calling user and a peer user. User-only (an agent token must not
create a user-user DM). The peer is resolved by user resource name
(&#34;users/&lt;id&gt;&#34;); self-address is rejected; the pair is canonicalized by the
store. This is the user-user twin of the user-agent GetOrCreateConversation.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| peer_user | [string](#string) |  |  |






<a name="laelia-v1-GetOrCreateUserUserDMResponse"></a>

### GetOrCreateUserUserDMResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






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






<a name="laelia-v1-JoinChannelRequest"></a>

### JoinChannelRequest
JoinChannel makes the calling agent a real member of a channel it can read
(via its own membership or owner-follow). Joining seeds the agent&#39;s
per-channel cursor to the current version, so the channel starts appearing in
ListChannelUpdates from that point on and the agent may post to it. Idempotent
for members. The gate is laelia.conversations.read (the agent may only join a
channel it can already read); a mutation gated by a read permission is
deliberate — &#34;join&#34; is exactly &#34;subscribe to a conversation I can see&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






<a name="laelia-v1-JoinChannelResponse"></a>

### JoinChannelResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [Conversation](#laelia-v1-Conversation) |  |  |






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






<a name="laelia-v1-ListAccessibleChannelsRequest"></a>

### ListAccessibleChannelsRequest
ListAccessibleChannels returns, for the authenticated agent, every
conversation it can read: its own memberships plus — when the agent&#39;s
follow_owner_permissions is enabled — every conversation its owner can read
(channels and DMs). This is the on-demand discovery surface (&#34;what channels
can I access&#34;); it is separate from ListChannelUpdates (the drain-loop
inbox), which stays limited to conversations the agent has joined so the
agent is not woken for every message in its owner&#39;s channels.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListAccessibleChannelsResponse"></a>

### ListAccessibleChannelsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| channels | [AccessibleChannel](#laelia-v1-AccessibleChannel) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListActivitiesRequest"></a>

### ListActivitiesRequest
ListActivities returns the authenticated user&#39;s activity feed: chat messages
relevant to them, tagged with category flags. The caller&#39;s own id is the
implicit filter (no cross-user listing). filter selects items by category
(empty = all categories; set = items whose categories intersect the requested
set, ANY flag). read_state_filter scopes by lifecycle and defaults to UNREAD
when UNSPECIFIED.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| filter | [ActivityCategory](#laelia-v1-ActivityCategory) | repeated |  |
| read_state_filter | [ActivityState](#laelia-v1-ActivityState) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListActivitiesResponse"></a>

### ListActivitiesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| activities | [Activity](#laelia-v1-Activity) | repeated |  |
| next_page_token | [string](#string) |  |  |






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
| wait_ms | [int32](#int32) |  | wait_ms turns the read into a long poll: when no messages exist beyond after_version, the server holds the request until a new message lands or wait_ms elapses, then returns the delta (possibly empty) with the current version so the client can advance its cursor and re-issue. Only valid with after_version &gt; 0; 0 (default) returns immediately. Capped server-side at 30000. |






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






<a name="laelia-v1-ListTaskCountsRequest"></a>

### ListTaskCountsRequest
ListTaskCountsRequest asks for per-status task totals for a conversation, so
the task board summary stays accurate regardless of how many tasks are loaded
into the paginated list view.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |






<a name="laelia-v1-ListTaskCountsResponse"></a>

### ListTaskCountsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| todo_count | [int32](#int32) |  |  |
| in_progress_count | [int32](#int32) |  |  |
| in_review_count | [int32](#int32) |  |  |
| done_count | [int32](#int32) |  |  |






<a name="laelia-v1-ListTasksRequest"></a>

### ListTasksRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| status_filter | [TaskStatus](#laelia-v1-TaskStatus) | repeated | status_filter, when non-empty, restricts the result to the given statuses. Empty returns tasks in every status. |
| page_size | [int32](#int32) |  | page_size is the maximum number of tasks to return in one page. The server clamps it to a sane range and applies a default when zero. |
| page_token | [string](#string) |  | page_token is the opaque cursor returned in the previous response&#39;s next_page_token; empty starts at the newest task. |






<a name="laelia-v1-ListTasksResponse"></a>

### ListTasksResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| tasks | [ChatMessage](#laelia-v1-ChatMessage) | repeated | tasks are one page of the channel&#39;s task root messages, each with task populated, ordered by task_number descending (newest first). |
| next_page_token | [string](#string) |  | next_page_token is the cursor for the next (older) page; empty when this page is the last. |






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
| wait_ms | [int32](#int32) |  | wait_ms turns the read into a long poll, mirroring ListConversationMessagesRequest.wait_ms: the server holds the request until a new reply lands or wait_ms elapses. Only valid with after_version &gt; 0; 0 (default) returns immediately. Capped server-side at 30000. |






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
| discover_providers | [DiscoverProviders](#laelia-v1-DiscoverProviders) |  | 7 was permission_decision; permissions are now auto-granted.

ask the agent daemon to re-probe installed LLM agent providers |
| workspace_list_request | [WorkspaceListRequest](#laelia-v1-WorkspaceListRequest) |  | ask the agent daemon to list one level of its workspace |
| workspace_read_request | [WorkspaceReadRequest](#laelia-v1-WorkspaceReadRequest) |  | ask the agent daemon to read a workspace file |
| steer | [SteerMessage](#laelia-v1-SteerMessage) |  | inject a follow-up message into the in-flight turn |






<a name="laelia-v1-MarkActivityDoneRequest"></a>

### MarkActivityDoneRequest
MarkActivityDone marks a single activity item DONE for the authenticated user,
hiding it from All and Unread. The caller&#39;s own id must own the row; the name
is &#34;users/{user}/activities/{message}&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-MarkActivityDoneResponse"></a>

### MarkActivityDoneResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| activity | [Activity](#laelia-v1-Activity) |  |  |






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






<a name="laelia-v1-SetConversationPinnedRequest"></a>

### SetConversationPinnedRequest
SetConversationPinned sets or clears the requesting user&#39;s per-conversation
pin. Pinning a channel or DM keeps it at the top of the user&#39;s left-rail
list independent of last message time. Per-user state
(conversation_member.pinned/pinned_at); only the caller&#39;s own pin is
affected.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| conversation | [string](#string) |  |  |
| pinned | [bool](#bool) |  |  |






<a name="laelia-v1-SetConversationPinnedResponse"></a>

### SetConversationPinnedResponse







<a name="laelia-v1-SteerCommandRequest"></a>

### SteerCommandRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| text | [string](#string) |  |  |






<a name="laelia-v1-SteerMessage"></a>

### SteerMessage
SteerMessage injects a follow-up message into the in-flight turn of a
running command. It is best-effort: an executor that does not support
mid-turn steering ignores it.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| command_id | [string](#string) |  |  |
| text | [string](#string) |  |  |






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






<a name="laelia-v1-TokenUsagePayload"></a>

### TokenUsagePayload
TokenUsagePayload is the token consumption of a single command execution
(this turn only, not session-cumulative). Fields are absent/zero when the
runtime did not report usage.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| input_tokens | [int64](#int64) |  |  |
| output_tokens | [int64](#int64) |  |  |
| cache_read_tokens | [int64](#int64) |  |  |
| cache_write_tokens | [int64](#int64) |  |  |
| total_tokens | [int64](#int64) |  |  |






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






<a name="laelia-v1-WorkspaceListRequest"></a>

### WorkspaceListRequest
WorkspaceListRequest asks the agent daemon to list one directory level of an
agent&#39;s workspace (~/.laelia/&lt;machineID&gt;/&lt;agentID&gt;/ on the machine). Paths
are relative to the workspace root; an empty dir_path lists the root. The
daemon replies with AgentStreamMessage.workspace_list_response.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | correlation id for the pending unary ListAgentWorkspace call |
| dir_path | [string](#string) |  | relative to the workspace root; empty = root |
| include_hidden | [bool](#bool) |  | show dotfiles (still filtered by the never-visible policy) |






<a name="laelia-v1-WorkspaceListResponse"></a>

### WorkspaceListResponse
WorkspaceListResponse carries one directory level back to the manager.
Entries are sorted directories-first, then by name. WorkspaceEntry is shared
with the unary ListAgentWorkspace RPC (defined in v1/agent.proto).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  |  |
| entries | [WorkspaceEntry](#laelia-v1-WorkspaceEntry) | repeated |  |






<a name="laelia-v1-WorkspaceReadRequest"></a>

### WorkspaceReadRequest
WorkspaceReadRequest asks the agent daemon to read a workspace file for
preview. Text and image content is returned inline (see
WorkspaceReadResponse); other binaries return metadata only. Sensitive files
(secret/credential/token patterns) are always rejected by the daemon.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | correlation id for the pending unary ReadAgentWorkspaceFile call |
| path | [string](#string) |  | relative to the workspace root |





 


<a name="laelia-v1-ActivityCategory"></a>

### ActivityCategory
ActivityCategory flags which kind of conversation event an Activity item
represents. A single Activity row may carry several categories OR-ed together
(a task-thread reply that @mentions a user is both TASK and MENTION). The
ListActivities filter selects items that have ANY of the requested categories
set. Values are bit flags so they compose in a single int column; the
ACTIVITY_CATEGORY prefix satisfies protobuf C&#43;&#43; scoping rules (sibling enums
cannot share value names), matching SenderType/TaskStatus.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ACTIVITY_CATEGORY_UNSPECIFIED | 0 |  |
| ACTIVITY_CATEGORY_MENTION | 1 |  |
| ACTIVITY_CATEGORY_TASK | 2 |  |
| ACTIVITY_CATEGORY_REMINDER | 4 |  |
| ACTIVITY_CATEGORY_THREAD | 8 |  |
| ACTIVITY_CATEGORY_DIRECT | 16 | DIRECT marks a 1:1 DM message (user&lt;-&gt;user or agent-&gt;user) addressed to the user but carrying no other category (no @mention, not a task/reminder, not a thread reply). It gives DMs a notifiable signal where they would otherwise produce no Activity row. |



<a name="laelia-v1-ActivityState"></a>

### ActivityState
ActivityState is the user-facing lifecycle of an Activity item. UNREAD -&gt; READ
happens when the user&#39;s per-channel read cursor advances past the message&#39;s
room_version (via MarkConversationRead); READ items stay visible under the All
filter. DONE is an explicit &#34;Mark as Done&#34; action that hides the item from All
and Unread. The ACTIVITY_STATE prefix satisfies protobuf C&#43;&#43; scoping rules.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ACTIVITY_STATE_UNSPECIFIED | 0 |  |
| ACTIVITY_STATE_UNREAD | 1 |  |
| ACTIVITY_STATE_READ | 2 |  |
| ACTIVITY_STATE_DONE | 3 |  |



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
| CONTEXT_COMPACTION_STARTED | 12 |  |
| CONTEXT_COMPACTION_FINISHED | 13 |  |
| CONTEXT_USAGE_UPDATE | 14 |  |
| TOKEN_USAGE | 15 |  |



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
| SteerCommand | [SteerCommandRequest](#laelia-v1-SteerCommandRequest) | [Command](#laelia-v1-Command) | SteerCommand injects a follow-up message into a running command&#39;s in-flight turn. Only executors that support mid-turn steering (the ACP v2 thread protocol&#39;s turn/steer) honor it; others ignore it. |
| WatchCommand | [WatchCommandRequest](#laelia-v1-WatchCommandRequest) | [CommandOutput](#laelia-v1-CommandOutput) stream |  |
| WatchCommandEvents | [WatchCommandEventsRequest](#laelia-v1-WatchCommandEventsRequest) | [CommandEvent](#laelia-v1-CommandEvent) stream |  |
| SearchChatHistory | [SearchChatHistoryRequest](#laelia-v1-SearchChatHistoryRequest) | [SearchChatHistoryResponse](#laelia-v1-SearchChatHistoryResponse) |  |
| GetCommandContext | [GetCommandContextRequest](#laelia-v1-GetCommandContextRequest) | [GetCommandContextResponse](#laelia-v1-GetCommandContextResponse) |  |
| GetOrCreateConversation | [GetOrCreateConversationRequest](#laelia-v1-GetOrCreateConversationRequest) | [GetOrCreateConversationResponse](#laelia-v1-GetOrCreateConversationResponse) |  |
| GetOrCreateUserUserDM | [GetOrCreateUserUserDMRequest](#laelia-v1-GetOrCreateUserUserDMRequest) | [GetOrCreateUserUserDMResponse](#laelia-v1-GetOrCreateUserUserDMResponse) | GetOrCreateUserUserDM opens (or reuses) the type-4 user-to-user DM between the calling user and a peer user. User-only. The peer is resolved by user resource name (&#34;users/&lt;id&gt;&#34;); self-address is rejected; the pair is canonicalized by the store. User-user twin of GetOrCreateConversation. |
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
| AddChannelMember | [AddChannelMemberRequest](#laelia-v1-AddChannelMemberRequest) | [AddChannelMemberResponse](#laelia-v1-AddChannelMemberResponse) |  |
| RemoveChannelMember | [RemoveChannelMemberRequest](#laelia-v1-RemoveChannelMemberRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| TransferChannelOwnership | [TransferChannelOwnershipRequest](#laelia-v1-TransferChannelOwnershipRequest) | [TransferChannelOwnershipResponse](#laelia-v1-TransferChannelOwnershipResponse) | TransferChannelOwnership hands channel ownership from the calling owner to another member: the target is promoted to Owner and the caller demoted to Member, atomically. The interceptor gates the call with conversations.manage (Admin&#43;Owner); the handler additionally enforces that the caller is the current Owner. Only channels (type 2) support ownership transfer. |
| UpdateChannelMemberRole | [UpdateChannelMemberRoleRequest](#laelia-v1-UpdateChannelMemberRoleRequest) | [ChannelMember](#laelia-v1-ChannelMember) | UpdateChannelMemberRole grants or revokes channel admin: the target member&#39;s role is set to the requested role (Member or Admin). The interceptor gates with conversations.manage (Admin&#43;Owner); the handler enforces that the caller is the Owner and the target role is Member or Admin (never Owner — ownership only moves via TransferChannelOwnership). |
| LeaveChannel | [LeaveChannelRequest](#laelia-v1-LeaveChannelRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | LeaveChannel removes the calling member from a channel. The interceptor gates with conversations.read (any member); the handler rejects the current Owner — an owner must transfer ownership or delete the channel first to avoid orphaning it. Only channels (type 2) support leaving. |
| ListChannelMembers | [ListChannelMembersRequest](#laelia-v1-ListChannelMembersRequest) | [ListChannelMembersResponse](#laelia-v1-ListChannelMembersResponse) |  |
| ListThreadParticipants | [ListThreadParticipantsRequest](#laelia-v1-ListThreadParticipantsRequest) | [ListThreadParticipantsResponse](#laelia-v1-ListThreadParticipantsResponse) | ListThreadParticipants lists the distinct senders (users and agents) that posted in a thread. Intended for the agent daemon. The caller must be a member of the conversation. |
| SendMessage | [SendMessageRequest](#laelia-v1-SendMessageRequest) | [ChatMessage](#laelia-v1-ChatMessage) |  |
| PostMessage | [PostMessageRequest](#laelia-v1-PostMessageRequest) | [PostMessageResponse](#laelia-v1-PostMessageResponse) |  |
| ConvertMessageToTask | [ConvertMessageToTaskRequest](#laelia-v1-ConvertMessageToTaskRequest) | [ConvertMessageToTaskResponse](#laelia-v1-ConvertMessageToTaskResponse) | ConvertMessageToTask turns an existing top-level message into a task by attaching task metadata (number, status=TODO, no assignee). Any channel member (user or agent) may convert. Emits a system notification row. |
| ListTasks | [ListTasksRequest](#laelia-v1-ListTasksRequest) | [ListTasksResponse](#laelia-v1-ListTasksResponse) | ListTasks returns one page of the task board for a conversation: the channel&#39;s tasks (root messages with task metadata), newest first, optionally filtered by status. Use page_size / page_token for pagination. |
| ListTaskCounts | [ListTaskCountsRequest](#laelia-v1-ListTaskCountsRequest) | [ListTaskCountsResponse](#laelia-v1-ListTaskCountsResponse) | ListTaskCounts returns per-status task totals for a conversation, so the task board summary stays accurate independent of list pagination. |
| CreateTask | [CreateTaskRequest](#laelia-v1-CreateTaskRequest) | [CreateTaskResponse](#laelia-v1-CreateTaskResponse) | CreateTask posts a new top-level task message in a channel (used by agents to break work into subtasks for others to claim). The new task is created unassigned (status TODO); the posting agent does NOT auto-claim it. Emits a system notification row and wakes other agent members. |
| ClaimTask | [ClaimTaskRequest](#laelia-v1-ClaimTaskRequest) | [ClaimTaskResponse](#laelia-v1-ClaimTaskResponse) | ClaimTask atomically transitions a TODO task to IN_PROGRESS and assigns it to the calling agent, subscribing the agent to the task&#39;s thread so approval replies wake it. Returns FAILED_PRECONDITION if the task is already claimed or not in TODO. Emits a system notification row. |
| UnclaimTask | [UnclaimTaskRequest](#laelia-v1-UnclaimTaskRequest) | [UnclaimTaskResponse](#laelia-v1-UnclaimTaskResponse) | UnclaimTask releases the calling agent&#39;s claim on a task it owns, setting it back to TODO so another agent may claim it. Not allowed on DONE (terminal). Emits a system notification row. |
| UpdateTaskStatus | [UpdateTaskStatusRequest](#laelia-v1-UpdateTaskStatusRequest) | [UpdateTaskStatusResponse](#laelia-v1-UpdateTaskStatusResponse) | UpdateTaskStatus advances a task&#39;s status. IN_PROGRESS -&gt; IN_REVIEW marks the assignee&#39;s work ready for human review; IN_REVIEW -&gt; DONE marks it complete (the assignee should call this only after detecting the human&#39;s approval in the task&#39;s thread). Only the assignee may call this. Emits a system notification row. |
| CloseTask | [CloseTaskRequest](#laelia-v1-CloseTaskRequest) | [CloseTaskResponse](#laelia-v1-CloseTaskResponse) | CloseTask lets a channel member (user or agent) close a task from the UI: any non-DONE task transitions to DONE (terminal), setting completed_at. Unlike UpdateTaskStatus it does not require assignee ownership and accepts every open status (TODO / IN_PROGRESS / IN_REVIEW), so the user can close a task without going through the agent. Closing an already-DONE task is idempotent. Emits a system notification row. |
| ConvertMessageToReminder | [ConvertMessageToReminderRequest](#laelia-v1-ConvertMessageToReminderRequest) | [ConvertMessageToReminderResponse](#laelia-v1-ConvertMessageToReminderResponse) | ConvertMessageToReminder turns an existing top-level message into a scheduled reminder owned by the calling agent (atomic create&#43;claim). The message must be a root in the conversation and not already a reminder. The agent is subscribed to the reminder&#39;s thread so discussion replies wake it. |
| ListReminders | [ListRemindersRequest](#laelia-v1-ListRemindersRequest) | [ListRemindersResponse](#laelia-v1-ListRemindersResponse) | ListReminders returns reminders, optionally filtered by owning agent, conversation, and status. Used by the agent-page Reminders tab (user) and the agent CLI (self-list). |
| GetReminder | [GetReminderRequest](#laelia-v1-GetReminderRequest) | [GetReminderResponse](#laelia-v1-GetReminderResponse) | GetReminder returns a single reminder by its resource name. |
| UpdateReminder | [UpdateReminderRequest](#laelia-v1-UpdateReminderRequest) | [UpdateReminderResponse](#laelia-v1-UpdateReminderResponse) | UpdateReminder edits the schedule (fire_at/cron_expr/tz) or task_content of a reminder. The caller is the owning agent or a workspace admin. Editing a DUE or MISSED reminder resets it to PENDING with the new schedule. |
| CancelReminder | [CancelReminderRequest](#laelia-v1-CancelReminderRequest) | [CancelReminderResponse](#laelia-v1-CancelReminderResponse) | CancelReminder cancels a reminder. The caller is the owning agent or a workspace admin. A cancelled reminder is terminal. |
| CompleteReminder | [CompleteReminderRequest](#laelia-v1-CompleteReminderRequest) | [CompleteReminderResponse](#laelia-v1-CompleteReminderResponse) | CompleteReminder marks a DUE reminder completed and atomically posts the result as a single system message in the reminder&#39;s thread. Only the owning agent may call this. Recurring reminders reschedule to the next cron fire. |
| FailReminder | [FailReminderRequest](#laelia-v1-FailReminderRequest) | [FailReminderResponse](#laelia-v1-FailReminderResponse) | FailReminder marks a DUE reminder failed with the given error and posts it as a system thread message. Recurring reminders reschedule. Only the owning agent may call this. |
| ListDueReminders | [ListDueRemindersRequest](#laelia-v1-ListDueRemindersRequest) | [ListDueRemindersResponse](#laelia-v1-ListDueRemindersResponse) | ListDueReminders returns the DUE reminders owned by the calling agent, for the autonomous drain loop to pick up fired work. Agent identity is resolved from the auth context. |
| ListChannelUpdates | [ListChannelUpdatesRequest](#laelia-v1-ListChannelUpdatesRequest) | [ListChannelUpdatesResponse](#laelia-v1-ListChannelUpdatesResponse) |  |
| ListAccessibleChannels | [ListAccessibleChannelsRequest](#laelia-v1-ListAccessibleChannelsRequest) | [ListAccessibleChannelsResponse](#laelia-v1-ListAccessibleChannelsResponse) |  |
| JoinChannel | [JoinChannelRequest](#laelia-v1-JoinChannelRequest) | [JoinChannelResponse](#laelia-v1-JoinChannelResponse) |  |
| ListThreadUpdates | [ListThreadUpdatesRequest](#laelia-v1-ListThreadUpdatesRequest) | [ListThreadUpdatesResponse](#laelia-v1-ListThreadUpdatesResponse) |  |
| AckProcessedVersion | [AckProcessedVersionRequest](#laelia-v1-AckProcessedVersionRequest) | [AckProcessedVersionResponse](#laelia-v1-AckProcessedVersionResponse) |  |
| FetchConversationActivity | [FetchConversationActivityRequest](#laelia-v1-FetchConversationActivityRequest) | [FetchConversationActivityResponse](#laelia-v1-FetchConversationActivityResponse) |  |
| MarkConversationRead | [MarkConversationReadRequest](#laelia-v1-MarkConversationReadRequest) | [MarkConversationReadResponse](#laelia-v1-MarkConversationReadResponse) |  |
| SetConversationPinned | [SetConversationPinnedRequest](#laelia-v1-SetConversationPinnedRequest) | [SetConversationPinnedResponse](#laelia-v1-SetConversationPinnedResponse) |  |
| UploadFile | [UploadFileRequest](#laelia-v1-UploadFileRequest) | [File](#laelia-v1-File) | UploadFile stores data in S3 and persists a file row. Intended for the agent daemon (browser uploads go through the Echo multipart route); bytes travel over Connect-JSON, and avoiding a /v1/files/{id} REST entry keeps it from colliding with the browser download route. |
| DownloadFile | [DownloadFileRequest](#laelia-v1-DownloadFileRequest) | [DownloadFileResponse](#laelia-v1-DownloadFileResponse) | DownloadFile fetches a file&#39;s bytes from S3. The caller must be a member of the file&#39;s conversation. Used by the agent daemon; browser downloads go through the Echo route. |
| ListFiles | [ListFilesRequest](#laelia-v1-ListFilesRequest) | [ListFilesResponse](#laelia-v1-ListFilesResponse) | ListFiles returns the files attached to a conversation. The caller must be a member. |
| ListActivities | [ListActivitiesRequest](#laelia-v1-ListActivitiesRequest) | [ListActivitiesResponse](#laelia-v1-ListActivitiesResponse) | ListActivities returns the authenticated user&#39;s activity feed: chat messages relevant to them, tagged with category flags (mention/task/reminder/thread). The caller&#39;s own id is the implicit filter; default read_state_filter is UNREAD. |
| MarkActivityDone | [MarkActivityDoneRequest](#laelia-v1-MarkActivityDoneRequest) | [MarkActivityDoneResponse](#laelia-v1-MarkActivityDoneResponse) | MarkActivityDone marks a single activity item DONE for the authenticated user, hiding it from All and Unread. The caller&#39;s own id must own the row. |

 



<a name="v1_group_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/group_service.proto



<a name="laelia-v1-BatchGetGroupsRequest"></a>

### BatchGetGroupsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| names | [string](#string) | repeated | The group resource names, in the form `groups/{email}`. |






<a name="laelia-v1-BatchGetGroupsResponse"></a>

### BatchGetGroupsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| groups | [Group](#laelia-v1-Group) | repeated |  |






<a name="laelia-v1-CreateGroupRequest"></a>

### CreateGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| group_email | [string](#string) |  | The group email, e.g. &#34;eng@example.com&#34;. Optional; groups without an email are referenced by their generated id. |
| group_id | [string](#string) |  | Optional stable id (lowercase alnum &#43; dash). When empty, a UUID is generated. The id is immutable after creation. |
| group | [Group](#laelia-v1-Group) |  | The group to create. The name field is ignored (the id/email are taken from group_id/group_email); title and members are required. |






<a name="laelia-v1-DeleteGroupRequest"></a>

### DeleteGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The group resource name, in the form `groups/{email}`. |






<a name="laelia-v1-GetGroupRequest"></a>

### GetGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The group resource name, in the form `groups/{email}`. |






<a name="laelia-v1-Group"></a>

### Group
Group is a named collection of users that can be bound in IAM policies
(workspace, agent, and conversation bindings accept groups/{email} members).
The IAM engine expands a group&#39;s members at authorization time.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the group, in the form `groups/{id}`. The id is the stable primary key; groups with an email also accept `groups/{email}` in IAM bindings and Get requests (resolved by the store). |
| email | [string](#string) |  | The group email, e.g. &#34;eng@example.com&#34;. Optional: groups without an email are referenced by their id only. |
| title | [string](#string) |  | Human-readable title. |
| description | [string](#string) |  | Longer description of the group. |
| members | [GroupMember](#laelia-v1-GroupMember) | repeated | The group&#39;s members. Each member is a user resource name (&#34;users/{uid}&#34;) with a role: OWNER (may manage the group) or MEMBER. |
| source | [string](#string) |  | Output only. When non-empty, the group is synced from an external source (e.g. SCIM) and is read-only over this API. |
| can_manage | [bool](#bool) |  | Output only. True when the caller may manage this group: the caller is a group OWNER or holds laelia.groups.update. Populated for the caller only. |






<a name="laelia-v1-GroupMember"></a>

### GroupMember



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| member | [string](#string) |  | The member&#39;s user resource name, in the form `users/{uid}`. |
| role | [GroupMemberRole](#laelia-v1-GroupMemberRole) |  | The member&#39;s role in the group. |






<a name="laelia-v1-GroupReference"></a>

### GroupReference
GroupReference is one policy (workspace, agent, or conversation) that binds
the group as a member.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| resource | [string](#string) |  | The resource name of the policy, e.g. &#34;workspaces/-&#34;, &#34;agents/{id}&#34;, or &#34;conversations/{id}&#34;. |
| resource_type | [string](#string) |  | The policy resource type: WORKSPACE, AGENT, or CONVERSATION. |






<a name="laelia-v1-GroupReferences"></a>

### GroupReferences



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| references | [GroupReference](#laelia-v1-GroupReference) | repeated |  |






<a name="laelia-v1-ListGroupsRequest"></a>

### ListGroupsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| filter | [string](#string) |  | Filter is used to filter groups returned in the list. Supported fields: - title: equality on the group title. - email: equality on the group email. Example: title == &#34;Engineering&#34; |






<a name="laelia-v1-ListGroupsResponse"></a>

### ListGroupsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| groups | [Group](#laelia-v1-Group) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-UpdateGroupRequest"></a>

### UpdateGroupRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| group | [Group](#laelia-v1-Group) |  | The group to update. The `name` field identifies the group. |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  | The list of fields to update: &#34;title&#34;, &#34;description&#34;, &#34;members&#34;. |





 


<a name="laelia-v1-GroupMemberRole"></a>

### GroupMemberRole


| Name | Number | Description |
| ---- | ------ | ----------- |
| GROUP_MEMBER_ROLE_UNSPECIFIED | 0 |  |
| OWNER | 1 | Owners manage the group (update/delete) without workspace-level group permissions. |
| MEMBER | 2 |  |


 

 


<a name="laelia-v1-GroupService"></a>

### GroupService
GroupService manages user groups. Groups are bindable in IAM policies;
group owners can manage their own groups without workspace-level group
permissions. Groups synced from an external source (SCIM/IdP) are read-only.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetGroup | [GetGroupRequest](#laelia-v1-GetGroupRequest) | [Group](#laelia-v1-Group) | Get a group. Group members and callers holding laelia.groups.get can read. |
| BatchGetGroups | [BatchGetGroupsRequest](#laelia-v1-BatchGetGroupsRequest) | [BatchGetGroupsResponse](#laelia-v1-BatchGetGroupsResponse) | Batch get groups. |
| ListGroups | [ListGroupsRequest](#laelia-v1-ListGroupsRequest) | [ListGroupsResponse](#laelia-v1-ListGroupsResponse) | List all groups. |
| CreateGroup | [CreateGroupRequest](#laelia-v1-CreateGroupRequest) | [Group](#laelia-v1-Group) | Create a group. Any authenticated member can create a group; the creator is not automatically added (the request must carry at least one OWNER). |
| UpdateGroup | [UpdateGroupRequest](#laelia-v1-UpdateGroupRequest) | [Group](#laelia-v1-Group) | Update a group (title, description, members). The group owner or a caller holding laelia.groups.update may update. |
| DeleteGroup | [DeleteGroupRequest](#laelia-v1-DeleteGroupRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Delete a group. The group owner or a caller holding laelia.groups.delete may delete. Existing IAM bindings referencing the group become no-ops. |
| GetGroupReferences | [GetGroupRequest](#laelia-v1-GetGroupRequest) | [GroupReferences](#laelia-v1-GroupReferences) | List the policies that bind this group as a member, so owners/admins can see the impact of deleting it. |

 



<a name="v1_iam_service-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/iam_service.proto



<a name="laelia-v1-BindingDelta"></a>

### BindingDelta
BindingDelta describes a single member-role-condition change between two
IAM policies. It is computed on Set and recorded in the audit log so an
operator can see who granted or removed what.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| action | [BindingDelta.Action](#laelia-v1-BindingDelta-Action) |  | The action that was performed on the binding. |
| member | [string](#string) |  | The affected member, e.g. &#34;users/101&#34;, &#34;groups/eng@example.com&#34;, &#34;agents/{rid}&#34;, or &#34;allUsers&#34;. |
| role | [string](#string) |  | The affected role, in the form &#34;roles/{role}&#34;. |
| condition | [google.type.Expr](#google-type-Expr) |  | The condition associated with the changed binding, if any. |






<a name="laelia-v1-GetAgentIamPolicyRequest"></a>

### GetAgentIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The agent resource name, in the form `agents/{agent}`. |






<a name="laelia-v1-GetMachineIamPolicyRequest"></a>

### GetMachineIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The machine resource name, in the form `machines/{machine}`. |






<a name="laelia-v1-GetWorkspaceIamPolicyRequest"></a>

### GetWorkspaceIamPolicyRequest







<a name="laelia-v1-IamPolicyChange"></a>

### IamPolicyChange
IamPolicyChange is the audit payload recorded for a successful SetIamPolicy
call: the target resource and the binding deltas applied.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| resource | [string](#string) |  | The resource the policy was set on, e.g. &#34;workspaces/-&#34; for the workspace policy or &#34;agents/{agent}&#34; for an agent policy. |
| binding_deltas | [BindingDelta](#laelia-v1-BindingDelta) | repeated | The binding changes applied by the Set. |






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






<a name="laelia-v1-PolicyDelta"></a>

### PolicyDelta
PolicyDelta describes the changes between two IAM policies.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| binding_deltas | [BindingDelta](#laelia-v1-BindingDelta) | repeated | The binding deltas between the previous and the new policy. |






<a name="laelia-v1-SetAgentIamPolicyRequest"></a>

### SetAgentIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The agent resource name, in the form `agents/{agent}`. |
| policy | [laelia.store.IamPolicy](#laelia-store-IamPolicy) |  |  |
| etag | [string](#string) |  |  |






<a name="laelia-v1-SetMachineIamPolicyRequest"></a>

### SetMachineIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The machine resource name, in the form `machines/{machine}`. |
| policy | [laelia.store.IamPolicy](#laelia-store-IamPolicy) |  |  |
| etag | [string](#string) |  |  |






<a name="laelia-v1-SetWorkspaceIamPolicyRequest"></a>

### SetWorkspaceIamPolicyRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| policy | [laelia.store.IamPolicy](#laelia-store-IamPolicy) |  |  |
| etag | [string](#string) |  |  |





 


<a name="laelia-v1-BindingDelta-Action"></a>

### BindingDelta.Action
Action is the type of change applied to a binding.

| Name | Number | Description |
| ---- | ------ | ----------- |
| ACTION_UNSPECIFIED | 0 | Unspecified action. |
| ADD | 1 | The member was added to the role. |
| REMOVE | 2 | The member was removed from the role. |


 

 


<a name="laelia-v1-IamService"></a>

### IamService
IamService exposes the workspace, per-agent, and per-machine IAM policies for
management. Get reads the full policy; Set replaces it whole, guarded by an
etag. The workspace/agent RPCs are gated by the IAM interceptor with
laelia.iam.getPolicy / setPolicy; the machine RPCs are handler-gated (the
machine&#39;s creator or a workspace admin) because a machine-scoped permission
cannot express the creator&#39;s implicit authority.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetWorkspaceIamPolicy | [GetWorkspaceIamPolicyRequest](#laelia-v1-GetWorkspaceIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Get the workspace IAM policy. |
| SetWorkspaceIamPolicy | [SetWorkspaceIamPolicyRequest](#laelia-v1-SetWorkspaceIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Set the workspace IAM policy (full replace, etag-guarded). |
| GetAgentIamPolicy | [GetAgentIamPolicyRequest](#laelia-v1-GetAgentIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Get the IAM policy attached to an agent. |
| SetAgentIamPolicy | [SetAgentIamPolicyRequest](#laelia-v1-SetAgentIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Set the IAM policy attached to an agent (full replace, etag-guarded). |
| GetMachineIamPolicy | [GetMachineIamPolicyRequest](#laelia-v1-GetMachineIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Get the IAM policy attached to a machine (who may create agents on it). |
| SetMachineIamPolicy | [SetMachineIamPolicyRequest](#laelia-v1-SetMachineIamPolicyRequest) | [IamPolicyView](#laelia-v1-IamPolicyView) | Set the IAM policy attached to a machine (full replace, etag-guarded). |

 



<a name="v1_machine-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/machine.proto



<a name="laelia-v1-AgentAssignment"></a>

### AgentAssignment
AgentAssignment is the per-agent configuration the machine app needs to run an
agent&#39;s drain loop. Pushed over the MachineChannel on CreateAgent, and sent
in full in ConnectMachineResponse.assigned_agents on (re)connect.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_name | [string](#string) |  | agents/{agent} |
| agent_display_name | [string](#string) |  | used in the init prompt |
| acp_config | [AgentACPConfig](#laelia-v1-AgentACPConfig) |  | server-owned per-agent ACP config |






<a name="laelia-v1-AgentConfigUpdate"></a>

### AgentConfigUpdate



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_name | [string](#string) |  |  |
| acp_config | [AgentACPConfig](#laelia-v1-AgentACPConfig) |  |  |






<a name="laelia-v1-ConnectMachineRequest"></a>

### ConnectMachineRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| registration_token | [string](#string) |  | first connection or after refresh failure |
| info | [MachineInfo](#laelia-v1-MachineInfo) |  |  |
| fingerprint | [string](#string) |  | client-generated connection fingerprint (hostname:os:arch) |






<a name="laelia-v1-ConnectMachineResponse"></a>

### ConnectMachineResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| access_token | [string](#string) |  | 15-minute validity |
| refresh_token | [string](#string) |  | 24-hour validity, single-use rotation |
| session_id | [string](#string) |  |  |
| access_token_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| initial_status | [MachineStatus](#laelia-v1-MachineStatus) |  |  |
| assigned_agents | [AgentAssignment](#laelia-v1-AgentAssignment) | repeated | The full set of agents this machine must host. The machine app opens one AgentChannel per entry immediately after connect (and on every reconnect). |






<a name="laelia-v1-CreateMachineRequest"></a>

### CreateMachineRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| machine | [Machine](#laelia-v1-Machine) |  |  |






<a name="laelia-v1-CreateMachineResponse"></a>

### CreateMachineResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| machine | [Machine](#laelia-v1-Machine) |  |  |
| registration_token | [string](#string) |  | 7-day validity, single-use on first connect |






<a name="laelia-v1-DeleteMachineRequest"></a>

### DeleteMachineRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-ForceDisconnectMachineRequest"></a>

### ForceDisconnectMachineRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="laelia-v1-GetMachineRequest"></a>

### GetMachineRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-ListMachineAgentsRequest"></a>

### ListMachineAgentsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListMachineAgentsResponse"></a>

### ListMachineAgentsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agents | [AgentSummary](#laelia-v1-AgentSummary) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-ListMachineWorkspacesRequest"></a>

### ListMachineWorkspacesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-ListMachineWorkspacesResponse"></a>

### ListMachineWorkspacesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| workspaces | [MachineWorkspaceSummary](#laelia-v1-MachineWorkspaceSummary) | repeated |  |






<a name="laelia-v1-ListMachinesRequest"></a>

### ListMachinesRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |
| show_deleted | [bool](#bool) |  |  |






<a name="laelia-v1-ListMachinesResponse"></a>

### ListMachinesResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| machines | [MachineSummary](#laelia-v1-MachineSummary) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-Machine"></a>

### Machine



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| state | [State](#laelia-v1-State) |  |  |
| title | [string](#string) |  |  |
| info | [MachineInfo](#laelia-v1-MachineInfo) |  |  |
| status | [MachineStatus](#laelia-v1-MachineStatus) |  |  |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| labels | [Machine.LabelsEntry](#laelia-v1-Machine-LabelsEntry) | repeated |  |
| created_by | [string](#string) |  | Creator&#39;s user resource name (users/{id}). |
| can_edit | [bool](#bool) |  | can_edit reports whether the current caller holds laelia.machines.edit (workspace-scope). |
| can_create_agent | [bool](#bool) |  | can_create_agent reports whether the current caller may create agents on this machine: the machine&#39;s creator, a workspace admin, or a principal bound to roles/machineAgentCreator in the machine&#39;s IAM policy. |
| can_manage | [bool](#bool) |  | can_manage reports whether the current caller may manage this machine&#39;s IAM policy (the machine&#39;s creator or a workspace admin). |






<a name="laelia-v1-Machine-LabelsEntry"></a>

### Machine.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-MachineDisconnectNotice"></a>

### MachineDisconnectNotice



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| reason | [string](#string) |  |  |






<a name="laelia-v1-MachineDisconnectRequest"></a>

### MachineDisconnectRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| reason | [string](#string) |  | &#34;shutdown&#34;, &#34;upgrade&#34; etc. |






<a name="laelia-v1-MachineHeartbeatRequest"></a>

### MachineHeartbeatRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |
| previous_nonce | [string](#string) |  | nonce from previous response (replay protection) |






<a name="laelia-v1-MachineHeartbeatResponse"></a>

### MachineHeartbeatResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| next_nonce | [string](#string) |  |  |
| next_heartbeat_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| access_token | [string](#string) |  | new access token (only if expiring soon) |
| access_token_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-MachineInfo"></a>

### MachineInfo



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| hostname | [string](#string) |  |  |
| os | [string](#string) |  |  |
| arch | [string](#string) |  |  |
| ip | [string](#string) |  |  |
| version | [string](#string) |  |  |
| labels | [MachineInfo.LabelsEntry](#laelia-v1-MachineInfo-LabelsEntry) | repeated |  |
| capability | [AgentCapability](#laelia-v1-AgentCapability) |  |  |
| available_providers | [AgentProviderInfo](#laelia-v1-AgentProviderInfo) | repeated | LLM agent providers auto-discovered by the machine app on its host. Machine-scoped: every agent hosted on this machine selects from this list. |






<a name="laelia-v1-MachineInfo-LabelsEntry"></a>

### MachineInfo.LabelsEntry



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| key | [string](#string) |  |  |
| value | [string](#string) |  |  |






<a name="laelia-v1-MachineReady"></a>

### MachineReady



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| session_id | [string](#string) |  |  |






<a name="laelia-v1-MachineStatus"></a>

### MachineStatus



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| state | [MachineStatus.ConnectionState](#laelia-v1-MachineStatus-ConnectionState) |  |  |
| last_heartbeat_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| connected_time | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| error_message | [string](#string) |  |  |
| active_session_id | [string](#string) |  |  |






<a name="laelia-v1-MachineStreamMessage"></a>

### MachineStreamMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| machine_ready | [MachineReady](#laelia-v1-MachineReady) |  | first message, carries the machine session id |
| ping | [Ping](#laelia-v1-Ping) |  |  |
| providers_discovered | [ProvidersDiscovered](#laelia-v1-ProvidersDiscovered) |  | response to DiscoverProviders |
| disconnect_notice | [MachineDisconnectNotice](#laelia-v1-MachineDisconnectNotice) |  | graceful shutdown |
| machine_workspace_scan_response | [MachineWorkspaceScanResponse](#laelia-v1-MachineWorkspaceScanResponse) |  | response to ManagerMachineStreamMessage.machine_workspace_scan_request |






<a name="laelia-v1-MachineSummary"></a>

### MachineSummary
MachineSummary is the lightweight list-view projection of a Machine returned
by ListMachines. It carries identity, lifecycle state, connection status, and
the count of agents bound to the machine.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| state | [State](#laelia-v1-State) |  |  |
| title | [string](#string) |  |  |
| status | [MachineStatus](#laelia-v1-MachineStatus) |  |  |
| agent_count | [int32](#int32) |  |  |
| created_by | [string](#string) |  | Creator&#39;s user resource name (users/{id}). |
| can_edit | [bool](#bool) |  | can_edit reports whether the current caller holds laelia.machines.edit (workspace-scope). |
| can_manage | [bool](#bool) |  | can_manage reports whether the current caller may manage this machine&#39;s IAM policy (the machine&#39;s creator or a workspace admin). |
| can_delete | [bool](#bool) |  | can_delete reports whether the current caller may delete this machine: the machine&#39;s creator or a holder of laelia.machines.delete. |






<a name="laelia-v1-MachineWorkspaceScanRequest"></a>

### MachineWorkspaceScanRequest
MachineWorkspaceScanRequest asks the machine app to summarize every
per-agent workspace directory under ~/.laelia/&lt;machineID&gt;/. The app replies
with MachineStreamMessage.machine_workspace_scan_response.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  | correlation id for the pending unary ListMachineWorkspaces call |






<a name="laelia-v1-MachineWorkspaceScanResponse"></a>

### MachineWorkspaceScanResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| request_id | [string](#string) |  |  |
| workspaces | [MachineWorkspaceSummary](#laelia-v1-MachineWorkspaceSummary) | repeated |  |






<a name="laelia-v1-MachineWorkspaceSummary"></a>

### MachineWorkspaceSummary
MachineWorkspaceSummary is one agent workspace directory&#39;s usage summary.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| directory_name | [string](#string) |  |  |
| total_size_bytes | [int64](#int64) |  |  |
| last_modified | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| file_count | [int64](#int64) |  |  |






<a name="laelia-v1-ManagerMachineStreamMessage"></a>

### ManagerMachineStreamMessage



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_assignment | [AgentAssignment](#laelia-v1-AgentAssignment) |  | host a new agent |
| remove_agent | [RemoveAgent](#laelia-v1-RemoveAgent) |  | tear down an agent&#39;s runner |
| agent_config_update | [AgentConfigUpdate](#laelia-v1-AgentConfigUpdate) |  | hot-reload an agent&#39;s ACP config |
| discover_providers | [DiscoverProviders](#laelia-v1-DiscoverProviders) |  | ask the machine to re-probe |
| pong | [Pong](#laelia-v1-Pong) |  |  |
| reload_agent_assignment | [ReloadAgentAssignment](#laelia-v1-ReloadAgentAssignment) |  | full re-sync of one agent |
| machine_workspace_scan_request | [MachineWorkspaceScanRequest](#laelia-v1-MachineWorkspaceScanRequest) |  | scan per-agent workspace directories on this machine |






<a name="laelia-v1-RefreshMachineProvidersRequest"></a>

### RefreshMachineProvidersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-RefreshMachineProvidersResponse"></a>

### RefreshMachineProvidersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| providers | [AgentProviderInfo](#laelia-v1-AgentProviderInfo) | repeated |  |






<a name="laelia-v1-RefreshMachineTokenRequest"></a>

### RefreshMachineTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| refresh_token | [string](#string) |  |  |
| fingerprint | [string](#string) |  |  |






<a name="laelia-v1-RefreshMachineTokenResponse"></a>

### RefreshMachineTokenResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| access_token | [string](#string) |  |  |
| refresh_token | [string](#string) |  | new refresh token (rotation) |
| access_token_expires_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |






<a name="laelia-v1-ReloadAgentAssignment"></a>

### ReloadAgentAssignment
ReloadAgentAssignment is a full re-sync of a single agent&#39;s assignment (used
after a config or display-name change, or to re-establish a runner).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_name | [string](#string) |  |  |
| assignment | [AgentAssignment](#laelia-v1-AgentAssignment) |  |  |






<a name="laelia-v1-RemoveAgent"></a>

### RemoveAgent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| agent_name | [string](#string) |  |  |






<a name="laelia-v1-RevokeMachineTokenRequest"></a>

### RevokeMachineTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="laelia-v1-RevokeMachineTokenResponse"></a>

### RevokeMachineTokenResponse







<a name="laelia-v1-RotateMachineTokenRequest"></a>

### RotateMachineTokenRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| reason | [string](#string) |  |  |






<a name="laelia-v1-RotateMachineTokenResponse"></a>

### RotateMachineTokenResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| registration_token | [string](#string) |  |  |





 


<a name="laelia-v1-MachineStatus-ConnectionState"></a>

### MachineStatus.ConnectionState


| Name | Number | Description |
| ---- | ------ | ----------- |
| CONNECTION_STATE_UNSPECIFIED | 0 |  |
| ONLINE | 1 |  |
| OFFLINE | 2 |  |
| ERROR | 3 |  |
| KICKED | 4 |  |


 

 


<a name="laelia-v1-MachineService"></a>

### MachineService
MachineService manages machines (a long-lived agent-application process a
user runs once on a host) and serves the machine-side authentication RPCs the
machine app calls to register itself. A machine authenticates once with a
registration token and then hosts one or more agents, each running its own
AgentChannel over the machine&#39;s access token.

========== Management APIs (IAM auth, admin only) ==========

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| CreateMachine | [CreateMachineRequest](#laelia-v1-CreateMachineRequest) | [CreateMachineResponse](#laelia-v1-CreateMachineResponse) |  |
| ListMachines | [ListMachinesRequest](#laelia-v1-ListMachinesRequest) | [ListMachinesResponse](#laelia-v1-ListMachinesResponse) |  |
| GetMachine | [GetMachineRequest](#laelia-v1-GetMachineRequest) | [Machine](#laelia-v1-Machine) |  |
| DeleteMachine | [DeleteMachineRequest](#laelia-v1-DeleteMachineRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | DeleteMachine soft-deletes a machine. Authorized in the handler for the machine&#39;s creator or a holder of laelia.machines.delete (workspace-scope); no permission annotation so the creator short-circuit can run. |
| RotateMachineToken | [RotateMachineTokenRequest](#laelia-v1-RotateMachineTokenRequest) | [RotateMachineTokenResponse](#laelia-v1-RotateMachineTokenResponse) | Token rotation: generate a new registration token; the machine app must re-ConnectMachine with it. Old tokens are revoked and all sessions dropped. Authorized in the handler for the machine&#39;s creator or a holder of laelia.machines.edit (workspace-scope); no permission annotation so the creator short-circuit can run. |
| RevokeMachineToken | [RevokeMachineTokenRequest](#laelia-v1-RevokeMachineTokenRequest) | [RevokeMachineTokenResponse](#laelia-v1-RevokeMachineTokenResponse) | Token revocation: revoke all tokens for the machine. Authorized in the handler for the machine&#39;s creator or a holder of laelia.machines.edit (workspace-scope); no permission annotation so the creator short-circuit can run. |
| ForceDisconnectMachine | [ForceDisconnectMachineRequest](#laelia-v1-ForceDisconnectMachineRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | Force-disconnects a machine: terminate all its sessions and fail all in-flight commands for every agent hosted on it. Authorized in the handler for the machine&#39;s creator or a holder of laelia.machines.edit (workspace-scope); no permission annotation so the creator short-circuit can run. |
| ListMachineAgents | [ListMachineAgentsRequest](#laelia-v1-ListMachineAgentsRequest) | [ListMachineAgentsResponse](#laelia-v1-ListMachineAgentsResponse) | List the agents hosted on a machine. |
| RefreshMachineProviders | [RefreshMachineProvidersRequest](#laelia-v1-RefreshMachineProvidersRequest) | [RefreshMachineProvidersResponse](#laelia-v1-RefreshMachineProvidersResponse) | Ask the machine app to re-probe its host for installed LLM agent providers and their models. Returns the freshly discovered provider list (also persisted into machine.info.available_providers). Authorized in the handler for the machine&#39;s creator or a holder of laelia.machines.edit (workspace-scope); no permission annotation so the creator short-circuit can run. |
| ListMachineWorkspaces | [ListMachineWorkspacesRequest](#laelia-v1-ListMachineWorkspacesRequest) | [ListMachineWorkspacesResponse](#laelia-v1-ListMachineWorkspacesResponse) | ListMachineWorkspaces summarizes every per-agent workspace directory on a machine (~/.laelia/&lt;machineID&gt;/). Workspace content is sensitive: authorized in the handler for the machine&#39;s creator or a workspace admin (isMachineAdmin, matching Machine.can_manage); no permission annotation. |
| ConnectMachine | [ConnectMachineRequest](#laelia-v1-ConnectMachineRequest) | [ConnectMachineResponse](#laelia-v1-ConnectMachineResponse) | Machine initial connection using a registration token. Returns access &#43; refresh tokens, the machine session id, and the full list of agents the machine must host (so the machine app can open an AgentChannel for each). |
| MachineHeartbeat | [MachineHeartbeatRequest](#laelia-v1-MachineHeartbeatRequest) | [MachineHeartbeatResponse](#laelia-v1-MachineHeartbeatResponse) |  |
| MachineDisconnect | [MachineDisconnectRequest](#laelia-v1-MachineDisconnectRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |
| RefreshMachineToken | [RefreshMachineTokenRequest](#laelia-v1-RefreshMachineTokenRequest) | [RefreshMachineTokenResponse](#laelia-v1-RefreshMachineTokenResponse) |  |


<a name="laelia-v1-MachineStreamService"></a>

### MachineStreamService
MachineStreamService is the machine-level control channel. It is separate
from the per-agent AgentStreamService.AgentChannel data plane: the
MachineChannel carries agent assignment (add/remove/config-update), provider
discovery, and liveness ping/pong, while each agent&#39;s drain loop runs over
its own AgentChannel.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| MachineChannel | [MachineStreamMessage](#laelia-v1-MachineStreamMessage) stream | [ManagerMachineStreamMessage](#laelia-v1-ManagerMachineStreamMessage) stream |  |

 



<a name="v1_mcp-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/mcp.proto



<a name="laelia-v1-CallMcpToolRequest"></a>

### CallMcpToolRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mcp_server_id | [string](#string) |  |  |
| tool_name | [string](#string) |  |  |
| arguments | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| expected_config_version | [int64](#int64) |  |  |
| expected_assignment_version | [int64](#int64) |  |  |






<a name="laelia-v1-CallMcpToolResponse"></a>

### CallMcpToolResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| content | [McpContentBlock](#laelia-v1-McpContentBlock) | repeated | Content blocks of the MCP tool result (text/image). |
| is_error | [bool](#bool) |  |  |
| structured_content | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |






<a name="laelia-v1-CreateMcpServerRequest"></a>

### CreateMcpServerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mcp_server | [McpServer](#laelia-v1-McpServer) |  |  |






<a name="laelia-v1-DeleteMcpServerRequest"></a>

### DeleteMcpServerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-GetMcpCatalogRequest"></a>

### GetMcpCatalogRequest







<a name="laelia-v1-GetMcpCatalogResponse"></a>

### GetMcpCatalogResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| catalog_version | [int32](#int32) |  | catalog_version is the catalog contract version. Currently always 1. |
| tools | [McpTool](#laelia-v1-McpTool) | repeated |  |






<a name="laelia-v1-GetMcpServerRequest"></a>

### GetMcpServerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |






<a name="laelia-v1-ListMcpServersRequest"></a>

### ListMcpServersRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| page_size | [int32](#int32) |  |  |
| page_token | [string](#string) |  |  |






<a name="laelia-v1-ListMcpServersResponse"></a>

### ListMcpServersResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mcp_servers | [McpServer](#laelia-v1-McpServer) | repeated |  |
| next_page_token | [string](#string) |  |  |






<a name="laelia-v1-McpContentBlock"></a>

### McpContentBlock



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| text | [McpTextContent](#laelia-v1-McpTextContent) |  |  |
| image | [McpImageContent](#laelia-v1-McpImageContent) |  |  |






<a name="laelia-v1-McpHeader"></a>

### McpHeader
McpHeader is one HTTP header attached to MCP transport requests. On write a
masked (&#34;****&#34;-prefixed) or empty value means &#34;keep the stored value&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| value | [string](#string) |  |  |
| masked_value | [string](#string) |  |  |






<a name="laelia-v1-McpHttpTransport"></a>

### McpHttpTransport
McpHttpTransport is a streamable-HTTP MCP transport.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| url | [string](#string) |  | The MCP streamable HTTP endpoint URL, e.g. &#34;https://mcp.example.com/mcp&#34;. |
| headers | [McpHeader](#laelia-v1-McpHeader) | repeated | Headers sent with every request to the MCP server (typically Authorization). Values are stored server-side and masked on read. |






<a name="laelia-v1-McpImageContent"></a>

### McpImageContent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| data | [string](#string) |  | base64-encoded image data |
| mime_type | [string](#string) |  |  |






<a name="laelia-v1-McpServer"></a>

### McpServer
McpServer is a managed MCP service. WORKSPACE servers are admin-managed and
shared through the members list; USER servers are private to their creator.
The manager holds the full transport configuration (URL and header values)
and only exposes a per-agent tool catalog to machines; header values are
masked on read.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | The resource name of the MCP server, in the form `mcpServers/{id}`. |
| title | [string](#string) |  | Human-readable title. |
| description | [string](#string) |  | Longer description of the MCP server. |
| http | [McpHttpTransport](#laelia-v1-McpHttpTransport) |  | Streamable HTTP transport. |
| sse | [McpSseTransport](#laelia-v1-McpSseTransport) |  | SSE transport. |
| members | [string](#string) | repeated | Users or groups allowed to use this MCP server, in IAM member format: `users/{uid}`, `groups/{email}`, `groups/{id}`, or `allUsers`. Access is checked when an agent references the server (and again at call time). |
| created_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| updated_at | [google.protobuf.Timestamp](#google-protobuf-Timestamp) |  |  |
| created_by | [string](#string) |  | Creator&#39;s user resource name (users/{id}). Display-only. |
| config_version | [int64](#int64) |  | config_version increments on every update. The agent gateway returns it in the tool catalog and re-checks it at call time so stale catalogs fail closed instead of silently running against changed server configuration. |
| scope | [McpServerScope](#laelia-v1-McpServerScope) |  | Scope of the server. On Create, WORKSPACE requires the management permission and USER requires the &#34;users may configure MCP servers&#34; workspace setting; the value is fixed for the lifetime of the server. |






<a name="laelia-v1-McpServerChange"></a>

### McpServerChange
McpServerChange is the audit payload recorded for a successful
CreateMcpServer/UpdateMcpServer. It carries only the server resource name —
never header values.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| server | [string](#string) |  |  |






<a name="laelia-v1-McpSseTransport"></a>

### McpSseTransport
McpSseTransport is an SSE MCP transport. The messages endpoint is derived
from the standard MCP SSE layout (`GET url` for the event stream and
`POST /messages?session_id=...` on the same origin).


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| url | [string](#string) |  | The MCP SSE endpoint URL, e.g. &#34;https://mcp.example.com/sse&#34;. |
| headers | [McpHeader](#laelia-v1-McpHeader) | repeated | Headers sent with requests to the MCP server. Values are stored server-side and masked on read. |






<a name="laelia-v1-McpTextContent"></a>

### McpTextContent



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| text | [string](#string) |  |  |






<a name="laelia-v1-McpTool"></a>

### McpTool
McpTool is one tool in a per-agent MCP tool catalog. The catalog is computed
by the manager from the agent&#39;s enabled MCP servers and the servers&#39; current
tool lists; machines never see the transport configuration.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mcp_server_id | [string](#string) |  | mcp_servers/{id} the tool belongs to. |
| server_name | [string](#string) |  | Display name of the MCP server. |
| tool_name | [string](#string) |  | The real MCP tool name on the server. |
| runtime_name | [string](#string) |  | The name exposed to the runtime; collision-scoped per server. |
| title | [string](#string) |  |  |
| description | [string](#string) |  |  |
| input_schema | [google.protobuf.Struct](#google-protobuf-Struct) |  |  |
| config_version | [int64](#int64) |  |  |
| assignment_version | [int64](#int64) |  |  |
| server_description | [string](#string) |  | Display description of the MCP server. |






<a name="laelia-v1-UpdateMcpServerRequest"></a>

### UpdateMcpServerRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| mcp_server | [McpServer](#laelia-v1-McpServer) |  |  |
| update_mask | [google.protobuf.FieldMask](#google-protobuf-FieldMask) |  |  |





 


<a name="laelia-v1-McpServerScope"></a>

### McpServerScope
McpServerScope distinguishes workspace-global servers (admin-managed) from
personal servers (owned by a single user and usable only by that user).

| Name | Number | Description |
| ---- | ------ | ----------- |
| MCP_SERVER_SCOPE_UNSPECIFIED | 0 |  |
| MCP_SERVER_SCOPE_WORKSPACE | 1 |  |
| MCP_SERVER_SCOPE_USER | 2 |  |


 

 


<a name="laelia-v1-McpGatewayService"></a>

### McpGatewayService
McpGatewayService is the agent-facing gateway: machines call it to fetch the
current authorized MCP tool catalog for an agent and to invoke allowlisted
tools. Callers are authenticated as an agent (machine token &#43; X-Laelia-Agent
header); every call re-checks the caller&#39;s current authorization.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetMcpCatalog | [GetMcpCatalogRequest](#laelia-v1-GetMcpCatalogRequest) | [GetMcpCatalogResponse](#laelia-v1-GetMcpCatalogResponse) |  |
| CallMcpTool | [CallMcpToolRequest](#laelia-v1-CallMcpToolRequest) | [CallMcpToolResponse](#laelia-v1-CallMcpToolResponse) |  |


<a name="laelia-v1-McpServerService"></a>

### McpServerService
McpServerService manages the MCP server registry. Get/Create/Update/Delete
are handler-gated: workspace servers require the laelia.mcpServers.*
permissions, while personal servers may be managed by their owner.
ListMcpServers returns workspace servers only (admin: all; other callers:
the servers they may use). ListMyMcpServers returns the caller&#39;s personal
servers; ListUserMcpServers is an admin read-only view of every personal
server.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetMcpServer | [GetMcpServerRequest](#laelia-v1-GetMcpServerRequest) | [McpServer](#laelia-v1-McpServer) |  |
| ListMcpServers | [ListMcpServersRequest](#laelia-v1-ListMcpServersRequest) | [ListMcpServersResponse](#laelia-v1-ListMcpServersResponse) |  |
| ListMyMcpServers | [ListMcpServersRequest](#laelia-v1-ListMcpServersRequest) | [ListMcpServersResponse](#laelia-v1-ListMcpServersResponse) |  |
| ListUserMcpServers | [ListMcpServersRequest](#laelia-v1-ListMcpServersRequest) | [ListMcpServersResponse](#laelia-v1-ListMcpServersResponse) |  |
| CreateMcpServer | [CreateMcpServerRequest](#laelia-v1-CreateMcpServerRequest) | [McpServer](#laelia-v1-McpServer) |  |
| UpdateMcpServer | [UpdateMcpServerRequest](#laelia-v1-UpdateMcpServerRequest) | [McpServer](#laelia-v1-McpServer) |  |
| DeleteMcpServer | [DeleteMcpServerRequest](#laelia-v1-DeleteMcpServerRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) |  |

 



<a name="v1_notification-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## v1/notification.proto



<a name="laelia-v1-CreatePushSubscriptionRequest"></a>

### CreatePushSubscriptionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| endpoint | [string](#string) |  | endpoint is the push service endpoint URL returned by PushSubscription.endpoint. |
| p256dh | [string](#string) |  | p256dh is the base64url ECDH P-256 public key from the subscription&#39;s keys. |
| auth | [string](#string) |  | auth is the base64url 16-byte auth secret from the subscription&#39;s keys. |






<a name="laelia-v1-DeletePushSubscriptionRequest"></a>

### DeletePushSubscriptionRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  | name is &#34;users/{user}/pushSubscriptions/{endpointKey}&#34;. |






<a name="laelia-v1-GetPushConfigRequest"></a>

### GetPushConfigRequest







<a name="laelia-v1-GetPushConfigResponse"></a>

### GetPushConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| enabled | [bool](#bool) |  | enabled is false when the manager has no VAPID keys configured; the frontend must not offer to subscribe in that case. |
| vapid_public_key | [string](#string) |  | vapid_public_key is the base64url VAPID public key, only meaningful when enabled is true. |
| http_proxy | [string](#string) |  | http_proxy is the configured outbound HTTP proxy for push delivery, only populated for admins (callers with laelia.pushConfig.update). Empty for non-admins or when no proxy is configured. |






<a name="laelia-v1-ListPushSubscriptionsRequest"></a>

### ListPushSubscriptionsRequest







<a name="laelia-v1-ListPushSubscriptionsResponse"></a>

### ListPushSubscriptionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| push_subscriptions | [PushSubscription](#laelia-v1-PushSubscription) | repeated | push_subscriptions are the caller&#39;s registered browser push endpoints, one per device/browser, ordered by creation time (oldest first). |






<a name="laelia-v1-PushSubscription"></a>

### PushSubscription
PushSubscription is a registered browser push endpoint for a user. The
resource name is &#34;users/{user}/pushSubscriptions/{endpointKey}&#34;.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| name | [string](#string) |  |  |
| endpoint | [string](#string) |  |  |






<a name="laelia-v1-UpdatePushConfigRequest"></a>

### UpdatePushConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| http_proxy | [string](#string) |  | http_proxy is the outbound HTTP(S) proxy URL, or empty to disable the proxy (direct connection). Only http:// and https:// schemes are accepted. |






<a name="laelia-v1-UpdatePushConfigResponse"></a>

### UpdatePushConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| http_proxy | [string](#string) |  | http_proxy echoes the stored proxy value (empty when the proxy is off). |





 

 

 


<a name="laelia-v1-NotificationService"></a>

### NotificationService
NotificationService manages per-user browser Web Push subscriptions so the
manager can deliver system notifications for directed messages (mentions,
thread replies, task/reminder updates, and 1:1 DMs) even when the user&#39;s
browser tab is closed. All RPCs are user-scoped: the caller&#39;s own principal
id is the implicit owner, mirroring ListActivities. The VAPID keypair is
auto-generated on first boot and stored in the setting table, so no env
config is required; GetPushConfig reports enabled=false if the keypair is
somehow absent. An optional outbound HTTP proxy (for networks that cannot
reach browser push services directly) is admin-configurable via
UpdatePushConfig.

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| GetPushConfig | [GetPushConfigRequest](#laelia-v1-GetPushConfigRequest) | [GetPushConfigResponse](#laelia-v1-GetPushConfigResponse) | GetPushConfig reports whether Web Push is enabled and, when it is, returns the VAPID public key the browser needs to subscribe. The http_proxy field is populated only for callers holding laelia.pushConfig.update (admins); other callers receive it empty. |
| UpdatePushConfig | [UpdatePushConfigRequest](#laelia-v1-UpdatePushConfigRequest) | [UpdatePushConfigResponse](#laelia-v1-UpdatePushConfigResponse) | UpdatePushConfig sets the optional outbound HTTP proxy used when the manager posts notifications to browser push services. Admin-only. An empty http_proxy disables the proxy (direct connection). The change takes effect immediately on the running manager. |
| ListPushSubscriptions | [ListPushSubscriptionsRequest](#laelia-v1-ListPushSubscriptionsRequest) | [ListPushSubscriptionsResponse](#laelia-v1-ListPushSubscriptionsResponse) | ListPushSubscriptions returns every push subscription registered for the authenticated user, one per device/browser. The frontend uses it to render whether the current browser is subscribed and to reconcile a browser-side subscription that is missing server-side. |
| CreatePushSubscription | [CreatePushSubscriptionRequest](#laelia-v1-CreatePushSubscriptionRequest) | [PushSubscription](#laelia-v1-PushSubscription) | CreatePushSubscription registers a browser push subscription for the authenticated user. Idempotent on (user, endpoint): re-subscribing the same browser refreshes its p256dh/auth keys. Returns FailedPrecondition when Web Push is disabled. |
| DeletePushSubscription | [DeletePushSubscriptionRequest](#laelia-v1-DeletePushSubscriptionRequest) | [.google.protobuf.Empty](#google-protobuf-Empty) | DeletePushSubscription removes a push subscription for the authenticated user. The name is &#34;users/{user}/pushSubscriptions/{endpointKey}&#34; where endpointKey is the URL-safe base64 of the subscription endpoint; the name&#39;s user must be the caller. |

 



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






<a name="laelia-v1-GetLlmAgentConfigRequest"></a>

### GetLlmAgentConfigRequest







<a name="laelia-v1-GetLlmAgentConfigResponse"></a>

### GetLlmAgentConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.LlmAgentConfigSetting](#laelia-store-LlmAgentConfigSetting) |  |  |






<a name="laelia-v1-GetS3ConfigRequest"></a>

### GetS3ConfigRequest







<a name="laelia-v1-GetS3ConfigResponse"></a>

### GetS3ConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.S3ConfigSetting](#laelia-store-S3ConfigSetting) |  |  |






<a name="laelia-v1-GetSetupStatusRequest"></a>

### GetSetupStatusRequest







<a name="laelia-v1-GetSetupStatusResponse"></a>

### GetSetupStatusResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| items | [SetupItem](#laelia-v1-SetupItem) | repeated |  |






<a name="laelia-v1-GetUserMcpConfigRequest"></a>

### GetUserMcpConfigRequest







<a name="laelia-v1-GetUserMcpConfigResponse"></a>

### GetUserMcpConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.UserMcpConfigSetting](#laelia-store-UserMcpConfigSetting) |  |  |






<a name="laelia-v1-SetupItem"></a>

### SetupItem
SetupItem describes one required-config item the admin onboarding overlay
surfaces. The backend is the source of truth for `configured`; the frontend
owns presentation (title/description/route) keyed by `id`.


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| configured | [bool](#bool) |  |  |






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






<a name="laelia-v1-UpdateLlmAgentConfigRequest"></a>

### UpdateLlmAgentConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.LlmAgentConfigSetting](#laelia-store-LlmAgentConfigSetting) |  |  |






<a name="laelia-v1-UpdateLlmAgentConfigResponse"></a>

### UpdateLlmAgentConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.LlmAgentConfigSetting](#laelia-store-LlmAgentConfigSetting) |  |  |






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






<a name="laelia-v1-UpdateUserMcpConfigRequest"></a>

### UpdateUserMcpConfigRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.UserMcpConfigSetting](#laelia-store-UserMcpConfigSetting) |  |  |






<a name="laelia-v1-UpdateUserMcpConfigResponse"></a>

### UpdateUserMcpConfigResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| config | [laelia.store.UserMcpConfigSetting](#laelia-store-UserMcpConfigSetting) |  |  |





 

 

 


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
| GetLlmAgentConfig | [GetLlmAgentConfigRequest](#laelia-v1-GetLlmAgentConfigRequest) | [GetLlmAgentConfigResponse](#laelia-v1-GetLlmAgentConfigResponse) | GetLlmAgentConfig reads the workspace LLM agent configuration. It is handler-gated (no permission annotation) so the agent create/edit forms — which members use — can read the toggle without a settings permission. |
| UpdateLlmAgentConfig | [UpdateLlmAgentConfigRequest](#laelia-v1-UpdateLlmAgentConfigRequest) | [UpdateLlmAgentConfigResponse](#laelia-v1-UpdateLlmAgentConfigResponse) | UpdateLlmAgentConfig updates the workspace LLM agent configuration. Admin (laelia.settings.update) only. |
| GetUserMcpConfig | [GetUserMcpConfigRequest](#laelia-v1-GetUserMcpConfigRequest) | [GetUserMcpConfigResponse](#laelia-v1-GetUserMcpConfigResponse) | GetUserMcpConfig reads whether users may configure personal MCP servers. It is handler-gated (no permission annotation) so any authenticated user can render the personal MCP settings page. |
| UpdateUserMcpConfig | [UpdateUserMcpConfigRequest](#laelia-v1-UpdateUserMcpConfigRequest) | [UpdateUserMcpConfigResponse](#laelia-v1-UpdateUserMcpConfigResponse) | UpdateUserMcpConfig updates whether users may configure personal MCP servers. Admin (laelia.settings.update) only. |
| GetSetupStatus | [GetSetupStatusRequest](#laelia-v1-GetSetupStatusRequest) | [GetSetupStatusResponse](#laelia-v1-GetSetupStatusResponse) | GetSetupStatus reports which required-config items are not yet configured, so the frontend can guide an admin to finish setting up the workspace. |
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

