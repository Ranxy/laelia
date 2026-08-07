package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScanSummarizesDirectories(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "agent-a")
	mustWrite(t, root, "agent-a/a.txt", "hello")          // 5 bytes
	mustWrite(t, root, "agent-a/sub/b.txt", "worldwide")  // 10 bytes
	mustWrite(t, root, "agent-a/sub/c.txt", "0123456789") // 10 bytes
	mustMkdir(t, root, "agent-b")
	mustWrite(t, root, "agent-b/empty.txt", "")
	mustWrite(t, root, "daemon.sock", "not a dir")

	summaries, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 directories, got %+v", summaries)
	}
	byName := map[string]Summary{}
	for _, s := range summaries {
		byName[s.DirectoryName] = s
	}
	a := byName["agent-a"]
	if a.FileCount != 3 || a.TotalSizeBytes != 24 {
		t.Fatalf("agent-a summary mismatch: %+v", a)
	}
	b := byName["agent-b"]
	if b.FileCount != 1 || b.TotalSizeBytes != 0 {
		t.Fatalf("agent-b summary mismatch: %+v", b)
	}
}

func TestScanMissingRoot(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestDelete(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "agent-a")
	mustWrite(t, root, "agent-a/file.txt", "x")

	if err := Delete(root, "agent-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "agent-a")); !os.IsNotExist(err) {
		t.Fatalf("directory should be gone, stat err=%v", err)
	}
}

func TestDeleteRejectsUnsafeNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", ".", "..", "../escape", "a/b", `a\b`, "a..b"} {
		if err := Delete(root, name); !errors.Is(err, ErrInvalidDirectoryName) {
			t.Fatalf("Delete(%q) = %v, want ErrInvalidDirectoryName", name, err)
		}
	}
}
