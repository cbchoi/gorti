package research

import (
	"errors"
	"fmt"

	"github.com/cbchoi/gorti/rti/internal/ownership"
	timepkg "github.com/cbchoi/gorti/rti/internal/time"
)

// Resolved is the output of Apply: the actual strategy instances
// looked up from the registry, plus the active determinism mode for
// downstream consumers (replay test gating, MOM introspection,
// metrics labels).
//
// Construction: Apply(cfg, reg). The fields are exported so the rtid
// composition root can thread them directly into time.Options +
// ownership.Options.
type Resolved struct {
	// Determinism is the mode the rtid is operating under. Replay
	// tests + the gating helpers in this struct consult it.
	Determinism DeterminismMode

	// Time exposes the resolved time-package strategies.
	Time ResolvedTime

	// Ownership exposes the resolved ownership-package strategy.
	Ownership ResolvedOwnership
}

// ResolvedTime is the resolved time-package strategy bundle.
type ResolvedTime struct {
	LBTS  timepkg.LBTSStrategy
	Grant timepkg.GrantStrategy
}

// ResolvedOwnership is the resolved ownership-package strategy bundle.
type ResolvedOwnership struct {
	Negotiation ownership.NegotiationStrategy
}

// AllPreserving returns true when every resolved strategy reports
// DeterminismPreserving() == true. Replay-test gating uses this when
// the active mode is "per-impl-opt-in": tests run when AllPreserving
// is true and skip otherwise. In strict mode AllPreserving is always
// true (the strict gate in Apply rejects boot otherwise); in off mode
// AllPreserving is informational only.
func (r Resolved) AllPreserving() bool {
	if r.Time.LBTS != nil && !r.Time.LBTS.DeterminismPreserving() {
		return false
	}
	if r.Time.Grant != nil && !r.Time.Grant.DeterminismPreserving() {
		return false
	}
	if r.Ownership.Negotiation != nil && !r.Ownership.Negotiation.DeterminismPreserving() {
		return false
	}
	return true
}

// Apply resolves cfg against reg, returning the wired strategies in a
// Resolved bundle, and enforces the strict-mode determinism gate.
//
// Errors:
//   - cfg or reg nil → returns an explicit error (programmer error).
//   - any selected strategy name missing from the registry → returns
//     an error naming the (category, name) pair.
//   - in strict mode, any resolved strategy reporting
//     DeterminismPreserving() == false → returns a NonPreservingError
//     naming the offending strategy. Callers in cmd/rtid log and
//     exit 2 (per design doc §8) so the rtid never half-starts in a
//     conformance-violating state.
func Apply(cfg *Config, reg *Registry) (Resolved, error) {
	if cfg == nil {
		return Resolved{}, errors.New("research: Apply: cfg must not be nil")
	}
	if reg == nil {
		return Resolved{}, errors.New("research: Apply: reg must not be nil")
	}

	out := Resolved{Determinism: cfg.Determinism}

	lbts, ok := reg.LookupLBTS(cfg.Time.LBTS)
	if !ok {
		return Resolved{}, fmt.Errorf("research: %s strategy %q not registered", CategoryTimeLBTS, cfg.Time.LBTS)
	}
	out.Time.LBTS = lbts

	grant, ok := reg.LookupGrant(cfg.Time.Grant)
	if !ok {
		return Resolved{}, fmt.Errorf("research: %s strategy %q not registered", CategoryTimeGrant, cfg.Time.Grant)
	}
	out.Time.Grant = grant

	neg, ok := reg.LookupNegotiation(cfg.Ownership.Negotiation)
	if !ok {
		return Resolved{}, fmt.Errorf("research: %s strategy %q not registered", CategoryOwnershipNegotiation, cfg.Ownership.Negotiation)
	}
	out.Ownership.Negotiation = neg

	if cfg.Determinism == DeterminismStrict {
		if !lbts.DeterminismPreserving() {
			return Resolved{}, &NonPreservingError{Category: CategoryTimeLBTS, Name: lbts.Name()}
		}
		if !grant.DeterminismPreserving() {
			return Resolved{}, &NonPreservingError{Category: CategoryTimeGrant, Name: grant.Name()}
		}
		if !neg.DeterminismPreserving() {
			return Resolved{}, &NonPreservingError{Category: CategoryOwnershipNegotiation, Name: neg.Name()}
		}
	}

	return out, nil
}

// NonPreservingError is returned by Apply in strict mode when any
// resolved strategy reports DeterminismPreserving() == false. The
// error names the (category, name) pair so the operator can either
// switch to per-impl-opt-in or pick a preserving alternative.
type NonPreservingError struct {
	Category Category
	Name     string
}

// Error implements the error interface.
func (e *NonPreservingError) Error() string {
	return fmt.Sprintf("research: strict-mode rejects non-preserving %s strategy %q", e.Category, e.Name)
}
