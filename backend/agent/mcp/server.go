package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

type contextKey string

const (
	ctxKeyAgentID        contextKey = "laelia_agent_id"
	ctxKeyPrincipalID    contextKey = "laelia_principal_id"
	ctxKeyConversationID contextKey = "laelia_conversation_id"
	ctxKeyCommandID      contextKey = "laelia_command_id"
)

type Server struct {
	mcpServer   *mcp.Server
	httpServer  *http.Server
	port        int
	managerURL  string
	agentName   string
	getToken    func() string
	httpClient  *http.Client
	clientCache v1connect.CommandServiceClient
}

func New(managerURL, agentName string, getToken func() string, httpClient *http.Client) (*Server, error) {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "laelia-chat", Version: "1.0.0"},
		&mcp.ServerOptions{},
	)

	ms := &Server{
		mcpServer:  srv,
		managerURL: managerURL,
		agentName:  agentName,
		getToken:   getToken,
		httpClient: httpClient,
	}

	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "search_chat_history",
			Description: "Search past chat messages by keyword and optional time range. Returns matching user messages and agent replies.",
		},
		ms.handleSearchChatHistory,
	)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "get_command_context",
			Description: "Get the full execution context (thinking process, tool calls, outputs) behind a specific agent reply, by its command/message ID.",
		},
		ms.handleGetCommandContext,
	)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "get_conversation_messages",
			Description: "Get messages from a conversation, with the current room version. Pass conversation (e.g. \"conversations/<id>\") and after_version to fetch only messages newer than a known version (use the processed_version returned by list_channel_updates). You MUST call this before post_message to obtain the latest base_version.",
		},
		ms.handleGetConversationMessages,
	)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "post_message",
			Description: "Post a reply to a conversation. Pass conversation (e.g. \"conversations/<id>\"), content, and base_version (the current_version from get_conversation_messages). If committed=false, new messages arrived while you were thinking — read them, reconsider, and call post_message again with the updated base_version.",
		},
		ms.handlePostMessage,
	)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "list_channel_updates",
			Description: "Call this first, every session. Returns the channels you are a member of that have unread messages, each with its conversation name, current_version, your processed_version, and new_message_count. If the list is empty, you are idle — end your turn without calling any other tool.",
		},
		ms.handleListChannelUpdates,
	)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "ack_processed_version",
			Description: "Advance your durable per-channel cursor to processed_version so the channel no longer reports as unread. Call this after you finish processing a channel — whether or not you replied — so you don't re-read it next session. Pass conversation (e.g. \"conversations/<id>\") and processed_version (the current_version from get_conversation_messages).",
		},
		ms.handleAckProcessedVersion,
	)

	return ms, nil
}

func (s *Server) client() v1connect.CommandServiceClient {
	if s.clientCache == nil {
		s.clientCache = v1connect.NewCommandServiceClient(
			s.httpClient,
			s.managerURL,
			connect.WithInterceptors(s.authInterceptor()),
		)
	}
	return s.clientCache
}

func (s *Server) authInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token := s.getToken(); token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			}
			return next(ctx, req)
		}
	}
}

func (s *Server) Start() error {
	handler := contextMiddleware(
		mcp.NewStreamableHTTPHandler(
			func(_ *http.Request) *mcp.Server { return s.mcpServer },
			&mcp.StreamableHTTPOptions{Stateless: true},
		),
	)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return errors.Wrap(err, "failed to bind MCP server")
	}
	s.port = listener.Addr().(*net.TCPAddr).Port

	s.httpServer = &http.Server{Handler: handler}
	go func() {
		slog.Info("MCP HTTP server started", "port", s.port)
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("MCP HTTP server error", "error", err)
		}
	}()
	return nil
}

func (s *Server) Port() int { return s.port }

func (s *Server) Stop() {
	if s.httpServer != nil {
		_ = s.httpServer.Close()
	}
}

func contextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if aid := r.URL.Query().Get("agent"); aid != "" {
			ctx = context.WithValue(ctx, ctxKeyAgentID, aid)
		}
		if pid := r.URL.Query().Get("principal"); pid != "" {
			ctx = context.WithValue(ctx, ctxKeyPrincipalID, pid)
		}
		if cid := r.URL.Query().Get("conversation"); cid != "" {
			ctx = context.WithValue(ctx, ctxKeyConversationID, cid)
		}
		if cmd := r.URL.Query().Get("command"); cmd != "" {
			ctx = context.WithValue(ctx, ctxKeyCommandID, cmd)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// resolveConversationName picks the conversation name from the tool input,
// falling back to the conversation pinned in the MCP URL. It normalizes a raw
// conversation ID into the "conversations/<id>" form the manager expects.
// Returns "" when neither is available.
func resolveConversationName(ctx context.Context, input string) string {
	if input != "" {
		return normalizeConversationName(input)
	}
	if v, ok := ctx.Value(ctxKeyConversationID).(string); ok && v != "" {
		return normalizeConversationName(v)
	}
	return ""
}

func normalizeConversationName(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "conversations/") {
		return s
	}
	return fmt.Sprintf("conversations/%s", s)
}

type searchChatHistoryInput struct {
	Conversation string `json:"conversation,omitempty"`
	Query        string `json:"query"`
	Since        string `json:"since,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	PageToken    string `json:"page_token,omitempty"`
}

type chatHistoryResult struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	SenderType string `json:"sender_type"`
	IsOwn      bool   `json:"is_own"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

type searchChatHistoryOutput struct {
	Results       []chatHistoryResult `json:"results"`
	NextPageToken string              `json:"next_page_token,omitempty"`
}

func (s *Server) handleSearchChatHistory(ctx context.Context, _ *mcp.CallToolRequest, input searchChatHistoryInput) (*mcp.CallToolResult, searchChatHistoryOutput, error) {
	conversationName := resolveConversationName(ctx, input.Conversation)

	limit := input.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	reqMsg := &v1pb.SearchChatHistoryRequest{
		Query:     input.Query,
		Limit:     int32(limit),
		PageToken: input.PageToken,
	}
	if conversationName != "" {
		reqMsg.Conversation = conversationName
	}
	req := connect.NewRequest(reqMsg)

	resp, err := s.client().SearchChatHistory(ctx, req)
	if err != nil {
		return nil, searchChatHistoryOutput{}, errors.Wrap(err, "failed to search chat history")
	}

	var results []chatHistoryResult
	for _, e := range resp.Msg.Entries {
		results = append(results, chatHistoryResult{
			MessageID:  e.Name,
			SenderName: e.SenderName,
			SenderType: senderTypeString(e.SenderType),
			IsOwn:      e.IsOwn,
			Content:    e.Content,
			Timestamp:  e.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		})
	}
	if len(results) == 0 {
		results = []chatHistoryResult{}
	}

	text := fmt.Sprintf("Found %d matching messages", len(results))
	if resp.Msg.NextPageToken != "" {
		text += " (more results available — use page_token to continue)"
	}
	text += ":\n"
	for _, r := range results {
		text += formatMessageLine(r.Timestamp, r.SenderName, r.SenderType, r.IsOwn, r.Content)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, searchChatHistoryOutput{Results: results, NextPageToken: resp.Msg.NextPageToken}, nil
}

func senderTypeString(t v1pb.SenderType) string {
	switch t {
	case v1pb.SenderType_SENDER_TYPE_USER:
		return "user"
	case v1pb.SenderType_SENDER_TYPE_AGENT:
		return "agent"
	case v1pb.SenderType_SENDER_TYPE_SYSTEM:
		return "system"
	default:
		return "unknown"
	}
}

// formatMessageLine renders one message for the tool's text output. Own
// messages are tagged "(YOU)" so the agent can recognize its own past messages
// at a glance and avoid replying to itself.
func formatMessageLine(timestamp, senderName, senderType string, isOwn bool, content string) string {
	if isOwn {
		return fmt.Sprintf("[%s] %s (%s, YOU): %s\n", timestamp, senderName, senderType, content)
	}
	return fmt.Sprintf("[%s] %s (%s): %s\n", timestamp, senderName, senderType, content)
}

type getCommandContextInput struct {
	CommandID string `json:"command_id"`
}

type eventEntry struct {
	SeqNo   int32  `json:"seq_no"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Payload string `json:"payload"`
}

type getCommandContextOutput struct {
	Instruction  string       `json:"instruction"`
	FinalSummary string       `json:"final_summary"`
	Events       []eventEntry `json:"events"`
}

func (s *Server) handleGetCommandContext(ctx context.Context, _ *mcp.CallToolRequest, input getCommandContextInput) (*mcp.CallToolResult, getCommandContextOutput, error) {
	agentName := s.agentName
	if v, ok := ctx.Value(ctxKeyAgentID).(string); ok && v != "" {
		agentName = v
	}

	commandName := fmt.Sprintf("agents/%s/commands/%s", agentName, input.CommandID)
	req := connect.NewRequest(&v1pb.GetCommandContextRequest{Name: commandName})

	resp, err := s.client().GetCommandContext(ctx, req)
	if err != nil {
		return nil, getCommandContextOutput{}, errors.Wrap(err, "failed to get command context")
	}

	var events []eventEntry
	for _, e := range resp.Msg.Events {
		payload := ""
		if p := e.GetPayload(); p != nil {
			payload = fmt.Sprintf("%v", p)
		}
		events = append(events, eventEntry{
			SeqNo:   e.SeqNo,
			Type:    e.Type.String(),
			Summary: e.Summary,
			Payload: payload,
		})
	}
	if events == nil {
		events = []eventEntry{}
	}

	output := getCommandContextOutput{
		Instruction:  resp.Msg.Command.Instruction,
		FinalSummary: resp.Msg.Command.FinalSummary,
		Events:       events,
	}

	text := fmt.Sprintf("Command context for %s:\nUser message: %s\nAgent reply: %s\nEvents (%d total):\n",
		input.CommandID, output.Instruction, output.FinalSummary, len(events))
	for _, ev := range events {
		text += fmt.Sprintf("  [%d] %s: %s\n", ev.SeqNo, ev.Type, ev.Summary)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, output, nil
}

type getConversationMessagesInput struct {
	Conversation string `json:"conversation"`
	AfterVersion int64  `json:"after_version,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type messageEntry struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	SenderType string `json:"sender_type"`
	IsOwn      bool   `json:"is_own"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

type getConversationMessagesOutput struct {
	Messages       []messageEntry `json:"messages"`
	CurrentVersion int64          `json:"current_version"`
}

func (s *Server) handleGetConversationMessages(ctx context.Context, _ *mcp.CallToolRequest, input getConversationMessagesInput) (*mcp.CallToolResult, getConversationMessagesOutput, error) {
	conversationName := resolveConversationName(ctx, input.Conversation)
	if conversationName == "" {
		return nil, getConversationMessagesOutput{}, errors.New("conversation is required (pass the conversation name from list_channel_updates)")
	}

	limit := input.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	req := connect.NewRequest(&v1pb.ListConversationMessagesRequest{
		Conversation: conversationName,
		PageSize:     int32(limit),
		AfterVersion: input.AfterVersion,
	})

	resp, err := s.client().ListConversationMessages(ctx, req)
	if err != nil {
		return nil, getConversationMessagesOutput{}, errors.Wrap(err, "failed to list conversation messages")
	}

	var messages []messageEntry
	for _, m := range resp.Msg.Messages {
		messages = append(messages, messageEntry{
			MessageID:  m.Name,
			SenderName: m.SenderName,
			SenderType: senderTypeString(m.SenderType),
			IsOwn:      m.IsOwn,
			Content:    m.Content,
			Timestamp:  m.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		})
	}
	if messages == nil {
		messages = []messageEntry{}
	}

	text := fmt.Sprintf("Conversation messages (current_version: %d):\n", resp.Msg.CurrentVersion)
	if len(messages) == 0 {
		text += "(no new messages)\n"
	} else {
		for _, m := range messages {
			text += formatMessageLine(m.Timestamp, m.SenderName, m.SenderType, m.IsOwn, m.Content)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, getConversationMessagesOutput{Messages: messages, CurrentVersion: resp.Msg.CurrentVersion}, nil
}

type postMessageInput struct {
	Conversation string `json:"conversation"`
	Content      string `json:"content"`
	BaseVersion  int64  `json:"base_version"`
}

type postMessageOutput struct {
	Committed           bool           `json:"committed"`
	MessageID           string         `json:"message_id,omitempty"`
	CurrentVersion      int64          `json:"current_version"`
	NewMessages         []messageEntry `json:"new_messages,omitempty"`
	ConflictDescription string         `json:"conflict_description,omitempty"`
}

func (s *Server) handlePostMessage(ctx context.Context, _ *mcp.CallToolRequest, input postMessageInput) (*mcp.CallToolResult, postMessageOutput, error) {
	conversationName := resolveConversationName(ctx, input.Conversation)
	if conversationName == "" {
		return nil, postMessageOutput{}, errors.New("conversation is required (pass the conversation name from list_channel_updates)")
	}

	commandID := ""
	if v, ok := ctx.Value(ctxKeyCommandID).(string); ok {
		commandID = v
	}

	req := connect.NewRequest(&v1pb.PostMessageRequest{
		Conversation: conversationName,
		Content:      input.Content,
		BaseVersion:  input.BaseVersion,
		CommandId:    commandID,
	})

	resp, err := s.client().PostMessage(ctx, req)
	if err != nil {
		return nil, postMessageOutput{}, errors.Wrap(err, "failed to post message")
	}

	output := postMessageOutput{
		Committed:      resp.Msg.Committed,
		CurrentVersion: resp.Msg.CurrentVersion,
	}

	if resp.Msg.Committed {
		output.MessageID = resp.Msg.Message.Name
		text := fmt.Sprintf("Message posted successfully (version: %d)", resp.Msg.CurrentVersion)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, output, nil
	}

	output.ConflictDescription = resp.Msg.ConflictDescription
	for _, m := range resp.Msg.NewMessages {
		timestamp := ""
		if m.CreatedAt != nil {
			timestamp = m.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z")
		}
		output.NewMessages = append(output.NewMessages, messageEntry{
			MessageID:  m.Name,
			SenderName: m.SenderName,
			SenderType: senderTypeString(m.SenderType),
			Content:    m.Content,
			Timestamp:  timestamp,
		})
	}

	text := resp.Msg.ConflictDescription + "\nNew messages:\n"
	if len(resp.Msg.NewMessages) == 0 {
		text += "(no new messages)\n"
	} else {
		for _, m := range resp.Msg.NewMessages {
			ts := ""
			if m.CreatedAt != nil {
				ts = m.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z")
			}
			text += fmt.Sprintf("[%s] %s: %s\n", ts, m.SenderName, m.Content)
		}
	}
	text += fmt.Sprintf("\nTo resolve: call get_conversation_messages(after_version=%d) to get full context, then call post_message again with the updated base_version=%d.", input.BaseVersion, resp.Msg.CurrentVersion)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, output, nil
}

type channelUpdateEntry struct {
	Conversation     string `json:"conversation"`
	CurrentVersion   int64  `json:"current_version"`
	ProcessedVersion int64  `json:"processed_version"`
	NewMessageCount  int32  `json:"new_message_count"`
}

type listChannelUpdatesOutput struct {
	Updates []channelUpdateEntry `json:"updates"`
}

// handleListChannelUpdates is the agent's "what's worth my context" discovery
// (AX Agent Inbox). It returns every channel the agent is a member of whose
// room_version is beyond the agent's durable cursor. An empty list means idle.
func (s *Server) handleListChannelUpdates(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listChannelUpdatesOutput, error) {
	req := connect.NewRequest(&v1pb.ListChannelUpdatesRequest{})
	resp, err := s.client().ListChannelUpdates(ctx, req)
	if err != nil {
		return nil, listChannelUpdatesOutput{}, errors.Wrap(err, "failed to list channel updates")
	}

	var updates []channelUpdateEntry
	for _, u := range resp.Msg.Updates {
		updates = append(updates, channelUpdateEntry{
			Conversation:     u.Conversation,
			CurrentVersion:   u.CurrentVersion,
			ProcessedVersion: u.ProcessedVersion,
			NewMessageCount:  u.NewMessageCount,
		})
	}
	if updates == nil {
		updates = []channelUpdateEntry{}
	}

	text := fmt.Sprintf("Channels with unread messages (%d):\n", len(updates))
	if len(updates) == 0 {
		text += "(none — you are idle; end your turn without calling any other tool)\n"
	} else {
		for _, u := range updates {
			text += fmt.Sprintf("- %s: %d new (current_version=%d, your processed_version=%d)\n",
				u.Conversation, u.NewMessageCount, u.CurrentVersion, u.ProcessedVersion)
		}
		text += "\nPick ONE channel. Call get_conversation_messages(conversation=<name>, after_version=<processed_version>) to read the new messages.\n"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, listChannelUpdatesOutput{Updates: updates}, nil
}

type ackProcessedVersionInput struct {
	Conversation     string `json:"conversation"`
	ProcessedVersion int64  `json:"processed_version"`
}

type ackProcessedVersionOutput struct {
	ProcessedVersion int64 `json:"processed_version"`
}

// handleAckProcessedVersion advances the agent's durable per-channel cursor.
// The agent MUST call this after finishing a channel (reply or silence) so the
// next list_channel_updates no longer reports it. command_id from the MCP URL
// links the session's command to the conversation for frontend visibility.
func (s *Server) handleAckProcessedVersion(ctx context.Context, _ *mcp.CallToolRequest, input ackProcessedVersionInput) (*mcp.CallToolResult, ackProcessedVersionOutput, error) {
	conversationName := resolveConversationName(ctx, input.Conversation)
	if conversationName == "" {
		return nil, ackProcessedVersionOutput{}, errors.New("conversation is required")
	}
	if input.ProcessedVersion <= 0 {
		return nil, ackProcessedVersionOutput{}, errors.New("processed_version must be positive")
	}

	commandID := ""
	if v, ok := ctx.Value(ctxKeyCommandID).(string); ok {
		commandID = v
	}

	req := connect.NewRequest(&v1pb.AckProcessedVersionRequest{
		Conversation:     conversationName,
		ProcessedVersion: input.ProcessedVersion,
		CommandId:        commandID,
	})
	resp, err := s.client().AckProcessedVersion(ctx, req)
	if err != nil {
		return nil, ackProcessedVersionOutput{}, errors.Wrap(err, "failed to ack processed version")
	}

	text := fmt.Sprintf("Cursor advanced to processed_version=%d for %s.", resp.Msg.ProcessedVersion, conversationName)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, ackProcessedVersionOutput{ProcessedVersion: resp.Msg.ProcessedVersion}, nil
}
