// Package atomicfile writes files with a temp + rename so a crash mid-write
// never leaves a truncated file that loads as a partial or empty state. It is
// used for all agent-runtime persistent state — the machine refresh token,
// ACP/pi session pointers, per-command state, and context state — where a
// half-written file would brick resume or reconnection.
package atomicfile

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path atomically: a temp file is created in the
// same directory, written, chmod'd to perm, and renamed over path. Readers
// therefore see either the previous complete file or the new one, never a
// partial. The parent directory is created (0700) if missing.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, false)
}

// WriteFileAtomicSync is WriteFileAtomic plus an fsync of the file and its
// parent directory before rename, so the new content and the rename are durable
// across a power loss. Use this for the most safety-critical state (the refresh
// token, whose truncation can brick reconnection); the extra fsync cost is
// negligible for the low write rate of these files. The directory fsync is
// best-effort: on platforms/filesystems that don't support fsync on a directory
// handle (e.g. Windows "access denied") it fails even though the rename
// succeeded, so its error is ignored — the content is durable, only the
// rename-entry durability is not guaranteed there.
func WriteFileAtomicSync(path string, data []byte, perm os.FileMode) error {
	return writeAtomic(path, data, perm, true)
}

// writeAtomic is the shared temp + rename body. When sync is true, the temp file
// (and, best-effort, the parent directory) is fsynced before the rename so the
// new content and the rename entry are durable across power loss. The parent
// directory is created (0700) if missing.
func writeAtomic(path string, data []byte, perm os.FileMode, sync bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if sync {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	if sync {
		// Best-effort: the rename already succeeded, so the content is visible
		// and durable (the temp file was fsynced). The directory fsync only
		// guarantees the rename ENTRY survives power loss; a failure here (e.g.
		// unsupported on the platform) must not turn a successful write into an
		// error that a caller would misread as "file not written".
		_ = syncDir(dir)
	}
	return nil
}

// syncDir fsyncs the directory so the rename entry is durable on disk.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
