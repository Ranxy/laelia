package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListSortsAndFilters(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "b-dir")
	mustMkdir(t, root, "a-dir")
	mustMkdir(t, root, "node_modules")
	mustMkdir(t, root, ".hidden-dir")
	mustMkdir(t, root, ".ssh")
	mustMkdir(t, root, ".env")
	mustWrite(t, root, "z.txt", "z")
	mustWrite(t, root, "a.txt", "a")
	mustWrite(t, root, "machine-token-1", "secret")

	entries, err := List(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	want := []string{"a-dir", "b-dir", "a.txt", "z.txt"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("got %v, want %v", names, want)
		}
	}
	if !entries[0].IsDir || entries[0].Path != "a-dir" {
		t.Fatalf("unexpected first entry %+v", entries[0])
	}
}

func TestListIncludeHidden(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".env", "x")
	mustMkdir(t, root, ".ssh")
	mustWrite(t, root, "file.txt", "x")

	entries, err := List(root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name] = true
	}
	if !names[".env"] {
		t.Error("hidden .env should appear with includeHidden=true")
	}
	if names[".ssh"] {
		t.Error(".ssh must stay hidden even with includeHidden=true")
	}
}

func TestListSubdirAndHiddenSubdir(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "src")
	mustWrite(t, root, "src/main.go", "x")
	mustMkdir(t, root, "src/.cache")
	mustWrite(t, root, "src/.cache/data", "x")

	entries, err := List(root, "src", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "main.go" {
		t.Fatalf("got %+v", entries)
	}

	// Hidden subdir without includeHidden is refused.
	got, err := List(root, "src/.cache", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("hidden subdir should be empty without includeHidden, got %+v", got)
	}

	got, err = List(root, "src/.cache", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "data" {
		t.Fatalf("got %+v", got)
	}
}

func TestListNeverVisibleSubdir(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, ".ssh")
	got, err := List(root, ".ssh", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("never-visible subdir should be empty, got %+v", got)
	}
}

func TestListMissingRootAndEscape(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "nope")
	entries, err := List(missing, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("missing root should be empty, got %+v", entries)
	}

	if _, err := List(root, "..", false); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("escape should be ErrAccessDenied, got %v", err)
	}
	if _, err := List(root, "../../etc", false); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("escape should be ErrAccessDenied, got %v", err)
	}
}

func mustMkdir(t *testing.T, root, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, outside, "id_rsa", "key")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, root, "ok.txt", "x")

	entries, err := List(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "ok.txt" {
		t.Fatalf("symlink must not be listed, got %+v", entries)
	}
}

func TestListThroughEscapingSymlinkDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, outside, "id_rsa", "key")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root, "link", false); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied, got %v", err)
	}
	if _, err := List(root, "link/id_rsa", false); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied, got %v", err)
	}
}

func TestListThroughSymlinkToNeverVisibleInsideRootEmpty(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, ".ssh")
	mustWrite(t, root, ".ssh/id_rsa", "key")
	if err := os.Symlink(filepath.Join(root, ".ssh"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// The resolved target .ssh is never-visible, so listing through the link
	// yields an empty list even with includeHidden.
	got, err := List(root, "link", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %+v", got)
	}
}
