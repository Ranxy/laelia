package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(threadCmd)
	threadCmd.AddCommand(threadCheckCmd, threadReadCmd, threadSendCmd)
}

var threadCmd = &cobra.Command{
	Use:   "thread",
	Short: "Read and post replies in threads (LLM-facing, used during drain sessions)",
}

// thread check — list subscribed threads with unread replies for this agent.
var threadCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "List subscribed threads with unread replies (run after `message check` per channel, before acking)",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if !call("/message/thread/check", daemonsrv.Request{}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// thread read <conversation> --root <root-msg-id> [--version V] [--after|--before] [--limit N]
var (
	threadReadRoot    string
	threadReadVersion int64
	threadReadBefore  bool
	threadReadLimit   int
)

var threadReadCmd = &cobra.Command{
	Use:   "read <conversation>",
	Short: "Read a thread (root message + replies) relative to a room version",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if threadReadRoot == "" {
			printError("INVALID_ARGUMENT_FAILED", "--root is required", "Pass --root <thread_root> from `thread check`.")
			return ErrCLIFailed
		}
		direction := "after"
		if threadReadBefore {
			direction = "before"
		}
		if !call("/message/thread/read", daemonsrv.Request{
			Conversation: args[0],
			Root:         threadReadRoot,
			Version:      threadReadVersion,
			Direction:    direction,
			Limit:        threadReadLimit,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// thread send <conversation> --root <root-msg-id> --content <text|-> --base-version V [--attach <file-id>...]
var (
	threadSendRoot        string
	threadSendContent     string
	threadSendBaseVersion int64
	threadSendAttachments []string
)

var threadSendCmd = &cobra.Command{
	Use:   "send <conversation>",
	Short: "Post a reply into a thread (optimistic concurrency on base_version)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if threadSendRoot == "" {
			printError("INVALID_ARGUMENT_FAILED", "--root is required", "Pass --root <thread_root> the reply belongs to.")
			return ErrCLIFailed
		}
		content, ok := readContentFlag(threadSendContent)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/message/thread/send", daemonsrv.Request{
			Conversation:  args[0],
			Root:          threadSendRoot,
			Content:       content,
			BaseVersion:   threadSendBaseVersion,
			AttachmentIDs: threadSendAttachments,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	threadReadCmd.Flags().StringVar(&threadReadRoot, "root", "", "thread root message id (required, from `thread check`)")
	threadReadCmd.Flags().Int64Var(&threadReadVersion, "version", 0, "room version to page relative to (defaults to reading from the start)")
	threadReadCmd.Flags().BoolVar(&threadReadBefore, "before", false, "page to replies older than --version (oldest→newest); default reads replies newer than --version")
	threadReadCmd.Flags().IntVar(&threadReadLimit, "limit", 0, "max replies to return (default 20, max 100)")

	threadSendCmd.Flags().StringVar(&threadSendRoot, "root", "", "thread root message id (required)")
	threadSendCmd.Flags().StringVar(&threadSendContent, "content", "", "reply text; \"-\" reads from stdin")
	threadSendCmd.Flags().Int64Var(&threadSendBaseVersion, "base-version", 0, "room version the reply is based on (from `thread read` current_version)")
	threadSendCmd.Flags().StringArrayVar(&threadSendAttachments, "attach", nil, "file id to attach to this reply (repeatable); the file must already be uploaded to this conversation via `file upload --conversation`")
}
