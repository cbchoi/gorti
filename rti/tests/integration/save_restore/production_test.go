package save_restore_test

import (
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const checkpointLabel = "before-mutation"

func checkpointFixture(t *testing.T) (*federation, uint64) {
	t.Helper()
	f := startFederation(t)
	f.declare(0, 1)
	old := f.register(0, "saved-object")
	f.callbacks[1].wait(t, 0, "saved object discovery", func(e *rtiv1.FederateEvent) bool { return e.GetDiscover().GetObjectHandle() == old })
	mark := f.callbacks[1].mark()
	value := []byte{0x40, 0x14, 0, 0, 0, 0, 0, 0}
	check(t, f.update(0, old, value))
	f.expectReflection(1, mark, old, value)
	f.reserve(0, "saved-reservation", true)
	f.enableTime()
	// These state-restoration checks exchange RO messages between grants.
	// Save explicit asynchronous delivery so idle constrained peers can receive.
	for _, h := range f.handles {
		_, err := f.time.EnableAsynchronousDelivery(f.ctx, &rtiv1.EnableAsynchronousDeliveryRequest{
			WireVersion: 1, FederationName: f.name, FederateHandle: h,
		})
		check(t, err)
	}
	f.advance(5)
	f.barrier()
	f.save(checkpointLabel)
	return f, old
}

func mutateCheckpoint(f *federation, old uint64) uint64 {
	f.t.Helper()
	mark := f.callbacks[1].mark()
	_, err := f.objects.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: old})
	check(f.t, err)
	f.callbacks[1].wait(f.t, mark, "saved object removal", func(e *rtiv1.FederateEvent) bool { return e.GetRemove().GetObjectHandle() == old })
	newObject := f.register(0, "post-save-object")
	f.release(0, "saved-reservation")
	f.reserve(1, "saved-reservation", true)
	f.reserve(1, "post-save-reservation", true)
	f.undeclare(0, 1)
	f.declare(1, 0)
	f.advance(10)
	// These controls establish that the live state really differs from the save.
	f.lookup("saved-object", 0)
	f.lookup("post-save-object", newObject)
	if err := f.send(0, []byte{0, 0, 0, 1}, nil); status.Code(err) != codes.FailedPrecondition {
		f.t.Fatalf("removed publication was not enforced: %v", err)
	}
	mark = f.callbacks[0].mark()
	check(f.t, f.send(1, []byte{0, 0, 0, 2}, nil))
	f.expectInteraction(0, mark, []byte{0, 0, 0, 2})
	f.barrier()
	return newObject
}

// All assertions use production RPCs and callbacks. The roster is unchanged;
// this test does not claim restart recovery or replay of consumed callbacks.
func TestProductionSaveRestoreCompleteState(t *testing.T) {
	f, old := checkpointFixture(t)
	postSave := mutateCheckpoint(f, old)
	f.restore(checkpointLabel)
	f.logicalTime(5)
	f.lookup("saved-object", old)
	f.lookup("post-save-object", 0)
	_, err := f.support.GetObjectInstanceName(f.ctx, &rtiv1.GetObjectInstanceNameRequest{WireVersion: 1, FederationName: f.name, ObjectHandle: postSave})
	if status.Code(err) != codes.NotFound {
		t.Errorf("post-save object handle remained addressable: %v", err)
	}
	f.reserve(1, "saved-reservation", false)
	f.release(0, "saved-reservation")
	f.reserve(0, "post-save-reservation", true)
	f.release(0, "post-save-reservation")
	// The saved subscriber already knew the object. Restoring knowledge must
	// not turn an idempotent subscription into another discovery callback.
	mark := f.callbacks[1].mark()
	_, err = f.decl.SubscribeObjectClassAttributes(f.ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[1], ObjectClassHandle: f.class, AttributeHandles: []uint64{f.attr}})
	check(t, err)
	f.barrier()
	f.callbacks[1].mu.Lock()
	for _, e := range f.callbacks[1].events[mark:] {
		if e.GetDiscover().GetObjectHandle() == old {
			t.Errorf("saved object knowledge was lost: repeated discovery %v", e)
		}
	}
	f.callbacks[1].mu.Unlock()

	mark = f.callbacks[1].mark()
	payload := []byte{0, 0, 0, 3}
	check(t, f.send(0, payload, nil))
	f.expectInteraction(1, mark, payload)
	if err := f.send(1, payload, nil); status.Code(err) != codes.FailedPrecondition {
		t.Errorf("post-save publisher retained its declaration: %v", err)
	}
	_, err = f.objects.RegisterObjectInstance(f.ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[1], ObjectClassHandle: f.class})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("post-save object publication retained: %v", err)
	}
	value := []byte{0x40, 0x14, 0, 0, 0, 0, 0, 0}
	mark = f.callbacks[1].mark()
	check(t, f.update(0, old, value))
	f.expectReflection(1, mark, old, value)
	// Ownership and restored object knowledge must agree with the registry.
	_, err = f.objects.RequestAttributeValueUpdate(f.ctx, &rtiv1.RequestAttributeValueUpdateRequest{WireVersion: 1,
		FederationName: f.name, FederateHandle: f.handles[1], ObjectHandle: old, AttributeHandles: []uint64{f.attr}})
	check(t, err)
	f.callbacks[0].wait(t, 0, "update request routed to saved owner", func(e *rtiv1.FederateEvent) bool {
		return e.GetProvideUpdate().GetObjectHandle() == old
	})
	newObject := f.register(0, "registered-after-restore")
	if newObject == old {
		t.Fatal("registration after restore reused a live saved object handle")
	}
	f.lookup("saved-object", old)
	f.lookup("registered-after-restore", newObject)
	f.advance(6)
	f.barrier()
}

func TestProductionSaveRestorePendingTSO(t *testing.T) {
	f := startFederation(t)
	f.declare(0, 1)
	f.enableTime()
	f.advance(5)
	timestamp := 7.0
	payload := []byte{0, 0, 0, 7}
	mark := f.callbacks[1].mark()
	check(t, f.send(0, payload, &timestamp))
	query := &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1]}
	lits, err := f.time.QueryLITS(f.ctx, query)
	check(t, err)
	if !lits.Finite || lits.Lits != 5.5 {
		t.Fatalf("LITS before save must be min(GALT=5.5, queued timestamp=7): %v", lits)
	}
	f.barrier()
	f.callbacks[1].mu.Lock()
	for _, e := range f.callbacks[1].events[mark:] {
		if e.GetReceive() != nil {
			t.Errorf("pending TSO delivered before save: %v", e)
		}
	}
	f.callbacks[1].mu.Unlock()
	f.save(checkpointLabel)
	grants := f.advance(10)
	firstDelivery := f.expectInteraction(1, mark, payload)
	if firstDelivery >= grants[1] {
		t.Fatal("TSO delivered after its first grant")
	}
	lits, err = f.time.QueryLITS(f.ctx, query)
	check(t, err)
	if !lits.Finite || lits.Lits != 10.5 {
		t.Fatalf("after delivery, LITS must use GALT=10.5: %v", lits)
	}
	for i, h := range f.handles {
		modified := float64(i + 2)
		_, err = f.time.ModifyLookahead(f.ctx, &rtiv1.ModifyLookaheadRequest{WireVersion: 1,
			FederationName: f.name, FederateHandle: h, Lookahead: modified})
		check(t, err)
		lookahead, err := f.time.QueryLookahead(f.ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h})
		check(t, err)
		if lookahead.Lookahead != modified {
			t.Fatalf("lookahead mutation was not applied for %d: got %g, want %g", h, lookahead.Lookahead, modified)
		}
	}
	f.restore(checkpointLabel)
	f.logicalTime(5)
	lits, err = f.time.QueryLITS(f.ctx, query)
	check(t, err)
	if !lits.Finite || lits.Lits != 5.5 {
		t.Fatalf("restored LITS must be min(GALT=5.5, queued timestamp=7): %v", lits)
	}
	for _, h := range f.handles {
		lookahead, err := f.time.QueryLookahead(f.ctx, &rtiv1.QueryFederateTimeRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h})
		check(t, err)
		if lookahead.Lookahead != 0.5 {
			t.Errorf("saved lookahead not restored for %d: %g", h, lookahead.Lookahead)
		}
	}
	mark = f.callbacks[1].mark()
	grants = f.advance(8)
	restoredDelivery := f.expectInteraction(1, mark, payload)
	if restoredDelivery >= grants[1] {
		t.Fatal("restored TSO delivered after its grant")
	}
	f.barrier()
	f.callbacks[1].mu.Lock()
	deliveries := 0
	for _, e := range f.callbacks[1].events[mark:] {
		if receive := e.GetReceive(); receive != nil && receive.InteractionClassHandle == f.inter {
			deliveries++
			if receive.LogicalTime == nil || *receive.LogicalTime != timestamp {
				t.Errorf("restored TSO timestamp changed: %v", receive)
			}
		}
	}
	f.callbacks[1].mu.Unlock()
	if deliveries != 1 {
		t.Errorf("restored pending message delivered %d times, want once", deliveries)
	}
}

func TestProductionRestoreFailureKeepsLiveState(t *testing.T) {
	for _, mode := range []string{"participant-failure", "abort"} {
		t.Run(mode, func(t *testing.T) {
			f, old := checkpointFixture(t)
			postSave := mutateCheckpoint(f, old)
			marks := f.beginRestore(checkpointLabel)
			f.completeRestore(0)
			state, err := f.savepoint.QueryRestoreState(f.ctx, &rtiv1.QueryRestoreStateRequest{WireVersion: 1, FederationName: f.name, Label: checkpointLabel})
			check(t, err)
			if state.State != rtiv1.RestoreState_RESTORE_STATE_INITIATED {
				t.Fatalf("first participant prematurely finalized restore: %v", state)
			}
			// Ordinary reads need not be permitted while the protocol is active.
			// State preservation is observed after failure, never through a bypass.
			if mode == "abort" {
				_, err = f.savepoint.AbortFederationRestore(f.ctx, &rtiv1.AbortFederationRestoreRequest{WireVersion: 1,
					FederationName: f.name, FederateHandle: &f.handles[0]})
			} else {
				_, err = f.savepoint.FederateRestoreNotComplete(f.ctx, &rtiv1.FederateRestoreResponseRequest{WireVersion: 1,
					FederationName: f.name, FederateHandle: f.handles[1]})
			}
			check(t, err)
			f.restoreOutcome(checkpointLabel, marks, false)
			f.logicalTime(10)
			f.lookup("saved-object", 0)
			f.lookup("post-save-object", postSave)
			f.release(1, "saved-reservation")
			f.release(1, "post-save-reservation")
			if err := f.send(0, []byte{0, 0, 0, 4}, nil); status.Code(err) != codes.FailedPrecondition {
				t.Errorf("failed restore changed the live publication: %v", err)
			}
			mark := f.callbacks[0].mark()
			check(t, f.send(1, []byte{0, 0, 0, 5}, nil))
			f.expectInteraction(0, mark, []byte{0, 0, 0, 5})
			f.barrier()
			// Failure must release admission and permit retry of the same save.
			f.restore(checkpointLabel)
			f.logicalTime(5)
			f.lookup("saved-object", old)
			f.lookup("post-save-object", 0)
		})
	}
}
