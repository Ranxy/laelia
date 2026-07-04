package provider

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/coder/acp-go-sdk"
)

// fakeAgent is a minimal in-process ACP agent used to exercise probeConn
// without spawning a real provider binary. It advertises a configurable set
// of session config options in its NewSession response.
type fakeAgent struct {
	conn          *acp.AgentSideConnection
	configOptions []acp.SessionConfigOption
}

func (a *fakeAgent) SetAgentConnection(c *acp.AgentSideConnection) { a.conn = c }

func (fakeAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{ProtocolVersion: acp.ProtocolVersionNumber}, nil
}

func (a *fakeAgent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	return acp.NewSessionResponse{
		SessionId:     acp.SessionId("probe-test"),
		ConfigOptions: a.configOptions,
	}, nil
}

func (fakeAgent) Prompt(context.Context, acp.PromptRequest) (acp.PromptResponse, error) {
	return acp.PromptResponse{}, nil
}
func (fakeAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }
func (fakeAgent) CloseSession(context.Context, acp.CloseSessionRequest) (acp.CloseSessionResponse, error) {
	return acp.CloseSessionResponse{}, nil
}
func (fakeAgent) LoadSession(context.Context, acp.LoadSessionRequest) (acp.LoadSessionResponse, error) {
	return acp.LoadSessionResponse{}, nil
}
func (fakeAgent) ResumeSession(context.Context, acp.ResumeSessionRequest) (acp.ResumeSessionResponse, error) {
	return acp.ResumeSessionResponse{}, nil
}
func (fakeAgent) Authenticate(context.Context, acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	return acp.AuthenticateResponse{}, nil
}
func (fakeAgent) SetSessionMode(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	return acp.SetSessionModeResponse{}, nil
}
func (fakeAgent) ListSessions(context.Context, acp.ListSessionsRequest) (acp.ListSessionsResponse, error) {
	return acp.ListSessionsResponse{}, nil
}
func (fakeAgent) SetSessionConfigOption(context.Context, acp.SetSessionConfigOptionRequest) (acp.SetSessionConfigOptionResponse, error) {
	return acp.SetSessionConfigOptionResponse{}, nil
}
func (fakeAgent) Logout(context.Context, acp.LogoutRequest) (acp.LogoutResponse, error) {
	return acp.LogoutResponse{}, nil
}

// runProbeWithFakeAgent wires an in-process fake agent against probeConn over
// two io.Pipe pairs and returns the discovered model config option.
func runProbeWithFakeAgent(t *testing.T, opts []acp.SessionConfigOption) (*acp.SessionConfigOptionSelect, error) {
	t.Helper()

	agentReader, clientWriter := io.Pipe() // client -> agent
	clientReader, agentWriter := io.Pipe() // agent -> client

	agent := &fakeAgent{configOptions: opts}
	agentConn := acp.NewAgentSideConnection(agent, agentWriter, agentReader)
	agent.SetAgentConnection(agentConn)

	// Close both ends when the agent side finishes so the client reader
	// unblocks and probeConn returns.
	go func() {
		<-agentConn.Done()
		_ = agentWriter.Close()
		_ = agentReader.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// probeConn returns once it has read the NewSession response. Close the
	// client-side writer so the agent's reader sees EOF and winds down.
	var (
		sel *acp.SessionConfigOptionSelect
		err error
		wg  sync.WaitGroup
	)
	wg.Go(func() {
		sel, err = probeConn(ctx, ".", clientWriter, clientReader)
		_ = clientWriter.Close()
	})
	wg.Wait()
	return sel, err
}

func modelCategory() *acp.SessionConfigOptionCategory {
	c := acp.SessionConfigOptionCategoryModel
	return &c
}

func TestProbeConnFindsModelConfigOption(t *testing.T) {
	ungrouped := acp.SessionConfigSelectOptionsUngrouped{
		{Value: "gpt-4o", Name: "GPT-4o"},
		{Value: "gpt-4o-mini", Name: "GPT-4o mini"},
	}
	opts := []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Id:       "model",
			Name:     "Model",
			Category: modelCategory(),
			Options:  acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
			Type:     "select",
		}},
	}

	sel, err := runProbeWithFakeAgent(t, opts)
	if err != nil {
		t.Fatalf("probeConn: %v", err)
	}
	if sel == nil {
		t.Fatal("expected a model config option, got nil")
	}
	models := selectOptionsToModels(sel.Options)
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Value != "gpt-4o" || models[0].Name != "GPT-4o" {
		t.Errorf("models[0] = %+v", models[0])
	}
	if models[1].Value != "gpt-4o-mini" {
		t.Errorf("models[1].Value = %q", models[1].Value)
	}
}

func TestProbeConnReturnsNilWhenNoModelOption(t *testing.T) {
	modeOpts := acp.SessionConfigSelectOptionsUngrouped{{Value: "plan", Name: "Plan"}}
	opts := []acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Id:      "mode",
			Name:    "Mode",
			Options: acp.SessionConfigSelectOptions{Ungrouped: &modeOpts},
			Type:    "select",
			// no category -> not a model option
		}},
	}
	sel, err := runProbeWithFakeAgent(t, opts)
	if err != nil {
		t.Fatalf("probeConn: %v", err)
	}
	if sel != nil {
		t.Fatalf("expected nil model option, got id=%q", sel.Id)
	}
}

func TestSelectOptionsFlattensGrouped(t *testing.T) {
	grouped := acp.SessionConfigSelectOptionsGrouped{
		{Group: "fast", Name: "Fast", Options: []acp.SessionConfigSelectOption{{Value: "a", Name: "A"}}},
		{Group: "smart", Name: "Smart", Options: []acp.SessionConfigSelectOption{{Value: "b", Name: "B"}}},
	}
	flat := selectOptions(acp.SessionConfigSelectOptions{Grouped: &grouped})
	if len(flat) != 2 || flat[0].Value != "a" || flat[1].Value != "b" {
		t.Fatalf("flat = %+v", flat)
	}
}
