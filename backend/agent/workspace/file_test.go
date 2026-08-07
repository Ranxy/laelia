package workspace

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadText(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "README.md", "# hello")

	res, err := Read(root, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if res.Binary || res.Content != "# hello" || res.Encoding != "utf-8" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestReadExtensionlessText(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "Dockerfile", "FROM scratch")

	res, err := Read(root, "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if res.Binary || res.Content != "FROM scratch" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestReadImage(t *testing.T) {
	root := t.TempDir()
	raw := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	mustWriteBytes(t, root, "pic.png", raw)

	res, err := Read(root, "pic.png")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Binary || res.MimeType != "image/png" || res.Encoding != "base64" {
		t.Fatalf("unexpected result %+v", res)
	}
	want := base64.StdEncoding.EncodeToString(raw)
	if res.Content != want {
		t.Fatal("base64 mismatch")
	}
}

func TestReadBinaryMetadataOnly(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "data.bin", "binary")

	res, err := Read(root, "data.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Binary || res.Content != "" || res.Encoding != "" || res.Size != 6 {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestReadSensitiveRefused(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, ".env", "KEY=value")
	mustWrite(t, root, ".ENV", "KEY=value")
	mustWrite(t, root, "credentials.json", "{}")
	mustWrite(t, root, "Credential.json", "{}")
	mustWrite(t, root, "SECRETS.json", "{}")
	mustWrite(t, root, "TOKEN.json", "{}")
	mustWrite(t, root, "machine-token-1", "secret")
	mustWrite(t, root, ".ssh/id_rsa", "key")

	for _, name := range []string{
		".env", ".ENV", "credentials.json", "Credential.json",
		"machine-token-1", ".ssh/id_rsa", "SECRETS.json", "TOKEN.json",
	} {
		res, err := Read(root, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.Error == "" || res.Content != "" {
			t.Fatalf("%s: expected refusal with no content, got %+v", name, res)
		}
	}
}

func TestReadTooLarge(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "big.txt", strings.Repeat("x", textFileMaxBytes+1))

	res, err := Read(root, "big.txt")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" {
		t.Fatalf("expected size refusal, got %+v", res)
	}
}

func TestReadDirectoryRefused(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, "dir")

	res, err := Read(root, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" {
		t.Fatalf("expected directory refusal, got %+v", res)
	}
}

func TestReadEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := Read(root, "../outside"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("escape should be ErrAccessDenied, got %v", err)
	}
}

func TestReadMissing(t *testing.T) {
	root := t.TempDir()
	if _, err := Read(root, "nope.txt"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func mustWriteBytes(t *testing.T, root, name string, content []byte) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

var _ = bytes.MinRead

func TestReadThroughEscapingSymlinkDenied(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, outside, "id_rsa", "key")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, "link/id_rsa"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied, got %v", err)
	}
	if _, err := Read(root, "link"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("expected ErrAccessDenied, got %v", err)
	}
}

func TestReadThroughSymlinkToSensitiveInsideRootRefused(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, root, ".ssh")
	mustWrite(t, root, ".ssh/id_rsa", "key")
	if err := os.Symlink(filepath.Join(root, ".ssh"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// The resolved target is never-visible, so the policy check on the real
	// path refuses the read even though the link name is innocuous.
	res, err := Read(root, "link/id_rsa")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" || res.Content != "" {
		t.Fatalf("expected refusal with no content, got %+v", res)
	}
}

func TestReadThroughSymlinkInsideRootAllowed(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root, "real.txt", "hello")
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	res, err := Read(root, "link.txt")
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" || res.Content != "hello" {
		t.Fatalf("unexpected result %+v", res)
	}
}
