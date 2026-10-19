package ownership

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestResignTransferRetainsPreprojectedTerminalIdentity(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "legacy"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			m, _ := newMgr(t, Options{})
			ctx := context.Background()
			scope := core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 3}
			if strict {
				if err := m.EnableOwnershipCallbackReceipts(ctx, scope); err != nil {
					t.Fatal(err)
				}
			}
			m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
			if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
				t.Fatal(err)
			}
			plan, err := m.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Release()
			projected := cloneCallback(plan.Deliveries()[0].Event.(*CallbackEvent))
			projected.Attributes = []core.AttributeHandle{77}
			plan.Commit()
			actualScope, id, receiptRequired := projected.OwnershipCallbackIdentity()
			if id == 0 || receiptRequired != strict || strict && actualScope != scope {
				t.Fatal("precommit projection lost terminal identity", actualScope, id, receiptRequired)
			}
			if !m.HasAcquisitionObligation("fed", 2, 100, nil) {
				t.Fatal("resign erased terminal before admission/invocation")
			}
		})
	}
}

func TestResignDeletedAcquisitionRequiresOutcomeBeforeRemoval(t *testing.T) {
	m, _ := newMgr(t, Options{})
	ctx := context.Background()
	for _, h := range []core.FederateHandle{2, 3} {
		if err := m.EnableOwnershipCallbackReceipts(ctx, core.OwnershipCallbackScope{Federation: "fed", Recipient: h, FederationGeneration: 4}); err != nil {
			t.Fatal(err)
		}
	}
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	for _, h := range []core.FederateHandle{3, 2} {
		if err := m.Acquire(ctx, "fed", h, 100, []core.AttributeHandle{2, 1}, []byte{byte(h)}); err != nil {
			t.Fatal(err)
		}
	}
	before := m.ExportState("fed")
	plan, err := m.PrepareResign("fed", 1, core.ResignActionDeleteObjects, []core.ObjectHandle{100})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Release()
	deliveries := plan.DeletionDeliveries()
	if len(deliveries) != 4 {
		t.Fatalf("deleted acquisition outcomes=%d", len(deliveries))
	}
	var recipients []core.FederateHandle
	for _, d := range deliveries {
		e := d.Event.(*CallbackEvent)
		if e.Kind != CallbackUnavailable || e.Object != 100 || len(e.Attributes) != 1 {
			t.Fatalf("deleted terminal=%+v", e)
		}
		recipients = append(recipients, d.Recipient)
	}
	if !slices.Equal(recipients, []core.FederateHandle{2, 2, 3, 3}) {
		t.Fatal("nondeterministic deletion fanout", recipients)
	}
	plan.Release()
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("aborted deletion changed acquisition state")
	}
	plan, err = m.PrepareResign("fed", 1, core.ResignActionDeleteObjects, []core.ObjectHandle{100})
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	plan.Finish(ctx, nil)
	state := m.ExportState("fed")
	if len(state.PendingAcquires) != 0 || len(state.PendingCallbacks) != 4 || len(state.Owners) != 0 {
		t.Fatal("deleted pending requests lost their terminal lifetime", state)
	}
}

func TestResignFinishCompletesLegacyAdmissionButNotStrictInvocation(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			t.Run(map[bool]string{false: "legacy", true: "strict"}[strict]+"/"+map[bool]string{false: "admitted", true: "failed"}[failed], func(t *testing.T) {
				m, _ := newMgr(t, Options{})
				ctx := context.Background()
				if strict {
					if err := m.EnableOwnershipCallbackReceipts(ctx, core.OwnershipCallbackScope{Federation: "fed", Recipient: 2, FederationGeneration: 1}); err != nil {
						t.Fatal(err)
					}
				}
				m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
				if err := m.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
					t.Fatal(err)
				}
				plan, err := m.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
				if err != nil {
					t.Fatal(err)
				}
				plan.Commit()
				var admissionError error
				if failed {
					admissionError = core.ErrOutboxUnavailable
				}
				plan.Finish(ctx, admissionError)
				plan.Finish(ctx, nil)
				if got := m.HasAcquisitionObligation("fed", 2, 100, nil); got != (strict || failed) {
					t.Fatal("incorrect terminal lifetime", got, strict, failed)
				}
			})
		}
	}
}

func TestResignFinishSolicitsOffersWithoutLocksOrDepartingMember(t *testing.T) {
	m, out := newMgr(t, Options{Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
		return []core.FederateHandle{1, 2, 2, 3}
	}})
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	refresh := 0
	m.SetOnOwnershipChanged(func(context.Context, core.FederationName, core.ObjectHandle) {
		if !m.mu.TryLock() {
			t.Fatal("ownership lock crossed post-commit refresh")
		}
		m.mu.Unlock()
		refresh++
	})
	plan, err := m.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	plan.Finish(context.Background(), nil)
	plan.Finish(context.Background(), nil)
	if refresh != 1 {
		t.Fatal("missing or duplicated control refresh", refresh)
	}
	if len(out.Sent()) != 4 {
		t.Fatal("offer count", out.Sent())
	}
	for _, d := range out.Sent() {
		if d.Federate == 1 || d.Event.(*CallbackEvent).Kind != CallbackAssumption {
			t.Fatal("departing member offered ownership", d)
		}
	}
	if len(m.ExportState("fed").UnownedOffers) != 2 {
		t.Fatal("surviving offers were not retained for subsequent publication")
	}
}

func TestResignCallbackWindowRejectsBeforeMutation(t *testing.T) {
	m, _ := newMgr(t, Options{CallbackWindow: 1})
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2})
	if err := m.Acquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{1, 2}, nil); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("fed")
	plan, err := m.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
	if plan != nil {
		plan.Release()
	}
	if !errors.Is(err, core.ErrOwnershipCallbackCapacity) || !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("callback capacity failure changed ownership", err)
	}
}

func TestGlobalDeletionPreparationPreservesMandatoryOutcomes(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	ctx := context.Background()
	id := unavailableTerminal(t, m, scope, 1)
	if err := m.Acquire(ctx, "fed", 3, 100, []core.AttributeHandle{2}, nil); err != nil {
		t.Fatal(err)
	}
	before := m.ExportState("fed")
	reservation, outcomes, finish, err := m.PrepareObjectDeletion(ctx, "fed", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0].Recipient != 3 || outcomes[0].Event.(*CallbackEvent).Kind != CallbackUnavailable {
		t.Fatal("deleted pending acquisition lacked unavailable result", outcomes)
	}
	reservation.Release()
	finish(ctx, nil)
	if !reflect.DeepEqual(before, m.ExportState("fed")) {
		t.Fatal("released global deletion changed ownership")
	}
	reservation, _, finish, err = m.PrepareObjectDeletion(ctx, "fed", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err != nil {
		t.Fatal(err)
	}
	finish(ctx, core.ErrOutboxUnavailable)
	state := m.ExportState("fed")
	if len(state.Owners) != 0 || len(state.PendingAcquires) != 0 || len(state.PendingCallbacks) != 2 {
		t.Fatal("global deletion forgot terminal callbacks", state)
	}
	receipt, complete, err := m.BeginOwnershipCallbackDelivery(ctx, scope, id)
	if err != nil {
		t.Fatal(err)
	}
	complete(true)
	if err := m.ReportOwnershipCallbackInvocation(ctx, scope, receipt); err != nil {
		t.Fatal(err)
	}
	if m.HasAcquisitionObligation("fed", 2, 100, nil) || !m.HasAcquisitionObligation("fed", 3, 100, nil) {
		t.Fatal("receipt cleared another recipient's result")
	}
}

func TestResignOfferWaitsForExistingTerminalInvocation(t *testing.T) {
	m, out, scope := strictTerminalManager(t, Options{Subscribers: func(context.Context, core.FederationName, core.ObjectHandle, []core.AttributeHandle) []core.FederateHandle {
		return []core.FederateHandle{2, 3}
	}})
	unavailableTerminal(t, m, scope, 1)
	before := len(out.Sent())
	plan, err := m.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	plan.Finish(context.Background(), nil)
	for _, d := range out.Sent()[before:] {
		e := d.Event.(*CallbackEvent)
		if d.Federate == 2 && e.Kind == CallbackAssumption && slices.Contains(e.Attributes, core.AttributeHandle(1)) {
			t.Fatal("assumption overtook uninvoked unavailable outcome")
		}
	}
}

func TestResignDeletionDoesNotDiscardOtherRecipientsTerminalOutcome(t *testing.T) {
	m, _, scope := strictTerminalManager(t, Options{})
	id := unavailableTerminal(t, m, scope, 1)
	plan, err := m.PrepareResign("fed", 1, core.ResignActionDeleteObjects, []core.ObjectHandle{100})
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	if !m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("deletion erased uninvoked terminal")
	}
	receipt, finish, err := m.BeginOwnershipCallbackDelivery(context.Background(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	finish(true)
	if err := m.ReportOwnershipCallbackInvocation(context.Background(), scope, receipt); err != nil {
		t.Fatal(err)
	}
	if m.HasAcquisitionObligation("fed", 2, 100, nil) {
		t.Fatal("invoked terminal survived receipt")
	}
}

func TestResignLegacyTerminalFailurePreventsSilentDeparture(t *testing.T) {
	m, _ := newMgr(t, Options{})
	m.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})
	if err := m.Acquire(context.Background(), "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatal(err)
	}
	plan, err := m.PrepareResign("fed", 1, core.ResignActionUnconditionallyDivestAttributes, nil)
	if err != nil {
		t.Fatal(err)
	}
	plan.Commit()
	if next, err := m.PrepareResign("fed", 2, core.ResignActionNoAction, nil); !errors.Is(err, core.ErrOwnershipAcquisitionPending) {
		if next != nil {
			next.Release()
		}
		t.Fatal("unadmitted legacy terminal silently forgotten", err)
	}
}
