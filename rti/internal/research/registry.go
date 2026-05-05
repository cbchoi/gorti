// Package research provides the Phase 3 research-platform infrastructure
// for gorti: a typed strategy registry, a TOML-based research-config
// loader, and the Apply helper that resolves a config against a registry
// and enforces the determinism gate.
//
// # Scope
//
// This package is the cross-cutting consumer that ties algorithm-level
// strategies (defined in their owning packages, e.g. internal/time and
// internal/ownership) into the rtid composition root. Phase 1 deliberately
// kept the strategy interfaces in their owning packages so that this
// package — and only this package — imports both internal/time and
// internal/ownership without creating an import cycle through internal/core.
//
// See docs/research-platform.md §7.2 for the design context.
package research

import (
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/ownership"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// Category identifies a strategy slot in the registry. The string values
// match the TOML keys (e.g. "time.lbts", "time.grant",
// "ownership.negotiation") so error messages are consistent across the
// CLI and the config-file syntax.
type Category string

// Known categories. Adding a new strategy slot means adding a new
// constant + a new typed map in Registry; the string value MUST match
// the TOML key path used in research-config files.
const (
	CategoryTimeLBTS             Category = "time.lbts"
	CategoryTimeGrant            Category = "time.grant"
	CategoryOwnershipNegotiation Category = "ownership.negotiation"
)

// Registry holds the strategy implementations available for selection
// by a research-config file. Categories are typed so callers cannot
// register a GrantStrategy under "time.lbts" by accident; the per-category
// Register/Lookup methods carry the static type through.
//
// Registry is NOT goroutine-safe. The expected lifecycle is "build at
// startup, treat as immutable thereafter"; the rtid composition root
// constructs one Registry, registers any alternative impls, and never
// mutates it again. Tests follow the same pattern.
type Registry struct {
	lbts        map[string]timepkg.LBTSStrategy
	grant       map[string]timepkg.GrantStrategy
	negotiation map[string]ownership.NegotiationStrategy
}

// NewRegistry returns an empty Registry. Use Default() to get one
// pre-populated with the package-default impls; NewRegistry is exposed
// for tests that want a known-empty starting point.
func NewRegistry() *Registry {
	return &Registry{
		lbts:        map[string]timepkg.LBTSStrategy{},
		grant:       map[string]timepkg.GrantStrategy{},
		negotiation: map[string]ownership.NegotiationStrategy{},
	}
}

// Default returns a Registry pre-populated with the package-default
// impls under the name "default". This is the registry shape rtid uses
// when no alternative impls have been linked into the binary; a TOML
// config that selects "default" everywhere resolves to behavior
// identical to today's hand-wired runtime.
func Default() *Registry {
	r := NewRegistry()
	// Errors from the Register* helpers can only come from duplicate-name
	// collisions. With a freshly-empty registry that never happens, so
	// the error returns are safe to ignore here. We keep Register*
	// returning errors for the public API (researchers calling
	// Register* on a populated registry).
	_ = r.RegisterLBTS("default", timepkg.DefaultLBTSStrategy())
	_ = r.RegisterGrant("default", timepkg.DefaultGrantStrategy())
	_ = r.RegisterNegotiation("default", ownership.DefaultNegotiationStrategy())
	return r
}

// RegisterLBTS registers an LBTSStrategy under name. Returns an error
// when name is empty, when impl is nil, or when name is already taken.
func (r *Registry) RegisterLBTS(name string, impl timepkg.LBTSStrategy) error {
	if name == "" {
		return fmt.Errorf("research: register %s: name must not be empty", CategoryTimeLBTS)
	}
	if impl == nil {
		return fmt.Errorf("research: register %s %q: impl must not be nil", CategoryTimeLBTS, name)
	}
	if _, dup := r.lbts[name]; dup {
		return fmt.Errorf("research: register %s %q: already registered", CategoryTimeLBTS, name)
	}
	r.lbts[name] = impl
	return nil
}

// RegisterGrant registers a GrantStrategy under name. Same error
// semantics as RegisterLBTS.
func (r *Registry) RegisterGrant(name string, impl timepkg.GrantStrategy) error {
	if name == "" {
		return fmt.Errorf("research: register %s: name must not be empty", CategoryTimeGrant)
	}
	if impl == nil {
		return fmt.Errorf("research: register %s %q: impl must not be nil", CategoryTimeGrant, name)
	}
	if _, dup := r.grant[name]; dup {
		return fmt.Errorf("research: register %s %q: already registered", CategoryTimeGrant, name)
	}
	r.grant[name] = impl
	return nil
}

// RegisterNegotiation registers an ownership.NegotiationStrategy under
// name. Same error semantics as RegisterLBTS.
func (r *Registry) RegisterNegotiation(name string, impl ownership.NegotiationStrategy) error {
	if name == "" {
		return fmt.Errorf("research: register %s: name must not be empty", CategoryOwnershipNegotiation)
	}
	if impl == nil {
		return fmt.Errorf("research: register %s %q: impl must not be nil", CategoryOwnershipNegotiation, name)
	}
	if _, dup := r.negotiation[name]; dup {
		return fmt.Errorf("research: register %s %q: already registered", CategoryOwnershipNegotiation, name)
	}
	r.negotiation[name] = impl
	return nil
}

// LookupLBTS returns the LBTSStrategy registered under name. The
// boolean is false when no impl is registered under that name.
func (r *Registry) LookupLBTS(name string) (timepkg.LBTSStrategy, bool) {
	v, ok := r.lbts[name]
	return v, ok
}

// LookupGrant returns the GrantStrategy registered under name.
func (r *Registry) LookupGrant(name string) (timepkg.GrantStrategy, bool) {
	v, ok := r.grant[name]
	return v, ok
}

// LookupNegotiation returns the NegotiationStrategy registered under
// name.
func (r *Registry) LookupNegotiation(name string) (ownership.NegotiationStrategy, bool) {
	v, ok := r.negotiation[name]
	return v, ok
}
