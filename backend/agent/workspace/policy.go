package workspace

import (
	"path/filepath"
	"regexp"
	"strings"
)

// textExtensions are the file extensions previewable as utf-8 text.
var textExtensions = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".js": true, ".ts": true,
	".jsx": true, ".tsx": true, ".yaml": true, ".yml": true, ".toml": true,
	".log": true, ".csv": true, ".xml": true, ".html": true, ".css": true,
	".sh": true, ".py": true,
}

// imageMimeByExt maps previewable image extensions to their mime types.
var imageMimeByExt = map[string]string{
	".apng": "image/apng", ".avif": "image/avif", ".gif": "image/gif",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".webp": "image/webp",
}

const (
	textFileMaxBytes     = 1 << 20 // 1MB: larger text files are not previewed
	imagePreviewMaxBytes = 5 << 20 // 5MB: larger images are not previewed
)

// secretFilePatterns mirror raft's four preview-disabled patterns, applied to
// each path segment, case-insensitively (raft compiles them with /i): dot-env
// files, and secret(s)/credential(s)/token(s) segment names. .slock* is
// deliberately not included — slock is raft's former name, laelia has no such
// artifacts.
var secretFilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\.env(?:\.|$)`),
	regexp.MustCompile(`(?i)(?:^|[._-])secret(?:s)?(?:[._-]|$)`),
	regexp.MustCompile(`(?i)(?:^|[._-])credential(?:s)?(?:[._-]|$)`),
	regexp.MustCompile(`(?i)(?:^|[._-])token(?:s)?(?:[._-]|$)`),
}

// neverVisibleHiddenNames are entries hidden even when the caller asks for
// hidden files: generic high-sensitivity credential directories an LLM agent
// may create inside its working dir. The machine's own credential file
// (~/.laelia/machine.json) lives outside the browsable roots.
var neverVisibleHiddenNames = map[string]bool{
	".aws": true, ".gnupg": true, ".ssh": true,
}

// isNeverVisibleEntry reports whether a single path segment is never shown,
// regardless of the includeHidden flag.
func isNeverVisibleEntry(name string) bool {
	return neverVisibleHiddenNames[name]
}

func pathParts(rel string) []string {
	parts := strings.Split(rel, string(filepath.Separator))
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isHiddenPath reports whether any segment of a relative path starts with ".".
func isHiddenPath(rel string) bool {
	for _, part := range pathParts(rel) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// isNeverVisiblePath reports whether any segment of a relative path is
// never-visible.
func isNeverVisiblePath(rel string) bool {
	for _, part := range pathParts(rel) {
		if isNeverVisibleEntry(part) {
			return true
		}
	}
	return false
}

// isSecretFilePath reports whether any segment of a relative path matches a
// secret file pattern; such files are never served.
func isSecretFilePath(rel string) bool {
	for _, part := range pathParts(rel) {
		for _, pattern := range secretFilePatterns {
			if pattern.MatchString(part) {
				return true
			}
		}
	}
	return false
}
