package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "state.json")
	want := []byte(`{"k":"v"}`)

	if err := WriteFileAtomic(path, want, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content = %q, want %q", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 0600", info.Mode().Perm())
	}

	// No leftover temp files in the target directory.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("leftover temp files: %v", names)
	}

	// Overwriting must replace, not append, and leave no temp behind.
	if err := WriteFileAtomic(path, []byte(`{"k":"v2"}`), 0o600); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != `{"k":"v2"}` {
		t.Fatalf("after overwrite = %q", got)
	}
	entries, _ = os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("leftover temp files after overwrite: %d", len(entries))
	}
}

func TestWriteFileAtomicSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "token")
	want := []byte("super-secret-refresh-token")

	if err := WriteFileAtomicSync(path, want, 0o600); err != nil {
		t.Fatalf("WriteFileAtomicSync: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("content = %q, want %q", got, want)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 0600", info.Mode().Perm())
	}
}
