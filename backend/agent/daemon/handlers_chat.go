package daemon

import (
	"net/http"

	"github.com/Ranxy/laelia/backend/agent/chattools"
)

func (s *Server) handleMessageCheck(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListChannelUpdates(r.Context(), s.deps(req))
		return text, asChatError(err)
	})
}

func (s *Server) handleMessageRead(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.GetConversationMessages(r.Context(), s.deps(req), chattools.GetConversationMessagesInput{
			Conversation: req.Conversation,
			Version:      req.Version,
			Direction:    req.Direction,
			Limit:        req.Limit,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleMessageSearch(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.SearchChatHistory(r.Context(), s.deps(req), chattools.SearchChatHistoryInput{
			Conversation: req.Conversation,
			Query:        req.Query,
			Since:        req.Since,
			Limit:        req.Limit,
			PageToken:    req.PageToken,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleMessageAck(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.AckProcessedVersion(r.Context(), s.deps(req), chattools.AckProcessedVersionInput{
			Conversation:     req.Conversation,
			ProcessedVersion: req.ProcessedVersion,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleMessageSend(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.PostMessage(r.Context(), s.deps(req), chattools.PostMessageInput{
			Conversation:  req.Conversation,
			Content:       req.Content,
			BaseVersion:   req.BaseVersion,
			AttachmentIDs: req.AttachmentIDs,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReactionAdd(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.AddReaction(r.Context(), s.deps(req), chattools.ReactionInput{
			Message: req.Message,
			Emoji:   req.ReactionEmoji,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReactionRemove(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.RemoveReaction(r.Context(), s.deps(req), chattools.ReactionInput{
			Message: req.Message,
			Emoji:   req.ReactionEmoji,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleThreadCheck(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListThreadUpdates(r.Context(), s.deps(req), chattools.ListThreadUpdatesInput{})
		return text, asChatError(err)
	})
}

func (s *Server) handleThreadRead(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.GetThreadMessages(r.Context(), s.deps(req), chattools.GetThreadMessagesInput{
			Conversation: req.Conversation,
			Root:         req.Root,
			Version:      req.Version,
			Direction:    req.Direction,
			Limit:        req.Limit,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleThreadSend(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.PostThreadMessage(r.Context(), s.deps(req), chattools.PostThreadMessageInput{
			Conversation:  req.Conversation,
			Root:          req.Root,
			Content:       req.Content,
			BaseVersion:   req.BaseVersion,
			AttachmentIDs: req.AttachmentIDs,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleTeamGet(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.GetMyTeam(r.Context(), s.deps(req), chattools.GetMyTeamInput{})
		return text, asChatError(err)
	})
}

func (s *Server) handleTeamShow(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.GetTeamByID(r.Context(), s.deps(req), chattools.GetTeamByIDInput{TeamID: req.TeamID})
		return text, asChatError(err)
	})
}

func (s *Server) handleCommandContext(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.GetCommandContext(r.Context(), s.deps(req), chattools.GetCommandContextInput{
			CommandID: req.CommandID,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListTasks(r.Context(), s.deps(req), chattools.ListTasksInput{
			Conversation: req.Conversation,
			Statuses:     req.Statuses,
			PageSize:     req.PageSize,
			PageToken:    req.PageToken,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleTaskClaim(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ClaimTask(r.Context(), s.deps(req), chattools.ClaimTaskInput{
			Message: req.Message,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleTaskUnclaim(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.UnclaimTask(r.Context(), s.deps(req), chattools.UnclaimTaskInput{
			Message: req.Message,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleTaskUpdate(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.UpdateTaskStatus(r.Context(), s.deps(req), chattools.UpdateTaskStatusInput{
			Message: req.Message,
			Status:  req.Status,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleTaskCreate(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.CreateTask(r.Context(), s.deps(req), chattools.CreateTaskInput{
			Conversation:  req.Conversation,
			Content:       req.Content,
			AttachmentIDs: req.AttachmentIDs,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderConvert(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ConvertMessageToReminder(r.Context(), s.deps(req), chattools.ConvertMessageToReminderInput{
			Message:     req.Message,
			TaskContent: req.Content,
			FireAt:      req.FireAt,
			CronExpr:    req.CronExpr,
			Tz:          req.Tz,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderList(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListReminders(r.Context(), s.deps(req), chattools.ListRemindersInput{
			Conversation: req.Conversation,
			Statuses:     req.Statuses,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderListDue(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListDueReminders(r.Context(), s.deps(req), chattools.ListDueRemindersInput{})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderUpdate(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.UpdateReminder(r.Context(), s.deps(req), chattools.UpdateReminderInput{
			Name:        req.Name,
			TaskContent: req.Content,
			FireAt:      req.FireAt,
			CronExpr:    req.CronExpr,
			Tz:          req.Tz,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderCancel(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.CancelReminder(r.Context(), s.deps(req), chattools.CancelReminderInput{
			Name: req.Name,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderComplete(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.CompleteReminder(r.Context(), s.deps(req), chattools.CompleteReminderInput{
			Name:   req.Name,
			Result: req.Result,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleReminderFail(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.FailReminder(r.Context(), s.deps(req), chattools.FailReminderInput{
			Name:  req.Name,
			Error: req.Error,
		})
		return text, asChatError(err)
	})
}

// asChatError narrows an error to *chattools.Error; non-chattools errors
// (should not happen) are reported as a generic server failure.
func asChatError(err error) *chattools.Error {
	if err == nil {
		return nil
	}
	if e, ok := err.(*chattools.Error); ok {
		return e
	}
	return &chattools.Error{Code: "SERVER_5XX", Message: err.Error()}
}

// handleMembers serves the single roster tool: conversation members when Root is
// empty, thread participants when Root is set. Each entry carries the member's
// public description inline, so the agent perceives who is present and each
// co-agent's public description in one call.
func (s *Server) handleMembers(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListMembers(r.Context(), s.deps(req), chattools.ListMembersInput{
			Conversation: req.Conversation,
			Root:         req.Root,
		})
		return text, asChatError(err)
	})
}

// handleAgentList serves the global peer-agent roster: every other agent with
// its display name, agents/<id> handle, connection state, and public
// description. It is the discovery tool the agent uses before delegating to a
// peer via `message send dm:@<peer>`.
func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListPeerAgents(r.Context(), s.deps(req), chattools.ListPeerAgentsInput{})
		return text, asChatError(err)
	})
}

// handleChannelList serves the on-demand channel discovery tool: every
// conversation the agent can read (its memberships plus, when
// follow_owner_permissions is enabled, its owner's channels/DMs), each tagged
// [joined] or [visible].
func (s *Server) handleChannelList(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListAccessibleChannels(r.Context(), s.deps(req), chattools.ListAccessibleChannelsInput{})
		return text, asChatError(err)
	})
}

// handleChannelJoin makes the agent a real member of a channel it can read,
// seeding its cursor so the channel appears in `message check` from then on.
func (s *Server) handleChannelJoin(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.JoinChannel(r.Context(), s.deps(req), chattools.JoinChannelInput{
			Conversation: req.Conversation,
		})
		return text, asChatError(err)
	})
}

// handleChannelLeave removes the agent from a channel it is a member of, so the
// channel stops appearing in `message check` and the agent can no longer post.
func (s *Server) handleChannelLeave(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.LeaveChannel(r.Context(), s.deps(req), chattools.LeaveChannelInput{
			Conversation: req.Conversation,
		})
		return text, asChatError(err)
	})
}

// handleChannelAddMember adds members (users or agents) to a channel the agent
// manages. The manager enforces the same rules as a user adding members: the
// caller must hold conversations.manageMembers (channel Admin/Owner, or an agent
// whose owner is a channel Admin/Owner with can_manage_channel_members enabled),
// and a private agent (allow_add_to_channel=false) cannot be added by another
// agent.
func (s *Server) handleChannelAddMember(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.AddChannelMember(r.Context(), s.deps(req), chattools.AddChannelMemberInput{
			Conversation: req.Conversation,
			Members:      req.Members,
		})
		return text, asChatError(err)
	})
}

// handleChannelRemoveMember removes members (users or agents) from a channel the
// agent manages, under the same conversations.manageMembers rule as adding.
func (s *Server) handleChannelRemoveMember(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.RemoveChannelMember(r.Context(), s.deps(req), chattools.RemoveChannelMemberInput{
			Conversation: req.Conversation,
			Members:      req.Members,
		})
		return text, asChatError(err)
	})
}
