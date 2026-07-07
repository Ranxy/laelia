package executor

import (
	"strings"
	"testing"
)

func TestBuildPromptOmitsPersonaWhenEmpty(t *testing.T) {
	got := buildPrompt("alice", "")
	if strings.Contains(got, "Your persona") {
		t.Fatalf("prompt should not contain persona section when empty, got:\n%s", got)
	}
	if !strings.Contains(got, agentIdentityText("alice")) {
		t.Fatal("prompt must still contain identity section")
	}
	if !strings.Contains(got, AgentCommunicationPrompt) {
		t.Fatal("prompt must still contain communication section")
	}
}

func TestBuildPromptInjectsPersonaAfterIdentity(t *testing.T) {
	got := buildPrompt("alice", "  Be concise and prefer Go.  ")
	identityIdx := strings.Index(got, agentIdentityText("alice"))
	personaIdx := strings.Index(got, "## Your persona")
	commIdx := strings.Index(got, AgentCommunicationPrompt)
	if personaIdx < 0 || !strings.Contains(got, "Be concise and prefer Go.") {
		t.Fatalf("prompt must contain trimmed persona text, got:\n%s", got)
	}
	if identityIdx >= personaIdx || personaIdx >= commIdx {
		t.Fatalf("persona must appear after identity and before communication: identity=%d persona=%d comm=%d", identityIdx, personaIdx, commIdx)
	}
}
