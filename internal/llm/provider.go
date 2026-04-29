// Package llm holds heterogeneous LLM provider drivers used by worker pools
// (PRISMCONDUCTOR_PLAN.md §6.6, issue #27).
//
// The Provider interface is the single seam through which the rest of the
// codebase reaches LLM-specific behavior (argv to spawn, models to list,
// whether spawning works today). Routing in workerpool/orchestrator stays
// provider-agnostic — those packages ask `CanSpawn()`, never compare against a
// specific Provider constant.
package llm

import (
	"context"
	"errors"

	"prismconductor/internal/types"
)

// ErrNotSupported signals that the operation isn't wired up yet for a provider.
// Non-Claude providers return it from SpawnArgs until harness-v1 lands.
var ErrNotSupported = errors.New("llm: provider does not support this operation yet")

// Provider drives one heterogeneous worker fleet kind.
type Provider interface {
	Kind() types.Provider
	DisplayName() string
	DefaultEndpoint() string
	NeedsAPIKey() bool

	// CanSpawn reports whether SpawnArgs returns a working argv today. The
	// worker-pool registry consults this to decide eligibility, so the
	// orchestrator never compares against `ProviderClaude` literally.
	CanSpawn() bool

	// ListModels probes the endpoint for available model IDs. Returns
	// ErrNotSupported if the provider has no listing API; the UI then keeps
	// the model field as free-text.
	ListModels(ctx context.Context, p types.Pool) ([]string, error)

	// SpawnArgs returns the argv the session manager should pty.Start.
	// Non-Claude providers return ErrNotSupported until harness-v1 lands.
	SpawnArgs(p types.Pool, prompt string) ([]string, error)
}

// Registry holds one Provider per kind plus a stable ordering for the UI.
type Registry struct {
	byKind map[types.Provider]Provider
	order  []types.Provider
}

func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byKind: make(map[types.Provider]Provider, len(ps))}
	for _, p := range ps {
		if _, dup := r.byKind[p.Kind()]; dup {
			continue
		}
		r.byKind[p.Kind()] = p
		r.order = append(r.order, p.Kind())
	}
	return r
}

func (r *Registry) Get(k types.Provider) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.byKind[k]
	return p, ok
}

func (r *Registry) All() []Provider {
	if r == nil {
		return nil
	}
	out := make([]Provider, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.byKind[k])
	}
	return out
}

// CanSpawn is the workerpool.Registry's eligibility predicate.
func (r *Registry) CanSpawn(k types.Provider) bool {
	p, ok := r.Get(k)
	return ok && p.CanSpawn()
}
