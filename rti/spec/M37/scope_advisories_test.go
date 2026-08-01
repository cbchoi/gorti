package m37spec

import (
	"context"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/object"
)

const scopeFed = core.FederationName("m37_scope_advisories")

// nullFOMHandle satisfies core.FOMHandle for tests that never resolve
// names (the update path takes pre-resolved handles).
type nullFOMHandle struct{}

func (nullFOMHandle) IsValid() bool { return true }
func (nullFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return core.InvalidObjectClassHandle, false
}
func (nullFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return core.InvalidInteractionClassHandle, false
}
func (nullFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return core.InvalidAttributeHandle, false
}
func (nullFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return core.InvalidParameterHandle, false
}

type nullFOMRepo struct{}

func (nullFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return nullFOMHandle{}, nil
}
func (nullFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return nullFOMHandle{}, nil
}

// stubDDM drives the DDM-aware update path with a configurable
// per-attribute overlap result — the test flips `subs` between updates
// to simulate region movement.
type stubDDM struct {
	subs map[core.AttributeHandle][]core.FederateHandle
}

func (s *stubDDM) HasObjectAssociations(core.FederationName, core.ObjectHandle) bool { return true }
func (s *stubDDM) PublisherRegionsFor(core.FederationName, core.ObjectHandle, core.AttributeHandle) []object.DDMRegionHandle {
	return []object.DDMRegionHandle{1}
}
func (s *stubDDM) SubscribersForUpdate(
	_ core.FederationName,
	_ core.ObjectClassHandle,
	attr core.AttributeHandle,
	_ []object.DDMRegionHandle,
) []core.FederateHandle {
	return s.subs[attr]
}
func (s *stubDDM) RegionSubscribersFor(
	core.FederationName, core.ObjectClassHandle, core.AttributeHandle,
) []core.FederateHandle {
	return nil
}

// TestSpec_M37_ScopeAdvisories_OverlapTransitions: IEEE 1516.1-2010
// §6.17/§6.18 — a subscriber entering the region-overlap set for an
// attribute receives AttributesInScope BEFORE the Reflect; when a later
// update no longer overlaps, the subscriber receives
// AttributesOutOfScope (and no Reflect).
func TestSpec_M37_ScopeAdvisories_OverlapTransitions(t *testing.T) {
	ctx := context.Background()
	outbox := newFakeOutbox()
	declMgr := declaration.New()
	ddm := &stubDDM{subs: map[core.AttributeHandle][]core.FederateHandle{}}
	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       outbox,
		FOMs:         nullFOMRepo{},
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
		DDM:          ddm,
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}

	cls := core.ObjectClassHandle(3)
	attrs := []core.AttributeHandle{1}
	if err := declMgr.PublishObjectClassAttributes(ctx, scopeFed, 1, cls, attrs); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	obj, _, err := reg.Register(ctx, scopeFed, 1, cls, "ball")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	outbox.Reset()

	// Update 1: federate 2 overlaps → InScope + Reflect.
	ddm.subs[1] = []core.FederateHandle{2}
	payload := map[core.AttributeHandle][]byte{1: {0xAA}}
	if err := reg.UpdateAttributes(ctx, scopeFed, 1, obj, payload, nil); err != nil {
		t.Fatalf("Update 1: %v", err)
	}
	evts := outbox.SentTo(2)
	if len(evts) != 2 {
		t.Fatalf("subscriber events after update 1 = %d, want 2 (in-scope, reflect); got %+v", len(evts), evts)
	}
	in := evts[0].GetAttributesInScope()
	if in == nil {
		t.Fatalf("event[0] = %+v, want AttributesInScope", evts[0])
	}
	if in.GetObjectHandle() != uint64(obj) || len(in.GetAttributeHandles()) != 1 || in.GetAttributeHandles()[0] != 1 {
		t.Errorf("in-scope = %+v, want object %d attrs [1]", in, obj)
	}
	if evts[1].GetReflect() == nil {
		t.Errorf("event[1] = %+v, want Reflect", evts[1])
	}

	// Update 2: still overlapping → Reflect only (no repeated advisory).
	if err := reg.UpdateAttributes(ctx, scopeFed, 1, obj, payload, nil); err != nil {
		t.Fatalf("Update 2: %v", err)
	}
	evts = outbox.SentTo(2)
	if len(evts) != 3 || evts[2].GetReflect() == nil {
		t.Fatalf("subscriber events after update 2 = %+v, want [+Reflect only]", evts)
	}

	// Update 3: overlap gone → OutOfScope, no Reflect.
	ddm.subs[1] = nil
	if err := reg.UpdateAttributes(ctx, scopeFed, 1, obj, payload, nil); err != nil {
		t.Fatalf("Update 3: %v", err)
	}
	evts = outbox.SentTo(2)
	if len(evts) != 4 {
		t.Fatalf("subscriber events after update 3 = %d, want 4 (+out-of-scope); got %+v", len(evts), evts)
	}
	out := evts[3].GetAttributesOutOfScope()
	if out == nil {
		t.Fatalf("event[3] = %+v, want AttributesOutOfScope", evts[3])
	}
	if out.GetObjectHandle() != uint64(obj) || len(out.GetAttributeHandles()) != 1 || out.GetAttributeHandles()[0] != 1 {
		t.Errorf("out-of-scope = %+v, want object %d attrs [1]", out, obj)
	}

	// Update 4: overlap returns → InScope fires again + Reflect.
	ddm.subs[1] = []core.FederateHandle{2}
	if err := reg.UpdateAttributes(ctx, scopeFed, 1, obj, payload, nil); err != nil {
		t.Fatalf("Update 4: %v", err)
	}
	evts = outbox.SentTo(2)
	if len(evts) != 6 || evts[4].GetAttributesInScope() == nil || evts[5].GetReflect() == nil {
		t.Fatalf("subscriber events after update 4 = %+v, want [+InScope, +Reflect]", evts)
	}
}
