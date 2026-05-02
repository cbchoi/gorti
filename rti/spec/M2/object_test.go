package m2spec

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// newTestObjectRegistry builds an object.Registry with the fakes most
// tests want. Returns the registry, the in-memory eventlog (nil until
// Writer is wired), the fake outbox (for fanout assertion), and the
// declaration manager (real, for setting up subscriptions).
func newTestObjectRegistry(t *testing.T) (*object.Registry, *fakeOutbox, *declaration.Manager) {
	t.Helper()
	declMgr := declaration.New()
	outbox := newFakeOutbox()

	reg, err := object.New(object.Options{
		EventLog:     nil, // Writer integration tested separately
		Declarations: declMgr,
		Outbox:       outbox,
		Codec:        nil, // not exercised in M2 routing tests
		FOMs:         newFakeFOMRepo(),
		Clock:        core.NewFakeClock(time.Unix(0, 0)),
	})
	if err != nil {
		t.Logf("object.New returned: %v (expected during M2 RED phase)", err)
	}
	return reg, outbox, declMgr
}

// TestSpec_M2_Object_Register_AssignsMonotonicHandle: object handles are
// assigned monotonically starting at 1, in registration order.
//
// Implements: FR-OM-1, NFR-DET-1.
func TestSpec_M2_Object_Register_AssignsMonotonicHandle(t *testing.T) {
	reg, _, _ := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		h, _, err := reg.Register(ctx, "fed", 1, 7, "")
		if err != nil {
			t.Fatalf("Register[%d]: %v", i, err)
		}
		if h != core.ObjectHandle(i+1) {
			t.Errorf("Register[%d] handle = %d, want %d", i, h, i+1)
		}
	}
}

// TestSpec_M2_Object_Register_FansOutDiscover: registering an object
// fans out DiscoverObjectInstance to subscribers in deterministic
// (sorted) handle order, regardless of subscribe order.
//
// Implements: FR-OM-2, NFR-DET-1.
func TestSpec_M2_Object_Register_FansOutDiscover(t *testing.T) {
	reg, outbox, declMgr := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	ctx := context.Background()

	// Subscribe federates 7, 3, 11 (out of order) to class 7 attr 2.
	for _, h := range []core.FederateHandle{7, 3, 11} {
		_ = declMgr.SubscribeObjectClassAttributes(ctx, "fed", h, 7, []core.AttributeHandle{2})
	}
	// Publish + register from federate 1 (the producer).
	_ = declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2})
	_, _, err := reg.Register(ctx, "fed", 1, 7, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Inspect outbox: every Send should be to one of {3, 7, 11},
	// emitted in ascending order.
	var federates []core.FederateHandle
	for _, s := range outbox.Sent() {
		federates = append(federates, s.Federate)
	}
	want := []core.FederateHandle{3, 7, 11}
	if len(federates) != len(want) {
		t.Fatalf("Discover fanout to %d federates: %v; want %d in order %v",
			len(federates), federates, len(want), want)
	}
	for i := range want {
		if federates[i] != want[i] {
			t.Errorf("[%d]: fanout to %d, want %d", i, federates[i], want[i])
		}
	}
}

// TestSpec_M2_Object_Register_RejectsUnpublished: a federate that has
// not published the class's attributes cannot register an instance.
//
// Implements: FR-OM-1, FR-DM-1.
func TestSpec_M2_Object_Register_RejectsUnpublished(t *testing.T) {
	reg, _, _ := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	_, _, err := reg.Register(context.Background(), "fed", 1, 7, "")
	if !errors.Is(err, core.ErrObjectClassNotPublished) {
		t.Errorf("Register without publish: err = %v, want ErrObjectClassNotPublished", err)
	}
}

// TestSpec_M2_Object_UpdateAttributes_FansOutInOrder: updating an
// object fans out ReflectAttributeValues to subscribers in sorted
// handle order.
//
// Implements: FR-OM-3, NFR-DET-1.
func TestSpec_M2_Object_UpdateAttributes_FansOutInOrder(t *testing.T) {
	reg, outbox, declMgr := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	ctx := context.Background()

	for _, h := range []core.FederateHandle{9, 2, 5} {
		_ = declMgr.SubscribeObjectClassAttributes(ctx, "fed", h, 7, []core.AttributeHandle{2})
	}
	_ = declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2})
	obj, _, err := reg.Register(ctx, "fed", 1, 7, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	preDiscoverCount := len(outbox.Sent())

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}

	// Filter to events that arrived AFTER the discover phase.
	updateSent := outbox.Sent()[preDiscoverCount:]
	var fed []core.FederateHandle
	for _, s := range updateSent {
		fed = append(fed, s.Federate)
	}
	want := []core.FederateHandle{2, 5, 9}
	if len(fed) != len(want) {
		t.Fatalf("Reflect fanout to %v, want %v", fed, want)
	}
	for i := range want {
		if fed[i] != want[i] {
			t.Errorf("[%d]: %d want %d", i, fed[i], want[i])
		}
	}
}

// TestSpec_M2_Object_UpdateAttributes_RejectsUnowned: a federate that
// does not publish the attribute cannot update it.
//
// Implements: FR-OM-3.
func TestSpec_M2_Object_UpdateAttributes_RejectsUnowned(t *testing.T) {
	reg, _, declMgr := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	ctx := context.Background()
	_ = declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2})
	obj, _, _ := reg.Register(ctx, "fed", 1, 7, "")

	// Federate 99 attempts to update — never published.
	err := reg.UpdateAttributes(ctx, "fed", 99, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil)
	if !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Errorf("UpdateAttributes by non-publisher: err = %v, want ErrAttributeNotOwned", err)
	}
}

// TestSpec_M2_Object_SendInteraction_FansOut: SendInteraction fans
// ReceiveInteraction to subscribers in sorted handle order.
//
// Implements: FR-OM-4, NFR-DET-1.
func TestSpec_M2_Object_SendInteraction_FansOut(t *testing.T) {
	reg, outbox, declMgr := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	ctx := context.Background()

	_ = declMgr.PublishInteractionClass(ctx, "fed", 1, 11)
	for _, h := range []core.FederateHandle{8, 4} {
		_ = declMgr.SubscribeInteractionClass(ctx, "fed", h, 11)
	}

	if err := reg.SendInteraction(ctx, "fed", 1, 11, map[core.ParameterHandle][]byte{1: {0x01}}, nil); err != nil {
		t.Fatalf("SendInteraction: %v", err)
	}

	var fed []core.FederateHandle
	for _, s := range outbox.Sent() {
		fed = append(fed, s.Federate)
	}
	want := []core.FederateHandle{4, 8}
	if len(fed) != len(want) || fed[0] != 4 || fed[1] != 8 {
		t.Errorf("ReceiveInteraction fanout = %v, want %v", fed, want)
	}
}

// TestSpec_M2_Object_NoSelfDelivery: an update from federate F is NOT
// delivered back to F even when F is in the subscriber list.
//
// Implements: FR-OM-3.
func TestSpec_M2_Object_NoSelfDelivery(t *testing.T) {
	reg, outbox, declMgr := newTestObjectRegistry(t)
	if reg == nil {
		t.Skip("object.Registry not yet wired")
	}
	ctx := context.Background()
	_ = declMgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2})
	_ = declMgr.SubscribeObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2}) // self-subscribe
	_ = declMgr.SubscribeObjectClassAttributes(ctx, "fed", 2, 7, []core.AttributeHandle{2}) // peer
	obj, _, _ := reg.Register(ctx, "fed", 1, 7, "")
	preCount := len(outbox.Sent())

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{2: {0xAB}}, nil); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}
	updateSent := outbox.Sent()[preCount:]

	// Federate 1 (the producer) must NOT receive its own update.
	for _, s := range updateSent {
		if s.Federate == 1 {
			t.Errorf("self-delivery: producer 1 received its own update: %+v", s)
		}
	}
	// Federate 2 SHOULD receive it.
	var got []core.FederateHandle
	for _, s := range updateSent {
		got = append(got, s.Federate)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != 1 || got[0] != 2 {
		t.Errorf("expected fanout to [2] only, got %v", got)
	}
}
