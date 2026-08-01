// Package m38spec — M38 acceptance tests.
//
// §6.6 updateAttributeValues precondition (IEEE 1516.1-2010): the
// joined federate must OWN every instance-attribute it updates —
// AttributeNotOwned otherwise. Ownership is per-INSTANCE-attribute
// (the §7 model, transferred by divest/acquire); class-level
// publication is the §5 precondition — necessary, NOT sufficient.
//
// Pre-M38 gap (found by the IVCT subset, tc_ownership_divest.py
// §7.2 old-owner-update xfail): object/update.go gated only on
// publication (producerOwnsAllAttrs → Declarations.PublishersFor),
// so a federate that divested an attribute could keep updating it.
//
// Fixture style follows rti/spec/M37: in-process composition of
// object.Registry + ownership.Manager over a recording outbox, with
// the production wiring pattern (OnRegister seeds initial ownership,
// Options.Ownership consults the manager on update ingestion).
package m38spec

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
	"github.com/cbchoi/gorti/rti/internal/ownership"
)

const (
	fedName = core.FederationName("m38-fed")
	clsVeh  = core.ObjectClassHandle(1)
	// Attribute handles deliberately avoid 1: the stub FOM resolves
	// HLAprivilegeToDeleteObject to handle 1, so 2/3 keep Position /
	// Velocity distinct from the implicit privilege attribute.
	attrPosition = core.AttributeHandle(2)
	attrVelocity = core.AttributeHandle(3)

	fedPub = core.FederateHandle(1)
	fedSub = core.FederateHandle(2)

	// internalMOMProducer mirrors mom.momProducer (max-uint64): the
	// RTI-internal producer of HLAmanager object instances (§11).
	internalMOMProducer = ^core.FederateHandle(0)
)

var vehicleAttrs = []core.AttributeHandle{attrPosition, attrVelocity}

// recordingOutbox captures every Send. Goroutine-safe.
type recordingOutbox struct {
	mu   sync.Mutex
	sent []struct {
		h   core.FederateHandle
		evt core.OutboundEvent
	}
}

func (o *recordingOutbox) Send(_ context.Context, _ core.FederationName, h core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, struct {
		h   core.FederateHandle
		evt core.OutboundEvent
	}{h, evt})
	return nil
}

// reflectsTo counts ReflectAttributeValues envelopes delivered to h.
func (o *recordingOutbox) reflectsTo(h core.FederateHandle) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, rec := range o.sent {
		if rec.h != h {
			continue
		}
		carrier, ok := rec.evt.(interface{ Inner() *rtiv1.FederateEvent })
		if !ok {
			continue
		}
		if carrier.Inner().GetReflect() != nil {
			n++
		}
	}
	return n
}

// Stub FOM: every lookup succeeds at handle 1 (same pattern as
// rti/spec/M37/harness_test.go).
type stubFOMHandle struct{}

func (*stubFOMHandle) IsValid() bool                                           { return true }
func (*stubFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) { return 1, true }
func (*stubFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (*stubFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

type stubFOMRepo struct{}

func (*stubFOMRepo) Load(_ context.Context, _ []core.FOMModule) (core.FOMHandle, error) {
	return &stubFOMHandle{}, nil
}
func (*stubFOMRepo) Get(_ context.Context, _ core.FederationName) (core.FOMHandle, error) {
	return &stubFOMHandle{}, nil
}

// newGatedFixture composes registry + ownership manager the way
// cmd/rtid does: OnRegister seeds initial ownership; Options.Ownership
// wires the per-instance §7 lookup into the update gate.
func newGatedFixture(t *testing.T) (*object.Registry, *declaration.Manager, *ownership.Manager, *recordingOutbox) {
	t.Helper()
	declMgr := declaration.New()
	out := &recordingOutbox{}
	ownMgr, err := ownership.New(ownership.Options{Outbox: out})
	if err != nil {
		t.Fatalf("ownership.New: %v", err)
	}
	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       out,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
		Ownership:    ownMgr,
		OnRegister: func(fed core.FederationName, owner core.FederateHandle, obj core.ObjectHandle, _ core.ObjectClassHandle, attrs []core.AttributeHandle) {
			ownMgr.RegisterInitialOwnership(fed, owner, obj, attrs)
		},
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	return reg, declMgr, ownMgr, out
}

// setupOwnedInstance publishes Vehicle{Position,Velocity} for fedPub,
// subscribes fedSub, and registers one instance owned by fedPub.
func setupOwnedInstance(t *testing.T, reg *object.Registry, declMgr *declaration.Manager) core.ObjectHandle {
	t.Helper()
	ctx := context.Background()
	if err := declMgr.PublishObjectClassAttributes(ctx, fedName, fedPub, clsVeh, vehicleAttrs); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := declMgr.SubscribeObjectClassAttributes(ctx, fedName, fedSub, clsVeh, vehicleAttrs); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	obj, _, err := reg.Register(ctx, fedName, fedPub, clsVeh, "veh-1")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return obj
}

func update(reg *object.Registry, producer core.FederateHandle, obj core.ObjectHandle, attrs ...core.AttributeHandle) error {
	m := map[core.AttributeHandle][]byte{}
	for _, a := range attrs {
		m[a] = []byte{0x01}
	}
	return reg.UpdateAttributes(context.Background(), fedName, producer, obj, m, nil)
}

// §6.6 happy path — the registrant owns its published attributes
// (§7 initial-ownership seeding) and updates flow to subscribers.
func TestM38_RegistrantUpdatesOwnedAttributes(t *testing.T) {
	reg, declMgr, _, out := newGatedFixture(t)
	obj := setupOwnedInstance(t, reg, declMgr)

	if err := update(reg, fedPub, obj, attrPosition, attrVelocity); err != nil {
		t.Fatalf("§6.6: registrant update of owned attributes must succeed, got %v", err)
	}
	if n := out.reflectsTo(fedSub); n != 1 {
		t.Fatalf("reflect fan-out: want 1 ReflectAttributeValues to subscriber, got %d", n)
	}
}

// §6.6 + §7.2 — after unconditionally divesting Position, the OLD
// owner's update of Position is rejected AttributeNotOwned; its update
// of the still-owned Velocity keeps working; a bundle containing the
// divested attribute is rejected whole (all-or-nothing).
func TestM38_OldOwnerUpdateRejectedAfterDivest(t *testing.T) {
	reg, declMgr, ownMgr, _ := newGatedFixture(t)
	obj := setupOwnedInstance(t, reg, declMgr)
	ctx := context.Background()

	if err := ownMgr.UnconditionalDivest(ctx, fedName, fedPub, obj, []core.AttributeHandle{attrPosition}); err != nil {
		t.Fatalf("§7.2 divest: %v", err)
	}

	if err := update(reg, fedPub, obj, attrPosition); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("§6.6: old owner's update of divested attribute must be ErrAttributeNotOwned, got %v", err)
	}
	if err := update(reg, fedPub, obj, attrVelocity); err != nil {
		t.Fatalf("§6.6: update of still-owned attribute must succeed, got %v", err)
	}
	if err := update(reg, fedPub, obj, attrPosition, attrVelocity); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("§6.6: bundle containing a divested attribute must be rejected whole, got %v", err)
	}
}

// §7.2 post-condition — after divest + acquire, the NEW owner's update
// succeeds and the old owner stays locked out. Publication alone
// (fedSub publishes the class too) must NOT be sufficient before the
// acquire.
func TestM38_AcquirerUpdatesTransferredAttribute(t *testing.T) {
	reg, declMgr, ownMgr, _ := newGatedFixture(t)
	obj := setupOwnedInstance(t, reg, declMgr)
	ctx := context.Background()

	// The acquirer publishes the class (§5 necessary condition + §7
	// acquisition-candidate requirement)...
	if err := declMgr.PublishObjectClassAttributes(ctx, fedName, fedSub, clsVeh, vehicleAttrs); err != nil {
		t.Fatalf("publish (sub): %v", err)
	}
	// ...but publication is NOT sufficient: fedSub does not own
	// Position yet.
	if err := update(reg, fedSub, obj, attrPosition); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("§6.6: publisher-but-not-owner update must be ErrAttributeNotOwned, got %v", err)
	}

	if err := ownMgr.UnconditionalDivest(ctx, fedName, fedPub, obj, []core.AttributeHandle{attrPosition}); err != nil {
		t.Fatalf("divest: %v", err)
	}
	// Unowned-after-divest: BOTH federates are rejected (owner=none).
	if err := update(reg, fedPub, obj, attrPosition); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("§6.6: update of unowned attribute (old owner) must be ErrAttributeNotOwned, got %v", err)
	}
	if err := update(reg, fedSub, obj, attrPosition); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("§6.6: update of unowned attribute (candidate) must be ErrAttributeNotOwned, got %v", err)
	}

	// §7.8 unowned fast path — the acquire completes immediately.
	if err := ownMgr.Acquire(ctx, fedName, fedSub, obj, []core.AttributeHandle{attrPosition}, nil); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := update(reg, fedSub, obj, attrPosition); err != nil {
		t.Fatalf("§7.2: new owner's update must succeed, got %v", err)
	}
	if err := update(reg, fedPub, obj, attrPosition); !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Fatalf("§7.2: old owner's update after transfer must be ErrAttributeNotOwned, got %v", err)
	}
	// The old owner still owns (and may update) Velocity.
	if err := update(reg, fedPub, obj, attrVelocity); err != nil {
		t.Fatalf("§6.6: old owner's update of retained attribute must succeed, got %v", err)
	}
}

// §11 — the RTI-internal MOM producer (mom.momProducer, max-uint64) is
// exempt from the per-instance gate: its FOM-resolved attribute handles
// may lie outside the cut-1 initial-ownership seeding probe range
// (object/registry.go fanoutAttrProbe covers handles 1..8 only), and
// its HLAmanager reflects are RTI-maintained state, not federate-owned
// instance attributes.
func TestM38_InternalMOMProducerExempt(t *testing.T) {
	reg, declMgr, _, _ := newGatedFixture(t)
	ctx := context.Background()

	beyondProbe := core.AttributeHandle(9) // outside fanoutAttrProbe → never seeded
	momAttrs := []core.AttributeHandle{attrPosition, beyondProbe}
	if err := declMgr.PublishObjectClassAttributes(ctx, fedName, internalMOMProducer, clsVeh, momAttrs); err != nil {
		t.Fatalf("publish (mom): %v", err)
	}
	obj, _, err := reg.Register(ctx, fedName, internalMOMProducer, clsVeh, "HLAfederation.m38")
	if err != nil {
		t.Fatalf("register (mom): %v", err)
	}
	if err := update(reg, internalMOMProducer, obj, beyondProbe); err != nil {
		t.Fatalf("§11: internal MOM producer's reflect must bypass the §7 gate, got %v", err)
	}
	if err := update(reg, internalMOMProducer, obj, attrPosition); err != nil {
		t.Fatalf("§11: internal MOM producer's seeded-attr reflect must flow, got %v", err)
	}
}

// Nil Options.Ownership — pre-M38 fallback: fixtures composed without
// an ownership manager keep the publication-only gate (documented
// relaxation, same pattern as the optional TSOGate / TSOValidator).
func TestM38_NilOwnershipLookupKeepsPublicationGate(t *testing.T) {
	declMgr := declaration.New()
	out := &recordingOutbox{}
	reg, err := object.New(object.Options{
		Declarations: declMgr,
		Outbox:       out,
		FOMs:         &stubFOMRepo{},
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	obj := setupOwnedInstance(t, reg, declMgr)
	ctx := context.Background()

	// A second publisher that never owned anything may update — the
	// publication-only relaxation for ownership-less fixtures.
	if err := declMgr.PublishObjectClassAttributes(ctx, fedName, fedSub, clsVeh, vehicleAttrs); err != nil {
		t.Fatalf("publish (sub): %v", err)
	}
	if err := update(reg, fedSub, obj, attrPosition); err != nil {
		t.Fatalf("nil-Ownership fallback: publication-gated update must succeed, got %v", err)
	}
}
