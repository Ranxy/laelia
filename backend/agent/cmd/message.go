package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(messageCmd)
	messageCmd.AddCommand(messageCheckCmd, messageReadCmd, messageSearchCmd, messageAckCmd, messageSendCmd)
}

var messageCmd = &cobra.Command{
	Use:   "message",
	Short: "Read and post messages in Laelia channels (LLM-facing, used during drain sessions)",
}

// message check — list channels with unread messages for this agent.
var messageCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "List channels with unread messages (call this first each session)",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		if !call("/message/check", daemonsrv.Request{}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// message read <address> [--version V] [--after|--before] [--limit N]
var (
	messageReadVersion int64
	messageReadBefore  bool
	messageReadLimit   int
)

var messageReadCmd = &cobra.Command{
	Use:   "read <address>",
	Short: "Read messages in a conversation relative to a room version",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		direction := "after"
		if messageReadBefore {
			direction = "before"
		}
		if !call("/message/read", daemonsrv.Request{
			Conversation: args[0],
			Version:      messageReadVersion,
			Direction:    direction,
			Limit:        messageReadLimit,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// message search [--conversation C] --query Q [--since T] [--limit N]
var (
	messageSearchConversation string
	messageSearchQuery        string
	messageSearchSince        string
	messageSearchLimit        int
)

var messageSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search past chat messages by keyword and optional time range",
	RunE: func(_ *cobra.Command, _ []string) error {
		if messageSearchQuery == "" {
			printError("INVALID_ARGUMENT_FAILED", "--query is required", "Run `laelia-agent message search --help` for usage.")
			return ErrCLIFailed
		}
		if !call("/message/search", daemonsrv.Request{
			Conversation: messageSearchConversation,
			Query:        messageSearchQuery,
			Since:        messageSearchSince,
			Limit:        messageSearchLimit,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// message ack <address> --processed-version V
var messageAckProcessedVersion int64

var messageAckCmd = &cobra.Command{
	Use:   "ack <address>",
	Short: "Advance the durable per-channel cursor so the channel stops reporting unread",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/message/ack", daemonsrv.Request{
			Conversation:     args[0],
			ProcessedVersion: messageAckProcessedVersion,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// message send <address> --content <text|- > --base-version V [--attach <file-id>...]
var (
	messageSendContent     string
	messageSendBaseVersion int64
	messageSendAttachments []string
)

var messageSendCmd = &cobra.Command{
	Use:   "send <address>",
	Short: "Post a reply to a conversation (optimistic concurrency on base_version)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		content, ok := readContentFlag(messageSendContent)
		if !ok {
			return ErrCLIFailed
		}
		if !call("/message/send", daemonsrv.Request{
			Conversation:  args[0],
			Content:       content,
			BaseVersion:   messageSendBaseVersion,
			AttachmentIDs: messageSendAttachments,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	messageReadCmd.Flags().Int64Var(&messageReadVersion, "version", 0, "room version to page relative to (defaults to reading from the start)")
	messageReadCmd.Flags().BoolVar(&messageReadBefore, "before", false, "page to messages older than --version (oldest→newest); default reads messages newer than --version")
	messageReadCmd.Flags().IntVar(&messageReadLimit, "limit", 0, "max messages to return (default 20, max 100)")

	messageSearchCmd.Flags().StringVar(&messageSearchConversation, "conversation", "", "limit search to a conversation")
	messageSearchCmd.Flags().StringVar(&messageSearchQuery, "query", "", "search query (required)")
	messageSearchCmd.Flags().StringVar(&messageSearchSince, "since", "", "only messages since this timestamp")
	messageSearchCmd.Flags().IntVar(&messageSearchLimit, "limit", 0, "max results (default 10, max 50)")

	messageAckCmd.Flags().Int64Var(&messageAckProcessedVersion, "processed-version", 0, "room version to advance the cursor to (required, from `message read` current_version)")

	messageSendCmd.Flags().StringVar(&messageSendContent, "content", "", "message text; \"-\" reads from stdin")
	messageSendCmd.Flags().Int64Var(&messageSendBaseVersion, "base-version", 0, "room version the reply is based on (from `message read` current_version)")
	messageSendCmd.Flags().StringArrayVar(&messageSendAttachments, "attach", nil, "file id to attach to this message (repeatable); the file must already be uploaded to this conversation via `file upload --conversation`")
}
