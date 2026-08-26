package client

import (
	"os"
	"path/filepath"
	"testing"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestBuildRuntimeForAgentProtocolDispatch verifies the runtime branch point:
// a built-in ThreadProvider always runs the thread executor, a custom provider
// runs the thread executor only when it declares protocol acp-v2, and
// everything else falls back to the v1 session executor.
func TestBuildRuntimeForAgentProtocolDispatch(t *testing.T) {
	r := &agentRunner{
		machine:   &MachineClient{machineID: "machine-1", binaryDir: "/tmp/bin"},
		daemon:    &daemonsrv.Server{},
		agentName: "agents/agent-1",
		agentID:   "agent-1",
	}
	newReq := func() executor.Request {
		return executor.Request{TimeoutSeconds: 60}
	}

	cases := []struct {
		name     string
		cfg      *v1pb.AgentACPConfig
		wantKind string // "thread" | "acp"
		wantErr  bool
	}{
		{
			name: "builtin codex runs thread executor",
			cfg: &v1pb.AgentACPConfig{
				Provider: "codex",
				Model:    "gpt-5",
			},
			wantKind: "thread",
		},
		{
			name: "custom acp-v2 runs thread executor",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
				Args:       []string{"serve"},
				Protocol:   executor.ProtocolV2,
			},
			wantKind: "thread",
		},
		{
			name: "custom acp-v1 runs session executor",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
				Protocol:   executor.ProtocolV1,
			},
			wantKind: "acp",
		},
		{
			name: "custom empty protocol runs session executor",
			cfg: &v1pb.AgentACPConfig{
				Provider:   "custom",
				Executable: "my-agent",
			},
			wantKind: "acp",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.setConfig(executor.BuildACPConfig(tc.cfg, "machine-1", "agent-1"))
			rt, err := r.buildRuntimeForAgent(newReq())
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			kind := "acp"
			if _, ok := rt.(*executor.ThreadExecutor); ok {
				kind = "thread"
			}
			if kind != tc.wantKind {
				t.Fatalf("runtime kind: got %s, want %s (%T)", kind, tc.wantKind, rt)
			}
		})
	}
}

// TestCustomThreadProviderAdapter ensures the adapter built for a custom
// acp-v2 agent carries the configured launch command.
func TestCustomThreadProviderAdapter(t *testing.T) {
	p := provider.NewCustomThreadProvider("my-agent", []string{"serve"})
	exe, args := p.ThreadCommand("/work")
	if exe != "my-agent" || len(args) != 1 || args[0] != "serve" {
		t.Fatalf("ThreadCommand: got (%q, %v)", exe, args)
	}
}

// TestAgentRunnerColdRestartClearsSessionState verifies that a cold restart
// removes the persisted ACP and pi session state files so the next turn
// starts from a fresh cold start.
func TestAgentRunnerColdRestartClearsSessionState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvDir, dir)
	machineID := "machine-1"
	agentID := "agent-1"

	acpPath := home.Join(machineID, agentID, "acp-session.json")
	piPath := home.Join(machineID, agentID, "pi-session.json")
	if err := os.MkdirAll(filepath.Dir(acpPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acpPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := &agentRunner{
		machine:   &MachineClient{machineID: machineID},
		agentName: "agents/agent-1",
		agentID:   agentID,
	}
	r.coldRestart()

	if _, err := os.Stat(acpPath); !os.IsNotExist(err) {
		t.Fatalf("acp session file should be removed, err=%v", err)
	}
	if _, err := os.Stat(piPath); !os.IsNotExist(err) {
		t.Fatalf("pi session file should be removed, err=%v", err)
	}
}
