package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(reminderCmd)
	reminderCmd.AddCommand(
		reminderConvertCmd,
		reminderListCmd,
		reminderListDueCmd,
		reminderUpdateCmd,
		reminderCancelCmd,
		reminderCompleteCmd,
		reminderFailCmd,
	)
}

var reminderCmd = &cobra.Command{
	Use:   "reminder",
	Short: "Convert messages to scheduled reminders, list due reminders, and complete/fail them (LLM-facing, used during drain sessions)",
}

// reminder convert <message-name> --content <text|-> --fire-at <RFC3339>
// [--cron <5-field>] [--tz <IANA>] atomically creates+claims a reminder owned by
// the caller, rooted at the trigger message. One-shot needs --fire-at;
// recurring needs --cron (+ optional --tz, default UTC) and --fire-at is the
// first fire (or it is computed from cron at the manager if omitted when --cron
// is set — but the CLI requires --fire-at for an explicit first fire).
var (
	reminderConvertContent string
	reminderConvertFireAt  string
	reminderConvertCron    string
	reminderConvertTz      string
)

var reminderConvertCmd = &cobra.Command{
	Use:   "convert <message-name>",
	Short: "Create+claim a reminder rooted at the trigger message (assignee=you) and subscribe to its thread",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		content, ok := readContentFlag(reminderConvertContent)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/reminder/convert", daemonsrv.Request{
			Message:  args[0],
			Content:  content,
			FireAt:   reminderConvertFireAt,
			CronExpr: reminderConvertCron,
			Tz:       reminderConvertTz,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// reminder list <conversation> [--status S]... lists reminders, optionally
// filtered by conversation and status. Each line prints the reminder resource
// name ("reminders/{message_id}") the agent passes to update/cancel/complete.
var reminderListStatuses []string

var reminderListCmd = &cobra.Command{
	Use:   "list [<conversation>]",
	Short: "List reminders, optionally filtered by conversation and status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			printError("INVALID_ARGUMENT_FAILED",
				fmt.Sprintf("%s expects 0 or 1 positional argument(s), got %d", cmd.CommandPath(), len(args)),
				fmt.Sprintf("Run `%s --help` for usage.", cmd.CommandPath()))
			return ErrCLIFailed
		}
		req := daemonsrv.Request{Statuses: reminderListStatuses}
		if len(args) == 1 {
			req.Conversation = args[0]
		}
		if !call("/reminder/list", req) {
			return ErrCLIFailed
		}
		return nil
	},
}

// reminder list-due returns the DUE reminders owned by the caller. Run this at
// the start of every drain session (before `message check`) and process each
// due reminder by doing its work and calling `reminder complete`/`fail`.
var reminderListDueCmd = &cobra.Command{
	Use:   "list-due",
	Short: "List the DUE reminders owned by you (run at the start of every session)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 0, args) {
			return ErrCLIFailed
		}
		if !call("/reminder/list-due", daemonsrv.Request{}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// reminder update <name> --content <text|-> --fire-at <RFC3339> [--cron] [--tz]
// replaces the reminder's schedule and task content (full-replacement
// semantics). Use when the user asks in the reminder's thread to change the
// schedule or task.
var (
	reminderUpdateContent string
	reminderUpdateFireAt  string
	reminderUpdateCron    string
	reminderUpdateTz      string
)

var reminderUpdateCmd = &cobra.Command{
	Use:   "update <name>",
	Short: "Update a reminder's schedule and task content (full-replacement)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		content, ok := readContentFlag(reminderUpdateContent)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/reminder/update", daemonsrv.Request{
			Name:     args[0],
			Content:  content,
			FireAt:   reminderUpdateFireAt,
			CronExpr: reminderUpdateCron,
			Tz:       reminderUpdateTz,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// reminder cancel <name> cancels a reminder owned by the caller (or by a
// conversation member / admin via the API; the CLI is the agent's own).
var reminderCancelCmd = &cobra.Command{
	Use:   "cancel <name>",
	Short: "Cancel a reminder",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/reminder/cancel", daemonsrv.Request{Name: args[0]}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// reminder complete <name> --result <text|-> marks a DUE reminder completed and
// posts the result to its thread. The backend posts the message atomically —
// do NOT also post it yourself. Recurring reminders reschedule to the next
// cron fire.
var reminderCompleteResult string

var reminderCompleteCmd = &cobra.Command{
	Use:   "complete <name>",
	Short: "Mark a DUE reminder completed and post the result to its thread (backend posts, do not double-post)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		result, ok := readContentFlag(reminderCompleteResult)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/reminder/complete", daemonsrv.Request{
			Name:   args[0],
			Result: result,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// reminder fail <name> --error <text|-> marks a DUE reminder failed and posts
// the error to its thread. Recurring reminders reschedule; one-shot stay
// FAILED.
var reminderFailError string

var reminderFailCmd = &cobra.Command{
	Use:   "fail <name>",
	Short: "Mark a DUE reminder failed and post the error to its thread",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		reason, ok := readContentFlag(reminderFailError)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/reminder/fail", daemonsrv.Request{
			Name:  args[0],
			Error: reason,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	reminderConvertCmd.Flags().StringVar(&reminderConvertContent, "content", "", "structured summary of the scheduled work; \"-\" reads from stdin")
	reminderConvertCmd.Flags().StringVar(&reminderConvertFireAt, "fire-at", "", "first fire time, RFC3339 (e.g. 2026-07-07T03:00:00Z); required")
	reminderConvertCmd.Flags().StringVar(&reminderConvertCron, "cron", "", "5-field cron expression for a recurring reminder (empty = one-shot)")
	reminderConvertCmd.Flags().StringVar(&reminderConvertTz, "tz", "", "IANA timezone for --cron (default UTC)")

	reminderListCmd.Flags().StringArrayVar(&reminderListStatuses, "status", nil, "filter by status (repeatable): pending, due, completed, cancelled, missed, failed")

	reminderUpdateCmd.Flags().StringVar(&reminderUpdateContent, "content", "", "new task content; \"-\" reads from stdin")
	reminderUpdateCmd.Flags().StringVar(&reminderUpdateFireAt, "fire-at", "", "new fire time, RFC3339; required for one-shot, optional with --cron (manager computes next)")
	reminderUpdateCmd.Flags().StringVar(&reminderUpdateCron, "cron", "", "new 5-field cron expression (empty = one-shot)")
	reminderUpdateCmd.Flags().StringVar(&reminderUpdateTz, "tz", "", "new IANA timezone for --cron (default UTC)")

	reminderCompleteCmd.Flags().StringVar(&reminderCompleteResult, "result", "", "completion report posted to the thread; \"-\" reads from stdin")
	reminderFailCmd.Flags().StringVar(&reminderFailError, "error", "", "failure reason posted to the thread; \"-\" reads from stdin")
}
