package ownership

import "github.com/cbchoi/gorti/rti/internal/core"

// NegotiationStrategy is the per-service algorithm hook for ownership
// negotiation policy. Researchers swap in alternative protocols
// (market-based, bidding, authority-based) by providing a type that
// implements this interface and wiring it through Options.Strategy.
//
// # Policy vs mechanism
//
// The ownership state machine in manager.go is a deliberate split:
//
//   - MECHANISM (stays in Manager): bookkeeping over the
//     pendingDivests / pendingAcquires / owners maps, pre-flight
//     ownership and error validation, event-log writes, outbox sends,
//     fanoutAssumption to subscribers, and completeTransfer's atomic
//     ownership swap. None of these are negotiable behavior — they
//     have to happen in some shape regardless of policy.
//
//   - POLICY (this strategy): "given an (obj, attr) the owner wants
//     to divest and a set of candidate acquirers, who wins?". That is
//     the one decision the cut-1 default takes opportunistically
//     (lowest-handle queued acquirer; on Acquire-vs-pending-divest,
//     the just-arrived caller always wins).
//
// The default impl matches today's behavior bit-for-bit (see
// defaultNegotiation below). Alternative strategies plug in via a
// single SelectAcquirer hook, which is sufficient for the three
// natural swap-points in manager.go:
//
//  1. NegotiatedDivest — on entry, if any attribute already has queued
//     acquirers, pick the winner to fire an opportunistic transfer.
//  2. Acquire — when an attribute has a pending divest, the just-
//     arrived caller takes the attribute (the candidate set is the
//     singleton {acquirer}, and the default selects it).
//  3. DivestIfWanted — opportunistic divest; pick the queued winner
//     for each attribute that has at least one queued acquirer.
//
// Phase 4 alternatives might pick a different candidate (highest-bid,
// authority-priority, randomised), or return InvalidFederateHandle to
// queue the transfer instead of firing now. The minimal interface
// keeps the strategy small enough for researchers to override without
// re-implementing the state machine.
//
// See docs/research-platform.md §6.3 for the design context.
type NegotiationStrategy interface {
	// SelectAcquirer picks the winning acquirer from the candidate set
	// at one of the swap-points described above. Returns
	// core.InvalidFederateHandle when no candidate should win in this
	// round (the transfer does not fire; affected pending state stays
	// untouched and the call returns nil).
	//
	// Candidates are passed in ascending-handle order to preserve
	// determinism for impls that consult the slice directly.
	//
	// MUST NOT mutate the caller's state; the strategy is a pure
	// decision function.
	SelectAcquirer(ctx SelectAcquirerContext) core.FederateHandle
	// Name is the registry key for Phase 3 TOML selection.
	Name() string
	// DeterminismPreserving reports whether this implementation honors
	// the NFR-DET-1 contract. Replay tests gate on this when
	// determinism mode is "per-impl-opt-in".
	DeterminismPreserving() bool
}

// SelectAcquirerPhase tags the call-site context passed into
// NegotiationStrategy.SelectAcquirer so policy impls can branch on the
// originating swap-point. The three values correspond to the three
// natural decisions in manager.go.
type SelectAcquirerPhase int

const (
	// PhaseNegotiatedDivest is the call from recordPendingDivest's
	// opportunistic-completion sweep: the owner has just called
	// NegotiatedDivest and one or more attributes already had queued
	// acquirers. Candidates are the queued acquirers for that attribute
	// (excluding the owner). Default: pick the lowest handle.
	PhaseNegotiatedDivest SelectAcquirerPhase = iota
	// PhaseAcquire is the call from Acquire where the attribute already
	// had a pendingDivest entry. Candidates are the singleton
	// {just-arrived acquirer}; default selects it (the cut-1 "first
	// acquirer to call Acquire after divest wins" rule).
	PhaseAcquire
	// PhaseDivestIfWanted is the call from DivestIfWanted: owner is
	// divesting opportunistically. Candidates are the queued acquirers
	// for the attribute (excluding the owner). Default: pick the lowest
	// handle, or InvalidFederateHandle when the queue is empty.
	PhaseDivestIfWanted
)

// SelectAcquirerContext is the input bundle for a single
// SelectAcquirer call. Passed by value so strategies cannot mutate the
// caller's state.
//
// Fields:
//   - Phase      : which swap-point is asking; see SelectAcquirerPhase.
//   - Federation : the federation hosting the negotiation.
//   - Object     : the object instance whose attribute is being negotiated.
//   - Attribute  : the attribute under negotiation.
//   - Owner      : the current (or prior) owner; for PhaseAcquire this
//                  is the federate that recorded the pendingDivest.
//   - Candidates : ascending-handle list of candidate acquirers. Never
//                  contains Owner. May be a singleton (PhaseAcquire).
//                  Empty list is possible for PhaseDivestIfWanted (no
//                  queued acquirers); the default returns
//                  InvalidFederateHandle in that case.
type SelectAcquirerContext struct {
	Phase      SelectAcquirerPhase
	Federation core.FederationName
	Object     core.ObjectHandle
	Attribute  core.AttributeHandle
	Owner      core.FederateHandle
	Candidates []core.FederateHandle
}

// defaultNegotiation is the package-default NegotiationStrategy. It
// preserves the cut-1 behavior verbatim:
//   - Empty candidate list → InvalidFederateHandle (no transfer).
//   - Otherwise pick the lowest-handle candidate. Since Candidates is
//     already sorted ascending, that's Candidates[0].
//
// This matches the pre-strategy code paths at all three call-sites:
// firstQueuedAcquirerLocked picked the lowest handle, and Acquire
// always fired on the singleton {acquirer} candidate.
type defaultNegotiation struct{}

// SelectAcquirer returns the lowest-handle candidate, or
// core.InvalidFederateHandle when the candidate set is empty.
func (defaultNegotiation) SelectAcquirer(c SelectAcquirerContext) core.FederateHandle {
	if len(c.Candidates) == 0 {
		return core.InvalidFederateHandle
	}
	return c.Candidates[0]
}

// Name returns "default" — the registry key for the package default.
func (defaultNegotiation) Name() string { return "default" }

// DeterminismPreserving returns true: lowest-handle selection over a
// pre-sorted list is fully deterministic.
func (defaultNegotiation) DeterminismPreserving() bool { return true }

// Compile-time assertion: defaultNegotiation satisfies NegotiationStrategy.
var _ NegotiationStrategy = (*defaultNegotiation)(nil)
