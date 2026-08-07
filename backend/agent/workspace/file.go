package workspace

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

// ReadResult is a file preview. Error carries a user-facing refusal reason
// (sensitive file, too large, directory) that the caller maps into the proto
// response; OS-level failures are returned as the error.
type ReadResult struct {
	Content  string
	Binary   bool
	Size     int64
	MimeType string
	Encoding string
	Error    string
}

// Read previews one file inside root, mirroring raft: text extensions (or
// extension-less files) up to 1MB as utf-8, images up to 5MB as base64, other
// binaries metadata-only. Never-visible and secret paths are refused.
func Read(root, path string) (ReadResult, error) {
	resolved, rootReal, err := resolveInRoot(root, path)
	if err != nil {
		return ReadResult{}, err
	}
	// Policy checks run against the resolved path (symlinks followed), so a
	// link pointing at a sensitive file cannot bypass the secret filter.
	rel, err := filepath.Rel(rootReal, resolved)
	if err != nil {
		return ReadResult{}, err
	}
	if isNeverVisiblePath(rel) || isSecretFilePath(rel) {
		return ReadResult{Error: "preview is disabled for sensitive workspace files"}, nil
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return ReadResult{}, err
	}
	if info.IsDir() {
		return ReadResult{Error: "cannot read a directory"}, nil
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	if textExtensions[ext] || ext == "" {
		if info.Size() > textFileMaxBytes {
			return ReadResult{Error: "file too large to preview"}, nil
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			return ReadResult{}, err
		}
		return ReadResult{Content: string(content), Binary: false, Size: info.Size(), Encoding: "utf-8"}, nil
	}
	if mime := imageMimeByExt[ext]; mime != "" {
		if info.Size() > imagePreviewMaxBytes {
			return ReadResult{Binary: true, Size: info.Size(), MimeType: mime, Error: "image too large to preview"}, nil
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return ReadResult{}, err
		}
		return ReadResult{
			Content:  base64.StdEncoding.EncodeToString(data),
			Binary:   true,
			Size:     info.Size(),
			MimeType: mime,
			Encoding: "base64",
		}, nil
	}
	return ReadResult{Binary: true, Size: info.Size()}, nil
}
