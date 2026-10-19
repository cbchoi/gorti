package ownership

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

type terminalTestOutbox struct {
	fakeOutbox
	fail       error
	beforeSend func(core.OutboundEvent)
}

type projectedTerminalOutbox struct {
	fakeOutbox
	reserveFailure error
	commitFailure  error
	beforeReserve  func()
	beforeCommit   func([]core.OutboxDelivery)
}

type projectedTerminalReservation struct {
	once   sync.Once
	commit func() error
	err    error
}

func (r *projectedTerminalReservation) Commit() error {
	r.once.Do(func() { r.err = r.commit() })
	return r.err
}
func (r *projectedTerminalReservation) Release() { r.once.Do(func() {}) }

func (o *projectedTerminalOutbox) Reserve(ctx context.Context, fed core.FederationName, deliveries []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.beforeReserve != nil {
		o.beforeReserve()
	}
	if o.reserveFailure != nil {
		return nil, o.reserveFailure
	}
	projected := make([]core.OutboxDelivery, len(deliveries))
	for i, d := range deliveries {
		e := cloneCallback(d.Event.(*CallbackEvent))
		e.Attributes = []core.AttributeHandle{77}
		projected[i] = core.OutboxDelivery{Recipient: d.Recipient, Event: e}
	}
	return &projectedTerminalReservation{commit: func() error {
		if o.beforeCommit != nil {
			o.beforeCommit(projected)
		}
		if o.commitFailure != nil {
			return o.commitFailure
		}
		for _, d := range projected {
			if err := o.Send(ctx, fed, d.Recipient, d.Event); err != nil {
				return err
			}
		}
		return nil
	}}, nil
}

func TestTerminalCallbackReleaseDeniedAtomicReservationAndProjectedIdentity(t *testing.T) {
	out := &projectedTerminalOutbox{}
	m, err := New(Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[core.FederateHandle]core.OwnershipCallbackScope{}
	for _, h := range []core.FederateHandle{2, 3} {
		s := core.OwnershipCallbackScope{Federation: "fed", Recipient: h, FederationGeneration: 8}
		scopes[h] = s
		if err := m.EnableOwnershipCallbackReceipts(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.Acquire(context.Background(), "fed", h, 100, []core.AttributeHandle{1}, nil); err != nil {
			t.Fatal(err)
		}
	}
	out.beforeReserve = func() {
		if !m.mu.TryLock() {
			t.Fatal("ownership lock held across fanout reservation")
		}
		m.mu.Unlock()
	}
	before := m.ExportState("fed")
	out.reserveFailure = core.ErrFederateOverflow
	if err := m.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{1}); !errors.Is(err, core.ErrFederateOverflow) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("failed fanout reserve mutated pending requests/IDs")
	}
	out.reserveFailure = nil
	out.commitFailure = errors.New("failed reserved commit")
	out.beforeCommit = func(ds []core.OutboxDelivery) {
		if len(ds) != 2 {
			t.Fatal("fanout split", ds)
		}
		for _, d := range ds {
			e := d.Event.(*CallbackEvent)
			scope, id, strict := e.OwnershipCallbackIdentity()
			if scope != scopes[d.Recipient] || id != 1 || !strict || e.CallbackID != 0 || !slices.Equal(e.Attributes, []core.AttributeHandle{77}) {
				t.Fatal("reserved projected copy lost commit-time identity", scope, id, strict, e)
			}
			if !m.HasAcquisitionObligation("fed", d.Recipient, 100, []core.AttributeHandle{1}) {
				t.Fatal("commit removed request without replacing callback lifetime")
			}
		}
	}
	if err := m.ReleaseDenied(context.Background(), "fed", 1, 100, []core.AttributeHandle{1}); err == nil {
		t.Fatal("failed commit not reported")
	}
	state := m.ExportState("fed")
	if len(state.PendingAcquires) != 0 || len(state.PendingCallbacks) != 2 || !m.IsOwnedBy("fed", 1, 100, 1) {
		t.Fatal("committed denial failed to retain both callbacks", state)
	}
	out.commitFailure = nil
	for h, s := range scopes {
		if err := m.RetryOwnershipCallbacks(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		if !m.HasAcquisitionObligation("fed", h, 100, nil) {
			t.Fatal("strict retry admission cleared obligation")
		}
	}
}

func TestTerminalCallbackLateCapabilityAndRebindFence(t *testing.T) {
	m, out := newMgr(t, Options{})
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	ctx := context.Background()
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 3}
	if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableOwnershipCallbackReceipts(ctx, scope); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatal("late strict enable changed existing contract", err)
	}
	if err := m.CancelAcquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if len(out.Sent()) != 1 {
		t.Fatal("legacy cancellation gained unsolicited callback")
	}
	if err := m.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
		t.Fatal(err)
	}
	id := unavailableTerminal(t, m, scope, 1)
	old := out.Sent()[len(out.Sent())-1].Event.(*CallbackEvent)
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(ctx, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	fresh := scope
	fresh.CallbackEpoch++
	if err := m.RebindOwnershipCallbackScope(fresh); err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReportOwnershipCallbackInvocation(ctx, scope, receipt); !errors.Is(err, core.ErrOwnershipCallbackScope) {
		t.Fatal("old transport ACK crossed rebind", err)
	}
	oldScope, oldID, _ := old.OwnershipCallbackIdentity()
	if oldScope != scope || oldID != id {
		t.Fatal("old queued identity changed in place")
	}
	if !m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("rebind/old finish cleared obligation")
	}
	if err := m.RetryOwnershipCallbacks(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	next := out.Sent()[len(out.Sent())-1].Event.(*CallbackEvent)
	newScope, newID, _ := next.OwnershipCallbackIdentity()
	if newScope != fresh || newID != id {
		t.Fatal("retry did not preserve ID/change scope")
	}
}

func TestTerminalCallbackResignCannotSilentlyForgetUninvokedOutcome(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	unavailableTerminal(t, m, scope, 1)
	before := m.ExportState("fed")
	if plan, err := m.PrepareResign("fed", 2, core.ResignActionNoAction, nil); !errors.Is(err, core.ErrOwnershipAcquisitionPending) || plan != nil {
		t.Fatal("resign silently forgot terminal notification", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("rejected resignation changed callback ledger")
	}
	plan, err := m.PrepareResign("fed", 2, core.ResignActionCancelPendingOwnership, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	if m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("explicit cancellation on departure retained departed recipient obligation")
	}
}

func strictTerminalManager(t *testing.T, opts Options) (*Manager, *terminalTestOutbox, core.OwnershipCallbackScope) {
	t.Helper()
	out := &terminalTestOutbox{}
	opts.Outbox = out
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 9, CallbackEpoch: 0}
	if err := m.EnableOwnershipCallbackReceipts(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	return m, out, scope
}

func unavailableTerminal(t *testing.T, m *Manager, scope core.OwnershipCallbackScope, attrs ...core.AttributeHandle) uint64 {
	t.Helper()
	if err := m.AcquireIfAvailable(context.Background(), scope.Federation, scope.Recipient, 100, attrs, nil); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("fed")
	if len(state.PendingCallbacks) == 0 {
		t.Fatal("terminal result not retained")
	}
	return state.PendingCallbacks[len(state.PendingCallbacks)-1].CallbackID
}

func TestTerminalCallbackStrictAdmissionIsNotInvocation(t *testing.T) {
	m, out, scope := strictTerminalManager(t, Options{})
	id := unavailableTerminal(t, m, scope, 1)
	if len(out.Sent()) != 1 || !m.HasAcquisitionObligation("fed", 2, 100, []core.AttributeHandle{1}) {
		t.Fatal("queue acceptance cleared invocation obligation")
	}
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if !m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("transport send cleared invocation obligation")
	}
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal(err)
	}
	if m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("authenticated invocation did not clear obligation")
	}
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal("duplicate receipt not idempotent", err)
	}
	if _, _, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, id); !errors.Is(err, core.ErrOwnershipCallbackComplete) {
		t.Fatal("queued acknowledged duplicate must be classified complete", err)
	}
}

func TestTerminalCallbackEarlyInvocationAndFailedSendRetry(t *testing.T) {
	m, out, scope := strictTerminalManager(t, Options{})
	id := unavailableTerminal(t, m, scope, 1)
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal(err)
	}
	if !m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("early report cleared unconfirmed send")
	}
	finish(false)
	finish(true)
	if !m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("failed send/idempotent finish lost result")
	}
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); !errors.Is(err, core.ErrOwnershipCallbackReceipt) {
		t.Fatal("failed attempt accepted", err)
	}
	if err := m.RetryOwnershipCallbacks(context.Background(), scope); err != nil {
		t.Fatal(err)
	}
	if len(out.Sent()) != 2 {
		t.Fatal("retry did not enqueue retained result")
	}
	next, done, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, id)
	if err != nil || next.DeliveryAttempt != receipt.DeliveryAttempt+1 {
		t.Fatal("retry identity/attempt", next, err)
	}
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, next); err != nil {
		t.Fatal(err)
	}
	done(true)
	if m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("successful retry invocation remains blocked")
	}
}

func TestTerminalCallbackCapacityAndLaterInvalidAttributeAtomic(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{CallbackWindow: 1})
	unavailableTerminal(t, m, scope, 1)
	before := m.ExportState("fed")
	if err := m.AcquireIfAvailable(context.Background(), "fed", 2, 100, []core.AttributeHandle{2}, nil); !errors.Is(err, core.ErrOwnershipCallbackCapacity) {
		t.Fatal("window was not enforced", err)
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("capacity failure partially changed ownership or IDs")
	}
	if err := m.Acquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{2, 0}, nil); err == nil {
		t.Fatal("invalid later handle accepted")
	}
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("invalid later handle partially changed state")
	}
}

func TestTerminalCallbackCancellationRetainsLocalDeleteObligation(t *testing.T) {
	m, out, scope := strictTerminalManager(t, Options{})
	if err := m.Acquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.CancelAcquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("fed")
	if len(state.PendingAcquires) != 0 || len(state.PendingCallbacks) != 1 || state.PendingCallbacks[0].Kind != CallbackAcquisitionCancellation {
		t.Fatal("cancellation did not transfer lifetime to callback", state)
	}
	if out.Sent()[len(out.Sent())-1].Event.(*CallbackEvent).Kind != CallbackAcquisitionCancellation {
		t.Fatal("cancellation callback missing")
	}
	release, err := m.PrepareLocalDelete("fed", 2, 100)
	if release != nil {
		release()
	}
	if !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatal("cancellation response prematurely permits local delete", err)
	}
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, state.PendingCallbacks[0].CallbackID)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal(err)
	}
	release, err = m.PrepareLocalDelete("fed", 2, 100)
	if err != nil {
		t.Fatal("received cancellation must permit local delete", err)
	}
	release()
}

func TestTerminalCallbackOutcomeTransferPathsRetainAfterCommit(t *testing.T) {
	for _, path := range []string{"unowned", "unconditional", "if-wanted", "negotiated", "confirmed"} {
		t.Run(path, func(t *testing.T) {
			m, _, scope := strictTerminalManager(t, Options{})
			ctx := context.Background()
			var err error
			if path == "unowned" {
				err = m.Acquire(ctx, "fed", 2, 101, []core.AttributeHandle{1}, nil)
			} else {
				if err = m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
					t.Fatal(err)
				}
				switch path {
				case "unconditional":
					err = m.UnconditionalDivest(ctx, "fed", 1, 100, []core.AttributeHandle{1})
				case "if-wanted":
					_, err = m.DivestIfWantedWithResult(ctx, "fed", 1, 100, []core.AttributeHandle{1})
				case "negotiated":
					err = m.NegotiatedDivest(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil)
				case "confirmed":
					err = m.NegotiatedDivestTwoPhase(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil)
					if err == nil {
						err = m.ConfirmDivestiture(ctx, "fed", 1, 100, []core.AttributeHandle{1})
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			state := m.ExportState("fed")
			if len(state.PendingCallbacks) != 1 || state.PendingCallbacks[0].Kind != CallbackAcquisitionNotification {
				t.Fatal("committed acquisition lost result", state)
			}
			obj := state.PendingCallbacks[0].Object
			if !m.IsOwnedBy("fed", 2, obj, 1) || !m.HasAcquisitionObligation("fed", 2, obj, nil) {
				t.Fatal("ownership commit and callback obligation must coexist")
			}
			receipt, finish, err := m.BeginOwnershipCallbackDelivery(ctx, scope, state.PendingCallbacks[0].CallbackID)
			if err != nil {
				t.Fatal(err)
			}
			finish(true)
			if err := m.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
				t.Fatal(err)
			}
			if m.HasAcquisitionObligation("fed", 2, obj, nil) {
				t.Fatal("received acquisition obligation not completed")
			}
		})
	}
}

func TestTerminalCallbackPersistenceFreshScopeAndDetachedPayload(t *testing.T) {
	m, out, scope := strictTerminalManager(t, Options{})
	id1 := unavailableTerminal(t, m, scope, 1)
	id2 := unavailableTerminal(t, m, scope, 2)
	r, finish, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, id2)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, r); err != nil {
		t.Fatal(err)
	}
	state := m.ExportState("fed")
	if !slices.Equal(state.CallbackSessions[0].CompletedIDs, []uint64{id2}) {
		t.Fatal("out-of-order completion hole missing")
	}
	if err := ValidateOwnershipCallbackState(state); err != nil {
		t.Fatal(err)
	}
	restored, _, _ := strictTerminalManager(t, Options{})
	commit, err := restored.PrepareImportState("fed", state)
	if err != nil {
		t.Fatal(err)
	}
	state.PendingCallbacks[0].Attributes[0] = 99
	commit()
	commit()
	if _, _, err := restored.BeginOwnershipCallbackDelivery(context.Background(), scope, id1); !errors.Is(err, core.ErrOwnershipCallbackScope) {
		t.Fatal("restored transport reused old scope", err)
	}
	newScope := scope
	newScope.CallbackEpoch++
	if err := restored.RebindOwnershipCallbackScope(newScope); err != nil {
		t.Fatal(err)
	}
	if _, _, err := restored.BeginOwnershipCallbackDelivery(context.Background(), scope, id1); !errors.Is(err, core.ErrOwnershipCallbackScope) {
		t.Fatal("stale scope accepted", err)
	}
	receipt, done, err := restored.BeginOwnershipCallbackDelivery(context.Background(), newScope, id1)
	if err != nil || receipt.DeliveryAttempt != 1 {
		t.Fatal(receipt, err)
	}
	done(true)
	if got := restored.ExportState("fed").PendingCallbacks[0].Attributes[0]; got != 1 {
		t.Fatal("import payload aliased caller", got)
	}
	oldEvent := out.Sent()[0].Event.(*CallbackEvent)
	oldScope, _, _ := oldEvent.OwnershipCallbackIdentity()
	if oldScope != scope {
		t.Fatal("old queued identity rebound in place")
	}
	if err := restored.ReportOwnershipCallbackInvocation(context.Background(), newScope, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalCallbackSnapshotRejectsMalformedWithoutMutation(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	unavailableTerminal(t, m, scope, 1)
	for _, mutate := range []func(*ManagerState){
		func(s *ManagerState) { s.CallbackSessions = append(s.CallbackSessions, s.CallbackSessions[0]) },
		func(s *ManagerState) { s.PendingCallbacks = append(s.PendingCallbacks, s.PendingCallbacks[0]) },
		func(s *ManagerState) { s.PendingCallbacks[0].Attributes = []core.AttributeHandle{1, 1} },
		func(s *ManagerState) { s.CallbackSessions[0].LastCallbackID++ },
		func(s *ManagerState) { s.CallbackSessions[0].FederationGeneration = 0 },
		func(s *ManagerState) { s.PendingCallbacks[0].Kind = CallbackAssumption },
	} {
		before := m.ExportState("fed")
		invalid := m.ExportState("fed")
		mutate(invalid)
		if _, err := m.PrepareImportState("fed", invalid); err == nil {
			t.Fatal("invalid callback snapshot accepted", invalid)
		}
		if !reflect.DeepEqual(before, m.ExportState("fed")) {
			t.Fatal("invalid snapshot changed live state")
		}
	}
	if err := ValidateOwnershipCallbackState(&ManagerState{}); err != nil {
		t.Fatal("old DTO incompatible", err)
	}
}

func (o *terminalTestOutbox) Send(ctx context.Context, fed core.FederationName, recipient core.FederateHandle, event core.OutboundEvent) error {
	if o.beforeSend != nil {
		o.beforeSend(event)
	}
	if o.fail != nil {
		return o.fail
	}
	return o.fakeOutbox.Send(ctx, fed, recipient, event)
}

func TestTerminalCallbackUnavailableObligationBeforeFailedSend(t *testing.T) {
	out := &terminalTestOutbox{fail: core.ErrFederateOverflow}
	m, err := New(Options{Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	checked := false
	out.beforeSend = func(event core.OutboundEvent) {
		if event.(*CallbackEvent).Kind != CallbackUnavailable {
			return
		}
		checked = true
		release, err := m.PrepareLocalDelete("fed", 2, 100)
		if release != nil {
			release()
		}
		if !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
			t.Errorf("unavailable callback had no pre-send acquisition obligation: %v", err)
		}
	}
	if err := m.AcquireIfAvailable(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("unavailable outcome was not attempted")
	}
	release, err := m.PrepareLocalDelete("fed", 2, 100)
	if release != nil {
		release()
	}
	if !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		t.Fatalf("failed send silently released local-delete obligation: %v", err)
	}
}
