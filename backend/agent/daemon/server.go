// Package daemon hosts the local loopback server that the LLM-driven CLI
// subcommands (`laelia-agent message ...` / `laelia-agent command context`)
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
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

// Server is the local loopback daemon. It is constructed once per agent
// process and lives for the whole agent lifetime.
type Server struct {
	managerURL      string
	agentResourceID string
	getToken        func() string
	httpClient      *http.Client

	socketPath   string
	sessionToken string
	// tempDir is the agent's temp workspace for file upload/download. It is a
	// "temp" subdir of the per-agent working dir, isolated from the agent's
	// other working files. File commands confine all local paths to it.
	tempDir string

	listener   net.Listener
	httpServer *http.Server

	clientOnce sync.Once
	client     v1connect.CommandServiceClient
}

// New creates a daemon bound to a unix socket at
// ~/.laelia/<resourceID>/daemon.sock. The caller must ensure the per-agent
// working dir already exists (the client creates it on connect). getToken
// returns the current agent access token (rotated by heartbeat).
func New(managerURL, agentResourceID, resourceID string, getToken func() string, httpClient *http.Client) (*Server, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.Wrap(err, "resolve home dir")
	}
	socketPath := filepath.Join(home, ".laelia", resourceID, "daemon.sock")
	tempDir := filepath.Join(home, ".laelia", resourceID, "temp")

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, errors.Wrap(err, "generate session token")
	}

	return &Server{
		managerURL:      managerURL,
		agentResourceID: agentResourceID,
		getToken:        getToken,
		httpClient:      httpClient,
		socketPath:      socketPath,
		sessionToken:    hex.EncodeToString(token),
		tempDir:         tempDir,
	}, nil
}

func (s *Server) SocketPath() string   { return s.socketPath }
func (s *Server) SessionToken() string { return s.sessionToken }
func (s *Server) TempDir() string      { return s.tempDir }

// client builds (once) a CommandServiceClient that carries the live access
// token on every call via an interceptor.
func (s *Server) commandClient() v1connect.CommandServiceClient {
	s.clientOnce.Do(func() {
		s.client = v1connect.NewCommandServiceClient(
			s.httpClient,
			s.managerURL,
			connect.WithInterceptors(s.authInterceptor()),
		)
	})
	return s.client
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

// Start binds the unix socket and serves HTTP/JSON in a background goroutine.
func (s *Server) Start() error {
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o700); err != nil {
		return errors.Wrap(err, "create daemon socket dir")
	}
	if err := os.MkdirAll(s.tempDir, 0o700); err != nil {
		return errors.Wrap(err, "create agent temp workspace")
	}
	_ = os.Remove(s.socketPath) // clear stale socket from a previous run

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
	mux.HandleFunc("/command/context", s.handleCommandContext)
	mux.HandleFunc("/file/upload", s.handleFileUpload)
	mux.HandleFunc("/file/download", s.handleFileDownload)
	mux.HandleFunc("/file/list", s.handleFileList)

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

	// File command fields.
	LocalPath    string `json:"local_path,omitempty"`
	FileID       string `json:"file_id,omitempty"`
	OutPath      string `json:"out_path,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
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
	agent := r.Agent
	if agent == "" {
		agent = s.agentResourceID
	}
	return chattools.Deps{
		Client:  s.commandClient(),
		Agent:   agent,
		Command: r.Command,
	}
}

// authorize validates the per-daemon session token. A missing/mismatched token
// is a local bootstrap error (TOKEN_*), reported without touching the manager.
func (s *Server) authorize(r *http.Request) *chattools.Error {
	got := r.Header.Get("Authorization")
	if got == "" {
		return &chattools.Error{Code: "TOKEN_MISSING", Message: "no session token (LAELIA_SESSION_TOKEN unset)", NextAction: "Run inside a drain session started by `laelia-agent daemon`."}
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
			Conversation: req.Conversation,
			Content:      req.Content,
			BaseVersion:  req.BaseVersion,
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

// validateWorkspacePath resolves path against the agent temp workspace and
// ensures the cleaned, symlink-resolved result stays inside tempDir. This
// prevents file commands from reading/writing outside the workspace.
func (s *Server) validateWorkspacePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.tempDir, path)
	}
	cleaned := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		// EvalSymlinks fails if the file doesn't exist yet (download target).
		// Fall back to the cleaned path and verify its parent resolves inside.
		resolved = cleaned
	}
	rel, err := filepath.Rel(s.tempDir, resolved)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", errors.Errorf("path %q escapes the agent temp workspace", path)
	}
	return resolved, nil
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	s.run(w, r, func(req Request) (string, *chattools.Error) {
		localPath, err := s.validateWorkspacePath(req.LocalPath)
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
	outPath := req.OutPath
	if outPath == "" {
		outPath = filepath.Join(s.tempDir, result.Name)
	}
	resolved, verr := s.validateWorkspacePath(outPath)
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
