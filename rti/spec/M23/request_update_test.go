// TASK-257 (M23 W2) — local_delete + request_attribute_value_update.

package m23spec

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestSpec_M23_LocalDeleteUnknownObjectInvalid pins error contract.
func TestSpec_M23_LocalDeleteUnknownObjectInvalid(t *testing.T) {
	reg, _, _ := newM23Registry(t)
	ctx := context.Background()
	err := reg.LocalDelete(ctx, "fed", 1, core.ObjectHandle(999))
	if err != core.ErrObjectHandleInvalid {
		t.Errorf("LocalDelete unknown obj err = %v, want ErrObjectHandleInvalid", err)
	}
}

// TestSpec_M23_LocalDeleteSucceedsForKnownObject — cut-1 simplification:
// LocalDelete validates the handle but does NOT mutate global state.
// Other federates continue to see the instance.
func TestSpec_M23_LocalDeleteSucceedsForKnownObject(t *testing.T) {
	reg, declMgr, _ := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const cls = core.ObjectClassHandle(7)
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.LocalDelete(ctx, fed, 2, obj); err != nil {
		t.Errorf("LocalDelete err = %v, want nil", err)
	}
	// Owner can still update — global state unchanged.
	attrs := map[core.AttributeHandle][]byte{1: []byte("v")}
	if err := reg.UpdateAttributes(ctx, fed, owner, obj, attrs, nil); err != nil {
		t.Errorf("UpdateAttributes after LocalDelete err = %v, want nil (global unchanged)", err)
	}
}

// TestSpec_M23_RequestAttributeValueUpdateEmitsProvide — owner receives
// ProvideAttributeValueUpdate event when a peer requests resync.
func TestSpec_M23_RequestAttributeValueUpdateEmitsProvide(t *testing.T) {
	reg, declMgr, out := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const owner = core.FederateHandle(1)
	const requester = core.FederateHandle(2)
	const cls = core.ObjectClassHandle(7)
	if err := declMgr.PublishObjectClassAttributes(ctx, fed, owner, cls, []core.AttributeHandle{1, 2}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, fed, owner, cls, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	beforeReq := len(out.snapshot())
	tag := []byte("plz")
	if err := reg.RequestAttributeValueUpdate(ctx, fed, requester, obj, []core.AttributeHandle{1, 2}, tag); err != nil {
		t.Fatalf("RequestAttributeValueUpdate: %v", err)
	}
	events := out.snapshot()
	if len(events) <= beforeReq {
		t.Fatalf("no event emitted by RequestAttributeValueUpdate (had %d, still %d)", beforeReq, len(events))
	}
	var found bool
	for _, ev := range events[beforeReq:] {
		if ev.h != owner {
			continue
		}
		fe, ok := unwrapFederateEvent(ev.evt)
		if !ok {
			continue
		}
		if pv := fe.GetProvideUpdate(); pv != nil {
			if pv.GetObjectHandle() != uint64(obj) {
				t.Errorf("provide.object_handle = %d, want %d", pv.GetObjectHandle(), obj)
			}
			if string(pv.GetUserSuppliedTag()) != "plz" {
				t.Errorf("provide.tag = %q, want \"plz\"", string(pv.GetUserSuppliedTag()))
			}
			if len(pv.GetAttributeHandles()) != 2 {
				t.Errorf("provide.attribute_handles len = %d, want 2", len(pv.GetAttributeHandles()))
			}
			found = true
		}
	}
	if !found {
		t.Errorf("owner didn't receive ProvideAttributeValueUpdate")
	}
}

// TestSpec_M23_RequestClassAttributeValueUpdateFansOut — class-scoped
// variant emits one Provide event per unique owner.
func TestSpec_M23_RequestClassAttributeValueUpdateFansOut(t *testing.T) {
	reg, declMgr, out := newM23Registry(t)
	ctx := context.Background()
	const fed core.FederationName = "fed"
	const cls = core.ObjectClassHandle(7)
	const ownerA = core.FederateHandle(1)
	const ownerB = core.FederateHandle(2)
	const requester = core.FederateHandle(3)
	for _, h := range []core.FederateHandle{ownerA, ownerB} {
		if err := declMgr.PublishObjectClassAttributes(ctx, fed, h, cls, []core.AttributeHandle{1}); err != nil {
			t.Fatalf("Publish %d: %v", h, err)
		}
	}
	if _, _, err := reg.Register(ctx, fed, ownerA, cls, "a1"); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if _, _, err := reg.Register(ctx, fed, ownerB, cls, "b1"); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	beforeReq := len(out.snapshot())
	if err := reg.RequestClassAttributeValueUpdate(ctx, fed, requester, cls, []core.AttributeHandle{1}, nil); err != nil {
		t.Fatalf("RequestClassAttributeValueUpdate: %v", err)
	}
	// Two instances, two unique owners → two ProvideAttributeValueUpdate events.
	provideCountByOwner := map[core.FederateHandle]int{}
	for _, ev := range out.snapshot()[beforeReq:] {
		fe, ok := unwrapFederateEvent(ev.evt)
		if !ok {
			continue
		}
		if fe.GetProvideUpdate() != nil {
			provideCountByOwner[ev.h]++
		}
	}
	if provideCountByOwner[ownerA] != 1 {
		t.Errorf("ownerA provide events = %d, want 1", provideCountByOwner[ownerA])
	}
	if provideCountByOwner[ownerB] != 1 {
		t.Errorf("ownerB provide events = %d, want 1", provideCountByOwner[ownerB])
	}
}
