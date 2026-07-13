package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskListCmd, taskClaimCmd, taskUnclaimCmd, taskReviewCmd, taskDoneCmd, taskCreateCmd)
}

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "List, claim, advance, and create channel tasks (LLM-facing, used during drain sessions)",
}

// task list <address> [--status S]... — the task board, optionally
// filtered by status. Each line prints the full message resource name the
// agent passes to claim/review/done.
var taskListStatuses []string

var taskListCmd = &cobra.Command{
	Use:   "list <address>",
	Short: "List tasks in a conversation (the task board), optionally filtered by status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/task/list", daemonsrv.Request{
			Conversation: args[0],
			Statuses:     taskListStatuses,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// task claim <message-handle> claims a TODO task (TODO→IN_PROGRESS) and assigns
// it to the caller. The message handle is the `<address>:<message-id>` form
// printed by `task list`.
var taskClaimCmd = &cobra.Command{
	Use:   "claim <message-handle>",
	Short: "Claim a TODO task (TODO→IN_PROGRESS, assignee=you) and subscribe to its thread",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/task/claim", daemonsrv.Request{Message: args[0]}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// task unclaim <message-handle> releases the caller's claim (IN_PROGRESS→TODO).
var taskUnclaimCmd = &cobra.Command{
	Use:   "unclaim <message-handle>",
	Short: "Release your claim on a task (IN_PROGRESS→TODO) so another agent may claim it",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/task/unclaim", daemonsrv.Request{Message: args[0]}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// task review <message-handle> marks the caller's task ready for human review
// (IN_PROGRESS→IN_REVIEW).
var taskReviewCmd = &cobra.Command{
	Use:   "review <message-handle>",
	Short: "Mark your task ready for human review (IN_PROGRESS→IN_REVIEW)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/task/update", daemonsrv.Request{Message: args[0], Status: "in_review"}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// task done <message-handle> marks the caller's task complete (IN_REVIEW→DONE)
// after the human approved it in the task's thread.
var taskDoneCmd = &cobra.Command{
	Use:   "done <message-handle>",
	Short: "Mark your task complete (IN_REVIEW→DONE) after the human approved it in the thread",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/task/update", daemonsrv.Request{Message: args[0], Status: "done"}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// task create <address> --content <text|-> [--attach <file-id>...] posts a
// new unassigned TODO task for other agents to claim.
var (
	taskCreateContent     string
	taskCreateAttachments []string
)

var taskCreateCmd = &cobra.Command{
	Use:   "create <address>",
	Short: "Post a new unassigned TODO task in a channel for other agents to claim",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		content, ok := readContentFlag(taskCreateContent)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/task/create", daemonsrv.Request{
			Conversation:  args[0],
			Content:       content,
			AttachmentIDs: taskCreateAttachments,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	taskListCmd.Flags().StringArrayVar(&taskListStatuses, "status", nil, "filter by status (repeatable): todo, in_progress, in_review, done")

	taskCreateCmd.Flags().StringVar(&taskCreateContent, "content", "", "task text; \"-\" reads from stdin")
	taskCreateCmd.Flags().StringArrayVar(&taskCreateAttachments, "attach", nil, "file id to attach to this task (repeatable); the file must already be uploaded to this conversation via `file upload --conversation`")
}
