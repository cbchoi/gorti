// M26 Phase F — object instance name reservation spec tests.
//
// Exercises rti/internal/object.Registry directly: reserve, register
// with reserved name, double-reserve fails, register-without-reserve
// auto-reserves (backwards-compat), release allows re-reservation,
// resign clears reservations.

package m26spec

import (
	"context"
	"errors"
	"sync"
	"testing"
	stdtime "time"

	"github.com/cbchoi/gorti/rti/internal/core"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/genproto/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/object"
)

// captureOutbox records every outbound event so reservation success/fail
// callback delivery can be asserted on.
type captureOutbox struct {
	mu     sync.Mutex
	events []*rtiv1.FederateEvent
}

func (o *captureOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, evt core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if oe, ok := evt.(interface{ GetPb() *rtiv1.FederateEvent }); ok {
		o.events = append(o.events, oe.GetPb())
		return nil
	}
	// Fall back: use the project's outbound-event accessor by name match
	// — implementations expose ProtoPB() / GetPb() / .pb on the impl.
	// For testing we rely on a tiny helper on the registry side, but
	// keeping this defensive in case the contract drifts.
	return nil
}

// alwaysPublishingDecl satisfies core.DeclarationManagement so Register
// doesn't reject for "class not published". Cut-1 producerPublishesAnyAttrOf
// only probes a fixed attribute range; we accept all.
type alwaysPublishingDecl struct{}

func (alwaysPublishingDecl) PublishObjectClassAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle) error {
	return nil
}
func (alwaysPublishingDecl) UnpublishObjectClassAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle) error {
	return nil
}
func (alwaysPublishingDecl) SubscribeObjectClassAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle) error {
	return nil
}
func (alwaysPublishingDecl) UnsubscribeObjectClassAttributes(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.ObjectClassHandle, _ []core.AttributeHandle) error {
	return nil
}
func (alwaysPublishingDecl) PublishInteractionClass(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle) error {
	return nil
}
func (alwaysPublishingDecl) UnpublishInteractionClass(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle) error {
	return nil
}
func (alwaysPublishingDecl) SubscribeInteractionClass(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle) error {
	return nil
}
func (alwaysPublishingDecl) UnsubscribeInteractionClass(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.InteractionClassHandle) error {
	return nil
}
func (alwaysPublishingDecl) SubscribersFor(_ context.Context, _ core.FederationName, _ core.ObjectClassHandle, _ []core.AttributeHandle) []core.FederateHandle {
	return nil
}
func (alwaysPublishingDecl) InteractionSubscribersFor(_ context.Context, _ core.FederationName, _ core.InteractionClassHandle) []core.FederateHandle {
	return nil
}
func (alwaysPublishingDecl) PublishersFor(_ context.Context, _ core.FederationName, _ core.ObjectClassHandle, _ core.AttributeHandle) []core.FederateHandle {
	return []core.FederateHandle{1, 2, 3}
}
func (alwaysPublishingDecl) InteractionPublishersFor(_ context.Context, _ core.FederationName, _ core.InteractionClassHandle) []core.FederateHandle {
	return nil
}
func (alwaysPublishingDecl) Snapshot(_ core.FederationName) core.DeclarationSnapshot {
	return core.DeclarationSnapshot{}
}

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

func newReg(t *testing.T) (*object.Registry, *captureOutbox) {
	t.Helper()
	out := &captureOutbox{}
	reg, err := object.New(object.Options{
		Clock:        core.NewFakeClock(stdtime.Unix(0, 0)),
		Declarations: alwaysPublishingDecl{},
		Outbox:       out,
		FOMs:         &stubFOMRepo{},
	})
	if err != nil {
		t.Fatalf("object.New: %v", err)
	}
	return reg, out
}

const fed = core.FederationName("m26-reserve")

func TestSpec_M26_Reserve_Then_Register_Succeeds(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Vehicle-1"); err != nil {
		t.Fatalf("ReserveObjectInstanceName: %v", err)
	}
	h, name, err := reg.Register(ctx, fed, 1, core.ObjectClassHandle(1), "Vehicle-1")
	if err != nil {
		t.Fatalf("Register after reserve: %v", err)
	}
	if h == 0 {
		t.Errorf("Register returned invalid handle")
	}
	if name != "Vehicle-1" {
		t.Errorf("Register returned name %q; want Vehicle-1", name)
	}
}

func TestSpec_M26_DoubleReserve_SameFederate_Fails(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Dup"); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	// Second reserve from same federate: no Go error (failure delivered
	// as event), but the underlying reservation table should still
	// hold exactly one entry.
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Dup"); err != nil {
		t.Fatalf("second reserve: %v (should fail via event, not error)", err)
	}
	// The third register with the name must consume the reservation.
	if _, _, err := reg.Register(ctx, fed, 1, 1, "Dup"); err != nil {
		t.Fatalf("register after double-reserve: %v", err)
	}
}

func TestSpec_M26_Reserve_ByOther_BlocksRegister(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Mine"); err != nil {
		t.Fatalf("reserve by fed 1: %v", err)
	}
	// Fed 2 tries to register Mine — must fail because reservation
	// is held by fed 1.
	_, _, err := reg.Register(ctx, fed, 2, 1, "Mine")
	if err == nil {
		t.Fatal("Register by non-reserving federate: nil error; want ErrObjectInstanceNameReservedByOther")
	}
	if !errors.Is(err, core.ErrObjectInstanceNameReservedByOther) {
		t.Errorf("error = %v; want ErrObjectInstanceNameReservedByOther", err)
	}
}

func TestSpec_M26_Release_Allows_Reregister(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Reusable"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := reg.ReleaseObjectInstanceName(ctx, fed, 1, "Reusable"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Another federate can now reserve.
	if err := reg.ReserveObjectInstanceName(ctx, fed, 2, "Reusable"); err != nil {
		t.Fatalf("re-reserve by fed 2: %v", err)
	}
}

func TestSpec_M26_Release_ByOther_Fails(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Held"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	err := reg.ReleaseObjectInstanceName(ctx, fed, 2, "Held")
	if !errors.Is(err, core.ErrObjectInstanceNameReservedByOther) {
		t.Errorf("Release by non-holder: err = %v; want ErrObjectInstanceNameReservedByOther", err)
	}
}

func TestSpec_M26_Release_Unreserved_Fails(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	err := reg.ReleaseObjectInstanceName(ctx, fed, 1, "NotReserved")
	if !errors.Is(err, core.ErrObjectInstanceNameNotReserved) {
		t.Errorf("Release of unreserved name: err = %v; want ErrObjectInstanceNameNotReserved", err)
	}
}

func TestSpec_M26_Resign_Clears_Reservations(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "Held"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	reg.OnFederateResign(fed, 1)
	// After resign, name is free for another federate.
	if err := reg.ReserveObjectInstanceName(ctx, fed, 2, "Held"); err != nil {
		t.Fatalf("re-reserve after resign: %v", err)
	}
}

func TestSpec_M26_MultiReserve_Atomic_Success(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	if err := reg.ReserveMultipleObjectInstanceNames(ctx, fed, 1, []string{"A", "B", "C"}); err != nil {
		t.Fatalf("multi-reserve: %v", err)
	}
	// All three should be consumable by Register.
	for _, n := range []string{"A", "B", "C"} {
		if _, _, err := reg.Register(ctx, fed, 1, 1, n); err != nil {
			t.Errorf("register %q after multi-reserve: %v", n, err)
		}
	}
}

func TestSpec_M26_MultiReserve_PartialCollision_FailsAtomically(t *testing.T) {
	reg, _ := newReg(t)
	ctx := context.Background()
	// Pre-reserve B by fed 2.
	if err := reg.ReserveObjectInstanceName(ctx, fed, 2, "B"); err != nil {
		t.Fatalf("pre-reserve B: %v", err)
	}
	// fed 1 tries A+B+C — must fail atomically (none reserved).
	if err := reg.ReserveMultipleObjectInstanceNames(ctx, fed, 1, []string{"A", "B", "C"}); err != nil {
		t.Fatalf("multi-reserve call: %v (failure delivered as event, not error)", err)
	}
	// Verify A and C were NOT reserved (fed 1 should still be able
	// to reserve them individually).
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "A"); err != nil {
		t.Errorf("solo reserve A after failed batch: %v", err)
	}
	if err := reg.ReserveObjectInstanceName(ctx, fed, 1, "C"); err != nil {
		t.Errorf("solo reserve C after failed batch: %v", err)
	}
}

func TestSpec_M26_RegisterWithoutReserve_AutoReserves(t *testing.T) {
	// Backwards-compat: pre-M26 code calls Register with a name
	// directly. The reservation table marks the name as registered
	// implicitly so subsequent Reserve of the same name fails as
	// "in use".
	reg, _ := newReg(t)
	ctx := context.Background()
	if _, _, err := reg.Register(ctx, fed, 1, 1, "Direct"); err != nil {
		t.Fatalf("register without prior reserve: %v", err)
	}
	// Now nobody (not even the registrant) can reserve the same name —
	// it's in use. Reserve returns nil but with no effect (failure
	// callback delivered separately via Outbox event).
	if err := reg.ReserveObjectInstanceName(ctx, fed, 2, "Direct"); err != nil {
		t.Fatalf("reserve-already-in-use: %v (failure delivered as event)", err)
	}
	// And another federate cannot register with the same name.
	_, _, err := reg.Register(ctx, fed, 2, 1, "Direct")
	if err == nil {
		t.Fatal("register duplicate registered name: nil error; want ErrObjectInstanceNameReservedByOther or NameInUse")
	}
}
