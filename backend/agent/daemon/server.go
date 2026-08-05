// Package daemon hosts the local loopback server that the LLM-driven CLI
// subcommands (`laelia-machine message ...` / `laelia-machine command context`)
// call into. It replaces the former MCP HTTP server: the LLM now invokes the
// agent binary directly from its shell, and the CLI forwards each call over a
// unix socket to this daemon, which holds the agent's live (rotating) access
// token and forwards to the manager. This keeps the long-lived token out of the
// subprocess environment — the CLI only carries a stable per-daemon session
// credential (LAELIA_SESSION_TOKEN), so token rotation never invalidates an
// in-flight drain session.
package daemon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/chattools"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// envKey* are the env vars the daemon injects into each ACP subprocess so the
// CLI subcommands can find and authenticate to the socket without any flags.
const (
	EnvDaemonSocket = "LAELIA_DAEMON_SOCKET"
	EnvSessionToken = "LAELIA_SESSION_TOKEN"
	EnvAgent        = "LAELIA_AGENT"
	EnvCommand      = "LAELIA_COMMAND"
)

// Server is the local loopback daemon. A machine runs ONE daemon for all its
// hosted agents: the socket lives at ~/.laelia/<machineID>/daemon.sock and the
// daemon routes each request to the agent named in LAELIA_AGENT (injected into
// every ACP subprocess). It is constructed once per machine process and lives
// for the whole machine lifetime.
type Server struct {
	managerURL        string
	machineResourceID string
	getToken          func() string
	httpClient        *http.Client
	homeDir           string

	socketPath   string
	sessionToken string

	listener   net.Listener
	httpServer *http.Server

	// agentClients caches a per-agent CommandServiceClient. Each carries the
	// live machine access token (Authorization) AND the X-Laelia-Agent header
	// (agents/{agent}) so the manager can route a machine-token call to the
	// agent the daemon is acting for. One daemon hosts many agents, so the
	// client varies per agent even though the token is shared.
	agentClientsMu sync.Mutex
	agentClients   map[string]v1connect.CommandServiceClient
}

// New creates a daemon bound to a unix socket at
// ~/.laelia/<machineResourceID>/daemon.sock. getToken returns the current
// machine access token (rotated by heartbeat), shared by every hosted agent.
func New(managerURL, machineResourceID string, getToken func() string, httpClient *http.Client) (*Server, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(err, "resolve home dir")
	}
	socketPath := filepath.Join(home, ".laelia", machineResourceID, "daemon.sock")

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, errors.Wrap(err, "generate session token")
	}

	sessionToken := hex.EncodeToString(token)
	// Never log the session token in full: it authenticates every CLI call to
	// this socket. Log only a short prefix + sha256 so logs are traceable but
	// not usable as a credential.
	slog.Debug("LAELIA_SESSION_TOKEN", slog.String("prefix", sessionToken[:8]), slog.String("sha256", sha256Prefix(sessionToken)))

	return &Server{
		managerURL:        managerURL,
		machineResourceID: machineResourceID,
		getToken:          getToken,
		httpClient:        httpClient,
		homeDir:           home,
		socketPath:        socketPath,
		sessionToken:      sessionToken,
		agentClients:      make(map[string]v1connect.CommandServiceClient),
	}, nil
}

func (s *Server) SocketPath() string   { return s.socketPath }
func (s *Server) SessionToken() string { return s.sessionToken }

// BatchDeps returns a Deps for the agent identified by agentBareID (the bare
// agents/{id} tail), for in-process calls from that agent's drain loop (the
// turn-batch builder). The Client carries the live machine access token and the
// X-Laelia-Agent header (agents/<agentBareID>) so the manager resolves the
// caller as that agent; Deps.Agent is the bare id chattools uses to build
// agents/<id>/commands/<id> resource names. Each agent runner passes its own
// agent id.
func (s *Server) BatchDeps(agentBareID string) chattools.Deps {
	return chattools.Deps{Client: s.agentClient(agentBareID), Agent: agentBareID}
}

// agentClient returns a cached CommandServiceClient for the agent identified by
// agentBareID (the bare uuid). Every call it makes carries the live machine
// access token (Authorization) and the X-Laelia-Agent header (agents/<id>), so
// the manager — which authenticates the machine token — can route the call to
// this specific agent. One daemon hosts many agents, so the client varies per
// agent even though the token is shared.
func (s *Server) agentClient(agentBareID string) v1connect.CommandServiceClient {
	s.agentClientsMu.Lock()
	defer s.agentClientsMu.Unlock()
	if s.agentClients == nil {
		s.agentClients = make(map[string]v1connect.CommandServiceClient)
	}
	if c, ok := s.agentClients[agentBareID]; ok {
		return c
	}
	c := v1connect.NewCommandServiceClient(
		s.httpClient,
		s.managerURL,
		connect.WithInterceptors(s.authInterceptor(agentBareID)),
	)
	s.agentClients[agentBareID] = c
	return c
}

// authInterceptor builds a unary interceptor that stamps every request with the
// live machine access token and the X-Laelia-Agent header (agents/<agentBareID>)
// for the given agent.
func (s *Server) authInterceptor(agentBareID string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token := s.getToken(); token != "" {
				req.Header().Set("Authorization", "Bearer "+token)
			}
			if agentBareID != "" {
				req.Header().Set("X-Laelia-Agent", agentResourceName(agentBareID))
			}
			return next(ctx, req)
		}
	}
}

// agentResourceName converts a bare agent id to its full resource name
// (agents/<id>) as the manager's X-Laelia-Agent header expects. A value that is
// already a full name is returned unchanged.
func agentResourceName(agentBareID string) string {
	if agentBareID == "" {
		return ""
	}
	if strings.HasPrefix(agentBareID, "agents/") {
		return agentBareID
	}
	return "agents/" + agentBareID
}

// bareAgentID strips the agents/ prefix from an agent resource name, returning
// the bare uuid used to namespace the agent's on-disk state.
func bareAgentID(agent string) string {
	return strings.TrimPrefix(agent, "agents/")
}

// workspaceFor returns the calling agent's persistent working directory
// (~/.laelia/<machineID>/<agentID>/), the same directory the executor runs the
// agent's shell in. File commands confine local paths to the temp subdirectory
// of this workspace so transient upload/download files never clutter the
// agent's persistent files.
func (s *Server) workspaceFor(agent string) (string, error) {
	agentID := bareAgentID(agent)
	if agentID == "" || agentID == "." || agentID == ".." || strings.ContainsAny(agentID, `/\`) {
		return "", errors.New("agent is required to resolve the file workspace")
	}
	return filepath.Join(s.homeDir, ".laelia", s.machineResourceID, agentID), nil
}

// fileWorkspace resolves the calling agent's working directory, its temp jail
// (~/.laelia/<machineID>/<agentID>/temp/), and the base for relative paths:
// the CLI process's cwd when available (normally the agent's working
// directory), falling back to the working directory itself.
func (s *Server) fileWorkspace(req Request) (tempDir, base string, err error) {
	workspace, err := s.workspaceFor(req.Agent)
	if err != nil {
		return "", "", err
	}
	tempDir = filepath.Join(workspace, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return "", "", errors.Wrap(err, "create agent temp workspace")
	}
	base = req.Cwd
	if base == "" || !filepath.IsAbs(base) {
		base = workspace
	}
	return tempDir, base, nil
}

// Start binds the unix socket and serves HTTP/JSON in a background goroutine.
func (s *Server) Start() error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return errors.Wrap(err, "create daemon socket dir")
	}
	// If a leftover socket file exists, probe it before clobbering: a live
	// socket means another daemon is already running on this resource ID, and
	// blindly removing it would steal the socket out from under that process.
	if err := s.ensureStaleSocket(); err != nil {
		return err
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return errors.Wrap(err, "bind daemon socket")
	}
	s.listener = listener
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		return errors.Wrap(err, "restrict daemon socket perms")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/message/check", s.handleMessageCheck)
	mux.HandleFunc("/message/read", s.handleMessageRead)
	mux.HandleFunc("/message/search", s.handleMessageSearch)
	mux.HandleFunc("/message/ack", s.handleMessageAck)
	mux.HandleFunc("/message/send", s.handleMessageSend)
	mux.HandleFunc("/message/thread/check", s.handleThreadCheck)
	mux.HandleFunc("/message/thread/read", s.handleThreadRead)
	mux.HandleFunc("/message/thread/send", s.handleThreadSend)
	mux.HandleFunc("/task/list", s.handleTaskList)
	mux.HandleFunc("/task/claim", s.handleTaskClaim)
	mux.HandleFunc("/task/unclaim", s.handleTaskUnclaim)
	mux.HandleFunc("/task/update", s.handleTaskUpdate)
	mux.HandleFunc("/task/create", s.handleTaskCreate)
	mux.HandleFunc("/reminder/convert", s.handleReminderConvert)
	mux.HandleFunc("/reminder/list", s.handleReminderList)
	mux.HandleFunc("/reminder/list-due", s.handleReminderListDue)
	mux.HandleFunc("/reminder/update", s.handleReminderUpdate)
	mux.HandleFunc("/reminder/cancel", s.handleReminderCancel)
	mux.HandleFunc("/reminder/complete", s.handleReminderComplete)
	mux.HandleFunc("/reminder/fail", s.handleReminderFail)
	mux.HandleFunc("/command/context", s.handleCommandContext)
	mux.HandleFunc("/file/upload", s.handleFileUpload)
	mux.HandleFunc("/file/download", s.handleFileDownload)
	mux.HandleFunc("/file/list", s.handleFileList)
	mux.HandleFunc("/members", s.handleMembers)
	mux.HandleFunc("/agent/list", s.handleAgentList)
	mux.HandleFunc("/channel/list", s.handleChannelList)
	mux.HandleFunc("/channel/join", s.handleChannelJoin)

	s.httpServer = &http.Server{Handler: mux}
	go func() {
		slog.Info("daemon socket listening", "path", s.socketPath)
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			slog.Error("daemon socket serve error", "error", err)
		}
	}()
	return nil
}

func (s *Server) Stop() {
	if s.httpServer != nil {
		_ = s.httpServer.Close()
	}
	_ = os.Remove(s.socketPath)
}

// Request is the shared envelope. Identity (agent/command) comes from the
// CLI's env vars; operation params are filled per endpoint. Fields not
// relevant to a given endpoint are simply ignored. The CLI reuses this type so
// the wire shape stays in sync with the daemon handlers.
type Request struct {
	Agent   string `json:"agent"`
	Command string `json:"command"`

	Conversation     string `json:"conversation,omitempty"`
	Version          int64  `json:"version,omitempty"`
	Direction        string `json:"direction,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Query            string `json:"query,omitempty"`
	Since            string `json:"since,omitempty"`
	PageToken        string `json:"page_token,omitempty"`
	Content          string `json:"content,omitempty"`
	BaseVersion      int64  `json:"base_version,omitempty"`
	ProcessedVersion int64  `json:"processed_version,omitempty"`
	CommandID        string `json:"command_id,omitempty"`
	// Root is the thread root message id, for thread read/send and for scoping
	// `members` to a thread's participants.
	Root string `json:"root,omitempty"`
	// Message is a task's full message resource name
	// ("conversations/{c}/messages/{m}"), for the task RPCs.
	Message string `json:"message,omitempty"`
	// Status is a single task status token (for `task review`/`task done`).
	Status string `json:"status,omitempty"`
	// Statuses is the repeatable status filter for `task list --status`.
	Statuses []string `json:"statuses,omitempty"`
	// PageSize caps one page of `task list` results (newest first); 0 uses the
	// server default.
	PageSize int32 `json:"page_size,omitempty"`

	// File command fields.
	Cwd          string `json:"cwd,omitempty"`
	LocalPath    string `json:"local_path,omitempty"`
	FileID       string `json:"file_id,omitempty"`
	OutPath      string `json:"out_path,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`

	// AttachmentIDs are file ids to attach to a posted message.
	AttachmentIDs []string `json:"attachment_ids,omitempty"`

	// Reminder fields. Name is a reminder resource name
	// ("reminders/{message_id}"). FireAt is an RFC3339 timestamp; CronExpr is a
	// 5-field cron expression (empty = one-shot); Tz is an IANA timezone.
	// Result/Error are the completion/failure reports posted to the thread.
	Name     string `json:"name,omitempty"`
	FireAt   string `json:"fire_at,omitempty"`
	CronExpr string `json:"cron_expr,omitempty"`
	Tz       string `json:"tz,omitempty"`
	Result   string `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Response is the shared envelope. Success: Text set, Code empty. Failure:
// Code set (CLI renders Error:/Code:/Next action: to stderr).
type Response struct {
	Text       string `json:"text,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
}

func (s *Server) deps(r Request) chattools.Deps {
	// r.Agent is the agents/{id} the CLI set from LAELIA_AGENT; the executor
	// injects it into every ACP subprocess, so it is always present for a
	// well-formed drain session. The CommandServiceClient is routed per-agent
	// (it stamps the X-Laelia-Agent header), and Deps.Agent is also set so the
	// chattools layer can pass the caller identity in the request body. An
	// empty value is passed through and fails server-side caller resolution
	// rather than silently routing to a default.
	return chattools.Deps{
		Client:  s.agentClient(r.Agent),
		Agent:   r.Agent,
		Command: r.Command,
	}
}

// authorize validates the per-daemon session token. A missing/mismatched token
// is a local bootstrap error (TOKEN_*), reported without touching the manager.
func (s *Server) authorize(r *http.Request) *chattools.Error {
	got := r.Header.Get("Authorization")
	if got == "" {
		return &chattools.Error{Code: "TOKEN_MISSING", Message: "no session token (LAELIA_SESSION_TOKEN unset)", NextAction: "Run inside a drain session started by `laelia-machine run`."}
	}
	if got != "Bearer "+s.sessionToken {
		return &chattools.Error{Code: "TOKEN_INVALID", Message: "session token does not match this daemon", NextAction: "The daemon restarted with a new token; this should not happen mid-session."}
	}
	return nil
}

func (*Server) decode(w http.ResponseWriter, r *http.Request, req *Request) bool {
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		writeError(w, &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: "failed to decode request body: " + err.Error()})
		return false
	}
	return true
}

func writeOK(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Response{Text: text})
}

func writeError(w http.ResponseWriter, e *chattools.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Response{Code: e.Code, Message: e.Message, NextAction: e.NextAction})
}

// run is the common dispatch: authorize → decode → call f → write response.
func (s *Server) run(w http.ResponseWriter, r *http.Request, f func(req Request) (string, *chattools.Error)) {
	if e := s.authorize(r); e != nil {
		writeError(w, e)
		return
	}
	var req Request
	if !s.decode(w, r, &req) {
		return
	}
	text, e := f(req)
	if e != nil {
		writeError(w, e)
		return
	}
	writeOK(w, text)
}

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

// validateWorkspacePath resolves path against base (the calling CLI process's
// cwd, normally the agent's working directory) and ensures the
// symlink-resolved result stays inside jail (the agent's temp workspace). This
// prevents file commands from reading/writing outside the temp jail.
//
// The previous version, when EvalSymlinks failed (a not-yet-existing download
// target), fell back to the lexical cleaned path. A dangling symlink inside
// the jail pointing outside it (jail/evil → /etc/laelia-shell) then
// passed the ".." check (rel == "evil") and a subsequent os.WriteFile followed
// the symlink out of the jail.
//
// Hardening:
//   - If the final component exists and is a symlink, refuse outright (a write
//     would follow it).
//   - If the final component exists and is a regular file/dir, resolve all
//     symlinks in the full path and confirm the result is inside the jail.
//   - If the final component does not exist (fresh target), resolve the parent
//     directory's symlinks and confirm the parent is inside the jail; the leaf
//     cannot itself be a symlink because it does not exist.
func validateWorkspacePath(base, jail, path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	cleaned := filepath.Clean(path)

	fi, err := os.Lstat(cleaned)
	switch {
	case err == nil:
		if fi.Mode()&os.ModeSymlink != 0 {
			// A symlink at the leaf — dangling or not — would be followed by a
			// write, so refuse rather than risk escaping the jail.
			return "", errors.Errorf("path %q is a symlink; refusing to follow it outside the workspace", path)
		}
		// Existing regular file/dir: resolve the whole path and confirm it
		// stays inside the jail.
		resolved, lerr := filepath.EvalSymlinks(cleaned)
		if lerr != nil {
			return "", errors.Errorf("failed to resolve path %q: %v", path, lerr)
		}
		if !insideWorkspace(jail, resolved) {
			return "", errors.Errorf("path %q escapes the agent workspace", path)
		}
		return resolved, nil
	case errors.Is(err, os.ErrNotExist):
		// Fresh target (e.g. a download destination). The leaf cannot be a
		// symlink, but an ancestor might be — resolve the parent and confirm
		// it is inside the jail, then rejoin the leaf onto the resolved parent.
		parent := filepath.Dir(cleaned)
		parentResolved, perr := filepath.EvalSymlinks(parent)
		if perr != nil {
			return "", errors.Errorf("failed to resolve parent directory %q: %v", parent, perr)
		}
		if !insideWorkspace(jail, parentResolved) {
			return "", errors.Errorf("path %q escapes the agent workspace", path)
		}
		return filepath.Join(parentResolved, filepath.Base(cleaned)), nil
	default:
		return "", errors.Errorf("failed to stat path %q: %v", path, err)
	}
}

// insideWorkspace reports whether target is at or below jail once both are
// cleaned. It does not itself resolve symlinks; callers resolve first.
func insideWorkspace(jail, target string) bool {
	rel, err := filepath.Rel(jail, target)
	if err != nil {
		return false
	}
	// ".." escapes; "../anything" escapes. A leaf literally named "..foo" does
	// not (it has no separator after the leading dots).
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		tempDir, base, werr := s.fileWorkspace(req)
		if werr != nil {
			return "", &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: werr.Error()}
		}
		localPath, err := validateWorkspacePath(base, tempDir, req.LocalPath)
		if err != nil {
			return "", &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: err.Error()}
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			return "", &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: "failed to read local file: " + err.Error()}
		}
		originalName := req.OriginalName
		if originalName == "" {
			originalName = filepath.Base(localPath)
		}
		text, err := chattools.UploadFile(r.Context(), s.deps(req), chattools.UploadFileInput{
			Conversation: req.Conversation,
			OriginalName: originalName,
			MimeType:     req.MimeType,
			Data:         data,
		})
		return text, asChatError(err)
	})
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if e := s.authorize(r); e != nil {
		writeError(w, e)
		return
	}
	var req Request
	if !s.decode(w, r, &req) {
		return
	}
	result, err := chattools.DownloadFile(r.Context(), s.deps(req), chattools.DownloadFileInput{
		FileID: req.FileID,
	})
	if err != nil {
		writeError(w, asChatError(err))
		return
	}
	tempDir, base, werr := s.fileWorkspace(req)
	if werr != nil {
		writeError(w, &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: werr.Error()})
		return
	}
	outPath := req.OutPath
	if outPath == "" {
		outPath = filepath.Join(tempDir, result.Name)
	}
	resolved, verr := validateWorkspacePath(base, tempDir, outPath)
	if verr != nil {
		writeError(w, &chattools.Error{Code: "INVALID_ARGUMENT_FAILED", Message: verr.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		writeError(w, &chattools.Error{Code: "SERVER_5XX", Message: "failed to create target dir: " + err.Error()})
		return
	}
	if err := os.WriteFile(resolved, result.Data, 0o600); err != nil {
		writeError(w, &chattools.Error{Code: "SERVER_5XX", Message: "failed to write file: " + err.Error()})
		return
	}
	writeOK(w, result.Text+"\nWrote to "+resolved)
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		text, err := chattools.ListFiles(r.Context(), s.deps(req), chattools.ListFilesInput{
			Conversation: req.Conversation,
		})
		return text, asChatError(err)
	})
}

// handleMembers serves the single roster tool: conversation members when Root is
// empty, thread participants when Root is set. Each entry carries the member's
// full description inline, so the agent perceives who is present and each
// co-agent's persona in one call.
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
// its display name, agents/<id> handle, connection state, and full persona. It
// is the discovery tool the agent uses before delegating to a peer via
// `message send dm:@<peer>`.
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

// ensureStaleSocket clears a leftover socket file only if nothing is listening
// on it. If a process answers the dial, another daemon for this machine is
// already running and we must not steal its socket.
func (s *Server) ensureStaleSocket() error {
	conn, err := net.DialTimeout("unix", s.socketPath, 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return errors.Errorf("daemon socket %q is live; another laelia-machine daemon is already running for this machine", s.socketPath)
	}
	// No listener: remove the stale file (or no-op if it is already gone) so
	// the subsequent net.Listen succeeds.
	_ = os.Remove(s.socketPath)
	return nil
}

// sha256Prefix returns the first 12 hex chars of sha256(token), enough to
// correlate log lines without leaking the credential.
func sha256Prefix(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:12]
}
