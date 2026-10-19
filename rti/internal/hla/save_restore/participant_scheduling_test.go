package savepoint

import (
	"context"
	"errors"
	"reflect"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type participantOutbox struct {
	fakeOutbox
	fail bool
}

func (o *participantOutbox) Reserve(ctx context.Context, fed core.FederationName, ds []core.OutboxDelivery) (core.OutboxReservation, error) {
	if o.fail {
		return nil, errors.New("injected reservation failure")
	}
	return &participantReservation{out: o, ctx: ctx, fed: fed, deliveries: ds}, nil
}

type participantReservation struct {
	out        *participantOutbox
	ctx        context.Context
	fed        core.FederationName
	deliveries []core.OutboxDelivery
}

func (r *participantReservation) Commit() error {
	for _, d := range r.deliveries {
		if err := r.out.Send(r.ctx, r.fed, d.Recipient, d.Event); err != nil {
			return err
		}
	}
	return nil
}
func (*participantReservation) Release() {}

func participantManagers[T core.LogicalTimeScalar](t *testing.T) (*ManagerOf[T], *timepkg.ManagerOf[T], *participantOutbox) {
	t.Helper()
	out := &participantOutbox{}
	tm, err := timepkg.NewOf[T](timepkg.OptionsOf[T]{Outbox: out, Clock: core.NewManualClock(stdtime.Unix(1, 0))})
	if err != nil {
		t.Fatal(err)
	}
	if err = tm.EnableRegulation(context.Background(), "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{2, 3} {
		if err = tm.EnableConstrained(context.Background(), "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	m, err := NewOf[T](OptionsOf[T]{Outbox: out, BundleStore: newMemStore(), Members: func(core.FederationName) []core.FederateHandle { return []core.FederateHandle{1, 2, 3, 4} }, ValidateSaveRequest: tm.ValidateScheduledSaveRequest, PrepareScheduledTime: tm.PrepareScheduledSave, PrepareImmediateTime: tm.PrepareImmediateSave, ResumeTime: tm.ResumeScheduledSave})
	if err != nil {
		t.Fatal(err)
	}
	return m, tm, out
}

func initiatedRecipients[T core.LogicalTimeScalar](out *participantOutbox, label string) []core.FederateHandle {
	var result []core.FederateHandle
	for _, record := range out.Sent() {
		if e, ok := record.Event.(*InitiateSaveEventOf[T]); ok && e.Label() == label {
			result = append(result, record.Federate)
		}
	}
	return result
}

func TestParticipantTimedSaveBothScalarsAndAdvanceBoundaries(t *testing.T) {
	for _, mode := range []string{"TAR", "TARA", "NMR", "NMRA", "FQR"} {
		t.Run("Float64/"+mode, func(t *testing.T) { participantSaveContract[core.LogicalTime](t, mode) })
		t.Run("Integer64/"+mode, func(t *testing.T) { participantSaveContract[int64](t, mode) })
	}
}
func participantSaveContract[T core.LogicalTimeScalar](t *testing.T, mode string) {
	m, tm, out := participantManagers[T](t)
	ctx := context.Background()
	target := T(10)
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "partial", &target); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 20); err != nil {
		t.Fatal(err)
	}
	advance := func(h core.FederateHandle, at T) error {
		switch mode {
		case "TARA":
			return tm.TimeAdvanceRequestAvailable(ctx, "fed", h, at)
		case "NMR":
			return tm.NextMessageRequest(ctx, "fed", h, at)
		case "NMRA":
			return tm.NextMessageRequestAvailable(ctx, "fed", h, at)
		case "FQR":
			return tm.FlushQueueRequest(ctx, "fed", h, at)
		default:
			return tm.TimeAdvanceRequest(ctx, "fed", h, at)
		}
	}
	if err := advance(2, 10); err != nil {
		t.Fatal(err)
	}
	if err := m.ProgressScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if mode == "TARA" || mode == "NMRA" || mode == "FQR" {
		if len(initiatedRecipients[T](out, "partial")) != 0 {
			t.Fatal("available equality initiated save")
		}
		if err := advance(2, 11); err != nil {
			t.Fatal(err)
		}
		if err := m.ProgressScheduledSave(ctx, "fed"); err != nil {
			t.Fatal(err)
		}
	}
	if got := initiatedRecipients[T](out, "partial"); !reflect.DeepEqual(got, []core.FederateHandle{2}) {
		t.Fatalf("first constrained waits for slower: %v", got)
	}
	if release, err := m.AcquireFederateService("fed", 2); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		if release != nil {
			release()
		}
		t.Fatalf("initiated participant admitted: %v", err)
	}
	if release, err := m.AcquireFederateService("fed", 3); err != nil {
		t.Fatal(err)
	} else {
		release()
	}
	if err := m.FederateSaveBegun(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.FederateSaveComplete(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "too-late", nil); !errors.Is(err, core.ErrSaveAlreadyInProgress) {
		t.Fatalf("active replacement: %v", err)
	}
	if err := advance(3, 11); err != nil {
		t.Fatal(err)
	}
	if err := m.ProgressScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if got := initiatedRecipients[T](out, "partial"); !reflect.DeepEqual(got, []core.FederateHandle{2, 1, 3, 4}) {
		t.Fatalf("individual/nonconstrained initiation: %v", got)
	}
	for _, h := range []core.FederateHandle{1, 3, 4} {
		if err := m.FederateSaveBegun(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
		if err := m.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	if m.QuerySaveState("fed", "partial") != StateSaved {
		t.Fatal("partial reports did not complete save")
	}
}

func TestWaitingTimedSaveReplacementIsAtomicBothScalars(t *testing.T) {
	t.Run("Float64", func(t *testing.T) { waitingReplacementContract[core.LogicalTime](t) })
	t.Run("Integer64", func(t *testing.T) { waitingReplacementContract[int64](t) })
}
func waitingReplacementContract[T core.LogicalTimeScalar](t *testing.T) {
	m, tm, out := participantManagers[T](t)
	ctx := context.Background()
	target := T(10)
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "old", &target); err != nil {
		t.Fatal(err)
	}
	invalid := T(-1)
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "bad", &invalid); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	if m.Snapshot("fed").SaveLabel != "old" {
		t.Fatal("invalid replacement erased old request")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.RequestFederationSaveFrom(cancelled, "fed", 1, "cancelled", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replacement: %v", err)
	}
	out.fail = true
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "failed", nil); err == nil {
		t.Fatal("failed reservation accepted")
	}
	out.fail = false
	if m.Snapshot("fed").SaveLabel != "old" {
		t.Fatal("failed replacement erased old request")
	}
	target = 12
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "new", &target); err != nil {
		t.Fatal(err)
	}
	if m.QuerySaveState("fed", "old") != StateIdle {
		t.Fatal("replaced waiting request remains active")
	}
	if err := m.RequestFederationSaveFrom(ctx, "fed", 1, "immediate", nil); err != nil {
		t.Fatal(err)
	}
	if len(initiatedRecipients[T](out, "immediate")) != 4 {
		t.Fatal("immediate replacement did not initiate all participants")
	}
	if tm.ShouldDeliverNow("fed", 2, 0) {
		t.Fatal("immediate replacement released recipient delivery pause")
	}
	reservation := tm.ReserveTSO("fed", []core.MessageDeliveryOf[T]{{Recipient: 2, Event: &eventRecord{seq: 77}, ReceiveOrder: true}})
	reservation.Commit(ctx)
	for _, record := range out.Sent() {
		if record.Event.Seq() == 77 {
			t.Fatal("message delivered during immediate replacement save")
		}
	}
	for _, h := range []core.FederateHandle{1, 2, 3, 4} {
		if err := m.FederateSaveBegun(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
		if err := m.FederateSaveComplete(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.ProgressScheduledSave(ctx, "fed"); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 30); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, 20); err != nil {
		t.Fatal(err)
	}
	saved, retained := false, false
	for _, record := range out.Sent() {
		if record.Federate != 2 {
			continue
		}
		if _, ok := record.Event.(*SavedEvent); ok {
			saved = true
		}
		if record.Event.Seq() == 77 {
			if !saved {
				t.Fatal("retained callback preceded federationSaved")
			}
			retained = true
		}
	}
	if !retained {
		t.Fatal("immediate replacement lost retained message")
	}
}
