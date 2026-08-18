package provider

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFakeExecutable(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeCodeDetectRequiresClaudeCLI(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "npx", "exit 0")
	t.Setenv("PATH", dir)

	p := &ClaudeCodeProvider{}
	_, present, err := p.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect with npx only: %v", err)
	}
	if present {
		t.Fatal("Claude Code must not be reported present when only npx is installed")
	}
}

func TestClaudeCodeDetectPresentWithNpxAndClaude(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "npx", "exit 0")
	writeFakeExecutable(t, dir, "claude", `echo "1.0.0"`)
	t.Setenv("PATH", dir)

	p := &ClaudeCodeProvider{}
	info, present, err := p.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect with npx+claude: %v", err)
	}
	if !present {
		t.Fatal("Claude Code should be present when both npx and claude are installed")
	}
	if info == nil {
		t.Fatal("expected detected info")
	}
	if info.Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", info.Version)
	}
	if filepath.Base(info.ExecutablePath) != "npx" {
		t.Fatalf("executable path = %q, want npx", info.ExecutablePath)
	}
}

func TestClaudeCodeDetectRequiresNpx(t *testing.T) {
	dir := t.TempDir()
	writeFakeExecutable(t, dir, "claude", `echo "1.0.0"`)
	t.Setenv("PATH", dir)

	p := &ClaudeCodeProvider{}
	_, present, err := p.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect with claude only: %v", err)
	}
	if present {
		t.Fatal("Claude Code must not be reported present when npx is missing")
	}
}
