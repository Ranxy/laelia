package provider

import (
	"context"
	"errors"
	"testing"
)

type fakeProvider struct {
	id          string
	display     string
	present     bool
	detectErr   error
	models      []ModelOption
	supports    bool
	probeErr    error
	buildCalled bool
}

func (f *fakeProvider) ID() string          { return f.id }
func (f *fakeProvider) DisplayName() string { return f.display }

func (f *fakeProvider) Detect(context.Context) (*Detected, bool, error) {
	if f.detectErr != nil {
		return nil, false, f.detectErr
	}
	if !f.present {
		return nil, false, nil
	}
	return &Detected{ProviderID: f.id, DisplayName: f.display, Version: "v1", ExecutablePath: "/bin/" + f.id}, true, nil
}

func (f *fakeProvider) BuildCommand(workspaceDir string) (string, []string) {
	f.buildCalled = true
	return f.id, []string{"--cwd", workspaceDir}
}

func (f *fakeProvider) ProbeModels(context.Context, string) ([]ModelOption, bool, error) {
	if f.probeErr != nil {
		return nil, false, f.probeErr
	}
	return f.models, f.supports, nil
}

func (*fakeProvider) ToolCallAdapter() ToolCallAdapter { return DefaultAdapter{} }

func TestRegistryLookup(t *testing.T) {
	r := New(&fakeProvider{id: "opencode", display: "OpenCode"}, &fakeProvider{id: "claude-code", display: "Claude Code"})
	if p, ok := r.Lookup("opencode"); !ok || p.ID() != "opencode" {
		t.Fatalf("lookup opencode: %+v ok=%v", p, ok)
	}
	if _, ok := r.Lookup("custom"); ok {
		t.Fatal("custom should not resolve to a builtin")
	}
	if len(r.All()) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(r.All()))
	}
}

func TestRegistryDiscoverReportsPresentProviders(t *testing.T) {
	r := New(
		&fakeProvider{id: "opencode", display: "OpenCode", present: true, models: []ModelOption{{Value: "m1", Name: "M1"}}, supports: true},
		&fakeProvider{id: "claude-code", display: "Claude Code", present: false},
	)
	got := r.Discover(context.Background())
	if len(got) != 1 {
		t.Fatalf("expected 1 discovered provider, got %d", len(got))
	}
	if got[0].ProviderID != "opencode" {
		t.Errorf("got[0].ProviderID = %q", got[0].ProviderID)
	}
	if !got[0].SupportsModelConfigOption || len(got[0].Models) != 1 || got[0].Models[0].Value != "m1" {
		t.Errorf("got[0] = %+v", got[0])
	}
}

func TestRegistryDiscoverKeepsProviderOnProbeFailure(t *testing.T) {
	r := New(
		&fakeProvider{id: "opencode", display: "OpenCode", present: true, probeErr: errors.New("boom")},
	)
	got := r.Discover(context.Background())
	if len(got) != 1 {
		t.Fatalf("expected provider kept on probe failure, got %d", len(got))
	}
	if got[0].SupportsModelConfigOption || len(got[0].Models) != 0 {
		t.Errorf("expected empty models on probe failure, got %+v", got[0])
	}
}

func TestRegistryDiscoverSkipsOnDetectError(t *testing.T) {
	r := New(
		&fakeProvider{id: "opencode", display: "OpenCode", present: false, detectErr: errors.New("nope")},
	)
	got := r.Discover(context.Background())
	if len(got) != 0 {
		t.Fatalf("expected no providers on detect error, got %d", len(got))
	}
}
