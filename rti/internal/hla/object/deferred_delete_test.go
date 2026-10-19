package object

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestTimestampedDeleteRetainsObjectUntilDelivery(t *testing.T) {
	for _, id := range []uint64{0, 41} {
		t.Run(map[bool]string{true: "retractable", false: "nonretractable"}[id != 0], func(t *testing.T) {
			r, decl, _ := newTestRegistry(t, nil)
			ctx := context.Background()
			if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
				t.Fatal(err)
			}
			if err := decl.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
				t.Fatal(err)
			}
			reserveFixtureName(t, r, "fed", 1, "future-delete")
			obj, _, err := r.Register(ctx, "fed", 1, 1, "future-delete")
			if err != nil {
				t.Fatal(err)
			}
			gate := &updateTSOGate{}
			r.opts.TSOGate = gate
			r.opts.Retractions = &recordingRetractionAdmission{}
			r.opts.Outbox = &atomicUpdateOutbox{}
			ts := core.LogicalTime(10)
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{Tag: []byte("delete-tag"), RetractionHandle: id}); err != nil {
				t.Fatalf("future deletion admission: %v", err)
			}
			if r.stateFor("fed").instances[obj] == nil {
				t.Fatal("future deletion removed object before timestamp delivery")
			}
			before := core.LogicalTime(5)
			if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: []byte("before deletion")}, &before); err != nil {
				t.Fatalf("update before deletion timestamp: %v", err)
			}
		})
	}
}

func liveDeletionFixture(t *testing.T, recipients ...core.FederateHandle) (*Registry, *timepkg.Manager, *atomicUpdateOutbox, core.ObjectHandle) {
	t.Helper()
	r, decl, _ := newTestRegistry(t, nil)
	ctx := context.Background()
	if err := decl.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	for _, h := range recipients {
		if err := decl.SubscribeObjectClassAttributes(ctx, "fed", h, 1, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
	}
	reserveFixtureName(t, r, "fed", 1, "timed-lifecycle")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "timed-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	out := &atomicUpdateOutbox{}
	tm, err := timepkg.New(timepkg.Options{Clock: r.opts.Clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetDeletionLifecycle(r)
	r.opts.Outbox = out
	r.opts.TSOGate = tm
	r.opts.Retractions = tm
	r.opts.TSOValidator = tm
	for _, h := range append([]core.FederateHandle{1}, recipients...) {
		if err := tm.EnableConstrained(ctx, "fed", h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	return r, tm, out, obj
}

func TestDeferredDeleteRealTimeOrderingAndTooLateRetraction(t *testing.T) {
	r, tm, out, obj := liveDeletionFixture(t, 2, 3)
	ctx := context.Background()
	ts := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{Tag: []byte{0, 255}, RetractionHandle: 71}); err != nil {
		t.Fatal(err)
	}
	earlier := core.LogicalTime(5)
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &earlier); err != nil {
		t.Fatal(err)
	}
	later := ts + 1
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {8}}, &later); !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
		t.Fatalf("update after delete time=%v", err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 3 {
		t.Fatalf("callbacks=%#v", out.records)
	}
	if e, ok := out.records[0].Event.(*CallbackEvent); !ok || e.Kind != CallbackReflect {
		t.Fatalf("first callback=%#v", out.records[0].Event)
	}
	remove, ok := out.records[1].Event.(*CallbackEvent)
	if !ok || remove.Kind != CallbackRemove || remove.Metadata.RetractionHandle != 71 || remove.Metadata.Producer != 1 || remove.Metadata.ReceivedOrder != core.OrderTimestamp || string(remove.UserSuppliedTag) != string([]byte{0, 255}) {
		t.Fatalf("remove=%#v", remove)
	}
	if _, ok := out.records[2].Event.(*timepkg.TimeAdvanceGrant); !ok {
		t.Fatalf("last callback=%T", out.records[2].Event)
	}
	if r.stateFor("fed").instances[obj] != nil {
		t.Fatal("delivered deletion left live object")
	}
	if err := r.RetractWithError(ctx, "fed", 1, 71); !errors.Is(err, core.ErrRetractionTooLate) {
		t.Fatalf("late retract=%v", err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 3, 10); err != nil {
		t.Fatal(err)
	}
	if len(out.records) != 6 {
		t.Fatal("late retract discarded another recipient's deletion")
	}
}

func TestDeferredDeleteRetractionAndNoSubscriberMarker(t *testing.T) {
	for _, recipients := range [][]core.FederateHandle{nil, {2, 3}} {
		r, tm, out, obj := liveDeletionFixture(t, recipients...)
		ctx := context.Background()
		ts := core.LogicalTime(10)
		if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 81}); err != nil {
			t.Fatal(err)
		}
		if r.stateFor("fed").instances[obj] == nil {
			t.Fatal("early retirement")
		}
		if err := r.RetractWithError(ctx, "fed", 1, 81); err != nil {
			t.Fatal(err)
		}
		if r.stateFor("fed").instances[obj] == nil || len(r.stateFor("fed").deletions) != 0 {
			t.Fatal("retract did not restore lifecycle")
		}
		if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 81}); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
			t.Fatalf("reused delete id=%v", err)
		}
		if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 82}); err != nil {
			t.Fatal(err)
		}
		if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
			t.Fatal(err)
		}
		if r.stateFor("fed").instances[obj] != nil {
			t.Fatal("time progression did not retire object")
		}
		for _, record := range out.records {
			if e, ok := record.Event.(*CallbackEvent); ok && e.Kind == CallbackDeleteMarker {
				t.Fatal("internal marker reached outbox")
			}
		}
	}
}

func TestDeferredDeleteDeliveryFailureAndConstraintBoundary(t *testing.T) {
	for _, mode := range []string{testReserve, testCommit, "disable-constrained"} {
		t.Run(mode, func(t *testing.T) {
			r, tm, out, obj := liveDeletionFixture(t, 2)
			ctx := context.Background()
			ts := core.LogicalTime(10)
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 91}); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("full callback queue")
			var err error
			switch mode {
			case testReserve:
				out.reserveErr = failure
				err = tm.FlushQueueRequest(ctx, "fed", 2, 10)
			case testCommit:
				out.commitErr = failure
				err = tm.FlushQueueRequest(ctx, "fed", 2, 10)
			case "disable-constrained":
				out.reserveErr = failure
				out.sendErr = failure
				err = tm.DisableConstrained(ctx, "fed", 2)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("delivery error=%v", err)
			}
			if r.stateFor("fed").instances[obj] == nil || len(out.records) != 0 {
				t.Fatal("failed admission partially committed deletion")
			}
			out.reserveErr = nil
			out.commitErr = nil
			out.sendErr = nil
			if err := tm.DisableConstrained(ctx, "fed", 2); err != nil {
				t.Fatal(err)
			}
			if r.stateFor("fed").instances[obj] == nil {
				t.Fatal("speculative receiver delivery retired authoritative object")
			}
			wantCallbacks := 1
			if mode != "disable-constrained" {
				wantCallbacks = 2
			}
			if len(out.records) != wantCallbacks {
				t.Fatalf("retry callbacks=%d", len(out.records))
			}
			if wantCallbacks == 2 {
				if _, grant := out.records[1].Event.(*timepkg.TimeAdvanceGrant); !grant {
					t.Fatal("constraint release did not complete the pending advance after removal")
				}
			}
			remove := out.records[0].Event.(*CallbackEvent)
			if remove.Metadata.ReceivedOrder != core.OrderReceive {
				t.Fatal("unconstrained remove did not retain actual received order")
			}
			if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
				t.Fatal(err)
			}
			if r.stateFor("fed").instances[obj] != nil {
				t.Fatal("sender marker failed to retire object")
			}
		})
	}
}

func TestDeferredDeleteOwnerResignPurgesPendingRemoval(t *testing.T) {
	for _, deleteOwned := range []bool{false, true} {
		t.Run(map[bool]string{false: "divest", true: testDelete}[deleteOwned], func(t *testing.T) {
			r, tm, out, obj := liveDeletionFixture(t, 2)
			ctx := context.Background()
			ts := core.LogicalTime(10)
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 101}); err != nil {
				t.Fatal(err)
			}
			plan := r.PrepareResignDeletions(ctx, "fed", 1, deleteOwned)
			plan.Commit()
			tm.OnFederateResign(ctx, "fed", 1)
			if len(r.stateFor("fed").deletions) != 0 {
				t.Fatal("resign retained pending deletion")
			}
			if err := tm.TimeAdvanceRequest(ctx, "fed", 2, 10); err != nil {
				t.Fatal(err)
			}
			for _, record := range out.records {
				if e, ok := record.Event.(*CallbackEvent); ok && e.Kind == CallbackRemove {
					t.Fatal("obsolete timed removal survived owner resign")
				}
			}
			_, exists := r.stateFor("fed").instances[obj]
			if exists == deleteOwned {
				t.Fatalf("object exists=%v deleteOwned=%v", exists, deleteOwned)
			}
		})
	}
}

func TestDeferredDeleteLateSubscriberGetsRemoval(t *testing.T) {
	r, tm, out, obj := liveDeletionFixture(t, 2)
	ctx := context.Background()
	ts := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{Tag: []byte("late"), RetractionHandle: 111}); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 3); err != nil {
		t.Fatal(err)
	}
	if err := r.opts.Declarations.(*declaration.Manager).SubscribeObjectClassAttributes(ctx, "fed", 3, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	r.ObjectClassSubscribed(ctx, "fed", 3, 1, []core.AttributeHandle{1})
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 3, 10); err != nil {
		t.Fatal(err)
	}
	var discovered, removed bool
	for _, record := range out.records {
		if record.Federate != 3 {
			continue
		}
		if e, ok := record.Event.(*CallbackEvent); ok && e.Object == obj {
			if e.Kind == CallbackDiscover {
				discovered = true
			}
			if e.Kind == CallbackRemove {
				removed = true
				if string(e.UserSuppliedTag) != "late" {
					t.Fatal("late subscriber tag lost")
				}
			}
		}
	}
	if !discovered || !removed {
		t.Fatalf("late subscriber discovered=%v removed=%v", discovered, removed)
	}
}

func TestDeferredDeleteRecipientResignDoesNotLoseLifecycle(t *testing.T) {
	r, tm, _, obj := liveDeletionFixture(t, 2)
	ctx := context.Background()
	ts := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 121}); err != nil {
		t.Fatal(err)
	}
	tm.OnFederateResign(ctx, "fed", 2)
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if r.stateFor("fed").instances[obj] != nil {
		t.Fatal("recipient resign discarded the only deferred lifecycle trigger")
	}
}

func TestDeferredDeleteUpdateRediscoveryRetainsRemoval(t *testing.T) {
	r, tm, out, obj := liveDeletionFixture(t, 2)
	ctx := context.Background()
	ts := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{Tag: []byte("rediscovery"), RetractionHandle: 131}); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableConstrained(ctx, "fed", 3); err != nil {
		t.Fatal(err)
	}
	if err := r.opts.Declarations.(*declaration.Manager).SubscribeObjectClassAttributes(ctx, "fed", 3, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	before := core.LogicalTime(5)
	if err := r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {9}}, &before); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 3, 10); err != nil {
		t.Fatal(err)
	}
	for _, record := range out.records {
		if e, ok := record.Event.(*CallbackEvent); ok && record.Federate == 3 && e.Kind == CallbackRemove {
			return
		}
	}
	t.Fatal("update-discovered recipient missed pending removal")
}

type deletionGrantFailOutbox struct {
	out     *atomicUpdateOutbox
	failure error
}

func (o *deletionGrantFailOutbox) Send(ctx context.Context, fed core.FederationName, h core.FederateHandle, event core.OutboundEvent) error {
	if _, grant := event.(*timepkg.TimeAdvanceGrant); grant {
		return o.failure
	}
	return o.out.Send(ctx, fed, h, event)
}

func TestDeferredDeleteNonreservableGrantFailureCannotUndoVisibleRemoval(t *testing.T) {
	r, _, out, obj := liveDeletionFixture(t, 2)
	failure := errors.New("grant rejected after accepted removal")
	tm, err := timepkg.New(timepkg.Options{Clock: r.opts.Clock, Outbox: &deletionGrantFailOutbox{out: out, failure: failure}})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetDeletionLifecycle(r)
	r.opts.TSOGate = tm
	r.opts.Retractions = tm
	ctx := context.Background()
	if err := tm.EnableConstrained(ctx, "fed", 2); err != nil {
		t.Fatal(err)
	}
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	out.records = nil
	ts := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 141}); err != nil {
		t.Fatal(err)
	}
	if err := tm.FlushQueueRequest(ctx, "fed", 2, 10); !errors.Is(err, failure) {
		t.Fatalf("grant=%v", err)
	}
	if len(out.records) != 1 {
		t.Fatalf("accepted removals=%d", len(out.records))
	}
	if r.stateFor("fed").instances[obj] == nil {
		t.Error("speculative receiver removal retired object lifecycle")
	}
	if err := r.RetractWithError(ctx, "fed", 1, 141); err != nil {
		t.Errorf("legal speculative removal retraction rejected: %v", err)
	}
}
