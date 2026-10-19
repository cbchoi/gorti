package grpc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type CallbackReceiptOptions struct {
	Authenticate              func(context.Context, core.FederationName, core.FederateHandle) error
	Observe                   func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error
	ObserveEntry              func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent) error
	ObserveRequired           func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error
	ValidateRequiredRecipient func(context.Context, core.FederationName, core.FederateHandle) error
	OnUnavailable             func(core.FederationName, core.FederateHandle, error)
	// Pending is a narrow accounting port; it must not reenter this receiver.
	Pending        func(core.FederationName, core.FederateHandle, int)
	AcquireService func(context.Context, core.FederationName) (func(), error)
	MaxPending     int
	MaxCompleted   int
	// ControlReserve leaves capacity within MaxPending for non-data callbacks.
	// Zero preserves unrestricted sharing for embedded compositions.
	ControlReserve int
	// LocalDataEntry delegates admission of delivered, ordinary RO data to the
	// session SDK. It is valid only with mandatory data receipts and an entry
	// observer that cannot reject nonregional, untimed data for domain reasons.
	LocalDataEntry bool
	// DataReports is an explicit composition capability for ordinary successful
	// RO data. Acquire authenticates and pins membership/checkpoint state. Apply
	// receives entry events (nil means completion) under the receipt lock; it
	// must not reenter receipts or perform I/O, and false must not mutate state.
	DataReports *DataCallbackReportBatch
}

type DataCallbackReportBatch struct {
	Acquire func(context.Context, core.FederationName, core.FederateHandle) (func(), error)
	Apply   func(core.FederationName, core.FederateHandle, []*rtiv1.FederateEvent) bool
}

type CallbackReceipts struct {
	publicationMu       sync.Mutex
	opts                CallbackReceiptOptions
	mu                  sync.Mutex
	entries             map[string]*callbackReceipt
	completed           []string
	pending             int
	reserved            int
	capacityChanged     chan struct{}
	nextCheckpointOrder uint64
}

type callbackReceipt struct {
	fed                       core.FederationName
	h                         core.FederateHandle
	event                     *rtiv1.FederateEvent
	sent, reported, applied   bool
	success                   bool
	exception                 string
	restoreEligible           bool
	entryApplied              bool
	entryWorking              chan struct{}
	outcomeWorking            chan struct{}
	required, admitted, retry bool
	checkpointOrder           uint64
	checkpointPublished       bool
	checkpointSupersededBy    uint64
	restorePrepared           bool
	recoveryIdentity          []byte
	recoveryOutcome           bool
	checkpointExcluded        bool
	ownershipBinding          *OwnershipCheckpointBinding
	source                    core.OutboundEvent
}

func NewCallbackReceipts(opts CallbackReceiptOptions) (*CallbackReceipts, error) {
	if opts.Authenticate == nil || opts.Observe == nil {
		return nil, fmt.Errorf("callback receipt authentication and observer required")
	}
	if opts.MaxPending <= 0 {
		opts.MaxPending = 8192
	}
	if opts.MaxCompleted <= 0 {
		opts.MaxCompleted = 8192
	}
	if opts.ControlReserve < 0 || opts.ControlReserve >= opts.MaxPending {
		return nil, fmt.Errorf("callback control reserve must be nonnegative and smaller than pending capacity")
	}
	return &CallbackReceipts{opts: opts, entries: map[string]*callbackReceipt{}}, nil
}

// Prepare never prevents callback delivery. Capacity/entropy failure instead
// invalidates metric coverage and omits the receipt. The event is copied so a
// later callback cannot be reported using caller-mutated class or arguments.
func (r *CallbackReceipts) Prepare(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) ([]byte, func(bool), error) {
	return r.prepare(ctx, fed, h, event, false)
}

// PrepareRequired admits lifecycle evidence before a mandatory callback enters
// the outbound queue. Unlike optional metric coverage, failure rejects admission.
func (r *CallbackReceipts) PrepareRequired(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent) ([]byte, func(bool), error) {
	return r.prepare(ctx, fed, h, event, true)
}

func (r *CallbackReceipts) prepare(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, required bool) ([]byte, func(bool), error) {
	return r.prepareWithCredits(ctx, fed, h, event, required, nil)
}

func (r *CallbackReceipts) prepareWithCredits(ctx context.Context, fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, required bool, credits *callbackCredits) ([]byte, func(bool), error) {
	noop := func(bool) {}
	fail := func(err error) ([]byte, func(bool), error) {
		r.unavailable(fed, h, err)
		if required {
			return nil, noop, err
		}
		return nil, noop, nil
	}
	if event == nil || callbackServiceName(event) == "" {
		if required {
			return fail(fmt.Errorf("required callback is not reportable"))
		}
		return nil, noop, nil
	}
	// Required admission is performed by an already-admitted server-side
	// outbox. It must not impersonate the recipient using the producer's token
	// or recursively acquire lifecycle/checkpoint readers.
	if !required && r.opts.AcquireService != nil {
		release, err := r.acquireCallbackService(ctx, fed)
		if err != nil {
			return fail(err)
		}
		defer release()
	}
	authenticate := r.opts.Authenticate
	if required && r.opts.ValidateRequiredRecipient != nil {
		authenticate = r.opts.ValidateRequiredRecipient
	}
	if err := authenticate(ctx, fed, h); err != nil {
		return fail(err)
	}
	ticket := make([]byte, 32)
	if _, err := rand.Read(ticket); err != nil {
		return fail(err)
	}
	entry, err := newCallbackReceipt(fed, h, event, ticket, required)
	if err != nil {
		return fail(err)
	}
	key := string(ticket)
	if err := r.admitCallbackReceipt(key, entry, event, credits); err != nil {
		return fail(err)
	}
	var once sync.Once
	return ticket, func(sent bool) { once.Do(func() { r.commit(key, entry, sent) }) }, nil
}

func (r *CallbackReceipts) commit(key string, entry *callbackReceipt, sent bool) {
	r.mu.Lock()
	if entry.required && !entry.admitted && !sent {
		removed := r.entries[key] == entry
		if removed {
			delete(r.entries, key)
			r.retirePendingLocked()
		}
		r.mu.Unlock()
		if removed {
			r.pendingDelta(entry, -1)
		}
		return
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r.opts.AcquireService != nil {
		release, err := r.acquireCallbackService(ctx, entry.fed)
		if err != nil {
			r.invalidateDelivery(key, entry, err)
			return
		}
		defer release()
	}
	if !sent {
		r.commitFailedSend(key, entry)
		return
	}
	r.mu.Lock()
	if r.entries[key] != entry {
		r.mu.Unlock()
		return
	}
	entry.sent = true
	r.publishCheckpointLocked(entry)
	apply := !entry.required && entry.reported && !entry.applied
	if apply {
		r.completeLocked(key, entry)
	}
	r.mu.Unlock()
	if apply {
		defer r.pendingDelta(entry, -1)
		if err := r.observe(ctx, entry); err != nil {
			r.unavailable(entry.fed, entry.h, err)
		}
	}
}

func (r *CallbackReceipts) invalidateDelivery(key string, entry *callbackReceipt, cause error) {
	r.mu.Lock()
	removed := r.entries[key] == entry && !entry.applied
	if removed {
		delete(r.entries, key)
		r.retirePendingLocked()
	}
	r.mu.Unlock()
	if removed {
		r.unavailable(entry.fed, entry.h, cause)
		r.pendingDelta(entry, -1)
	}
}

// Report runs inside normal RPC checkpoint/member admission. Authentication is
// required even for replayed receipts and is checked before inspecting a ticket.
func (r *CallbackReceipts) Report(ctx context.Context, fed core.FederationName, h core.FederateHandle, ticket []byte, success bool, exception string) error {
	if err := r.opts.Authenticate(ctx, fed, h); err != nil {
		return err
	}
	if len(ticket) != 32 || (success && exception != "") || (!success && exception == "") {
		return status.Error(codes.InvalidArgument, "invalid callback outcome or receipt")
	}
	r.mu.Lock()
	key := string(ticket)
	entry := r.entries[key]
	if entry == nil || entry.fed != fed || entry.h != h {
		r.mu.Unlock()
		return status.Error(codes.FailedPrecondition, "callback receipt expired or unavailable")
	}
	if entry.required && !entry.entryApplied {
		r.mu.Unlock()
		return status.Error(codes.FailedPrecondition, "required callback entry not reported")
	}
	if entry.required {
		r.mu.Unlock()
		return r.reportRequiredOutcome(ctx, key, entry, success, exception)
	}
	return r.reportOptionalOutcomeLocked(ctx, key, entry, success, exception)
}

func (r *CallbackReceipts) completeLocked(key string, entry *callbackReceipt) {
	entry.applied = true
	r.retirePendingLocked()
	r.completed = append(r.completed, key)
	r.trimCompletedLocked()
}

func (r *CallbackReceipts) observe(ctx context.Context, entry *callbackReceipt) error {
	err := r.opts.Observe(ctx, entry.fed, entry.h, entry.event, entry.success, entry.exception)
	// Replay validation needs the ticket/outcome only, not retained payloads.
	r.mu.Lock()
	entry.event = nil
	r.mu.Unlock()
	return err
}
func (r *CallbackReceipts) unavailable(fed core.FederationName, h core.FederateHandle, err error) {
	if r.opts.OnUnavailable != nil {
		r.opts.OnUnavailable(fed, h, err)
	}
}
func (r *CallbackReceipts) pendingDelta(entry *callbackReceipt, delta int) {
	if r.opts.Pending != nil {
		r.opts.Pending(entry.fed, entry.h, delta)
	}
}

// PreserveRestoreTransition wraps the already validated identity/outbox
// reservation. Its caller holds exclusive checkpoint admission through Commit
// or Release. Receipt changes must follow successful snapshot/identity commit:
// preparation and rollback leave the current lifecycle untouched.
func (r *CallbackReceipts) PreserveRestoreTransition(fed core.FederationName, label string, participants []core.RestoreParticipant, transition core.OutboxReservation) core.OutboxReservation {
	targets := make(map[core.FederateHandle]core.FederateHandle, len(participants))
	for _, participant := range participants {
		targets[participant.CurrentHandle] = participant.SavedHandle
	}
	return &callbackRestoreReservation{transition: transition, committed: func() { r.restoreCommitted(fed, label, targets) }}
}

type callbackRestoreReservation struct {
	transition core.OutboxReservation
	committed  func()
	mu         sync.Mutex
	done       bool
}

func (r *callbackRestoreReservation) Commit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return nil
	}
	if err := r.transition.Commit(); err != nil {
		return err
	}
	r.committed()
	r.done = true
	return nil
}

func (r *callbackRestoreReservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.done {
		r.transition.Release()
		r.done = true
	}
}

func (r *CallbackReceipts) restoreCommitted(fed core.FederationName, label string, targets map[core.FederateHandle]core.FederateHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, entry := range r.entries {
		if entry.fed != fed {
			continue
		}
		if entry.restorePrepared {
			entry.restorePrepared = false
			entry.admitted = true
			if entry.applied {
				r.completed = append(r.completed, key)
			} else {
				r.pendingDelta(entry, 1)
			}
			continue
		}
		target := targets[entry.h]
		initiated := entry.event.GetRestoreInitiate()
		if !entry.applied && entry.restoreEligible && target != 0 && initiated != nil &&
			initiated.Label == label && initiated.FederateHandle == uint64(target) {
			entry.h = target
			entry.restoreEligible = false
			// The restored MOM snapshot replaced the old pending baseline. Only
			// this still-unreported invocation belongs to the new live baseline.
			r.pendingDelta(entry, 1)
			continue
		}
		if !entry.applied {
			r.retirePendingLocked()
		}
		delete(r.entries, key)
	}
	r.pruneCompletedLocked()
	r.trimCompletedLocked()
}

func (r *CallbackReceipts) ClearFederate(fed core.FederationName, h core.FederateHandle) {
	r.clear(fed, h, false)
}
func (r *CallbackReceipts) ClearFederation(fed core.FederationName) { r.clear(fed, 0, true) }
func (r *CallbackReceipts) clear(fed core.FederationName, h core.FederateHandle, all bool) {
	r.mu.Lock()
	var removed []*callbackReceipt
	for key, entry := range r.entries {
		if entry.fed == fed && (all || entry.h == h) {
			if !entry.applied {
				r.retirePendingLocked()
				removed = append(removed, entry)
			}
			delete(r.entries, key)
		}
	}
	r.pruneCompletedLocked()
	r.mu.Unlock()
	for _, entry := range removed {
		r.unavailable(entry.fed, entry.h, fmt.Errorf("MOM: outstanding callback receipt invalidated"))
		r.pendingDelta(entry, -1)
	}
}

func (r *CallbackReceipts) pruneCompletedLocked() {
	kept := r.completed[:0]
	for _, key := range r.completed {
		if r.entries[key] != nil {
			kept = append(kept, key)
		}
	}
	clear(r.completed[len(kept):])
	r.completed = kept
}

func (r *CallbackReceipts) trimCompletedLocked() {
	for len(r.completed) > r.opts.MaxCompleted {
		victim := -1
		for i, key := range r.completed {
			entry := r.entries[key]
			if entry != nil && entry.event.GetCallbackRecovery().GetCompleted() && (!entry.sent || entry.retry) {
				continue
			}
			victim = i
			break
		}
		if victim < 0 {
			// Replay admission reserves marker capacity before publication.
			return
		}
		delete(r.entries, r.completed[victim])
		last := len(r.completed) - 1
		if victim < last-victim {
			// Shift only the shorter prefix, then advance the slice. Ordinary
			// FIFO eviction copies nothing; a few pinned markers stay cheap.
			copy(r.completed[1:victim+1], r.completed[:victim])
			r.completed[0] = ""
			r.completed = r.completed[1:]
		} else {
			copy(r.completed[victim:], r.completed[victim+1:])
			r.completed[last] = ""
			r.completed = r.completed[:last]
		}
	}
}

func callbackServiceName(event *rtiv1.FederateEvent) string {
	if event == nil {
		return ""
	}
	if change := event.GetTransportationChangeConfirmed(); change != nil {
		if change.GetObjectHandle() != 0 && change.GetInteractionClassHandle() == 0 {
			return "confirmAttributeTransportationTypeChange"
		}
		if change.GetInteractionClassHandle() != 0 && change.GetObjectHandle() == 0 {
			return "confirmInteractionTransportationTypeChange"
		}
		return ""
	}
	m := event.ProtoReflect()
	oneof := m.Descriptor().Oneofs().ByName("event")
	field := m.WhichOneof(oneof)
	if field == nil {
		return ""
	}
	return callbackServices[string(field.Name())]
}

var callbackServices = map[string]string{
	"ownership_acquisition_cancelled": "confirmAttributeOwnershipAcquisitionCancellation",
	"time_regulation_enabled":         "timeRegulationEnabled", "time_constrained_enabled": "timeConstrainedEnabled",
	"discover": "discoverObjectInstance", "reflect": "reflectAttributeValues", "remove": "removeObjectInstance", callbackReceive: "receiveInteraction", "grant": "timeAdvanceGrant", "provide_update": "provideAttributeValueUpdate",
	"sync_announced": "announceSynchronizationPoint", "sync_synchronized": "federationSynchronized", "sync_registration_succeeded": "synchronizationPointRegistrationSucceeded", "sync_registration_failed": "synchronizationPointRegistrationFailed",
	"ownership_assumption": "requestAttributeOwnershipAssumption", "ownership_acquired": "attributeOwnershipAcquisitionNotification", "ownership_divest_confirmed": "requestDivestitureConfirmation", "ownership_release_requested": "requestAttributeOwnershipRelease", "ownership_unavailable": "attributeOwnershipUnavailable",
	"save_initiate": "initiateFederateSave", "save_completed": "federationSaved", "save_failed": "federationNotSaved", "restore_initiate": "initiateFederateRestore", "restore_completed": "federationRestored", "restore_failed": "federationNotRestored", "restore_request_succeeded": "requestFederationRestoreSucceeded", "restore_request_failed": "requestFederationRestoreFailed", "restore_begun": "federationRestoreBegun",
	"reservation_succeeded": "objectInstanceNameReservationSucceeded", "reservation_failed": "objectInstanceNameReservationFailed", "reservation_multi_succeeded": "multipleObjectInstanceNameReservationSucceeded", "reservation_multi_failed": "multipleObjectInstanceNameReservationFailed",
	"start_registration": "startRegistrationForObjectClass", "stop_registration": "stopRegistrationForObjectClass", "turn_interactions_on": "turnInteractionsOn", "turn_interactions_off": "turnInteractionsOff", "attributes_in_scope": "attributesInScope", "attributes_out_of_scope": "attributesOutOfScope", "retraction_requested": "requestRetraction", "turn_updates_on": "turnUpdatesOnForObjectInstance", "turn_updates_off": "turnUpdatesOffForObjectInstance",
}

func newCallbackReceipt(fed core.FederationName, h core.FederateHandle, event *rtiv1.FederateEvent, ticket []byte, required bool) (*callbackReceipt, error) {
	entry := &callbackReceipt{fed: fed, h: h, event: proto.Clone(event).(*rtiv1.FederateEvent), required: required}
	entry.event.CallbackReceipt = nil
	identity := sha256.Sum256(ticket)
	entry.recoveryIdentity = identity[:]
	if recovery := event.GetCallbackRecovery(); recovery != nil {
		if len(recovery.InvocationIdentity) != 32 || recovery.Completed {
			return nil, fmt.Errorf("invalid callback recovery metadata")
		}
		entry.recoveryIdentity = append([]byte(nil), recovery.InvocationIdentity...)
	}
	return entry, nil
}

func (r *CallbackReceipts) admitCallbackReceipt(key string, entry *callbackReceipt, event *rtiv1.FederateEvent, credits *callbackCredits) error {
	fed, h := entry.fed, entry.h
	r.mu.Lock()
	defer r.mu.Unlock()
	// A later restore attempt must not revive an older initiation receipt, even
	// when the label and target handle are reused. Failed attempts stay reportable
	// in their original lifecycle, but cannot cross a subsequent successful restore.
	if event.GetRestoreBegun() != nil || event.GetRestoreInitiate() != nil || event.GetRestoreFailed() != nil {
		for _, previous := range r.entries {
			if previous.fed == fed && previous.h == h {
				previous.restoreEligible = false
			}
		}
	}
	entry.restoreEligible = event.GetRestoreInitiate() != nil
	limit := r.opts.MaxPending
	if event.GetReflect() != nil || event.GetReceive() != nil {
		limit -= r.opts.ControlReserve
	}
	if credits == nil && r.pending+r.reserved >= limit {
		return fmt.Errorf("MOM: callback receipt capacity exceeded")
	}
	if _, collision := r.entries[key]; collision {
		return fmt.Errorf("MOM: callback receipt collision")
	}
	if r.nextCheckpointOrder == ^uint64(0) {
		return fmt.Errorf("callback receipt admission order exhausted")
	}
	r.entries[key] = entry
	if credits != nil {
		credits.remaining--
		r.reserved--
	}
	r.nextCheckpointOrder++
	entry.checkpointOrder = r.nextCheckpointOrder
	r.pending++
	r.pendingDelta(entry, 1)
	return nil
}

func (r *CallbackReceipts) commitFailedSend(key string, entry *callbackReceipt) {
	r.mu.Lock()
	if r.entries[key] == entry && entry.required && entry.admitted {
		// A late transport failure cannot revoke authenticated invocation
		// evidence or retire an already completed callback a second time.
		entry.retry = !entry.entryApplied && !entry.applied
		r.mu.Unlock()
		return
	}
	removed, reported := false, false
	if r.entries[key] == entry {
		delete(r.entries, key)
		r.retirePendingLocked()
		removed = true
		reported = entry.reported
	}
	r.mu.Unlock()
	if removed {
		if reported {
			r.unavailable(entry.fed, entry.h, fmt.Errorf("MOM: callback was reported before failed send commit"))
		}
		r.pendingDelta(entry, -1)
	}
	return
}

func (r *CallbackReceipts) reportOptionalOutcomeLocked(ctx context.Context, key string, entry *callbackReceipt, success bool, exception string) error {
	if entry.reported {
		matches := entry.success == success && entry.exception == exception
		r.mu.Unlock()
		if !matches {
			return status.Error(codes.InvalidArgument, "conflicting callback outcome")
		}
		return nil
	}
	entry.reported = true
	entry.success = success
	entry.exception = exception
	apply := entry.sent || entry.required && entry.entryApplied
	if apply {
		defer r.pendingDelta(entry, -1)
		r.completeLocked(key, entry)
	}
	r.mu.Unlock()
	if apply {
		if err := r.observe(ctx, entry); err != nil {
			r.unavailable(entry.fed, entry.h, err)
			return err
		}
	}
	return nil
}
func (r *CallbackReceipts) acquireCallbackService(ctx context.Context, fed core.FederationName) (func(), error) {
	release, err := r.opts.AcquireService(ctx, fed)
	if err == nil && release == nil {
		err = fmt.Errorf("MOM: callback admission returned no release")
	}
	return release, err
}
