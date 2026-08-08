package provider

import (
	"context"
	"sync"
	"time"
)

// probeTimeout bounds a single provider's model probe. Providers that take
// longer (e.g. npx downloading on first run) are reported with an empty model
// list rather than blocking discovery.
const probeTimeout = 30 * time.Second

// Registry holds the set of built-in providers. The zero value is not usable;
// construct one with New or use Default.
type Registry struct {
	byID     map[string]Provider
	order    []string
	builtins []Provider
}

// New builds a Registry from the given providers. Duplicate ids panic.
func New(providers ...Provider) *Registry {
	r := &Registry{byID: map[string]Provider{}}
	for _, p := range providers {
		if _, ok := r.byID[p.ID()]; ok {
			panic("provider: duplicate id " + p.ID())
		}
		r.byID[p.ID()] = p
		r.order = append(r.order, p.ID())
		r.builtins = append(r.builtins, p)
	}
	return r
}

// Default is the registry of built-in providers, ordered by preference.
func Default() *Registry {
	return New(&OpenCodeProvider{}, &ClaudeCodeProvider{}, &CodexProvider{})
}

// Lookup returns the provider with the given id, or (nil, false) when unknown.
// The special "custom" id resolves to nil,false here — callers handle it as
// the raw executable/args escape hatch.
func (r *Registry) Lookup(id string) (Provider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// All returns every registered provider in registration order.
func (r *Registry) All() []Provider {
	out := make([]Provider, len(r.builtins))
	copy(out, r.builtins)
	return out
}

// Discover runs Detect + ProbeModels for every registered provider concurrently
// and returns the providers that were present on the host, in registration
// order. A provider whose Detect reports absent is skipped; a provider whose
// model probe fails or times out is still reported (with an empty model list
// and SupportsModelConfigOption=false) so the UI can show it as detected.
func (r *Registry) Discover(ctx context.Context) []Discovered {
	results := make([]Discovered, len(r.builtins))
	var wg sync.WaitGroup
	for i, p := range r.builtins {
		wg.Go(func() {
			results[i] = discoverOne(ctx, p)
		})
	}
	wg.Wait()

	present := results[:0]
	for _, d := range results {
		if d.ProviderID != "" {
			present = append(present, d)
		}
	}
	return present
}

func discoverOne(ctx context.Context, p Provider) Discovered {
	info, present, err := p.Detect(ctx)
	if err != nil || !present || info == nil {
		return Discovered{}
	}
	d := Discovered{
		ProviderID:     info.ProviderID,
		DisplayName:    info.DisplayName,
		Version:        info.Version,
		ExecutablePath: info.ExecutablePath,
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if tp, ok := p.(ThreadProvider); ok {
		// v2 providers advertise models through model/list; the model is
		// passed directly via thread/start's model param instead of a v1
		// config-option round trip.
		models, err := tp.ProbeModelsV2(probeCtx, "")
		if err == nil {
			d.Models = models
			d.SupportsModelConfigOption = true
		}
		return d
	}
	models, supports, err := p.ProbeModels(probeCtx, "")
	if err != nil {
		// Detection succeeded but probing failed: report the provider as
		// available with no model info rather than dropping it entirely.
		return d
	}
	d.Models = models
	d.SupportsModelConfigOption = supports
	return d
}
