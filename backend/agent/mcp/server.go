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
	ctxKeyAgentID     contextKey = "laelia_agent_id"
	ctxKeyPrincipalID contextKey = "laelia_principal_id"
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
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type searchChatHistoryInput struct {
	Query string `json:"query"`
	Since string `json:"since,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type chatHistoryResult struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type searchChatHistoryOutput struct {
	Results []chatHistoryResult `json:"results"`
}

func (s *Server) handleSearchChatHistory(ctx context.Context, _ *mcp.CallToolRequest, input searchChatHistoryInput) (*mcp.CallToolResult, searchChatHistoryOutput, error) {
	agentName := s.agentName
	principalID := "0"
	if v, ok := ctx.Value(ctxKeyAgentID).(string); ok && v != "" {
		agentName = v
	}
	if v, ok := ctx.Value(ctxKeyPrincipalID).(string); ok && v != "" {
		principalID = v
	}

	limit := input.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	req := connect.NewRequest(&v1pb.SearchChatHistoryRequest{
		Agent:       fmt.Sprintf("agents/%s", agentName),
		Query:       input.Query,
		PrincipalId: principalID,
		Limit:       int32(limit),
	})

	resp, err := s.client().SearchChatHistory(ctx, req)
	if err != nil {
		return nil, searchChatHistoryOutput{}, errors.Wrap(err, "failed to search chat history")
	}

	var results []chatHistoryResult
	for _, e := range resp.Msg.Entries {
		results = append(results, chatHistoryResult{
			MessageID: e.MessageId,
			Role:      e.Role,
			Content:   e.Content,
			Timestamp: e.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		})
	}
	if len(results) == 0 {
		results = []chatHistoryResult{}
	}

	text := fmt.Sprintf("Found %d matching messages:\n", len(results))
	for _, r := range results {
		text += fmt.Sprintf("[%s] %s (%s): %s\n", r.Timestamp, r.Role, r.MessageID, r.Content)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, searchChatHistoryOutput{Results: results}, nil
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
