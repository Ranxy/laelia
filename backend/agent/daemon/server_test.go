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

func TestDepsRoutesByRequestAgent(t *testing.T) {
	s := &Server{}
	d := s.deps(Request{Agent: "agents/explicit", Command: "c"})
	assert.Equal(t, "agents/explicit", d.Agent)
	assert.Equal(t, "c", d.Command)
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

// TestWorkspaceForJailsPerAgent: file commands must resolve under the calling
// agent's working directory (~/.laelia/<machineID>/<agentID>/), not a
// machine-shared temp dir.
func TestWorkspaceForJailsPerAgent(t *testing.T) {
	home := t.TempDir()
	s := &Server{homeDir: home, machineResourceID: "machine-1"}

	got, err := s.workspaceFor("agents/agent-2")
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".laelia", "machine-1", "agent-2"), got)

	got, err = s.workspaceFor("agent-2")
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".laelia", "machine-1", "agent-2"), got)
}

func TestWorkspaceForRejectsMissingAgent(t *testing.T) {
	s := &Server{homeDir: t.TempDir(), machineResourceID: "machine-1"}
	for _, agent := range []string{"", "agents/", "../evil"} {
		if _, err := s.workspaceFor(agent); err == nil {
			t.Fatalf("expected error for agent %q, got nil", agent)
		}
	}
}

// TestValidateWorkspacePath_RejectsDanglingSymlinkEscape: a symlink inside the
// jail pointing outside it (dangling or not) must be rejected, not followed by a
// later write. The pre-fix lexical fallback let this escape.
func TestValidateWorkspacePath_RejectsDanglingSymlinkEscape(t *testing.T) {
	jail := t.TempDir()
	outside := filepath.Join(t.TempDir(), "laelia-shell-target")
	if err := os.Symlink(outside, filepath.Join(jail, "evil")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := validateWorkspacePath(jail, jail, "evil"); err == nil {
		t.Fatal("expected error for dangling symlink escaping the jail, got nil")
	}
}

// TestValidateWorkspacePath_AllowsFreshPathInsideJail: a not-yet-existing file
// whose parent is a real directory inside the jail must resolve and be allowed.
func TestValidateWorkspacePath_AllowsFreshPathInsideJail(t *testing.T) {
	jail := t.TempDir()
	sub := filepath.Join(jail, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := validateWorkspacePath(jail, jail, filepath.Join("sub", "new.txt"))
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
	jail := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(jail, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := validateWorkspacePath(jail, jail, filepath.Join("link", "file.txt")); err == nil {
		t.Fatal("expected error for path escaping via symlinked parent, got nil")
	}
}

// TestValidateWorkspacePath_ResolvesCwdRelativeIntoAgentTemp is the regression
// test for the machine-layer change: from the agent's working directory,
// `file upload temp/docker-report.md` must resolve to
// ~/.laelia/<machineID>/<agentID>/temp/docker-report.md.
func TestValidateWorkspacePath_ResolvesCwdRelativeIntoAgentTemp(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := validateWorkspacePath(home, tempDir, filepath.Join("temp", "docker-report.md"))
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "docker-report.md"), got)
}

// TestValidateWorkspacePath_RejectsWorkspaceRootFile: a file written directly
// in the agent's working directory (outside temp/) must be rejected, so
// upload/download files stay isolated from persistent workspace files.
func TestValidateWorkspacePath_RejectsWorkspaceRootFile(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := validateWorkspacePath(home, tempDir, "docker-report.md"); err == nil {
		t.Fatal("expected path outside temp jail to be rejected, got nil")
	}
}

// TestValidateWorkspacePath_ResolvesFromTempCwd: when the CLI runs with the
// temp directory itself as cwd, a bare file name must resolve inside the jail.
func TestValidateWorkspacePath_ResolvesFromTempCwd(t *testing.T) {
	tempDir := t.TempDir()
	got, err := validateWorkspacePath(tempDir, tempDir, "docker-report.md")
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "docker-report.md"), got)
}

// TestValidateWorkspacePath_RejectsOldMachineTempEscape: the pre-machine temp
// path (~/.laelia/<machine>/temp/) is now outside every agent's workspace, so
// ../temp/... must be rejected.
func TestValidateWorkspacePath_RejectsOldMachineTempEscape(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := validateWorkspacePath(home, tempDir, filepath.Join("..", "temp", "x.txt")); err == nil {
		t.Fatal("expected path escaping via ../temp to be rejected")
	}
}
