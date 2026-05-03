package m8spec

import (
	"context"
	"errors"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	ownpkg "github.com/cbchoi/gorti/rti/internal/ownership"
)

// newTestOwnershipManager builds an ownership.Manager. Returns nil on
// stub state so tests skip cleanly until M8 W1 lands.
func newTestOwnershipManager(t *testing.T) (*ownpkg.Manager, *fakeOutbox, *permissiveEventLog) {
	t.Helper()
	outbox := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := ownpkg.New(ownpkg.Options{
		Outbox:   outbox,
		EventLog: log,
	})
	if err != nil {
		t.Logf("ownership.New returned: %v (expected during pre-dispatch)", err)
	}
	return mgr, outbox, log
}

// TestSpec_M8_Ownership_QueryAfterRegister: after a federate registers
// an object instance, it owns all the attributes it published. (Cut 1
// already implements this implicitly via the object registry; M8
// promotes ownership to a first-class queryable concept.)
//
// Wiring choice (Agent A, M8 W1): ownership.Manager owns its own state
// and exposes RegisterInitialOwnership for the wiring layer (cmd/rtid
// composition) to call after object.Registry.Register succeeds. The
// spec test exercises that surface directly.
//
// Implements: FR-OWN-5.
func TestSpec_M8_Ownership_QueryAfterRegister(t *testing.T) {
	mgr, _, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired (M8 RED state)")
	}
	mgr.RegisterInitialOwnership("fed", 7, 100, []core.AttributeHandle{1, 2, 3})

	for _, attr := range []core.AttributeHandle{1, 2, 3} {
		owner, ok := mgr.QueryOwnership("fed", 100, attr)
		if !ok {
			t.Errorf("QueryOwnership(100, %d) ok = false; want true after RegisterInitialOwnership", attr)
			continue
		}
		if owner != 7 {
			t.Errorf("QueryOwnership(100, %d) owner = %d; want 7", attr, owner)
		}
		if !mgr.IsOwnedBy("fed", 7, 100, attr) {
			t.Errorf("IsOwnedBy(7, 100, %d) = false; want true", attr)
		}
	}
}

// TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption: after
// the owner calls NegotiatedDivest, all subscribers receive an
// announce-assumption callback. (FR-OWN-2 phase 1.)
//
// Wiring choice (Agent A, M8 W1): the M8 ownership.Manager exposes a
// SubscribersResolver hook in Options; the spec test wires a fake
// resolver that returns federates {2, 3} as subscribers to the
// object's class. After NegotiatedDivest, the outbox MUST hold one
// assumption envelope per subscriber (excluding the owner).
//
// Implements: FR-OWN-2.
func TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption(t *testing.T) {
	outbox := newFakeOutbox()
	log := newPermissiveEventLog()
	mgr, err := ownpkg.New(ownpkg.Options{
		Outbox:   outbox,
		EventLog: log,
		Subscribers: func(_ context.Context, _ core.FederationName, _ core.ObjectHandle, _ []core.AttributeHandle) []core.FederateHandle {
			return []core.FederateHandle{2, 3}
		},
	})
	if err != nil {
		if errors.Is(err, ownpkg.ErrNotImplemented) {
			t.Skip("ownership.New not yet implemented")
		}
		t.Fatalf("ownership.New: %v", err)
	}
	mgr.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})

	pre := len(outbox.Sent())
	if err := mgr.NegotiatedDivest(context.Background(), "fed", 1, 100, []core.AttributeHandle{1}, []byte("tag")); err != nil {
		if errors.Is(err, ownpkg.ErrNotImplemented) {
			t.Skip("NegotiatedDivest not yet implemented (M8 RED state)")
		}
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	post := len(outbox.Sent())
	if post-pre != 2 {
		t.Errorf("outbox emissions after divest = %d; want 2 (one per subscriber)", post-pre)
	}
	// Verify recipients are exactly {2, 3} (owner excluded).
	got := map[core.FederateHandle]int{}
	for _, rec := range outbox.Sent()[pre:post] {
		got[rec.Federate]++
	}
	if got[1] != 0 {
		t.Errorf("owner federate 1 received an assumption envelope; want 0")
	}
	if got[2] != 1 || got[3] != 1 {
		t.Errorf("recipient histogram = %v; want {2:1, 3:1}", got)
	}
}

// TestSpec_M8_Ownership_AcquireAfterDivest_TransfersOwnership: when a
// subscriber Acquires after the owner NegotiatedDivests, ownership
// transitions to the acquirer. Both parties get callbacks (one
// divestiture-notification to the prior owner, one
// acquisition-notification to the acquirer).
//
// Wiring choice (Agent A, M8 W1): no Subscribers resolver — fan-out
// to subscribers is independently exercised by
// TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption. This
// test focuses on the ownership-state transition + the two completion
// callbacks emitted by the manager when Acquire fires after a pending
// divest.
//
// Implements: FR-OWN-2, FR-OWN-4.
func TestSpec_M8_Ownership_AcquireAfterDivest_TransfersOwnership(t *testing.T) {
	mgr, outbox, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired")
	}
	mgr.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1})

	ctx := context.Background()
	if err := mgr.NegotiatedDivest(ctx, "fed", 1, 100, []core.AttributeHandle{1}, nil); err != nil {
		if errors.Is(err, ownpkg.ErrNotImplemented) {
			t.Skip("NegotiatedDivest not yet implemented")
		}
		t.Fatalf("NegotiatedDivest: %v", err)
	}
	pre := len(outbox.Sent())
	if err := mgr.Acquire(ctx, "fed", 2, 100, []core.AttributeHandle{1}, nil); err != nil {
		if errors.Is(err, ownpkg.ErrNotImplemented) {
			t.Skip("Acquire not yet implemented")
		}
		t.Fatalf("Acquire: %v", err)
	}
	owner, ok := mgr.QueryOwnership("fed", 100, 1)
	if !ok || owner != 2 {
		t.Errorf("post-Acquire owner = (%d, %v); want (2, true)", owner, ok)
	}
	if !mgr.IsOwnedBy("fed", 2, 100, 1) {
		t.Errorf("IsOwnedBy(2, 100, 1) = false; want true")
	}
	if mgr.IsOwnedBy("fed", 1, 100, 1) {
		t.Errorf("IsOwnedBy(1, 100, 1) = true; want false (federate 1 has divested)")
	}
	post := len(outbox.Sent())
	if post-pre < 2 {
		t.Errorf("outbox emissions after Acquire = %d; want >= 2 (divestiture + acquisition notifications)", post-pre)
	}
	// Verify both parties were notified.
	got := map[core.FederateHandle]int{}
	for _, rec := range outbox.Sent()[pre:post] {
		got[rec.Federate]++
	}
	if got[1] == 0 {
		t.Errorf("prior owner federate 1 received no notification; want >= 1")
	}
	if got[2] == 0 {
		t.Errorf("new owner federate 2 received no notification; want >= 1")
	}
}

// TestSpec_M8_Ownership_DivestNotOwner_Rejected: a federate that
// doesn't currently own the attribute cannot divest it.
//
// Implements: FR-OWN-2.
func TestSpec_M8_Ownership_DivestNotOwner_Rejected(t *testing.T) {
	mgr, _, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired")
	}
	// Federate 99 is not the owner.
	err := mgr.NegotiatedDivest(context.Background(), "fed", 99, 100, []core.AttributeHandle{1}, nil)
	if errors.Is(err, ownpkg.ErrNotImplemented) {
		t.Skip()
	}
	if !errors.Is(err, core.ErrAttributeNotOwned) {
		t.Errorf("divest-not-owner: err = %v, want ErrAttributeNotOwned", err)
	}
}

// TestSpec_M8_Ownership_QueryUnowned_ReturnsZeroFalse: querying
// ownership of an unknown (obj, attr) returns (0, false).
//
// Implements: FR-OWN-5.
func TestSpec_M8_Ownership_QueryUnowned_ReturnsZeroFalse(t *testing.T) {
	mgr, _, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired")
	}
	owner, ok := mgr.QueryOwnership("fed", 999, 999)
	if owner != 0 || ok {
		t.Errorf("QueryOwnership(unknown) = (%d, %v), want (0, false)", owner, ok)
	}
}
