package workspace

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
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

// textSniffBytes is how much of a file the text probe reads, matching git's
// binary-detection window.
const textSniffBytes = 8000

// Read previews one file inside root: text files (by known extension or
// content sniffing) up to 1MB as utf-8, images up to 5MB as base64, other
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

	// Known text extensions are trusted; anything else (unknown suffixes like
	// .go or .tsbuildinfo, extension-less names) is decided by content sniffing
	// so text files are previewed regardless of their name.
	isText := textExtensions[ext]
	if !isText {
		isText, err = looksLikeTextFile(resolved)
		if err != nil {
			return ReadResult{}, err
		}
	}
	if !isText {
		return ReadResult{Binary: true, Size: info.Size()}, nil
	}
	if info.Size() > textFileMaxBytes {
		return ReadResult{Error: "file too large to preview"}, nil
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return ReadResult{}, err
	}
	return ReadResult{Content: string(content), Binary: false, Size: info.Size(), Encoding: "utf-8"}, nil
}

// looksLikeTextFile reports whether the leading bytes of path look like text:
// no NUL byte and valid UTF-8. Binary formats almost always contain NULs
// early, so this mirrors git's binary heuristic.
func looksLikeTextFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, textSniffBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	buf = buf[:n]
	if bytes.IndexByte(buf, 0) >= 0 {
		return false, nil
	}
	return utf8.Valid(buf), nil
}
