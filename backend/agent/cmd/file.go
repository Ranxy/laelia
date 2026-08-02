package cmd

import (
	"github.com/spf13/cobra"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
)

func init() {
	rootCmd.AddCommand(fileCmd)
	fileCmd.AddCommand(fileUploadCmd, fileDownloadCmd, fileListCmd)
}

var fileCmd = &cobra.Command{
	Use:   "file",
	Short: "Upload, download, and list files in Laelia (LLM-facing, used during drain sessions)",
}

// file upload <local-path> [--conversation C] [--mime-type M]
var (
	fileUploadConversation string
	fileUploadMimeType     string
)

var fileUploadCmd = &cobra.Command{
	Use:   "upload <local-path>",
	Short: "Upload a file from your temp workspace to S3 and return its file id",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/file/upload", daemonsrv.Request{
			LocalPath:    args[0],
			Conversation: fileUploadConversation,
			MimeType:     fileUploadMimeType,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// file download <file-id> [--out P]
var fileDownloadOutPath string

var fileDownloadCmd = &cobra.Command{
	Use:   "download <file-id>",
	Short: "Download a file from S3 into your temp workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !requireArgs(cmd, 1, args) {
			return ErrCLIFailed
		}
		if !call("/file/download", daemonsrv.Request{
			FileID:  args[0],
			OutPath: fileDownloadOutPath,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

// file list --conversation C
var fileListConversation string

var fileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List files attached to a conversation",
	RunE: func(_ *cobra.Command, _ []string) error {
		if !call("/file/list", daemonsrv.Request{
			Conversation: fileListConversation,
		}) {
			return ErrCLIFailed
		}
		return nil
	},
}

func init() {
	fileUploadCmd.Flags().StringVar(&fileUploadConversation, "conversation", "", "conversation the file is attached to (the agent must be a member)")
	fileUploadCmd.Flags().StringVar(&fileUploadMimeType, "mime-type", "", "MIME type (auto-detected if empty)")

	fileDownloadCmd.Flags().StringVar(&fileDownloadOutPath, "out", "", "destination path inside your temp workspace (defaults to <temp-dir>/<original-name>)")

	fileListCmd.Flags().StringVar(&fileListConversation, "conversation", "", "conversation to list files for (required)")
}
