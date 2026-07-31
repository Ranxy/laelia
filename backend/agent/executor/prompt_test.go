package executor

import (
	"strings"
	"testing"
)

func TestBuildPromptOmitsPersonaWhenEmpty(t *testing.T) {
	got := BuildPrompt("alice", "")
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
	got := BuildPrompt("alice", "  Be concise and prefer Go.  ")
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

func TestBuildReanchorPrompt(t *testing.T) {
	got := BuildReanchorPrompt("bob")
	if strings.Contains(got, "{{name}}") {
		t.Fatalf("re-anchor template must not contain unrendered placeholders:\n%s", got)
	}
	if !strings.Contains(got, `You are "bob"`) || !strings.Contains(got, "@bob") {
		t.Fatalf("re-anchor prompt must render the agent name, got:\n%s", got)
	}
	if !strings.Contains(got, "Read MEMORY.md first") {
		t.Fatalf("re-anchor prompt must keep the MEMORY.md recovery instruction, got:\n%s", got)
	}
}
