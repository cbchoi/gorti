package ownership

import (
	"context"
	"errors"
	"slices"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// ErrNotImplemented is retained as an exported sentinel for callers
// that matched on it during the M8 RED state. Implemented methods
// never return it; spec tests in rti/spec/M8/ use it to skip cleanly
// during pre-dispatch.
var ErrNotImplemented = errors.New("ownership: not implemented ( M8 deliverable)")

// SubscribersResolver returns the federate handles subscribed to the
// given object's class for at least one of attrs, in deterministic
// (sorted) order. Optional; when non-nil, NegotiatedDivest fans out
// requestAttributeOwnershipAssumption to those federates.
//
// The resolver receives the object handle (not the class) because
// ownership.Manager does not track class identity for an object — the
// resolver implementation is expected to consult object.Registry +
// declaration.Manager to translate. Cut-1 wiring leaves Subscribers
// nil and skips fan-out; production wiring is M8 W2 follow-up.
type SubscribersResolver func(
	ctx context.Context,
	fed core.FederationName,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) []core.FederateHandle

// Options bundles Manager dependencies.
//
// Required: Outbox. Optional: EventLog (cut-1: nil silently drops;
// proto Event variants for ownership transitions are not yet defined),
// Subscribers (cut-1: nil disables fan-out — see SubscribersResolver
// doc), Strategy (Phase 2b: nil → defaultNegotiation, see strategy.go).
type Options struct {
	Outbox      core.Outbox
	EventLog    core.EventLog
	Subscribers SubscribersResolver

	// Strategy is the OPTIONAL algorithm hook for ownership negotiation
	// policy. Nil → the package default (defaultNegotiation, which
	// picks the lowest-handle candidate — preserving cut-1 behavior).
	// See strategy.go for the interface and
	// docs/research-platform.md §6.3 for the design context.
	//
	// Phase 2b swap-point: production wires nil and gets unchanged
	// behavior; researchers wire an alternative impl through this slot.
	Strategy NegotiationStrategy
}

// Manager owns per-federation, per-(object, attribute) ownership
// state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-OWN-1..6. Bodies implemented in
// M8 W1.
type Manager struct {
	opts Options

	mu  gosync.RWMutex
	fed map[core.FederationName]*federationState
}

// Compile-time assertion: *Manager satisfies core.OwnershipCoordinator.
// Phase 1 of the research-platform refactor (docs/research-platform.md
// §5.2) introduced the interface; production keeps using *Manager.
var _ core.OwnershipCoordinator = (*Manager)(nil)

// federationState holds the per-federation ownership map.
type federationState struct {
	owners map[ownershipKey]ownershipRecord
	// pendingDivests indexed by (obj, attr) — set when the owner
	// has called NegotiatedDivest but no acquirer has yet completed
	// the transfer. Maps to the divest tag (opaque user data).
	pendingDivests map[ownershipKey]pendingDivest
	// pendingAcquires indexed by (obj, attr, acquirer) so multiple
	// federates can have outstanding acquires on the same attribute.
	pendingAcquires map[acquireKey]pendingAcquire
}

func newFederationState() *federationState {
	return &federationState{
		owners:          map[ownershipKey]ownershipRecord{},
		pendingDivests:  map[ownershipKey]pendingDivest{},
		pendingAcquires: map[acquireKey]pendingAcquire{},
	}
}

// OnFederationDestroyed drops ownership and pending transfer state for fed.
func (m *Manager) OnFederationDestroyed(fed core.FederationName) {
	m.mu.Lock()
	delete(m.fed, fed)
	m.mu.Unlock()
}

// ownershipKey identifies an (object, attribute) pair within a federation.
type ownershipKey struct {
	obj  core.ObjectHandle
	attr core.AttributeHandle
}

// ownershipRecord tracks the current owner.
type ownershipRecord struct {
	owner core.FederateHandle
}

// pendingDivest captures the in-flight NegotiatedDivest state for one
// (obj, attr). The tag is preserved so the eventual acquirer's
// ownership-assumption notification echoes the original divest tag.
// twoPhase marks a §7.6 REAL two-phase divest (M37): the
// transfer completes only on ConfirmDivestiture, never opportunistically.
type pendingDivest struct {
	owner    core.FederateHandle
	tag      []byte
	twoPhase bool
}

// acquireKey identifies a pending acquire request.
type acquireKey struct {
	obj      core.ObjectHandle
	attr     core.AttributeHandle
	acquirer core.FederateHandle
}

// pendingAcquire captures the in-flight Acquire state.
type pendingAcquire struct {
	tag []byte
}

// New constructs a Manager. Returns an error if Outbox is nil.
func New(opts Options) (*Manager, error) {
	if opts.Outbox == nil {
		return nil, errors.New("ownership.New: Options.Outbox is required")
	}
	// Strategy slot defaults to the package-default impl. nil → default
	// preserves existing call-site behavior; researchers override via
	// Options. See strategy.go.
	if opts.Strategy == nil {
		opts.Strategy = defaultNegotiation{}
	}
	return &Manager{
		opts: opts,
		fed:  map[core.FederationName]*federationState{},
	}, nil
}

// stateForLocked returns (creating if needed) the per-federation state.
// Caller MUST hold m.mu (write lock).
func (m *Manager) stateForLocked(fed core.FederationName) *federationState {
	st, ok := m.fed[fed]
	if !ok {
		st = newFederationState()
		m.fed[fed] = st
	}
	return st
}

// RegisterInitialOwnership records the initial owner of every attribute
// in attrs for object obj in federation fed. Called by the wiring code
// (cmd/rtid composition or object.Registry hook) immediately after a
// successful object registration so that subsequent divest / acquire /
// query calls have ground-truth ownership state to consult.
//
// Idempotent: re-registering the same (obj, attr, owner) is a no-op;
// re-registering with a DIFFERENT owner overwrites (matches the
// HLA semantics that ownership is single-writer at any instant).
func (m *Manager) RegisterInitialOwnership(
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) {
	if owner == core.InvalidFederateHandle || obj == core.InvalidObjectHandle {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(fed)
	for _, a := range attrs {
		st.owners[ownershipKey{obj: obj, attr: a}] = ownershipRecord{owner: owner}
	}
}

// UnconditionalDivest implements §7.2 — unconditionalAttributeOwnershipDivestiture.
// FR-OWN-1. Cut 1 already has this via federation resign; M8 promotes
// it to a first-class API call.
//
// Errors:
//   - core.ErrAttributeNotOwned if the caller is not the current owner
//     of any of the listed attributes
func (m *Manager) UnconditionalDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrAttributeNotOwned
	}
	// Pre-flight: caller must own EVERY attribute in attrs.
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		rec, ok := st.owners[k]
		if !ok || rec.owner != owner {
			m.mu.Unlock()
			return core.ErrAttributeNotOwned
		}
	}
	// Mark each attribute unowned.
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		delete(st.owners, k)
		// Clear any pending divest on the same key — the unconditional
		// path supersedes a prior negotiated divest.
		delete(st.pendingDivests, k)
	}
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtUnconditionalDivest, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}
	return nil
}

// NegotiatedDivest implements §7.3 — negotiatedAttributeOwnershipDivestiture.
// Phase 1 of the two-phase ownership transfer protocol (FR-OWN-2).
//
// The owner announces its desire to divest; the RTI broadcasts
// requestAttributeOwnershipAssumption to subscribers (when
// Options.Subscribers is wired). A subscriber then calls Acquire to
// take ownership.
//
// If a pending Acquire already exists on any of the listed attributes
// when NegotiatedDivest is called, the transfer to the FIRST queued
// acquirer (sorted handle order) completes immediately for that
// attribute (matches §7.7 "if-wanted" semantics applied opportunistically).
//
// Errors:
//   - core.ErrAttributeNotOwned if caller is not the current owner of
//     any of the listed attributes
//   - core.ErrOwnershipDivestPending if any of the listed attributes
//     already has a pending divest
func (m *Manager) NegotiatedDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	return m.negotiatedDivest(ctx, fed, owner, obj, attrs, tag, false)
}

// NegotiatedDivestTwoPhase is NegotiatedDivest under the REAL §7.3/§7.6
// two-phase protocol (M37): when an acquirer is (or becomes)
// engaged, the divester receives requestDivestitureConfirmation and the
// transfer completes ONLY on ConfirmDivestiture. The gRPC handler
// duck-types for this method when NegotiatedDivestRequest.two_phase is
// set; the frozen core.OwnershipCoordinator NegotiatedDivest keeps the
// pre-M37 one-phase flow.
func (m *Manager) NegotiatedDivestTwoPhase(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	return m.negotiatedDivest(ctx, fed, owner, obj, attrs, tag, true)
}

func (m *Manager) negotiatedDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
	twoPhase bool,
) error {
	tagCopy := cloneTag(tag)

	completions, err := m.recordPendingDivest(fed, owner, obj, attrs, tagCopy, twoPhase)
	if err != nil {
		return err
	}

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtNegotiatedDivest, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}

	m.fanoutAssumption(ctx, fed, owner, obj, attrs, tagCopy)

	if twoPhase {
		// §7.5 (M37) — an acquirer is already queued: ask the
		// divester to confirm instead of transferring. One event covers
		// the batch of confirmable attrs.
		if len(completions) > 0 {
			confirmable := make([]core.AttributeHandle, 0, len(completions))
			for _, c := range completions {
				confirmable = append(confirmable, c.attr)
			}
			slices.Sort(confirmable)
			_ = m.opts.Outbox.Send(ctx, fed, owner, divestNotificationEvent(obj, confirmable))
		}
		return nil
	}

	// Complete any opportunistic transfers (pre-M37 one-phase flow).
	for _, c := range completions {
		if err := m.completeTransfer(ctx, fed, obj, []core.AttributeHandle{c.attr}, owner, c.acquirer); err != nil {
			// Should not happen — completeTransfer only fails on
			// internal inconsistencies, which we surface as a
			// fatal-ish error to make debugging visible.
			return err
		}
	}
	return nil
}

// ConfirmDivestiture implements §7.6 — confirmDivestiture (M37).
// It completes a two-phase negotiated divestiture: each listed
// attribute must have a pending divest owned by `owner` AND at least
// one queued acquirer; the strategy-selected acquirer receives
// ownership + the §7.7 acquisition notification. The divester receives
// nothing further (the §7.5 confirmation request already fired).
//
// Errors (no state mutation on failure):
//   - core.ErrOwnershipNotInTransfer when any listed attribute has no
//     pending divest by `owner` or no queued acquirer.
func (m *Manager) ConfirmDivestiture(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st := m.stateForLocked(fed)
	winners := map[core.AttributeHandle]core.FederateHandle{}
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		pd, pending := st.pendingDivests[k]
		if !pending || pd.owner != owner {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
		winner := m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{
			Phase:      PhaseNegotiatedDivest,
			Federation: fed,
			Object:     obj,
			Attribute:  a,
			Owner:      owner,
			Candidates: st.queuedAcquirersLocked(obj, a),
		})
		if winner == core.InvalidFederateHandle {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
		winners[a] = winner
	}
	// Mutate only after the whole set validated (M36 DC-5 pattern).
	for a, winner := range winners {
		k := ownershipKey{obj: obj, attr: a}
		st.owners[k] = ownershipRecord{owner: winner}
		delete(st.pendingDivests, k)
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: a, acquirer: winner})
	}
	m.mu.Unlock()

	// Group the acquisition notifications by winner (usually one).
	byWinner := map[core.FederateHandle][]core.AttributeHandle{}
	for a, w := range winners {
		byWinner[w] = append(byWinner[w], a)
	}
	for _, w := range sortedWinnerHandles(byWinner) {
		wAttrs := byWinner[w]
		slices.Sort(wAttrs)
		if m.opts.EventLog != nil {
			_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{
				kind:  evtTransferred,
				obj:   obj,
				attrs: cloneAttrs(wAttrs),
				from:  owner,
				to:    w,
			})
		}
		_ = m.opts.Outbox.Send(ctx, fed, w, acquireNotificationEvent(obj, wAttrs, w))
	}
	return nil
}

// sortedWinnerHandles materializes the byWinner keys in ascending order.
func sortedWinnerHandles(m map[core.FederateHandle][]core.AttributeHandle) []core.FederateHandle {
	out := make([]core.FederateHandle, 0, len(m))
	for h := range m {
		out = append(out, h)
	}
	slices.Sort(out)
	return out
}

// pendingCompletion captures an attribute that already had a queued
// Acquire when NegotiatedDivest was called — the transfer to that
// acquirer fires immediately.
type pendingCompletion struct {
	attr     core.AttributeHandle
	acquirer core.FederateHandle
}

// recordPendingDivest validates ownership pre-conditions, records the
// pending-divest entry for each attribute, and reports the queued
// acquirers that the caller should now transfer to. Returns
// ErrAttributeNotOwned or ErrOwnershipDivestPending on validation
// failure (no state mutation in that case).
func (m *Manager) recordPendingDivest(
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tagCopy []byte,
	twoPhase bool,
) ([]pendingCompletion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stateForLocked(fed)
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		rec, ok := st.owners[k]
		if !ok || rec.owner != owner {
			return nil, core.ErrAttributeNotOwned
		}
		if _, pending := st.pendingDivests[k]; pending {
			return nil, core.ErrOwnershipDivestPending
		}
	}
	var completions []pendingCompletion
	for _, a := range attrs {
		st.pendingDivests[ownershipKey{obj: obj, attr: a}] = pendingDivest{owner: owner, tag: tagCopy, twoPhase: twoPhase}
		// Strategy hook: ask the policy which queued acquirer (if any)
		// wins the opportunistic transfer. Default picks the
		// lowest-handle candidate — equivalent to the prior
		// firstQueuedAcquirerLocked helper.
		candidates := st.queuedAcquirersLocked(obj, a)
		winner := m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{
			Phase:      PhaseNegotiatedDivest,
			Federation: fed,
			Object:     obj,
			Attribute:  a,
			Owner:      owner,
			Candidates: candidates,
		})
		if winner != core.InvalidFederateHandle {
			completions = append(completions, pendingCompletion{attr: a, acquirer: winner})
		}
	}
	return completions, nil
}

// queuedAcquirersLocked returns every federate with a pending Acquire
// for (obj, attr), in ascending handle order. Empty slice when none
// are queued. Caller MUST hold m.mu.
//
// Used by the strategy-driven swap-points (recordPendingDivest,
// DivestIfWanted) to assemble the SelectAcquirerContext.Candidates
// list. The default NegotiationStrategy picks Candidates[0] —
// equivalent to the prior firstQueuedAcquirerLocked helper.
func (st *federationState) queuedAcquirersLocked(obj core.ObjectHandle, attr core.AttributeHandle) []core.FederateHandle {
	var out []core.FederateHandle
	for ak := range st.pendingAcquires {
		if ak.obj != obj || ak.attr != attr {
			continue
		}
		out = append(out, ak.acquirer)
	}
	slices.Sort(out)
	return out
}

// fanoutAssumption emits requestAttributeOwnershipAssumption to
// subscribers (when Options.Subscribers is wired). The owner is
// excluded from recipients per HLA semantics.
func (m *Manager) fanoutAssumption(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tagCopy []byte,
) {
	if m.opts.Subscribers == nil {
		return
	}
	for _, h := range m.opts.Subscribers(ctx, fed, obj, attrs) {
		if h == owner {
			continue
		}
		_ = m.opts.Outbox.Send(ctx, fed, h, assumptionEvent(obj, attrs, tagCopy, owner))
	}
}

// Acquire implements §7.4 — attributeOwnershipAcquisition. Phase 2 of
// the two-phase protocol. The acquirer requests ownership; if the
// current owner has already called NegotiatedDivest, the transfer
// completes and both parties get callbacks. Otherwise the acquire is
// queued; when the owner eventually divests, the transfer fires.
//
// M36 DC-5 atomicity: classification of the requested attribute set is
// a read-only first pass; state mutates only after the whole set
// validates. A duplicate pending acquire therefore rejects the call
// with NO side effects (previously the inline check could reject after
// earlier attributes had already transitioned from-unowned, leaving
// granted-but-unnotified ownership behind).
//
// §7.9 residual (documented, M36 DC-5): the wire has no
// "if-available" flag, so acquisitionIfAvailable is EMULATED by the
// DLC as query-then-acquire. When two federates race an unowned
// attribute, the loser's Acquire lands here AFTER the winner's grant
// and — this being plain §7.4 semantics — is deterministically queued
// as a pending acquire rather than rejected. The loser thus receives
// neither §7.7 nor §7.10, and the queued acquire could later transfer
// ownership the federate only requested "if available". Closing this
// needs an if-available semantic flag on the AcquireRequest proto
// (out of scope for M36; rejecting here instead would break legitimate
// §7.4 callers and throw through the DLC's emulation path).
func (m *Manager) Acquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	return m.acquire(ctx, fed, acquirer, obj, attrs, tag, false)
}

// AcquireIfAvailable implements §7.9 —
// attributeOwnershipAcquisitionIfAvailable (M37). Atomically
// grants ONLY the currently-available attributes (unowned, or
// mid-divest with this acquirer selected); the unavailable remainder
// produces one AttributeOwnershipUnavailable callback (§7.10) and NO
// pending acquire entry. The gRPC handler duck-types for this method;
// the frozen core.OwnershipCoordinator Acquire keeps §7.8 queueing.
func (m *Manager) AcquireIfAvailable(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	return m.acquire(ctx, fed, acquirer, obj, attrs, tag, true)
}

func (m *Manager) acquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
	ifAvailable bool,
) error {
	tagCopy := cloneTag(tag)

	m.mu.Lock()
	st := m.stateForLocked(fed)

	type ready struct {
		attr  core.AttributeHandle
		owner core.FederateHandle
	}
	var readyAttrs []ready
	// M17.27 — attributes that are currently UNOWNED transition
	// directly to the acquirer (no prior owner → no divest
	// confirmation, only an acquired notification). Distinct from
	// readyAttrs which models the divest-then-acquire path with a
	// concrete oldOwner.
	var fromUnownedAttrs []core.AttributeHandle
	// Attributes that stay pending (owned, no completed divest).
	var queueAttrs []core.AttributeHandle
	// §7.10 (M37) — in ifAvailable mode, owned-with-no-divest
	// attrs are NOT queued; they are reported unavailable instead.
	var unavailableAttrs []core.AttributeHandle
	// §7.11 (M37) — queued attrs grouped by their CURRENT
	// owner, so RequestAttributeOwnershipRelease can target each owner
	// after the lock drops.
	queuedByOwner := map[core.FederateHandle][]core.AttributeHandle{}
	// §7.5/§7.6 (M37) — attrs whose pending divest is
	// TWO-PHASE: the acquire queues and the DIVESTER gets a
	// requestDivestitureConfirmation; transfer waits for
	// ConfirmDivestiture.
	confirmByOwner := map[core.FederateHandle][]core.AttributeHandle{}
	// Pass 1 — classify. READ-ONLY: no state mutation until the whole
	// attribute set has validated (M36 DC-5).
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		ak := acquireKey{obj: obj, attr: a, acquirer: acquirer}
		if pd, ok := st.pendingDivests[k]; ok {
			if pd.twoPhase {
				// §7.6 (M37) — REAL two-phase divest: never
				// transfer here. ifAvailable callers see the attr as
				// unavailable (still owned until confirm); plain
				// acquirers queue and the divester is asked to
				// confirm.
				if ifAvailable {
					unavailableAttrs = append(unavailableAttrs, a)
					continue
				}
				if _, dup := st.pendingAcquires[ak]; dup {
					m.mu.Unlock()
					return core.ErrOwnershipAcquirePending
				}
				queueAttrs = append(queueAttrs, a)
				confirmByOwner[pd.owner] = append(confirmByOwner[pd.owner], a)
				continue
			}
			// Strategy hook: owner has already divested — does the
			// just-arrived acquirer win the transfer right now? The
			// candidate set is the singleton {acquirer}; the default
			// selects it (cut-1 "first acquirer to call Acquire after
			// divest wins" rule). A market-based strategy might queue
			// instead by returning InvalidFederateHandle.
			winner := m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{
				Phase:      PhaseAcquire,
				Federation: fed,
				Object:     obj,
				Attribute:  a,
				Owner:      pd.owner,
				Candidates: []core.FederateHandle{acquirer},
			})
			if winner == acquirer {
				// The transfer fires now.
				readyAttrs = append(readyAttrs, ready{attr: a, owner: pd.owner})
				continue
			}
			// Strategy declined immediate transfer; fall through to the
			// queue-the-acquire path so the request stays pending.
		} else if _, owned := st.owners[k]; !owned {
			// M17.27 — attribute currently has no owner (someone
			// unconditional-divested earlier, or it was never
			// registered). Acquire on an unowned attribute completes
			// IMMEDIATELY: the acquirer becomes the owner, and
			// only the acquired notification fires (no prior owner
			// to receive divest-confirmation).
			fromUnownedAttrs = append(fromUnownedAttrs, a)
			continue
		}
		if ifAvailable {
			// §7.9 — owned with no completed divest: unavailable, no
			// pending entry.
			unavailableAttrs = append(unavailableAttrs, a)
			continue
		}
		if _, dup := st.pendingAcquires[ak]; dup {
			m.mu.Unlock()
			return core.ErrOwnershipAcquirePending
		}
		queueAttrs = append(queueAttrs, a)
		if rec, owned := st.owners[k]; owned {
			queuedByOwner[rec.owner] = append(queuedByOwner[rec.owner], a)
		}
	}
	// Pass 2 — mutate, now that the whole set validated.
	for _, a := range fromUnownedAttrs {
		st.owners[ownershipKey{obj: obj, attr: a}] = ownershipRecord{owner: acquirer}
	}
	for _, a := range queueAttrs {
		st.pendingAcquires[acquireKey{obj: obj, attr: a, acquirer: acquirer}] = pendingAcquire{tag: tagCopy}
	}
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtAcquire, obj: obj, attrs: cloneAttrs(attrs), to: acquirer})
	}

	// Process ready transfers grouped by old-owner so that one
	// transfer event covers the natural batch (single owner, multiple
	// attrs).
	byOwner := map[core.FederateHandle][]core.AttributeHandle{}
	for _, r := range readyAttrs {
		byOwner[r.owner] = append(byOwner[r.owner], r.attr)
	}
	for oldOwner, oldAttrs := range byOwner {
		slices.Sort(oldAttrs)
		if err := m.completeTransfer(ctx, fed, obj, oldAttrs, oldOwner, acquirer); err != nil {
			return err
		}
	}
	// M17.27 — fire acquired notification for attributes that
	// transitioned from unowned. No divest-confirmation fan-out
	// because there's no prior owner.
	if len(fromUnownedAttrs) > 0 {
		slices.Sort(fromUnownedAttrs)
		attrCopy := cloneAttrs(fromUnownedAttrs)
		if m.opts.EventLog != nil {
			_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{
				kind:  evtTransferred,
				obj:   obj,
				attrs: attrCopy,
				to:    acquirer,
			})
		}
		_ = m.opts.Outbox.Send(ctx, fed, acquirer,
			acquireNotificationEvent(obj, attrCopy, acquirer))
	}
	// §7.11 (M37) — the acquire stayed queued for attributes
	// owned by another federate: ask each current owner to release.
	// The pending entry is untouched (pre-M37 behavior preserved); the
	// owner unblocks it via divestiture / DivestIfWanted.
	for owner, ownedAttrs := range queuedByOwner {
		slices.Sort(ownedAttrs)
		_ = m.opts.Outbox.Send(ctx, fed, owner,
			releaseRequestEvent(obj, ownedAttrs, tagCopy))
	}
	// §7.5/§7.6 (M37) — ask each two-phase divester to
	// confirm; the queued acquire above waits for ConfirmDivestiture.
	for _, owner := range sortedWinnerHandles(confirmByOwner) {
		ownerAttrs := confirmByOwner[owner]
		slices.Sort(ownerAttrs)
		_ = m.opts.Outbox.Send(ctx, fed, owner,
			divestNotificationEvent(obj, ownerAttrs))
	}
	// §7.10 (M37) — report the unavailable remainder of an
	// ifAvailable acquire back to the acquirer.
	if len(unavailableAttrs) > 0 {
		slices.Sort(unavailableAttrs)
		_ = m.opts.Outbox.Send(ctx, fed, acquirer,
			unavailableEvent(obj, unavailableAttrs))
	}
	return nil
}

// completeTransfer moves ownership of (obj, attrs) from old to new,
// clears the corresponding pending state, and emits the
// divestiture+acquisition notifications.
func (m *Manager) completeTransfer(
	ctx context.Context,
	fed core.FederationName,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	oldOwner core.FederateHandle,
	newOwner core.FederateHandle,
) error {
	m.mu.Lock()
	st := m.stateForLocked(fed)
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		st.owners[k] = ownershipRecord{owner: newOwner}
		delete(st.pendingDivests, k)
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: a, acquirer: newOwner})
	}
	m.mu.Unlock()

	attrCopy := cloneAttrs(attrs)
	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{
			kind:  evtTransferred,
			obj:   obj,
			attrs: attrCopy,
			from:  oldOwner,
			to:    newOwner,
		})
	}

	// Notify the prior owner — divestiture confirmed.
	_ = m.opts.Outbox.Send(ctx, fed, oldOwner, divestNotificationEvent(obj, attrCopy))
	// Notify the new owner — acquisition confirmed.
	_ = m.opts.Outbox.Send(ctx, fed, newOwner, acquireNotificationEvent(obj, attrCopy, newOwner))
	return nil
}

// CancelDivest implements §7.5 — cancelNegotiatedAttributeOwnershipDivestiture.
// Owner withdraws a pending NegotiatedDivest before any acquirer
// claims it.
//
// Errors:
//   - core.ErrOwnershipNotInTransfer if no pending divest exists for
//     any of the listed attributes
func (m *Manager) CancelDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrOwnershipNotInTransfer
	}
	// Pre-flight: caller owns AND has a pending divest for every attr.
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		pd, pending := st.pendingDivests[k]
		if !pending || pd.owner != owner {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
	}
	for _, a := range attrs {
		delete(st.pendingDivests, ownershipKey{obj: obj, attr: a})
	}
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtCancelDivest, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}
	// Cut-1 simplification: §7.5 cancel-confirm
	// callbacks are not emitted here; subscribers learn the divest is
	// cancelled implicitly by the absence of a follow-on transfer.
	return nil
}

// CancelAcquire implements §7.6 — cancelAttributeOwnershipAcquisition.
// Acquirer withdraws a pending Acquire request.
//
// Errors:
//   - core.ErrOwnershipNotInTransfer if no pending acquire exists for
//     this acquirer on any of the listed attributes
func (m *Manager) CancelAcquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrOwnershipNotInTransfer
	}
	for _, a := range attrs {
		ak := acquireKey{obj: obj, attr: a, acquirer: acquirer}
		if _, pending := st.pendingAcquires[ak]; !pending {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
	}
	for _, a := range attrs {
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: a, acquirer: acquirer})
	}
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtCancelAcquire, obj: obj, attrs: cloneAttrs(attrs), to: acquirer})
	}
	return nil
}

// DivestIfWanted implements §7.7 — attributeOwnershipDivestitureIfWanted.
// Owner divests opportunistically — if a queued acquirer exists for an
// attribute, the transfer completes; otherwise the attribute stays
// with the owner (no pending state recorded).
//
// Errors:
//   - core.ErrAttributeNotOwned if the caller is not the current owner
//     of any listed attribute
func (m *Manager) DivestIfWanted(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrAttributeNotOwned
	}
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		rec, ok := st.owners[k]
		if !ok || rec.owner != owner {
			m.mu.Unlock()
			return core.ErrAttributeNotOwned
		}
	}
	type wanted struct {
		attr     core.AttributeHandle
		acquirer core.FederateHandle
	}
	var transfers []wanted
	for _, a := range attrs {
		// Strategy hook: ask the policy which queued acquirer (if any)
		// wins the opportunistic transfer. Default picks the
		// lowest-handle candidate; an empty queue yields
		// InvalidFederateHandle and the attribute stays with the owner.
		candidates := st.queuedAcquirersLocked(obj, a)
		winner := m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{
			Phase:      PhaseDivestIfWanted,
			Federation: fed,
			Object:     obj,
			Attribute:  a,
			Owner:      owner,
			Candidates: candidates,
		})
		if winner != core.InvalidFederateHandle {
			transfers = append(transfers, wanted{attr: a, acquirer: winner})
		}
	}
	m.mu.Unlock()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtDivestIfWanted, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}

	for _, t := range transfers {
		if err := m.completeTransfer(ctx, fed, obj, []core.AttributeHandle{t.attr}, owner, t.acquirer); err != nil {
			return err
		}
	}
	return nil
}

// QueryOwnership implements §7.8 — queryAttributeOwnership. Returns
// the current owner of (obj, attr). Returns (0, false) if the
// attribute is unowned (e.g. mid-transfer or never registered).
func (m *Manager) QueryOwnership(
	fed core.FederationName,
	obj core.ObjectHandle,
	attr core.AttributeHandle,
) (core.FederateHandle, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return 0, false
	}
	rec, ok := st.owners[ownershipKey{obj: obj, attr: attr}]
	if !ok {
		return 0, false
	}
	return rec.owner, true
}

// IsOwnedBy implements §7.9 — isAttributeOwnedByFederate. Convenience
// wrapper over QueryOwnership.
func (m *Manager) IsOwnedBy(
	fed core.FederationName,
	h core.FederateHandle,
	obj core.ObjectHandle,
	attr core.AttributeHandle,
) bool {
	owner, ok := m.QueryOwnership(fed, obj, attr)
	return ok && owner == h
}

// Snapshot returns aggregate ownership counts for the AdminService
// handler. Phase 1 of the rtid-TUI plan: counts only — per-attribute
// ownership history is explicitly excluded by docs/rtid-tui.md §3.2.
// Read under the manager RLock; cheap.
func (m *Manager) Snapshot(fed core.FederationName) core.OwnershipSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st, ok := m.fed[fed]
	if !ok {
		return core.OwnershipSnapshot{}
	}
	return core.OwnershipSnapshot{
		OwnedAttributesCount: uint32(len(st.owners)),
		PendingDivestsCount:  uint32(len(st.pendingDivests)),
		PendingAcquiresCount: uint32(len(st.pendingAcquires)),
	}
}

// cloneAttrs makes a defensive copy of an attribute slice.
func cloneAttrs(attrs []core.AttributeHandle) []core.AttributeHandle {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]core.AttributeHandle, len(attrs))
	copy(out, attrs)
	return out
}

// cloneTag makes a defensive copy of a byte tag.
func cloneTag(tag []byte) []byte {
	if len(tag) == 0 {
		return nil
	}
	out := make([]byte, len(tag))
	copy(out, tag)
	return out
}
