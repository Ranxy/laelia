package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"

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
			Description: "Get recent messages from the current conversation with the current room version. Use after_version to only fetch messages newer than a known version. You MUST call this before calling post_message to obtain the latest base_version.",
		},
		ms.handleGetConversationMessages,
	)
	mcp.AddTool(srv,
		&mcp.Tool{
			Name:        "post_message",
			Description: "Post a reply to the current conversation. You MUST call get_conversation_messages first to obtain the base_version. If committed=false, new messages arrived while you were thinking — read them, reconsider, and call post_message again with the updated base_version.",
		},
		ms.handlePostMessage,
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

type searchChatHistoryInput struct {
	Query     string `json:"query"`
	Since     string `json:"since,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type chatHistoryResult struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	SenderType string `json:"sender_type"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

type searchChatHistoryOutput struct {
	Results       []chatHistoryResult `json:"results"`
	NextPageToken string              `json:"next_page_token,omitempty"`
}

func (s *Server) handleSearchChatHistory(ctx context.Context, _ *mcp.CallToolRequest, input searchChatHistoryInput) (*mcp.CallToolResult, searchChatHistoryOutput, error) {
	conversationID := ""
	if v, ok := ctx.Value(ctxKeyConversationID).(string); ok && v != "" {
		conversationID = v
	}

	limit := input.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	reqMsg := &v1pb.SearchChatHistoryRequest{
		Query:     input.Query,
		Limit:     int32(limit),
		PageToken: input.PageToken,
	}
	if conversationID != "" {
		reqMsg.Conversation = fmt.Sprintf("conversations/%s", conversationID)
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
		text += fmt.Sprintf("[%s] %s (%s): %s\n", r.Timestamp, r.SenderName, r.SenderType, r.Content)
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
	AfterVersion int64 `json:"after_version,omitempty"`
	Limit        int   `json:"limit,omitempty"`
}

type messageEntry struct {
	MessageID  string `json:"message_id"`
	SenderName string `json:"sender_name"`
	SenderType string `json:"sender_type"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

type getConversationMessagesOutput struct {
	Messages       []messageEntry `json:"messages"`
	CurrentVersion int64          `json:"current_version"`
}

func (s *Server) handleGetConversationMessages(ctx context.Context, _ *mcp.CallToolRequest, input getConversationMessagesInput) (*mcp.CallToolResult, getConversationMessagesOutput, error) {
	conversationID := ""
	if v, ok := ctx.Value(ctxKeyConversationID).(string); ok && v != "" {
		conversationID = v
	}
	if conversationID == "" {
		return nil, getConversationMessagesOutput{}, errors.New("no conversation context available")
	}

	limit := input.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	req := connect.NewRequest(&v1pb.ListConversationMessagesRequest{
		Conversation: fmt.Sprintf("conversations/%s", conversationID),
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
			text += fmt.Sprintf("[%s] %s (%s): %s\n", m.Timestamp, m.SenderName, m.SenderType, m.Content)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, getConversationMessagesOutput{Messages: messages, CurrentVersion: resp.Msg.CurrentVersion}, nil
}

type postMessageInput struct {
	Content     string `json:"content"`
	BaseVersion int64  `json:"base_version"`
}

type postMessageOutput struct {
	Committed           bool           `json:"committed"`
	MessageID           string         `json:"message_id,omitempty"`
	CurrentVersion      int64          `json:"current_version"`
	NewMessages         []messageEntry `json:"new_messages,omitempty"`
	ConflictDescription string         `json:"conflict_description,omitempty"`
}

func (s *Server) handlePostMessage(ctx context.Context, _ *mcp.CallToolRequest, input postMessageInput) (*mcp.CallToolResult, postMessageOutput, error) {
	conversationID := ""
	if v, ok := ctx.Value(ctxKeyConversationID).(string); ok && v != "" {
		conversationID = v
	}
	if conversationID == "" {
		return nil, postMessageOutput{}, errors.New("no conversation context available")
	}

	commandID := ""
	if v, ok := ctx.Value(ctxKeyCommandID).(string); ok {
		commandID = v
	}

	req := connect.NewRequest(&v1pb.PostMessageRequest{
		Conversation: fmt.Sprintf("conversations/%s", conversationID),
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
