package ownership

import (
	"context"
	"errors"
	"fmt"
	"slices"
	gosync "sync"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// ErrNotImplemented is retained as an exported sentinel for callers
// that matched on it during the M8 RED state. Implemented methods
// never return it; spec tests in rti/spec/M8/ use it to skip cleanly
// during pre-dispatch.
var ErrNotImplemented = errors.New("ownership: not implemented ( M8 deliverable)")

// SubscribersResolver is the legacy name of the assumption-candidate resolver.
// It must include publishers eligible at the object's known class, even when
// they are not currently subscribed. Manager checks knowledge/publication using
// PrepareAcquisition and excludes owners and pending acquirers per attribute.
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
	// PublicationAttributes maps a class publication into an object's registered
	// coordinates before matching outstanding ownership offers.
	PublicationAttributes func(context.Context, core.FederationName, core.ObjectHandle, core.ObjectClassHandle, []core.AttributeHandle) []core.AttributeHandle
	// PrepareAcquisition validates object knowledge, FOM attributes and
	// publication. Its idempotent release function pins the object until
	// ownership mutation completes. The application supplies this port;
	// isolated ownership algorithms may omit it when no registry exists.
	PrepareAcquisition func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (release func(), err error)
	// PrepareReleaseDenied validates knowledge and defined attributes without
	// requiring the owner's acquisition publication, and pins object identity.
	PrepareReleaseDenied func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (release func(), err error)
	// PrepareDivestiture validates object knowledge and defined attributes,
	// without acquisition publication checks, and pins identity until mutation.
	// The application supplies this port; isolated algorithms may omit it.
	PrepareDivestiture func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) (release func(), err error)
	// Known-coordinate ports map attributes and pin object knowledge in one
	// registry transaction. Canonical internal calls retain the ports above.
	PrepareKnownAcquisition func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error)
	PrepareKnownOperation   func(context.Context, core.FederationName, core.FederateHandle, core.ObjectHandle, []core.AttributeHandle) ([]core.AttributeHandle, func(), error)

	// Strategy is the OPTIONAL algorithm hook for ownership negotiation
	// policy. Nil → the package default (defaultNegotiation, which
	// picks the lowest-handle candidate — preserving cut-1 behavior).
	// See strategy.go for the interface and
	// docs/research-platform.md §6.3 for the design context.
	//
	// Phase 2b swap-point: production wires nil and gets unchanged
	// behavior; researchers wire an alternative impl through this slot.
	Strategy NegotiationStrategy
	// Bounds pending callbacks plus out-of-order completed identities per
	// recipient. Exhaustion rejects the operation before ownership mutation.
	CallbackWindow          uint64
	OnTerminalDeliveryError func(core.FederationName, core.FederateHandle, uint64, error)
}

// Manager owns per-federation, per-(object, attribute) ownership
// state. Goroutine-safe.
//
// FROZEN-shape per docs/srs.md FR-OWN-1..6. Bodies implemented in
// M8 W1.
type Manager struct {
	opts Options

	mu                 gosync.RWMutex
	fed                map[core.FederationName]*federationState
	onOwnershipChanged func(context.Context, core.FederationName, core.ObjectHandle)
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
	// unownedOffers retains the former divester while 7.2 solicitation continues.
	unownedOffers    map[ownershipKey]core.FederateHandle
	callbackSessions map[core.FederateHandle]*ownershipCallbackSession
}

func newFederationState() *federationState {
	return &federationState{
		owners:           map[ownershipKey]ownershipRecord{},
		pendingDivests:   map[ownershipKey]pendingDivest{},
		pendingAcquires:  map[acquireKey]pendingAcquire{},
		unownedOffers:    map[ownershipKey]core.FederateHandle{},
		callbackSessions: map[core.FederateHandle]*ownershipCallbackSession{},
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
	owner                 core.FederateHandle
	tag                   []byte
	twoPhase              bool
	confirmationRequested bool
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
	if opts.CallbackWindow == 0 {
		opts.CallbackWindow = MaxCallbackStateCount
	}
	if opts.CallbackWindow > MaxCallbackStateCount {
		return nil, errors.New("ownership callback window exceeds bounded state limit")
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
		key := ownershipKey{obj: obj, attr: a}
		st.owners[key] = ownershipRecord{owner: owner}
		delete(st.unownedOffers, key)
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
	return m.unconditionalDivest(ctx, fed, owner, obj, attrs, canonicalPreparation(m.prepareDivestiture))
}

func (m *Manager) unconditionalDivest(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, prepare ownershipPreparation) error {
	attrs, release, err := prepare(ctx, fed, owner, obj, attrs)
	if err != nil {
		return err
	}
	defer release()
	attrs = slices.Clone(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrAttributeNotOwned
	}
	if err := st.validateOwnedAttributes(obj, owner, attrs); err != nil {
		m.mu.Unlock()
		return err
	}
	type transfer struct {
		attr      core.AttributeHandle
		winner    core.FederateHandle
		remaining []core.OutboxDelivery
		terminal  *CallbackEvent
	}
	var transfers []transfer
	var unowned []core.AttributeHandle
	// Select all recipients before changing ownership, including validation of
	// strategy results. Each callback covers one attribute to preserve 7.1.5.
	for _, a := range attrs {
		candidates := st.queuedAcquirersLocked(obj, a)
		winner := m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{
			Phase: PhaseUnconditionalDivest, Federation: fed, Object: obj,
			Attribute: a, Owner: owner, Candidates: slices.Clone(candidates),
		})
		if winner != core.InvalidFederateHandle && !slices.Contains(candidates, winner) {
			m.mu.Unlock()
			return fmt.Errorf("ownership: unconditional divest strategy selected noncandidate %d", winner)
		}
		if winner == core.InvalidFederateHandle {
			unowned = append(unowned, a)
			continue
		}
		tr := transfer{attr: a, winner: winner, terminal: acquireNotificationEvent(obj, []core.AttributeHandle{a}, winner)}
		tr.remaining = st.remainingReleaseRequests(obj, a, winner, candidates)
		transfers = append(transfers, tr)
	}
	var terminalDeliveries []core.OutboxDelivery
	for _, tr := range transfers {
		terminalDeliveries = append(terminalDeliveries, core.OutboxDelivery{Recipient: tr.winner, Event: tr.terminal})
	}
	if err := m.registerTerminalCallbacksLocked(st, fed, terminalDeliveries); err != nil {
		m.mu.Unlock()
		return err
	}
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		delete(st.owners, k)
		st.unownedOffers[k] = owner
		// Clear any pending divest on the same key — the unconditional
		// path supersedes a prior negotiated divest.
		delete(st.pendingDivests, k)
	}
	for _, tr := range transfers {
		st.owners[ownershipKey{obj: obj, attr: tr.attr}] = ownershipRecord{owner: tr.winner}
		delete(st.unownedOffers, ownershipKey{obj: obj, attr: tr.attr})
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: tr.attr, acquirer: tr.winner})
	}
	m.mu.Unlock()
	release()
	defer m.notifyOwnershipChanged(ctx, fed, obj)

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtUnconditionalDivest, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}
	for _, tr := range transfers {
		m.notifyAcquisition(ctx, fed, obj, []core.AttributeHandle{tr.attr}, owner, tr.winner, nil, tr.terminal)
		for _, delivery := range tr.remaining {
			_ = m.opts.Outbox.Send(ctx, fed, delivery.Recipient, delivery.Event)
		}
	}
	m.fanoutAssumption(ctx, fed, owner, obj, unowned, nil, true)
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
	return m.negotiatedDivest(ctx, fed, owner, obj, attrs, tag, false, canonicalPreparation(m.prepareDivestiture))
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
	return m.negotiatedDivest(ctx, fed, owner, obj, attrs, tag, true, canonicalPreparation(m.prepareDivestiture))
}

func (m *Manager) negotiatedDivest(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
	twoPhase bool,
	prepare ownershipPreparation,
) error {
	attrs, release, err := prepare(ctx, fed, owner, obj, attrs)
	if err != nil {
		return err
	}
	defer release()
	tagCopy := cloneTag(tag)

	completions, err := m.recordPendingDivest(fed, owner, obj, attrs, tagCopy, twoPhase)
	if err != nil {
		return err
	}
	release()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtNegotiatedDivest, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}

	m.fanoutAssumption(ctx, fed, owner, obj, attrs, tagCopy, false)

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
		m.notifyTransfer(ctx, fed, obj, []core.AttributeHandle{c.attr}, owner, c.acquirer, tagCopy, c.terminal)
	}
	if len(completions) > 0 {
		m.notifyOwnershipChanged(ctx, fed, obj)
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
// Errors (ownership is unchanged on failure):
//   - core.ErrAttributeNotOwned when the caller does not own every attribute.
//   - core.ErrOwnershipConfirmationNotRequested without a prior request to confirm.
//   - core.ErrOwnershipNoAcquisitionPending when all acquirers have withdrawn;
//     affected attributes return to waiting for a new owner (7.6).
func (m *Manager) ConfirmDivestiture(
	ctx context.Context,
	fed core.FederationName,
	owner core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
) error {
	return m.ConfirmDivestitureWithTag(ctx, fed, owner, obj, attrs, nil)
}

// ConfirmDivestitureWithTag preserves the explicit confirmation tag without
// changing the legacy coordinator method.
func (m *Manager) ConfirmDivestitureWithTag(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte) error {
	return m.confirmDivestiture(ctx, fed, owner, obj, attrs, tag, canonicalPreparation(m.prepareDivestiture))
}

func (m *Manager) confirmDivestiture(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, tag []byte, prepare ownershipPreparation) error {
	attrs, release, err := prepare(ctx, fed, owner, obj, attrs)
	if err != nil {
		return err
	}
	defer release()
	tagCopy := cloneTag(tag)
	m.mu.Lock()
	st := m.fed[fed]
	if st == nil {
		m.mu.Unlock()
		return core.ErrAttributeNotOwned
	}
	if err := st.validateOwnedAttributes(obj, owner, attrs); err != nil {
		m.mu.Unlock()
		return err
	}
	candidates, err := st.confirmationCandidates(obj, owner, attrs)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	winners := map[core.AttributeHandle]core.FederateHandle{}
	for _, a := range attrs {
		winner := m.opts.Strategy.SelectAcquirer(SelectAcquirerContext{
			Phase:      PhaseNegotiatedDivest,
			Federation: fed,
			Object:     obj,
			Attribute:  a,
			Owner:      owner,
			Candidates: candidates[a],
		})
		if winner == core.InvalidFederateHandle {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
		if _, queued := st.pendingAcquires[acquireKey{obj: obj, attr: a, acquirer: winner}]; !queued {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
		winners[a] = winner
	}
	byWinner := map[core.FederateHandle][]core.AttributeHandle{}
	for a, winner := range winners {
		byWinner[winner] = append(byWinner[winner], a)
	}
	terminals := map[core.FederateHandle]*CallbackEvent{}
	var terminalDeliveries []core.OutboxDelivery
	for _, winner := range sortedWinnerHandles(byWinner) {
		slices.Sort(byWinner[winner])
		event := acquireNotificationEvent(obj, byWinner[winner], winner, tagCopy)
		terminals[winner] = event
		terminalDeliveries = append(terminalDeliveries, core.OutboxDelivery{Recipient: winner, Event: event})
	}
	if err := m.registerTerminalCallbacksLocked(st, fed, terminalDeliveries); err != nil {
		m.mu.Unlock()
		return err
	}
	// Mutate only after the whole set validated (M36 DC-5 pattern).
	for a, winner := range winners {
		k := ownershipKey{obj: obj, attr: a}
		st.owners[k] = ownershipRecord{owner: winner}
		delete(st.pendingDivests, k)
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: a, acquirer: winner})
	}
	m.mu.Unlock()
	release()

	// Group the acquisition notifications by winner (usually one).
	defer m.notifyOwnershipChanged(ctx, fed, obj)
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
		_ = m.sendTerminalCallback(ctx, fed, w, terminals[w])
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
	terminal *CallbackEvent
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
	st := m.fed[fed]
	if st == nil {
		return nil, core.ErrAttributeNotOwned
	}
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
		if _, queued := st.pendingAcquires[acquireKey{obj: obj, attr: a, acquirer: winner}]; winner != core.InvalidFederateHandle && queued {
			completions = append(completions, pendingCompletion{attr: a, acquirer: winner, terminal: acquireNotificationEvent(obj, []core.AttributeHandle{a}, winner, tagCopy)})
		}
	}
	if !twoPhase {
		var deliveries []core.OutboxDelivery
		for _, c := range completions {
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: c.acquirer, Event: c.terminal})
		}
		if err := m.registerTerminalCallbacksLocked(st, fed, deliveries); err != nil {
			return nil, err
		}
	}
	for _, a := range attrs {
		st.pendingDivests[ownershipKey{obj: obj, attr: a}] = pendingDivest{owner: owner, tag: tagCopy, twoPhase: twoPhase}
	}
	if twoPhase {
		for _, c := range completions {
			key := ownershipKey{obj: obj, attr: c.attr}
			pd := st.pendingDivests[key]
			pd.confirmationRequested = true
			st.pendingDivests[key] = pd
		}
	}
	if !twoPhase {
		for _, c := range completions {
			k := ownershipKey{obj: obj, attr: c.attr}
			st.owners[k] = ownershipRecord{owner: c.acquirer}
			delete(st.pendingDivests, k)
			delete(st.pendingAcquires, acquireKey{obj: obj, attr: c.attr, acquirer: c.acquirer})
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

// Acquire requests attribute ownership. Available attributes and legacy
// one-phase offers can transfer immediately; two-phase offers wait for owner
// confirmation. The complete request is validated before ownership mutation.
// State commits before journal observers or callbacks run. Delivery failure
// atomicity and the remaining ordinary divestiture paths are separate concerns.
func (m *Manager) Acquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
) error {
	return m.acquire(ctx, fed, acquirer, obj, attrs, tag, false, canonicalPreparation(m.opts.PrepareAcquisition))
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
	return m.acquire(ctx, fed, acquirer, obj, attrs, tag, true, canonicalPreparation(m.opts.PrepareAcquisition))
}

func (m *Manager) acquire(
	ctx context.Context,
	fed core.FederationName,
	acquirer core.FederateHandle,
	obj core.ObjectHandle,
	attrs []core.AttributeHandle,
	tag []byte,
	ifAvailable bool,
	prepare ownershipPreparation,
) error {
	attrs, release, err := prepare(ctx, fed, acquirer, obj, attrs)
	if err != nil {
		return err
	}
	defer release()
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
		if rec, owned := st.owners[k]; owned && rec.owner == acquirer {
			m.mu.Unlock()
			return core.ErrFederateOwnsAttributes
		}
		if rec, owned := st.owners[k]; owned && rec.owner == ^core.FederateHandle(0) {
			// The MOM producer never transfers its predefined attributes (11.2.1).
			unavailableAttrs = append(unavailableAttrs, a)
			continue
		}
		if _, pending := st.pendingAcquires[ak]; pending {
			if ifAvailable {
				m.mu.Unlock()
				return core.ErrOwnershipAcquirePending
			}
			// IEEE 1516.1-2010 7.8: repeats retain the original request
			// and tag, without another release or confirmation callback.
			continue
		}
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
		queueAttrs = append(queueAttrs, a)
		if rec, owned := st.owners[k]; owned {
			queuedByOwner[rec.owner] = append(queuedByOwner[rec.owner], a)
		}
	}
	byOwner := map[core.FederateHandle][]core.AttributeHandle{}
	for _, r := range readyAttrs {
		byOwner[r.owner] = append(byOwner[r.owner], r.attr)
	}
	readyTerminals := map[core.FederateHandle]*CallbackEvent{}
	var terminalDeliveries []core.OutboxDelivery
	for _, oldOwner := range sortedWinnerHandles(byOwner) {
		slices.Sort(byOwner[oldOwner])
		event := acquireNotificationEvent(obj, byOwner[oldOwner], acquirer, tagCopy)
		readyTerminals[oldOwner] = event
		terminalDeliveries = append(terminalDeliveries, core.OutboxDelivery{Recipient: acquirer, Event: event})
	}
	var fromUnownedTerminal, unavailableTerminal *CallbackEvent
	if len(fromUnownedAttrs) > 0 {
		slices.Sort(fromUnownedAttrs)
		fromUnownedTerminal = acquireNotificationEvent(obj, fromUnownedAttrs, acquirer, tagCopy)
		terminalDeliveries = append(terminalDeliveries, core.OutboxDelivery{Recipient: acquirer, Event: fromUnownedTerminal})
	}
	if len(unavailableAttrs) > 0 {
		slices.Sort(unavailableAttrs)
		unavailableTerminal = unavailableEvent(obj, unavailableAttrs)
		terminalDeliveries = append(terminalDeliveries, core.OutboxDelivery{Recipient: acquirer, Event: unavailableTerminal})
	}
	if err := m.registerTerminalCallbacksLocked(st, fed, terminalDeliveries); err != nil {
		m.mu.Unlock()
		return err
	}
	// Pass 2 — mutate, now that the whole set validated.
	for _, a := range fromUnownedAttrs {
		st.owners[ownershipKey{obj: obj, attr: a}] = ownershipRecord{owner: acquirer}
		delete(st.unownedOffers, ownershipKey{obj: obj, attr: a})
	}
	for _, a := range queueAttrs {
		st.pendingAcquires[acquireKey{obj: obj, attr: a, acquirer: acquirer}] = pendingAcquire{tag: tagCopy}
	}
	for _, confirmAttrs := range confirmByOwner {
		for _, a := range confirmAttrs {
			k := ownershipKey{obj: obj, attr: a}
			pd := st.pendingDivests[k]
			pd.confirmationRequested = true
			st.pendingDivests[k] = pd
		}
	}
	for _, r := range readyAttrs {
		k := ownershipKey{obj: obj, attr: r.attr}
		st.owners[k] = ownershipRecord{owner: acquirer}
		delete(st.pendingDivests, k)
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: r.attr, acquirer: acquirer})
	}
	m.mu.Unlock()
	release()
	if len(fromUnownedAttrs) > 0 || len(readyAttrs) > 0 {
		defer m.notifyOwnershipChanged(ctx, fed, obj)
	}

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtAcquire, obj: obj, attrs: cloneAttrs(attrs), to: acquirer})
	}

	// Process ready transfers grouped by old-owner so that one
	// transfer event covers the natural batch (single owner, multiple
	// attrs).
	for _, oldOwner := range sortedWinnerHandles(byOwner) {
		oldAttrs := byOwner[oldOwner]
		slices.Sort(oldAttrs)
		m.notifyTransfer(ctx, fed, obj, oldAttrs, oldOwner, acquirer, tagCopy, readyTerminals[oldOwner])
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
		_ = m.sendTerminalCallback(ctx, fed, acquirer, fromUnownedTerminal)
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
		_ = m.sendTerminalCallback(ctx, fed, acquirer, unavailableTerminal)
	}
	return nil
}

func (m *Manager) notifyTransfer(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle, oldOwner, newOwner core.FederateHandle, _ []byte, terminal *CallbackEvent) {
	attrCopy := cloneAttrs(attrs)
	m.recordTransfer(ctx, fed, obj, attrCopy, oldOwner, newOwner)
	// Retained only for the legacy one-phase negotiated entrypoint.
	_ = m.opts.Outbox.Send(ctx, fed, oldOwner, divestNotificationEvent(obj, attrCopy))
	_ = m.sendTerminalCallback(ctx, fed, newOwner, terminal)
}

func (m *Manager) notifyAcquisition(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle, oldOwner, newOwner core.FederateHandle, _ []byte, terminal *CallbackEvent) {
	attrCopy := cloneAttrs(attrs)
	m.recordTransfer(ctx, fed, obj, attrCopy, oldOwner, newOwner)
	_ = m.sendTerminalCallback(ctx, fed, newOwner, terminal)
}

func (m *Manager) recordTransfer(ctx context.Context, fed core.FederationName, obj core.ObjectHandle, attrs []core.AttributeHandle, oldOwner, newOwner core.FederateHandle) {
	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{
			kind:  evtTransferred,
			obj:   obj,
			attrs: attrs,
			from:  oldOwner,
			to:    newOwner,
		})
	}
}

// CancelDivest implements §7.14 — cancelNegotiatedAttributeOwnershipDivestiture.
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
	return m.cancelDivest(ctx, fed, owner, obj, attrs, canonicalPreparation(m.prepareDivestiture))
}

func (m *Manager) cancelDivest(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, prepare ownershipPreparation) error {
	attrs, release, err := prepare(ctx, fed, owner, obj, attrs)
	if err != nil {
		return err
	}
	defer release()
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrAttributeNotOwned
	}
	// Pre-flight: caller owns AND has a pending divest for every attr.
	for _, a := range attrs {
		if rec, owned := st.owners[ownershipKey{obj: obj, attr: a}]; !owned || rec.owner != owner {
			m.mu.Unlock()
			return core.ErrAttributeNotOwned
		}
	}
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
	release()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtCancelDivest, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}
	// 7.14 has no confirmation callback.
	return nil
}

// CancelAcquire implements §7.15 — cancelAttributeOwnershipAcquisition.
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
	return m.cancelAcquire(ctx, fed, acquirer, obj, attrs, canonicalPreparation(m.prepareDivestiture))
}

func (m *Manager) cancelAcquire(ctx context.Context, fed core.FederationName, acquirer core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, prepare ownershipPreparation) error {
	// Cancellation requires object knowledge and defined attributes, not a
	// new acquisition's publication preconditions.
	attrs, release, err := prepare(ctx, fed, acquirer, obj, attrs)
	if err != nil {
		return err
	}
	defer release()
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return core.ErrOwnershipNotInTransfer
	}
	for _, a := range attrs {
		if rec, owned := st.owners[ownershipKey{obj: obj, attr: a}]; owned && rec.owner == acquirer {
			m.mu.Unlock()
			return core.ErrFederateOwnsAttributes
		}
	}
	for _, a := range attrs {
		ak := acquireKey{obj: obj, attr: a, acquirer: acquirer}
		if _, pending := st.pendingAcquires[ak]; !pending {
			m.mu.Unlock()
			return core.ErrOwnershipNotInTransfer
		}
	}
	var terminal *CallbackEvent
	if session := st.callbackSessions[acquirer]; session != nil && session.enabled && len(attrs) > 0 {
		terminal = cancellationEvent(obj, attrs)
		if err := m.registerTerminalCallbacksLocked(st, fed, []core.OutboxDelivery{{Recipient: acquirer, Event: terminal}}); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	for _, a := range attrs {
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: a, acquirer: acquirer})
	}
	m.mu.Unlock()
	release()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtCancelAcquire, obj: obj, attrs: cloneAttrs(attrs), to: acquirer})
	}
	if terminal != nil {
		_ = m.sendTerminalCallback(ctx, fed, acquirer, terminal)
	}
	return nil
}

// DivestIfWanted implements §7.13 — attributeOwnershipDivestitureIfWanted.
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
	_, err := m.DivestIfWantedWithResult(ctx, fed, owner, obj, attrs)
	return err
}

// DivestIfWantedWithResult returns the attributes transferred by this call.
// Selection, validation and mutation share one critical section; observers and
// delivery cannot cancel or overwrite a selected request before commit.
func (m *Manager) DivestIfWantedWithResult(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle) ([]core.AttributeHandle, error) {
	return m.divestIfWanted(ctx, fed, owner, obj, attrs, canonicalPreparation(m.prepareDivestiture))
}

func (m *Manager) divestIfWanted(ctx context.Context, fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, attrs []core.AttributeHandle, prepare ownershipPreparation) ([]core.AttributeHandle, error) {
	attrs, release, err := prepare(ctx, fed, owner, obj, attrs)
	if err != nil {
		return nil, err
	}
	defer release()
	attrs = cloneAttrs(attrs)
	slices.Sort(attrs)
	attrs = slices.Compact(attrs)
	m.mu.Lock()
	st, ok := m.fed[fed]
	if !ok {
		m.mu.Unlock()
		return nil, core.ErrAttributeNotOwned
	}
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		rec, ok := st.owners[k]
		if !ok || rec.owner != owner {
			m.mu.Unlock()
			return nil, core.ErrAttributeNotOwned
		}
	}
	type wanted struct {
		attr     core.AttributeHandle
		acquirer core.FederateHandle
		tag      []byte
		terminal *CallbackEvent
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
		if pa, queued := st.pendingAcquires[acquireKey{obj: obj, attr: a, acquirer: winner}]; winner != core.InvalidFederateHandle && queued {
			transfers = append(transfers, wanted{attr: a, acquirer: winner, tag: cloneTag(pa.tag), terminal: acquireNotificationEvent(obj, []core.AttributeHandle{a}, winner, pa.tag)})
		}
	}
	var terminalDeliveries []core.OutboxDelivery
	for _, tr := range transfers {
		terminalDeliveries = append(terminalDeliveries, core.OutboxDelivery{Recipient: tr.acquirer, Event: tr.terminal})
	}
	if err := m.registerTerminalCallbacksLocked(st, fed, terminalDeliveries); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	result := make([]core.AttributeHandle, 0, len(transfers))
	for _, tr := range transfers {
		k := ownershipKey{obj: obj, attr: tr.attr}
		st.owners[k] = ownershipRecord{owner: tr.acquirer}
		delete(st.pendingDivests, k)
		delete(st.pendingAcquires, acquireKey{obj: obj, attr: tr.attr, acquirer: tr.acquirer})
		result = append(result, tr.attr)
	}
	m.mu.Unlock()
	release()

	if m.opts.EventLog != nil {
		_ = m.opts.EventLog.Append(ctx, fed, &eventRecord{kind: evtDivestIfWanted, obj: obj, attrs: cloneAttrs(attrs), from: owner})
	}

	for _, t := range transfers {
		m.notifyAcquisition(ctx, fed, obj, []core.AttributeHandle{t.attr}, owner, t.acquirer, t.tag, t.terminal)
	}
	if len(transfers) > 0 {
		m.notifyOwnershipChanged(ctx, fed, obj)
	}
	return result, nil
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
func (st *federationState) validateOwnedAttributes(obj core.ObjectHandle, owner core.FederateHandle, attrs []core.AttributeHandle) error {
	for _, attr := range attrs {
		if rec, exists := st.owners[ownershipKey{obj: obj, attr: attr}]; !exists || rec.owner != owner {
			return core.ErrAttributeNotOwned
		}
	}
	return nil
}

func (st *federationState) remainingReleaseRequests(obj core.ObjectHandle, attr core.AttributeHandle, winner core.FederateHandle, candidates []core.FederateHandle) []core.OutboxDelivery {
	var deliveries []core.OutboxDelivery
	for _, candidate := range candidates {
		if candidate != winner {
			request := st.pendingAcquires[acquireKey{obj: obj, attr: attr, acquirer: candidate}]
			deliveries = append(deliveries, core.OutboxDelivery{Recipient: winner, Event: releaseRequestEvent(obj, []core.AttributeHandle{attr}, request.tag)})
		}
	}
	return deliveries
}

func (st *federationState) confirmationCandidates(obj core.ObjectHandle, owner core.FederateHandle, attrs []core.AttributeHandle) (map[core.AttributeHandle][]core.FederateHandle, error) {
	for _, a := range attrs {
		k := ownershipKey{obj: obj, attr: a}
		pd, pending := st.pendingDivests[k]
		if !pending || pd.owner != owner || !pd.twoPhase || !pd.confirmationRequested {
			return nil, core.ErrOwnershipConfirmationNotRequested
		}
	}
	candidates := make(map[core.AttributeHandle][]core.FederateHandle, len(attrs))
	var waiting []ownershipKey
	for _, a := range attrs {
		candidates[a] = st.queuedAcquirersLocked(obj, a)
		if len(candidates[a]) == 0 {
			waiting = append(waiting, ownershipKey{obj: obj, attr: a})
		}
	}
	if len(waiting) > 0 {
		// 7.6 requires this state transition even though no ownership transfers.
		for _, k := range waiting {
			pd := st.pendingDivests[k]
			pd.confirmationRequested = false
			st.pendingDivests[k] = pd
		}
		return nil, core.ErrOwnershipNoAcquisitionPending
	}
	return candidates, nil
}
