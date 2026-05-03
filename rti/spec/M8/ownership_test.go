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
// Implements: FR-OWN-5.
func TestSpec_M8_Ownership_QueryAfterRegister(t *testing.T) {
	mgr, _, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired (M8 RED state)")
	}
	// Cut-1 path: object.Registry tells the ownership manager about
	// new instances. For the spec test, we assume the ownership
	// manager has been informed via whatever wiring Agent A chose.
	// Until that wiring lands, this test skips.
	t.Skip("requires object.Registry → ownership.Manager wiring; Agent A unskips in M8 W1")
}

// TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption: after
// the owner calls NegotiatedDivest, all subscribers receive an
// announce-assumption callback. (FR-OWN-2 phase 1.)
//
// SCAFFOLD pending the object/declaration/ownership wiring.
//
// Implements: FR-OWN-2.
func TestSpec_M8_Ownership_NegotiatedDivest_AnnouncesAssumption(t *testing.T) {
	mgr, _, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired")
	}
	err := mgr.NegotiatedDivest(context.Background(), "fed", 1, 100, []core.AttributeHandle{1}, []byte("tag"))
	if errors.Is(err, ownpkg.ErrNotImplemented) {
		t.Skip("NegotiatedDivest not yet implemented (M8 RED state)")
	}
	// Real test body Agent A wires post-implementation: assert that
	// outbox has an assume-attribute-ownership event for each
	// subscriber of the class+attribute pair.
	t.Skip("Agent A wires the assume-callback assertion in M8 W1")
}

// TestSpec_M8_Ownership_AcquireAfterDivest_TransfersOwnership: when a
// subscriber Acquires after the owner NegotiatedDivests, ownership
// transitions to the acquirer. Both parties get callbacks.
//
// SCAFFOLD.
//
// Implements: FR-OWN-2, FR-OWN-4.
func TestSpec_M8_Ownership_AcquireAfterDivest_TransfersOwnership(t *testing.T) {
	mgr, _, _ := newTestOwnershipManager(t)
	if mgr == nil {
		t.Skip("ownership.Manager not yet wired")
	}
	err := mgr.NegotiatedDivest(context.Background(), "fed", 1, 100, []core.AttributeHandle{1}, nil)
	if errors.Is(err, ownpkg.ErrNotImplemented) {
		t.Skip("NegotiatedDivest not yet implemented")
	}
	t.Skip("Agent A wires the transfer-and-callback assertion in M8 W1")
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
