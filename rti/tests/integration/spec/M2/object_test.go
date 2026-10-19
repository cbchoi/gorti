package m2spec

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/application/composition"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/declaration"
	"github.com/cbchoi/gorti/rti/internal/hla/federation"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
	"github.com/cbchoi/gorti/rti/internal/hla/ownership"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

type objectFixture struct {
	registry      *object.Registry
	outbox        *fakeOutbox
	declarations  *declaration.Manager
	class         core.ObjectClassHandle
	first, second core.AttributeHandle
	interaction   core.InteractionClassHandle
	parameter     core.ParameterHandle
}

const objectFixtureXML = `<objectModel>
<objects><objectClass><name>HLAobjectRoot</name><objectClass><name>Item</name>
<attribute><name>first</name><order>Receive</order></attribute>
<attribute><name>second</name><order>Receive</order></attribute>
</objectClass></objectClass></objects>
<interactions><interactionClass><name>HLAinteractionRoot</name>
<interactionClass><name>Event</name><order>Receive</order>
<parameter><name>payload</name></parameter></interactionClass>
</interactionClass></interactions>
</objectModel>`

// The recording outbox observes real service admission after create/join.
func newTestObjectRegistry(t *testing.T, lastMember ...core.FederateHandle) objectFixture {
	t.Helper()
	ctx := context.Background()
	clock := core.NewManualClock(time.Unix(0, 0))
	repo := composition.NewFOMRepository()
	modules := []core.FOMModule{{Path: "m2-object.xml", XML: []byte(objectFixtureXML)}}
	roster, err := federation.New(federation.Options{Clock: clock, FOMs: repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := roster.CreateFederation(ctx, core.CreateFederationRequest{Name: "fed", FOMModules: modules}); err != nil {
		t.Fatal(err)
	}
	fom, err := roster.FOMFor("fed")
	if err != nil {
		t.Fatal(err)
	}
	repo.RememberFor("fed", fom)
	declMgr := declaration.New()
	declMgr.SetFOMRepository(repo)
	outbox := newFakeOutbox()
	tm, err := timepkg.New(timepkg.Options{Clock: clock, Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := ownership.New(ownership.Options{Outbox: outbox})
	if err != nil {
		t.Fatal(err)
	}
	maximum := core.FederateHandle(11)
	if len(lastMember) != 0 {
		maximum = lastMember[0]
	}
	for expected := core.FederateHandle(1); expected <= maximum; expected++ {
		h, err := roster.JoinFederation(ctx, core.JoinFederationRequest{Federation: "fed", FederateName: fmt.Sprintf("member-%d", expected)})
		if err != nil {
			t.Fatal(err)
		}
		if h != expected {
			t.Fatalf("joined handle %d, want %d", h, expected)
		}
		if err := tm.OnFederateJoined("fed", h); err != nil {
			t.Fatal(err)
		}
	}
	reg, err := object.New(object.Options{
		EventLog:     nil, // Writer integration tested separately
		Declarations: declMgr,
		Outbox:       outbox,
		Codec:        nil, // not exercised in M2 routing tests
		FOMs:         repo,
		Clock:        clock,
		Federations:  roster,
		TSOGate:      tm,
		TSOValidator: tm,
		Retractions:  tm,
		Ownership:    owners,
		OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			owners.RegisterInitialOwnership(fed, owner, obj, attrs)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetQueuedMessageFilter(reg)
	declMgr.SetOnSubscribeObjectClass(reg.ObjectClassSubscribed)
	class, ok := fom.LookupObjectClass("HLAobjectRoot.Item")
	if !ok {
		t.Fatal("missing Item class")
	}
	first, ok := fom.LookupAttribute(class, "first")
	if !ok {
		t.Fatal("missing first attribute")
	}
	second, ok := fom.LookupAttribute(class, "second")
	if !ok {
		t.Fatal("missing second attribute")
	}
	interaction, ok := fom.LookupInteractionClass("HLAinteractionRoot.Event")
	if !ok {
		t.Fatal("missing Event class")
	}
	parameter, ok := fom.LookupParameter(interaction, "payload")
	if !ok {
		t.Fatal("missing payload parameter")
	}
	return objectFixture{reg, outbox, declMgr, class, first, second, interaction, parameter}
}

// TestSpec_M2_Object_Register_AssignsMonotonicHandle: object handles are
// assigned monotonically starting at 1, in registration order.
//
// Implements: FR-OM-1, NFR-DET-1.
func TestSpec_M2_Object_Register_AssignsMonotonicHandle(t *testing.T) {
	f := newTestObjectRegistry(t)
	reg, declMgr := f.registry, f.declarations
	ctx := context.Background()

	// Producer must publish before Register; this is the auth check
	// asserted independently by Register_RejectsUnpublished. The current
	// test focuses on the monotonic-handle invariant once auth passes.
	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, f.class, []core.AttributeHandle{f.first}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	for i := 0; i < 5; i++ {
		h, _, err := reg.Register(ctx, "fed", 1, f.class, "")
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
	f := newTestObjectRegistry(t)
	reg, outbox, declMgr := f.registry, f.outbox, f.declarations
	ctx := context.Background()

	// Subscribe federates 7, 3, 11 (out of order) to Item.second.
	for _, h := range []core.FederateHandle{7, 3, 11} {
		if err := declMgr.SubscribeObjectClassAttributes(ctx, "fed", h, f.class, []core.AttributeHandle{f.second}); err != nil {
			t.Fatal(err)
		}
	}
	// Publish + register from federate 1 (the producer).
	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	_, _, err := reg.Register(ctx, "fed", 1, f.class, "")
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
	f := newTestObjectRegistry(t)
	reg := f.registry
	_, _, err := reg.Register(context.Background(), "fed", 1, f.class, "")
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
	f := newTestObjectRegistry(t)
	reg, outbox, declMgr := f.registry, f.outbox, f.declarations
	ctx := context.Background()

	for _, h := range []core.FederateHandle{9, 2, 5} {
		if err := declMgr.SubscribeObjectClassAttributes(ctx, "fed", h, f.class, []core.AttributeHandle{f.second}); err != nil {
			t.Fatal(err)
		}
	}
	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := reg.Register(ctx, "fed", 1, f.class, "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	preDiscoverCount := len(outbox.Sent())

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{f.second: {0xAB}}, nil); err != nil {
		t.Fatalf("UpdateAttributes: %v", err)
	}

	// Filter to events that arrived AFTER the discover phase.
	updateSent := outbox.Sent()[preDiscoverCount:]
	var fed []core.FederateHandle
	var scope []core.FederateHandle
	for _, s := range updateSent {
		event, ok := s.Event.(*object.CallbackEvent)
		if !ok || event.Object != obj {
			t.Fatalf("unexpected update callback: %+v", s)
		}
		switch event.Kind {
		case object.CallbackReflect:
			fed = append(fed, s.Federate)
		case object.CallbackAttributesInScope:
			if len(fed) != 0 || len(event.AttributeHandles) != 1 || event.AttributeHandles[0] != f.second {
				t.Fatalf("scope callback order/attributes: %+v", event)
			}
			scope = append(scope, s.Federate)
		default:
			t.Fatalf("unexpected update callback kind: %v", event.Kind)
		}
	}
	want := []core.FederateHandle{2, 5, 9}
	if len(scope) != len(want) {
		t.Fatalf("scope fanout to %v, want %v", scope, want)
	}
	if len(fed) != len(want) {
		t.Fatalf("Reflect fanout to %v, want %v", fed, want)
	}
	for i := range want {
		if fed[i] != want[i] {
			t.Errorf("[%d]: %d want %d", i, fed[i], want[i])
		}
		if scope[i] != want[i] {
			t.Errorf("scope[%d]: %d want %d", i, scope[i], want[i])
		}
	}
}

// TestSpec_M2_Object_UpdateAttributes_RejectsUnowned: a federate that
// does not publish the attribute cannot update it.
//
// Implements: FR-OM-3.
func TestSpec_M2_Object_UpdateAttributes_RejectsUnowned(t *testing.T) {
	f := newTestObjectRegistry(t, 99)
	reg, declMgr := f.registry, f.declarations
	ctx := context.Background()
	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	obj, _, err := reg.Register(ctx, "fed", 1, f.class, "")
	if err != nil {
		t.Fatal(err)
	}

	// Federate 99 attempts to update — never published.
	err = reg.UpdateAttributes(ctx, "fed", 99, obj, map[core.AttributeHandle][]byte{f.second: {0xAB}}, nil)
	if !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Errorf("UpdateAttributes by non-publisher: err = %v, want ErrAttributeNotOwned", err)
	}
}

// TestSpec_M2_Object_SendInteraction_FansOut: SendInteraction fans
// ReceiveInteraction to subscribers in sorted handle order.
//
// Implements: FR-OM-4, NFR-DET-1.
func TestSpec_M2_Object_SendInteraction_FansOut(t *testing.T) {
	f := newTestObjectRegistry(t)
	reg, outbox, declMgr := f.registry, f.outbox, f.declarations
	ctx := context.Background()

	if err := declMgr.PublishInteractionClass(ctx, "fed", 1, f.interaction); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{8, 4} {
		if err := declMgr.SubscribeInteractionClass(ctx, "fed", h, f.interaction); err != nil {
			t.Fatal(err)
		}
	}

	if err := reg.SendInteraction(ctx, "fed", 1, f.interaction, map[core.ParameterHandle][]byte{f.parameter: {0x01}}, nil); err != nil {
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
	f := newTestObjectRegistry(t)
	reg, outbox, declMgr := f.registry, f.outbox, f.declarations
	ctx := context.Background()
	if err := declMgr.PublishObjectClassAttributes(ctx, "fed", 1, f.class, []core.AttributeHandle{f.second}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []core.FederateHandle{1, 2} {
		if err := declMgr.SubscribeObjectClassAttributes(ctx, "fed", h, f.class, []core.AttributeHandle{f.second}); err != nil {
			t.Fatal(err)
		}
	}
	obj, _, err := reg.Register(ctx, "fed", 1, f.class, "")
	if err != nil {
		t.Fatal(err)
	}
	preCount := len(outbox.Sent())

	if err := reg.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{f.second: {0xAB}}, nil); err != nil {
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
	scopes := 0
	for _, s := range updateSent {
		event, ok := s.Event.(*object.CallbackEvent)
		if !ok || s.Federate != 2 || event.Object != obj {
			t.Fatalf("unexpected peer update callback: %+v", s)
		}
		switch event.Kind {
		case object.CallbackReflect:
			got = append(got, s.Federate)
		case object.CallbackAttributesInScope:
			scopes++
		default:
			t.Fatalf("unexpected peer update callback kind: %v", event.Kind)
		}
	}
	if scopes != 1 {
		t.Errorf("peer scope callbacks=%d, want 1", scopes)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != 1 || got[0] != 2 {
		t.Errorf("expected fanout to [2] only, got %v", got)
	}
}
