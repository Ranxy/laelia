package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Ranxy/laelia/backend/agent/chattools"
)

func TestAuthorize(t *testing.T) {
	s := &Server{sessionToken: "sekret"}

	// Missing header → TOKEN_MISSING.
	r := mustRequest(http.Header{})
	e := s.authorize(r)
	assert.Equal(t, "TOKEN_MISSING", e.Code)

	// Wrong token → TOKEN_INVALID.
	r = mustRequest(http.Header{"Authorization": []string{"Bearer wrong"}})
	e = s.authorize(r)
	assert.Equal(t, "TOKEN_INVALID", e.Code)

	// Correct token → authorized.
	r = mustRequest(http.Header{"Authorization": []string{"Bearer sekret"}})
	assert.Nil(t, s.authorize(r))
}

func TestDepsFallsBackToAgentResourceID(t *testing.T) {
	s := &Server{agentResourceID: "default-agent"}
	d := s.deps(Request{Agent: "", Command: "c"})
	assert.Equal(t, "default-agent", d.Agent)
	assert.Equal(t, "c", d.Command)

	d = s.deps(Request{Agent: "explicit"})
	assert.Equal(t, "explicit", d.Agent)
}

func TestAsChatError(t *testing.T) {
	assert.Nil(t, asChatError(nil))
	e := asChatError(&chattools.Error{Code: "NOT_FOUND_FAILED", Message: "x"})
	assert.Equal(t, "NOT_FOUND_FAILED", e.Code)

	// Non-chattools errors are wrapped as a generic server failure.
	e = asChatError(http.ErrAbortHandler)
	assert.Equal(t, "SERVER_5XX", e.Code)
}

func mustRequest(h http.Header) *http.Request {
	r := &http.Request{Header: h}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	return r
}

// ---- T20: validateWorkspacePath symlink-escape hardening ----

// newJailServer builds a Server whose tempDir is a fresh t.TempDir-backed jail.
func newJailServer(t *testing.T) *Server {
	t.Helper()
	jail := t.TempDir()
	return &Server{tempDir: jail}
}

// TestValidateWorkspacePath_RejectsDanglingSymlinkEscape: a symlink inside the
// jail pointing outside it (dangling or not) must be rejected, not followed by a
// later write. The pre-fix lexical fallback let this escape.
func TestValidateWorkspacePath_RejectsDanglingSymlinkEscape(t *testing.T) {
	s := newJailServer(t)
	outside := filepath.Join(t.TempDir(), "laelia-shell-target")
	if err := os.Symlink(outside, filepath.Join(s.tempDir, "evil")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := s.validateWorkspacePath("evil"); err == nil {
		t.Fatal("expected error for dangling symlink escaping the jail, got nil")
	}
}

// TestValidateWorkspacePath_AllowsFreshPathInsideJail: a not-yet-existing file
// whose parent is a real directory inside the jail must resolve and be allowed.
func TestValidateWorkspacePath_AllowsFreshPathInsideJail(t *testing.T) {
	s := newJailServer(t)
	sub := filepath.Join(s.tempDir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := s.validateWorkspacePath(filepath.Join("sub", "new.txt"))
	if err != nil {
		t.Fatalf("expected fresh path inside jail to pass, got: %v", err)
	}
	want := filepath.Join(sub, "new.txt")
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// TestValidateWorkspacePath_RejectsSymlinkParentEscape: when an ancestor of the
// target is a symlink pointing outside the jail, resolving the parent must land
// outside and the path must be rejected.
func TestValidateWorkspacePath_RejectsSymlinkParentEscape(t *testing.T) {
	s := newJailServer(t)
	outside := t.TempDir()
	link := filepath.Join(s.tempDir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := s.validateWorkspacePath(filepath.Join("link", "file.txt")); err == nil {
		t.Fatal("expected error for path escaping via symlinked parent, got nil")
	}
}
