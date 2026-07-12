package chattools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// --- Reminder inputs ------------------------------------------------------

type ConvertMessageToReminderInput struct {
	Message     string `json:"message"`
	TaskContent string `json:"task_content"`
	FireAt      string `json:"fire_at"`
	CronExpr    string `json:"cron_expr,omitempty"`
	Tz          string `json:"tz,omitempty"`
}

type ListDueRemindersInput struct{}

type ListRemindersInput struct {
	Conversation string   `json:"conversation"`
	Statuses     []string `json:"statuses,omitempty"`
}

type CompleteReminderInput struct {
	Name   string `json:"name"`
	Result string `json:"result"`
}

type FailReminderInput struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type UpdateReminderInput struct {
	Name        string `json:"name"`
	TaskContent string `json:"task_content"`
	FireAt      string `json:"fire_at"`
	CronExpr    string `json:"cron_expr,omitempty"`
	Tz          string `json:"tz,omitempty"`
}

type CancelReminderInput struct {
	Name string `json:"name"`
}

// reminderStatusString renders a ReminderStatus for tool output.
func reminderStatusString(s v1pb.ReminderStatus) string {
	switch s {
	case v1pb.ReminderStatus_REMINDER_STATUS_PENDING:
		return "PENDING"
	case v1pb.ReminderStatus_REMINDER_STATUS_DUE:
		return "DUE"
	case v1pb.ReminderStatus_REMINDER_STATUS_COMPLETED:
		return "COMPLETED"
	case v1pb.ReminderStatus_REMINDER_STATUS_CANCELLED:
		return "CANCELLED"
	case v1pb.ReminderStatus_REMINDER_STATUS_MISSED:
		return "MISSED"
	case v1pb.ReminderStatus_REMINDER_STATUS_FAILED:
		return "FAILED"
	}
	return "UNSPECIFIED"
}

// formatReminderLine renders one reminder for `reminder list-due` output: the
// reminder resource name (so the agent can pass it straight to `reminder
// complete`/`fail`), the conversation, status, fire time, and a one-line task
// excerpt.
func formatReminderLine(r *v1pb.Reminder) string {
	task := strings.ReplaceAll(r.TaskContent, "\n", " ")
	task = strings.TrimSpace(task)
	if len([]rune(task)) > 80 {
		task = string([]rune(task)[:80]) + "…"
	}
	fire := ""
	if r.FireAt != nil {
		fire = r.FireAt.AsTime().Format(time.RFC3339)
	}
	return fmt.Sprintf("- %s  status=%s  fire_at=%s  tz=%s  cron=%q  %s\n",
		r.Name, reminderStatusString(r.Status), fire, r.Tz, r.CronExpr, task)
}

// parseFireAtTime parses an RFC3339 fire-at string into a time.Time. The empty
// string is an error: a one-shot reminder must have an explicit fire time
// (callers that allow an empty fire_at in favor of --cron check for empty
// before calling).
func parseFireAtTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, localError("INVALID_ARGUMENT_FAILED", "fire_at is required (RFC3339, e.g. 2026-07-07T03:00:00Z)", "Pass --fire-at with an RFC3339 timestamp.")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("invalid fire_at %q (want RFC3339)", s), "Use an RFC3339 timestamp like 2026-07-07T03:00:00Z.")
	}
	return t, nil
}

// ConvertMessageToReminder turns the trigger message into a scheduled reminder
// owned by the caller (atomic create+claim). The agent recognizes the scheduling
// intent in a channel message and calls this with a structured task summary and
// the schedule (one-shot fire_at, or recurring cron + tz).
func ConvertMessageToReminder(ctx context.Context, d Deps, in ConvertMessageToReminderInput) (string, error) {
	if in.Message == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "message is required (the trigger message's `<address>:<message-id>` handle)", "Pass the message handle from `laelia-agent message read`.")
	}
	if strings.TrimSpace(in.TaskContent) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "task_content is required (a structured summary of the scheduled work)", "Pass --task-content with the work summary.")
	}
	// fire_at is required for a one-shot reminder; for a recurring reminder
	// (--cron) it may be omitted and the manager computes the first fire from
	// the cron expression starting at now. The manager returns the resolved
	// fire_at in the Reminder, so the agent learns the first fire without
	// computing it itself.
	var fireAtPB *timestamppb.Timestamp
	if strings.TrimSpace(in.FireAt) != "" {
		fireAt, err := parseFireAtTime(in.FireAt)
		if err != nil {
			return "", err
		}
		fireAtPB = timestamppb.New(fireAt)
	} else if strings.TrimSpace(in.CronExpr) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED",
			"either --fire-at (one-shot) or --cron (recurring) is required",
			"Pass --fire-at with an RFC3339 timestamp for a one-shot reminder, or --cron for a recurring reminder (the manager computes the first fire).")
	}
	// Resolve the trigger message AFTER the schedule is validated: resolving a
	// "dm:@<peer>" message address creates the DM as a side effect, which must
	// not leak on a request that is about to fail validation.
	message, err := resolveMessageName(ctx, d, in.Message)
	if err != nil {
		return "", err
	}
	resp, err := d.Client.ConvertMessageToReminder(ctx, connect.NewRequest(&v1pb.ConvertMessageToReminderRequest{
		Message:     message,
		TaskContent: in.TaskContent,
		FireAt:      fireAtPB,
		CronExpr:    in.CronExpr,
		Tz:          in.Tz,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	r := resp.Msg.Reminder
	return fmt.Sprintf("Created reminder %s (status=%s, fire_at=%s, cron=%q, tz=%s). You own it; its discussion thread is rooted at the trigger message. Post schedule changes in the thread, or run `reminder update %s ...`.", r.Name, reminderStatusString(r.Status), r.FireAt.AsTime().Format(time.RFC3339), r.CronExpr, r.Tz, r.Name), nil
}

// parseReminderStatus maps a CLI status token to the proto enum. The empty
// string maps to UNSPECIFIED with ok=true so `reminder list` without --status is
// unconstrained (the manager skips UNSPECIFIED filters).
func parseReminderStatus(s string) (v1pb.ReminderStatus, bool) {
	switch strings.ToLower(s) {
	case "", "pending":
		return v1pb.ReminderStatus_REMINDER_STATUS_PENDING, true
	case "due":
		return v1pb.ReminderStatus_REMINDER_STATUS_DUE, true
	case "completed":
		return v1pb.ReminderStatus_REMINDER_STATUS_COMPLETED, true
	case "cancelled":
		return v1pb.ReminderStatus_REMINDER_STATUS_CANCELLED, true
	case "missed":
		return v1pb.ReminderStatus_REMINDER_STATUS_MISSED, true
	case "failed":
		return v1pb.ReminderStatus_REMINDER_STATUS_FAILED, true
	}
	return 0, false
}

// ListReminders returns reminders for the calling agent (and optionally a
// conversation / status filter). The agent uses this to see its own reminder
// board (mirrors `task list`).
func ListReminders(ctx context.Context, d Deps, in ListRemindersInput) (string, error) {
	var filter []v1pb.ReminderStatus
	for _, s := range in.Statuses {
		if s == "" {
			continue
		}
		st, ok := parseReminderStatus(s)
		if !ok {
			return "", localError("INVALID_ARGUMENT_FAILED", fmt.Sprintf("unknown reminder status %q (want pending, due, completed, cancelled, missed, or failed)", s), "Pass --status with a valid value.")
		}
		filter = append(filter, st)
	}
	req := &v1pb.ListRemindersRequest{StatusFilter: filter}
	if in.Conversation != "" {
		name, err := resolveConversationAddress(ctx, d, in.Conversation)
		if err != nil {
			return "", err
		}
		req.Conversation = name
	}
	resp, err := d.Client.ListReminders(ctx, connect.NewRequest(req))
	if err != nil {
		return "", wrapManagerError(err)
	}
	text := fmt.Sprintf("Reminders (%d):\n", len(resp.Msg.Reminders))
	if len(resp.Msg.Reminders) == 0 {
		text += "(none)\n"
		return text, nil
	}
	for _, r := range resp.Msg.Reminders {
		text += formatReminderLine(r)
	}
	return text, nil
}

// ListDueReminders returns the DUE reminders owned by the calling agent, for
// the autonomous drain loop. Run this at step 0 of the cold-start init prompt
// (warm/resumed turns are nudged to run it by a line appended to the turn
// batch instead), and process each due reminder by doing its work and calling
// `reminder complete` (or `reminder fail`).
func ListDueReminders(ctx context.Context, d Deps, _ ListDueRemindersInput) (string, error) {
	resp, err := d.Client.ListDueReminders(ctx, connect.NewRequest(&v1pb.ListDueRemindersRequest{}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	text := fmt.Sprintf("Due reminders (%d):\n", len(resp.Msg.Reminders))
	if len(resp.Msg.Reminders) == 0 {
		text += "(none)\n"
		return text, nil
	}
	for _, r := range resp.Msg.Reminders {
		text += formatReminderLine(r)
	}
	text += "\nFor each due reminder: do the work, then run `laelia-agent reminder complete <name> --result \"...\"` (or `reminder fail <name> --error \"...\"`).\n"
	return text, nil
}

// CompleteReminder marks a DUE reminder completed and posts the result to its
// thread. The backend posts the message atomically — do NOT also post to the
// thread yourself. Recurring reminders reschedule to the next cron fire.
func CompleteReminder(ctx context.Context, d Deps, in CompleteReminderInput) (string, error) {
	if in.Name == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "name is required (the reminder's reminders/{message_id} name from `reminder list-due`)", "Pass the name from `laelia-agent reminder list-due`.")
	}
	if strings.TrimSpace(in.Result) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "result is required (the completion report, posted to the reminder's thread)", "Pass --result with the completion report.")
	}
	resp, err := d.Client.CompleteReminder(ctx, connect.NewRequest(&v1pb.CompleteReminderRequest{Name: in.Name, Result: in.Result}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	r := resp.Msg.Reminder
	return fmt.Sprintf("Reminder %s is now %s. The completion report was posted to its thread by the manager — do not post it again.", r.Name, reminderStatusString(r.Status)), nil
}

// FailReminder marks a DUE reminder failed and posts the error to its thread.
// Recurring reminders reschedule; one-shot reminders stay FAILED.
func FailReminder(ctx context.Context, d Deps, in FailReminderInput) (string, error) {
	if in.Name == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "name is required", "Pass the name from `laelia-agent reminder list-due`.")
	}
	if strings.TrimSpace(in.Error) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "error is required (the failure reason, posted to the thread)", "Pass --error with the failure reason.")
	}
	resp, err := d.Client.FailReminder(ctx, connect.NewRequest(&v1pb.FailReminderRequest{Name: in.Name, Error: in.Error}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	r := resp.Msg.Reminder
	return fmt.Sprintf("Reminder %s is now %s. The failure report was posted to its thread by the manager.", r.Name, reminderStatusString(r.Status)), nil
}

// UpdateReminder edits a reminder's schedule and/or task content. The request
// replaces the schedule entirely, so pass all fields (load the current reminder
// first). Used when the user asks in the reminder's thread to change the
// schedule or task.
func UpdateReminder(ctx context.Context, d Deps, in UpdateReminderInput) (string, error) {
	if in.Name == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "name is required", "Pass the reminder's reminders/{message_id} name.")
	}
	if strings.TrimSpace(in.TaskContent) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "task_content is required", "Pass --task-content.")
	}
	// fire_at may be omitted when cron is set; the manager computes the next
	// fire. Re-allow an empty fire_at here only when cron is provided.
	var fireAtPB *timestamppb.Timestamp
	if strings.TrimSpace(in.FireAt) != "" {
		fireAt, err := parseFireAtTime(in.FireAt)
		if err != nil {
			return "", err
		}
		fireAtPB = timestamppb.New(fireAt)
	} else if strings.TrimSpace(in.CronExpr) == "" {
		return "", localError("INVALID_ARGUMENT_FAILED",
			"either --fire-at (one-shot) or --cron (recurring) is required",
			"Pass --fire-at with an RFC3339 timestamp for a one-shot reminder, or --cron for a recurring reminder (the manager computes the first fire).")
	}
	resp, err := d.Client.UpdateReminder(ctx, connect.NewRequest(&v1pb.UpdateReminderRequest{
		Name:        in.Name,
		FireAt:      fireAtPB,
		CronExpr:    in.CronExpr,
		Tz:          in.Tz,
		TaskContent: in.TaskContent,
	}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	r := resp.Msg.Reminder
	return fmt.Sprintf("Reminder %s updated (status=%s, fire_at=%s, cron=%q, tz=%s).", r.Name, reminderStatusString(r.Status), r.FireAt.AsTime().Format(time.RFC3339), r.CronExpr, r.Tz), nil
}

// CancelReminder cancels a reminder owned by the caller.
func CancelReminder(ctx context.Context, d Deps, in CancelReminderInput) (string, error) {
	if in.Name == "" {
		return "", localError("INVALID_ARGUMENT_FAILED", "name is required", "Pass the reminder's reminders/{message_id} name.")
	}
	resp, err := d.Client.CancelReminder(ctx, connect.NewRequest(&v1pb.CancelReminderRequest{Name: in.Name}))
	if err != nil {
		return "", wrapManagerError(err)
	}
	r := resp.Msg.Reminder
	return fmt.Sprintf("Reminder %s is now %s.", r.Name, reminderStatusString(r.Status)), nil
}
