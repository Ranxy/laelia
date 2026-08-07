package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ErrAccessDenied is returned when a requested path escapes the workspace root.
var ErrAccessDenied = errors.New("workspace path escapes the workspace root")

// Entry is one directory entry, with Path relative to the workspace root.
type Entry struct {
	Name       string
	Path       string
	IsDir      bool
	Size       int64
	ModifiedAt time.Time
	IsHidden   bool
}

// withinRoot reports whether path is root itself or a descendant of root.
func withinRoot(root, path string) bool {
	if path == root {
		return true
	}
	if root == string(os.PathSeparator) {
		return true
	}
	return strings.HasPrefix(path, root+string(os.PathSeparator))
}

// resolveInRoot resolves dirPath inside root and rejects paths escaping it.
// Symlinks are followed and the resolved target must still be inside the
// resolved root, so a link inside the workspace can never reach host files
// outside it. It returns the resolved path and the resolved root so callers
// run policy checks against the real target (a link pointing at a sensitive
// path must not bypass the secret filter).
func resolveInRoot(root, dirPath string) (string, string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	resolved, err := filepath.Abs(filepath.Join(rootAbs, dirPath))
	if err != nil {
		return "", "", err
	}
	if !withinRoot(rootAbs, resolved) {
		return "", "", ErrAccessDenied
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		// Missing root: keep the lexical root as the containment base; the
		// downstream EvalSymlinks failure surfaces normally (List tolerates
		// it as an empty directory, Read reports the OS error).
		rootReal = rootAbs
	}
	final, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", "", err
	}
	if !withinRoot(rootReal, final) {
		return "", "", ErrAccessDenied
	}
	return final, rootReal, nil
}

// List returns one directory level of the workspace, mirroring raft: a missing
// root or unreadable directory yields an empty list, node_modules is always
// skipped, never-visible entries are always skipped, and hidden entries are
// skipped unless includeHidden is set. Symlinks are never listed (following
// one could escape the root or expose a sensitive target); the target
// directory itself is checked after symlink resolution. Entries are sorted
// directories first, then by name.
func List(root, dirPath string, includeHidden bool) ([]Entry, error) {
	target, rootReal, err := resolveInRoot(root, dirPath)
	if err != nil {
		if errors.Is(err, ErrAccessDenied) {
			return nil, err
		}
		// A missing or unresolvable target mirrors os.ReadDir failing below:
		// raft returns an empty list for an unreadable directory.
		return nil, nil //nolint:nilerr
	}
	rel, err := filepath.Rel(rootReal, target)
	if err != nil {
		return nil, err
	}
	if rel != "." {
		if isNeverVisiblePath(rel) {
			return nil, nil
		}
		if !includeHidden && isHiddenPath(rel) {
			return nil, nil
		}
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, nil //nolint:nilerr // raft: an unreadable directory yields an empty list
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		isHidden := strings.HasPrefix(name, ".")
		if name == "node_modules" || isNeverVisibleEntry(name) {
			continue
		}
		if isHidden && !includeHidden {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			// DirEntry.Info does not follow symlinks; never surface them.
			continue
		}
		relPath, err := filepath.Rel(rootReal, filepath.Join(target, name))
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Name:       name,
			Path:       filepath.ToSlash(relPath),
			IsDir:      fi.IsDir(),
			Size:       fi.Size(),
			ModifiedAt: fi.ModTime(),
			IsHidden:   isHidden,
		})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}
