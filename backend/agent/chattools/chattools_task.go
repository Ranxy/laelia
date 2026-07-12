package chattools

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// --- Task inputs ----------------------------------------------------------

type ListTasksInput struct {
	Conversation string   `json:"conversation"`
	Statuses     []string `json:"statuses,omitempty"`
}

type ClaimTaskInput struct {
	Message string `json:"message"`
}

type UnclaimTaskInput struct {
	Message string `json:"message"`
}

type UpdateTaskStatusInput struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

type CreateTaskInput struct {
	Conversation  string   `json:"conversation"`
	Content       string   `json:"content"`
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
}

// parseTaskStatus maps a CLI status token to the proto enum. The empty string
// maps to TODO with ok=true so `task list` without --status is unconstrained
// (the caller skips UNSPECIFIED filters). An unknown token is ok=false.
func parseTaskStatus(s string) (v1pb.TaskStatus, bool) {
	switch strings.ToLower(s) {
	case "", "todo":
		return v1pb.TaskStatus_TASK_STATUS_TODO, true
	case "in_progress":
		return v1pb.TaskStatus_TASK_STATUS_IN_PROGRESS, true
	case "in_review":
		return v1pb.TaskStatus_TASK_STATUS_IN_REVIEW, true
	case "done":
		return v1pb.TaskStatus_TASK_STATUS_DONE, true
	}
	return 0, false
}

// taskStatusString renders a TaskStatus for tool output (TODO / IN_PROGRESS /
// IN_REVIEW / DONE), matching the [task #N status=...] badge form.
func taskStatusString(s v1pb.TaskStatus) string {
	switch s {
	case v1pb.TaskStatus_TASK_STATUS_TODO:
		return "TODO"
	case v1pb.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return "IN_PROGRESS"
	case v1pb.TaskStatus_TASK_STATUS_IN_REVIEW:
		return "IN_REVIEW"
	case v1pb.TaskStatus_TASK_STATUS_DONE:
		return "DONE"
	}
	return "UNSPECIFIED"
}

// formatTaskLine renders one task for `task list` output: the message handle
// "<address>:<message-id>" (so the agent can pass it straight to `task claim`/
// `review`/`done`), the task number, status, assignee, and a one-line content
// excerpt. addr is the conversation's display address (resolved once by the
// caller since every task in a listing shares one conversation).
func formatTaskLine(addr string, m *v1pb.ChatMessage) string {
	handle := messageHandle(addr, m.GetName())
	assignee := "none"
	if m.Task != nil && m.Task.AssigneeName != "" {
		assignee = m.Task.AssigneeName
	}
	title := strings.ReplaceAll(m.Content, "\n", " ")
	title = strings.TrimSpace(title)
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80]) + "…"
	}
	return fmt.Sprintf("- %s  #%d  status=%s  assignee=%s  %s\n",
		handle, m.Task.GetTaskNumber(), taskStatusString(m.Task.GetStatus()), assignee, title)
}

// --- Task operations ------------------------------------------------------

// ListTasks returns the task board for a conversation, optionally filtered by
// status. Each line carries the full message resource name so the agent can
// claim/review/done it without reconstructing the name. Run this each drain to
// discover TODO tasks the agent has already acked past (message read only
// returns the cursor delta, so old tasks need an explicit listing).
func ListTasks(ctx context.Context, d Deps, in ListTasksInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required (pass the address from the batch header or `laelia-agent message check`, e.g. #general or dm:@alice)", "")
	}

	var filter []v1pb.TaskStatus
	for _, s := range in.Statuses {
		if s == "" {
			continue
		}
		st, ok := parseTaskStatus(s)
		if !ok {
			return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("unknown task status %q (want todo, in_progress, in_review, or done)", s), "Pass --status with a valid value.")
		}
		filter = append(filter, st)
	}

	resp, err := d.Client.ListTasks(ctx, connect.NewRequest(&v1pb.ListTasksRequest{
		Conversation: name,
		StatusFilter: filter,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}

	addr := conversationAddress(ctx, d, name)
	text := fmt.Sprintf("Tasks in %s (%d):\n", addr, len(resp.Msg.Tasks))
	if len(resp.Msg.Tasks) == 0 {
		text += "(none)\n"
		return text, nil
	}
	for _, t := range resp.Msg.Tasks {
		text += formatTaskLine(addr, t)
	}
	text += "\nPass a task's `<address>:<message-id>` handle to `laelia-agent task claim` (TODO→IN_PROGRESS), `task review` (IN_PROGRESS→IN_REVIEW), or `task done` (IN_REVIEW→DONE).\n"
	return text, nil
}

// ClaimTask atomically claims a TODO task (TODO→IN_PROGRESS, assignee=caller)
// and subscribes the caller to the task's thread so the human's approval reply
// later wakes it. Returns FAILED_PRECONDITION when another agent already owns
// the task or it is not in TODO — the prompt tells the agent to move on to
// other tasks rather than retry.
func ClaimTask(ctx context.Context, d Deps, in ClaimTaskInput) (string, error) {
	if in.Message == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "message is required (the task's `<address>:<message-id>` handle from `task list`)", "Pass the message handle from `laelia-agent task list`.")
	}
	message, err := resolveMessageName(ctx, d, in.Message)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.ClaimTask(ctx, connect.NewRequest(&v1pb.ClaimTaskRequest{Message: message}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	t := resp.Msg.Message.GetTask()
	// Echo the conversation address + the thread-root handle so the agent has
	// the exact thread-send command ready without reconstructing it, and tell
	// it to post ALL work in the task's thread (not the main channel) — the root
	// cause of agents posting task completion to the channel is that the path to
	// the thread was not obvious right after claiming.
	addr := conversationAddress(ctx, d, resp.Msg.Message.GetConversation())
	rootID := resp.Msg.Message.GetName()
	return fmt.Sprintf("Claimed task #%d (status=%s, assignee=you). The task's thread is now subscribed; the human's approval reply will wake you.\n"+
		"Post ALL work on this task in its THREAD — not the main channel. Run `thread read %s --root %s --version <your processed_version>` to get the --base-version, then `thread send %s --root %s --content \"...\" --base-version <that version>`. Do NOT use `message send` for task progress or completion.",
		t.GetTaskNumber(), taskStatusString(t.GetStatus()), addr, rootID, addr, rootID), nil
}

// UnclaimTask releases the caller's claim on a task it owns (IN_PROGRESS→TODO)
// so another agent may claim it. DONE is terminal and cannot be unclaimed.
func UnclaimTask(ctx context.Context, d Deps, in UnclaimTaskInput) (string, error) {
	if in.Message == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "message is required (the task's `<address>:<message-id>` handle)", "Pass the message handle from `laelia-agent task list`.")
	}
	message, err := resolveMessageName(ctx, d, in.Message)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.UnclaimTask(ctx, connect.NewRequest(&v1pb.UnclaimTaskRequest{Message: message}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	t := resp.Msg.Message.GetTask()
	return fmt.Sprintf("Released task #%d back to %s; another agent may now claim it.", t.GetTaskNumber(), taskStatusString(t.GetStatus())), nil
}

// UpdateTaskStatus advances a task the caller owns: IN_PROGRESS→IN_REVIEW
// (ready for human review) or IN_REVIEW→DONE (complete, after detecting the
// human's approval in the task's thread). TODO→IN_PROGRESS is performed by
// ClaimTask, not here.
func UpdateTaskStatus(ctx context.Context, d Deps, in UpdateTaskStatusInput) (string, error) {
	if in.Message == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "message is required (the task's `<address>:<message-id>` handle)", "Pass the message handle from `laelia-agent task list`.")
	}
	target, ok := parseTaskStatus(in.Status)
	if !ok || target == v1pb.TaskStatus_TASK_STATUS_TODO || target == v1pb.TaskStatus_TASK_STATUS_UNSPECIFIED {
		return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("status must be in_review or done, got %q", in.Status), "Use `task review` for in_review or `task done` for done.")
	}
	message, err := resolveMessageName(ctx, d, in.Message)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.UpdateTaskStatus(ctx, connect.NewRequest(&v1pb.UpdateTaskStatusRequest{Message: message, Status: target}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	t := resp.Msg.Message.GetTask()
	return fmt.Sprintf("Task #%d is now %s.", t.GetTaskNumber(), taskStatusString(t.GetStatus())), nil
}

// CreateTask posts a new top-level task message in a channel (an agent breaks
// work into subtasks for others to claim). The new task is unassigned (TODO);
// the posting agent does NOT auto-claim it. Other agent members are woken so
// they can claim it.
func CreateTask(ctx context.Context, d Deps, in CreateTaskInput) (string, error) {
	name, err := resolveConversationAddress(ctx, d, in.Conversation)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", localError("MISSING_CONVERSATION", "conversation is required", "")
	}
	if in.Content == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "content is required", "Pass --content <text|->.")
	}

	var attachments []*v1pb.Attachment
	for _, id := range in.AttachmentIDs {
		if id == "" {
			continue
		}
		attachments = append(attachments, &v1pb.Attachment{Id: id})
	}

	resp, err := d.Client.CreateTask(ctx, connect.NewRequest(&v1pb.CreateTaskRequest{
		Conversation: name,
		Content:      in.Content,
		Attachments:  attachments,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	t := resp.Msg.Message.GetTask()
	return fmt.Sprintf("Created task #%d (status=%s) in %s; it is unassigned — other agents may claim it.", t.GetTaskNumber(), taskStatusString(t.GetStatus()), conversationAddress(ctx, d, name)), nil
}
