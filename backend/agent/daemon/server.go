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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/chattools"
	"github.com/Ranxy/laelia/backend/agent/home"
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
// hosted agents: the socket lives at the well-known daemon.sock under the
// Laelia data root (default ~/.laelia/daemon.sock, or LAELIA_HOME when set)
// and the daemon routes each request to the agent named in LAELIA_AGENT
// (injected into every ACP subprocess). It is constructed once per machine
// process and lives for the whole machine lifetime.
//
// Server owns the socket lifecycle, routing, and dependency injection. The
// per-agent Connect client caches live in clients.go, MCP proxy/handlers in
// handlers_mcp.go, chat/task/reminder/channel handlers in handlers_chat.go,
// and workspace file handlers in handlers_file.go.
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

	// mcpClients caches per-agent McpGatewayService clients using the same
	// machine token + X-Laelia-Agent routing as agentClients.
	mcpClientsMu sync.Mutex
	mcpClients   map[string]v1connect.McpGatewayServiceClient

	// mcpProxy* is the localhost TCP proxy pi extensions call. Tokens map to
	// the agent resource name; the proxy forwards to the manager gateway.
	mcpProxyMu     sync.Mutex
	mcpProxyServer *http.Server
	mcpProxyBase   string
	mcpProxyTokens map[string]string
	mcpProxyAgents map[string]string

	// userClients caches a per-agent UserServiceClient (same token + agent
	// header), used by `channel add-member` to resolve user display names.
	userClientsMu sync.Mutex
	userClients   map[string]v1connect.UserServiceClient

	// agentTeamClients caches per-agent AgentTeamService clients (same token +
	// agent header), used by `team get` / `team show`.
	agentTeamClientsMu sync.Mutex
	agentTeamClients   map[string]v1connect.AgentTeamServiceClient
}

// New creates a daemon bound to the well-known unix socket under the Laelia
// data root (default ~/.laelia/daemon.sock, or LAELIA_HOME when set).
// machineResourceID still keys the per-agent workspace dirs
// (<root>/<machineID>/<agentID>/). getToken returns the current machine access
// token (rotated by heartbeat), shared by every hosted agent.
func New(managerURL, machineResourceID string, getToken func() string, httpClient *http.Client) (*Server, error) {
	socketPath := DefaultSocketPath()

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
		homeDir:           home.Dir(),
		socketPath:        socketPath,
		sessionToken:      sessionToken,
		agentClients:      make(map[string]v1connect.CommandServiceClient),
		mcpClients:        make(map[string]v1connect.McpGatewayServiceClient),
		mcpProxyTokens:    make(map[string]string),
		mcpProxyAgents:    make(map[string]string),
		userClients:       make(map[string]v1connect.UserServiceClient),
		agentTeamClients:  make(map[string]v1connect.AgentTeamServiceClient),
	}, nil
}

func (s *Server) SocketPath() string { return s.socketPath }

// DefaultSocketPath returns the well-known daemon socket location (default
// ~/.laelia/daemon.sock, or under LAELIA_HOME when set). setup/run probe it to
// detect an already-running laelia-machine before starting a second instance.
func DefaultSocketPath() string {
	return home.Join("daemon.sock")
}
func (s *Server) SessionToken() string { return s.sessionToken }

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
	mux.HandleFunc("/reaction/add", s.handleReactionAdd)
	mux.HandleFunc("/reaction/remove", s.handleReactionRemove)
	mux.HandleFunc("/message/thread/check", s.handleThreadCheck)
	mux.HandleFunc("/message/thread/read", s.handleThreadRead)
	mux.HandleFunc("/message/thread/send", s.handleThreadSend)
	mux.HandleFunc("/task/list", s.handleTaskList)
	mux.HandleFunc("/task/claim", s.handleTaskClaim)
	mux.HandleFunc("/task/unclaim", s.handleTaskUnclaim)
	mux.HandleFunc("/task/update", s.handleTaskUpdate)
	mux.HandleFunc("/task/create", s.handleTaskCreate)
	mux.HandleFunc("/team/get", s.handleTeamGet)
	mux.HandleFunc("/team/show", s.handleTeamShow)
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
	mux.HandleFunc("/mcp/tools", s.handleMcpTools)
	mux.HandleFunc("/mcp/call", s.handleMcpCall)
	mux.HandleFunc("/channel/leave", s.handleChannelLeave)
	mux.HandleFunc("/channel/add-member", s.handleChannelAddMember)
	mux.HandleFunc("/channel/remove-member", s.handleChannelRemoveMember)

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
	// TeamID is the agent team resource id (for `team show <id>`).
	TeamID string `json:"team_id,omitempty"`
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

	// Members are the member arguments of `channel add-member` (display names or
	// agents/<id> / users/<id> handles).
	Members []string `json:"members,omitempty"`

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

	// ReactionEmoji is the single emoji for the reaction add/remove commands.
	// The target message is carried in Message (as a "<address>:<message-id>"
	// handle, not a task resource name, for the reaction endpoints).
	ReactionEmoji string `json:"reaction_emoji,omitempty"`
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
		Client:          s.agentClient(r.Agent),
		UserClient:      s.userClient(r.Agent),
		AgentTeamClient: s.agentTeamClient(r.Agent),
		Agent:           r.Agent,
		Command:         r.Command,
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

// ensureStaleSocket clears a leftover socket file only if nothing is listening
// on it. If a process answers the dial, another daemon for this machine is
// already running and we must not steal its socket.
func (s *Server) ensureStaleSocket() error {
	conn, err := net.DialTimeout("unix", s.socketPath, 500*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return errors.Errorf("daemon socket %q is live; another laelia-machine daemon is already running", s.socketPath)
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
