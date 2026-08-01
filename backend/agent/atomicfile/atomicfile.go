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
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// WriteFileAtomicSync is WriteFileAtomic plus an fsync of the file and its
// parent directory before rename, so the new content and the rename are durable
// across a power loss. Use this for the most safety-critical state (the refresh
// token, whose truncation can brick reconnection); the extra fsync cost is
// negligible for the low write rate of these files.
func WriteFileAtomicSync(path string, data []byte, perm os.FileMode) error {
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanup = false
	return syncDir(dir)
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
