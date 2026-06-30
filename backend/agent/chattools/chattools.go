// Package chattools contains the transport-agnostic logic for the six chat
// operations an autonomous drain session performs against the manager. Both the
// local daemon server (which the LLM-driven CLI subcommands call over a unix
// socket) and tests call into these functions; neither MCP nor any specific
// transport is involved here.
//
// Each function takes a Deps (the live CommandServiceClient plus the per-call
// identity) and an operation-specific input, and returns the canonical
// human-readable text the CLI prints to stdout on success, or a *Error whose
// Code/NextAction the CLI renders to stderr on failure.
package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// Deps bundles the per-call dependencies: a Connect client carrying the live
// access token, plus the identity that the manager scopes the call to. Agent is
// the "agents/<id>" resource name (used to resolve command context). Command is
// the drain session's command_id, linked to post_message/ack so the frontend
// can attribute the conversation activity.
type Deps struct {
	Client  v1connect.CommandServiceClient
	Agent   string
	Command string
}

// Error is the canonical failure envelope. Code is a stable machine-oriented
// code (see the prefix legend in the prompt); Message is a human-readable
// summary; NextAction is an optional recovery hint.
type Error struct {
	Code       string
	Message    string
	NextAction string
}

func (e *Error) Error() string { return e.Message }

// wrapManagerError maps a Connect error returned by the manager into a stable
// *Error. 4xx failures become *_FAILED; 5xx / transport failures become
// SERVER_5XX.
func wrapManagerError(err error) *Error {
	if err == nil {
		return nil
	}
	switch connect.CodeOf(err) {
	case connect.CodeNotFound:
		return &Error{Code: "NOT_FOUND_FAILED", Message: err.Error(), NextAction: "Check the conversation/command id; it may not exist or you may not be a member."}
	case connect.CodePermissionDenied:
		return &Error{Code: "PERMISSION_FAILED", Message: err.Error(), NextAction: "You lack access to this resource; do not retry unchanged."}
	case connect.CodeInvalidArgument:
		return &Error{Code: "INVALID_ARGUMENT_FAILED", Message: err.Error(), NextAction: "Fix the command arguments and retry."}
	case connect.CodeUnauthenticated:
		return &Error{Code: "AUTH_FAILED", Message: err.Error(), NextAction: "The agent's access token was rejected; this is transient if the daemon is mid-rotation — retry once."}
	case connect.CodeInternal, connect.CodeUnavailable, connect.CodeUnknown, connect.CodeDeadlineExceeded:
		return &Error{Code: "SERVER_5XX", Message: err.Error(), NextAction: "The manager is unavailable or crashed; retry with backoff."}
	default:
		return &Error{Code: "REQUEST_FAILED", Message: err.Error()}
	}
}

// localError builds a local bootstrap-phase *Error (MISSING_*/TOKEN_* prefix).
func localError(code, message, nextAction string) *Error {
	return &Error{Code: code, Message: message, NextAction: nextAction}
}

// normalizeConversationName turns a raw conversation id into the
// "conversations/<id>" form the manager expects. Names already in that form
// pass through unchanged.
func normalizeConversationName(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "conversations/") {
		return s
	}
	return fmt.Sprintf("conversations/%s", s)
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

// formatAttachments renders a message's attachments as indented lines that
// mirror the `file list` format (id/name/size/mime), or "" when there are none.
// Surfacing them here is what lets the agent tie a message like "test file" to
// the file it must `file download <id>` to actually read — without this the
// attachment metadata the manager returns never reaches the LLM.
func formatAttachments(attachments []*v1pb.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	out := "  attachments:\n"
	for _, a := range attachments {
		out += fmt.Sprintf("    - id=%s  name=%s  size=%d  mime=%s\n", a.Id, a.Name, a.SizeBytes, a.MimeType)
	}
	return out
}

// formatMessageLine renders one message for the text output. Own messages are
// tagged "(YOU)" so the agent can recognize its own past messages at a glance
// and avoid replying to itself. Any attachments follow on indented lines.
func formatMessageLine(timestamp, senderName, senderType string, isOwn bool, content string, attachments []*v1pb.Attachment) string {
	senderTag := senderType
	if isOwn {
		senderTag += ", YOU"
	}
	return fmt.Sprintf("[%s] %s (%s): %s\n", timestamp, senderName, senderTag, content) + formatAttachments(attachments)
}

// --- Inputs ---------------------------------------------------------------

type SearchChatHistoryInput struct {
	Conversation string `json:"conversation,omitempty"`
	Query        string `json:"query"`
	Since        string `json:"since,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	PageToken    string `json:"page_token,omitempty"`
}

type GetCommandContextInput struct {
	CommandID string `json:"command_id"`
}

type GetConversationMessagesInput struct {
	Conversation string `json:"conversation"`
	Version      int64  `json:"version,omitempty"`
	Direction    string `json:"direction,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type PostMessageInput struct {
	Conversation  string   `json:"conversation"`
	Content       string   `json:"content"`
	BaseVersion   int64    `json:"base_version"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
}

type AckProcessedVersionInput struct {
	Conversation     string `json:"conversation"`
	ProcessedVersion int64  `json:"processed_version"`
}

type UploadFileInput struct {
	Conversation string `json:"conversation,omitempty"`
	OriginalName string `json:"original_name"`
	MimeType     string `json:"mime_type,omitempty"`
	Data         []byte `json:"data"`
}

type DownloadFileInput struct {
	FileID string `json:"file_id"`
}

type ListFilesInput struct {
	Conversation string `json:"conversation"`
}

// --- Operations ------------------------------------------------------------

// SearchChatHistory searches past chat messages by keyword and optional time
// range, returning matching user messages and agent replies.
func SearchChatHistory(ctx context.Context, d Deps, in SearchChatHistoryInput) (string, error) {
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	reqMsg := &v1pb.SearchChatHistoryRequest{
		Query:     in.Query,
		Limit:     int32(limit),
		PageToken: in.PageToken,
	}
	if name := normalizeConversationName(in.Conversation); name != "" {
		reqMsg.Conversation = name
	}
	resp, err := d.Client.SearchChatHistory(ctx, connect.NewRequest(reqMsg))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Found %d matching messages", len(resp.Msg.Entries))
	if resp.Msg.NextPageToken != "" {
		text += " (more results available — use page_token to continue)"
	}
	text += ":\n"
	for _, e := range resp.Msg.Entries {
		text += formatMessageLine(
			e.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
			e.SenderName, senderTypeString(e.SenderType), e.IsOwn, e.Content, e.Attachments,
		)
	}
	return text, nil
}

// GetCommandContext returns the full execution context (instruction, agent
// reply, event log) behind a specific agent reply, by its command id. When
// CommandID is empty it falls back to the session's command id in d.Command.
func GetCommandContext(ctx context.Context, d Deps, in GetCommandContextInput) (string, error) {
	commandID := in.CommandID
	if commandID == "" {
		commandID = d.Command
	}
	if commandID == "" {
		return "", localError("MISSING_COMMAND", "command_id is required (pass --command-id or run within a drain session)", "")
	}
	if d.Agent == "" {
		return "", localError("MISSING_AGENT", "agent resource name is required", "")
	}

	name := fmt.Sprintf("agents/%s/commands/%s", d.Agent, commandID)
	resp, err := d.Client.GetCommandContext(ctx, connect.NewRequest(&v1pb.GetCommandContextRequest{Name: name}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	events := resp.Msg.Events
	text := fmt.Sprintf("Command context for %s:\nUser message: %s\nAgent reply: %s\nEvents (%d total):\n",
		commandID, resp.Msg.Command.Instruction, resp.Msg.Command.FinalSummary, len(events))
	for _, ev := range events {
		text += fmt.Sprintf("  [%d] %s: %s\n", ev.SeqNo, ev.Type.String(), ev.Summary)
	}
	return text, nil
}

// GetConversationMessages lists messages in a conversation relative to a known
// room version. direction="after" (default) returns messages newer than version;
// "before" returns up to limit prior messages (oldest→newest) for context
// recovery. The returned text states current_version, which the caller needs as
// base_version for PostMessage and processed_version for AckProcessedVersion.
func GetConversationMessages(ctx context.Context, d Deps, in GetConversationMessagesInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required (pass the conversation name from `laelia-agent message check`)", "")
	}

	direction := in.Direction
	if direction == "" {
		direction = "after"
	}
	if direction != "before" && direction != "after" {
		return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("direction must be \"before\" or \"after\", got %q", direction), "Use --after or --before.")
	}

	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	reqMsg := &v1pb.ListConversationMessagesRequest{
		Conversation: name,
		PageSize:     int32(limit),
	}
	if in.Version > 0 {
		if direction == "before" {
			reqMsg.BeforeVersion = in.Version
		} else {
			reqMsg.AfterVersion = in.Version
		}
	}
	resp, err := d.Client.ListConversationMessages(ctx, connect.NewRequest(reqMsg))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Conversation messages (current_version: %d):\n", resp.Msg.CurrentVersion)
	if len(resp.Msg.Messages) == 0 {
		text += "(no messages)\n"
	} else {
		for _, m := range resp.Msg.Messages {
			text += formatMessageLine(
				m.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
				m.SenderName, senderTypeString(m.SenderType), m.IsOwn, m.Content, m.Attachments,
			)
		}
		if direction == "before" && len(resp.Msg.Messages) == limit {
			oldest := resp.Msg.Messages[0].RoomVersion
			text += fmt.Sprintf("(older messages may exist — call again with --version %d --before to page further back)\n", oldest)
		}
	}
	return text, nil
}

// PostMessage posts a reply to a conversation using optimistic concurrency. If
// the server reports committed=false, new messages arrived while the agent was
// thinking — this is NOT an error; the returned text lists the new messages and
// tells the agent to re-read and retry with the updated base_version.
func PostMessage(ctx context.Context, d Deps, in PostMessageInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required (pass the conversation name from `laelia-agent message check`)", "")
	}

	// Build id-only attachment references; the manager resolves each id to full
	// metadata (name/mime/size) from the file row and checks it belongs to this
	// conversation. The agent only ever has the id (from `file upload` output).
	var attachments []*v1pb.Attachment
	for _, id := range in.AttachmentIDs {
		if id == "" {
			continue
		}
		attachments = append(attachments, &v1pb.Attachment{Id: id})
	}

	req := connect.NewRequest(&v1pb.PostMessageRequest{
		Conversation: name,
		Content:      in.Content,
		BaseVersion:  in.BaseVersion,
		CommandId:    d.Command,
		Attachments:  attachments,
	})
	resp, err := d.Client.PostMessage(ctx, req)
	if err != nil {
		return "", wrapManagerError(err)
	}

	if resp.Msg.Committed {
		return fmt.Sprintf("Message posted successfully (version: %d)", resp.Msg.CurrentVersion), nil
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
			text += formatMessageLine(ts, m.SenderName, senderTypeString(m.SenderType), m.IsOwn, m.Content, m.Attachments)
		}
	}
	text += fmt.Sprintf("\nTo resolve: call `laelia-agent message read %s --version %d --after` to get full context, then call `laelia-agent message send` again with --base-version %d.",
		name, resp.Msg.CurrentVersion, resp.Msg.CurrentVersion)
	return text, nil
}

// ListChannelUpdates is the agent's "what's worth my context" discovery (AX
// Agent Inbox). It returns every channel the agent is a member of whose
// room_version is beyond the agent's durable cursor. An empty list means idle.
func ListChannelUpdates(ctx context.Context, d Deps) (string, error) {
	resp, err := d.Client.ListChannelUpdates(ctx, connect.NewRequest(&v1pb.ListChannelUpdatesRequest{}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Channels with unread messages (%d):\n", len(resp.Msg.Updates))
	if len(resp.Msg.Updates) == 0 {
		text += "(none — you are idle; end your turn without calling any other command)\n"
	} else {
		for _, u := range resp.Msg.Updates {
			text += fmt.Sprintf("- %s: %d new (current_version=%d, your processed_version=%d)\n",
				u.Conversation, u.NewMessageCount, u.CurrentVersion, u.ProcessedVersion)
		}
		text += "\nPick ONE channel. Call `laelia-agent message read <conversation> --version <processed_version> --after` to read the new messages.\n"
	}
	return text, nil
}

// AckProcessedVersion advances the agent's durable per-channel cursor. The
// agent MUST call this after finishing a channel (reply or silence) so the next
// ListChannelUpdates no longer reports it. The session's command_id links the
// session's command to the conversation for frontend visibility.
func AckProcessedVersion(ctx context.Context, d Deps, in AckProcessedVersionInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	if in.ProcessedVersion <= 0 {
		return "", localError("INVALID_ARGUMENT_FAILED", "processed_version must be positive", "Pass --processed-version with the current_version from `laelia-agent message read`.")
	}

	resp, err := d.Client.AckProcessedVersion(ctx, connect.NewRequest(&v1pb.AckProcessedVersionRequest{
		Conversation:     name,
		ProcessedVersion: in.ProcessedVersion,
		CommandId:        d.Command,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return fmt.Sprintf("Cursor advanced to processed_version=%d for %s.", resp.Msg.ProcessedVersion, name), nil
}

// UploadFile uploads a blob to S3 via the manager. Returns the canonical text
// (including the new file id) on success, or a *Error on failure.
func UploadFile(ctx context.Context, d Deps, in UploadFileInput) (string, error) {
	if in.OriginalName == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "original_name is required", "")
	}
	if len(in.Data) == 0 {
		return "", localError("INVALID_ARGUMENT_FAILED", "data is empty", "")
	}

	reqMsg := &v1pb.UploadFileRequest{
		OriginalName: in.OriginalName,
		MimeType:     in.MimeType,
		Data:         in.Data,
	}
	if name := normalizeConversationName(in.Conversation); name != "" {
		reqMsg.Conversation = name
	}
	resp, err := d.Client.UploadFile(ctx, connect.NewRequest(reqMsg))
	if err != nil {
		return "", wrapManagerError(err)
	}
	return fmt.Sprintf("Uploaded file %s (%s, %d bytes)", resp.Msg.Id, resp.Msg.OriginalName, resp.Msg.SizeBytes), nil
}

// DownloadFileResult holds the downloaded bytes and a canonical summary text.
type DownloadFileResult struct {
	Text string
	Data []byte
	Name string
}

// DownloadFile fetches a file's bytes from S3 via the manager. The returned
// Data is meant to be written to the agent's temp workspace by the daemon; the
// caller does not print it.
func DownloadFile(ctx context.Context, d Deps, in DownloadFileInput) (*DownloadFileResult, error) {
	if in.FileID == "" {
		return nil, localError("INVALID_ARGUMENT_FAILED", "file_id is required", "")
	}
	resp, err := d.Client.DownloadFile(ctx, connect.NewRequest(&v1pb.DownloadFileRequest{Id: in.FileID}))
	if err != nil {
		return nil, wrapManagerError(err)
	}
	return &DownloadFileResult{
		Text: fmt.Sprintf("Downloaded file %s (%s, %d bytes)", resp.Msg.File.Id, resp.Msg.File.OriginalName, resp.Msg.File.SizeBytes),
		Data: resp.Msg.Data,
		Name: resp.Msg.File.OriginalName,
	}, nil
}

// ListFiles lists the files attached to a conversation. The agent must be a
// member.
func ListFiles(ctx context.Context, d Deps, in ListFilesInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	resp, err := d.Client.ListFiles(ctx, connect.NewRequest(&v1pb.ListFilesRequest{Conversation: name}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	text := fmt.Sprintf("Files in %s (%d):\n", name, len(resp.Msg.Files))
	if len(resp.Msg.Files) == 0 {
		text += "(none)\n"
		return text, nil
	}
	for _, f := range resp.Msg.Files {
		text += fmt.Sprintf("- id=%s  name=%s  size=%d  mime=%s\n", f.Id, f.OriginalName, f.SizeBytes, f.MimeType)
	}
	text += "\nPass an id to `laelia-agent file download <id>` to fetch a file into your temp workspace.\n"
	return text, nil
}

// --- Thread inputs ---------------------------------------------------------

type ListThreadUpdatesInput struct{}

type GetThreadMessagesInput struct {
	Conversation string `json:"conversation"`
	Root         string `json:"root"`
	Version      int64  `json:"version,omitempty"`
	Direction    string `json:"direction,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type PostThreadMessageInput struct {
	Conversation  string   `json:"conversation"`
	Root          string   `json:"root"`
	Content       string   `json:"content"`
	BaseVersion   int64    `json:"base_version"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
}

// --- Thread operations ----------------------------------------------------

// ListThreadUpdates is the agent's thread inbox: every thread the agent is
// subscribed to (via @mention or having replied) that has replies beyond the
// agent's per-channel cursor for that conversation. Run this after
// `message check` for each channel with updates, BEFORE acking — acking
// advances the conversation cursor past unread thread replies, so the agent
// must read every subscribed thread first.
func ListThreadUpdates(ctx context.Context, d Deps, _ ListThreadUpdatesInput) (string, error) {
	resp, err := d.Client.ListThreadUpdates(ctx, connect.NewRequest(&v1pb.ListThreadUpdatesRequest{}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Threads with unread replies (%d):\n", len(resp.Msg.Updates))
	if len(resp.Msg.Updates) == 0 {
		text += "(none — no subscribed thread has new replies)\n"
		return text, nil
	}
	for _, u := range resp.Msg.Updates {
		text += fmt.Sprintf("- %s thread %s: %d new replies (latest_version=%d)\n",
			u.Conversation, u.ThreadRoot, u.NewReplyCount, u.LatestVersion)
	}
	text += "\nFor each thread, call `laelia-agent thread read <conversation> --root <thread_root> --version <your processed_version for that conversation>` (default direction returns replies newer than that version) to read the new replies, then reply with `laelia-agent thread send` if you should respond.\n"
	return text, nil
}

// GetThreadMessages reads one thread — the root message (as context) followed
// by its replies — relative to a known room version. The root is always
// included first so the agent has the thread context even on a delta read.
// direction="after" (default) returns replies newer than version; "before"
// returns up to limit prior replies (oldest→newest). The returned text states
// current_version, which the caller needs as base_version for thread send and
// (with the rest of the channel) processed_version for message ack.
func GetThreadMessages(ctx context.Context, d Deps, in GetThreadMessagesInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	if in.Root == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "root is required (the thread root message id from `thread check`)", "Pass --root <thread_root>.")
	}

	direction := in.Direction
	if direction == "" {
		direction = "after"
	}
	if direction != "before" && direction != "after" {
		return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("direction must be \"before\" or \"after\", got %q", direction), "Use --after or --before.")
	}

	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	reqMsg := &v1pb.ListThreadMessagesRequest{
		Conversation: name,
		ThreadRoot:   in.Root,
		PageSize:     int32(limit),
	}
	if in.Version > 0 {
		if direction == "before" {
			reqMsg.BeforeVersion = in.Version
		} else {
			reqMsg.AfterVersion = in.Version
		}
	}
	resp, err := d.Client.ListThreadMessages(ctx, connect.NewRequest(reqMsg))
	if err != nil {
		return "", wrapManagerError(err)
	}

	text := fmt.Sprintf("Thread messages (current_version: %d):\n", resp.Msg.CurrentVersion)
	if len(resp.Msg.Messages) == 0 {
		text += "(no messages)\n"
		return text, nil
	}
	// The first message is the thread root (context); label it so the agent
	// distinguishes the root from the replies it must respond to.
	for i, m := range resp.Msg.Messages {
		ts := m.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z")
		line := formatMessageLine(ts, m.SenderName, senderTypeString(m.SenderType), m.IsOwn, m.Content, m.Attachments)
		if i == 0 {
			text += "[ROOT] " + line
		} else {
			text += line
		}
	}
	if direction == "before" && len(resp.Msg.Messages)-1 == limit {
		oldest := resp.Msg.Messages[1].RoomVersion
		text += fmt.Sprintf("(older replies may exist — call again with --version %d --before to page further back)\n", oldest)
	}
	return text, nil
}

// PostThreadMessage posts a reply into a thread using optimistic concurrency,
// mirroring PostMessage. The thread_root anchors the reply to the thread. On a
// committed=false conflict the returned text lists the new messages and tells
// the agent to re-read and retry with the updated base_version.
func PostThreadMessage(ctx context.Context, d Deps, in PostThreadMessageInput) (string, error) {
	name := normalizeConversationName(in.Conversation)
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	if in.Root == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "root is required (the thread root message id)", "Pass --root <thread_root>.")
	}

	var attachments []*v1pb.Attachment
	for _, id := range in.AttachmentIDs {
		if id == "" {
			continue
		}
		attachments = append(attachments, &v1pb.Attachment{Id: id})
	}

	req := connect.NewRequest(&v1pb.PostMessageRequest{
		Conversation: name,
		Content:      in.Content,
		BaseVersion:  in.BaseVersion,
		CommandId:    d.Command,
		Attachments:  attachments,
		ThreadRoot:   in.Root,
	})
	resp, err := d.Client.PostMessage(ctx, req)
	if err != nil {
		return "", wrapManagerError(err)
	}

	if resp.Msg.Committed {
		return fmt.Sprintf("Thread reply posted successfully (version: %d)", resp.Msg.CurrentVersion), nil
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
			text += formatMessageLine(ts, m.SenderName, senderTypeString(m.SenderType), m.IsOwn, m.Content, m.Attachments)
		}
	}
	text += fmt.Sprintf("\nTo resolve: call `laelia-agent thread read %s --root %s --version %d` to get full context, then call `laelia-agent thread send` again with --base-version %d.",
		name, in.Root, resp.Msg.CurrentVersion, resp.Msg.CurrentVersion)
	return text, nil
}
