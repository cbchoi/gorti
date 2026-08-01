package m2spec

import (
	"context"
	"sort"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/internal/declaration"
)

// TestSpec_M2_Declaration_PublishObjectClass_Records: a published
// (federate, class, attr) shows up in PublishersFor.
//
// Implements: FR-DM-1.
func TestSpec_M2_Declaration_PublishObjectClass_Records(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	if err := mgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2, 3}); err != nil {
		t.Skipf("PublishObjectClassAttributes not yet implemented: %v", err)
	}

	pubs := mgr.PublishersFor(ctx, "fed", 7, 2)
	if len(pubs) != 1 || pubs[0] != 1 {
		t.Errorf("PublishersFor(fed,7,2) = %v, want [1]", pubs)
	}
}

// TestSpec_M2_Declaration_SubscribersFor_DeterministicOrder: subscribers
// returned by SubscribersFor are in sorted handle order regardless of
// subscribe call order.
//
// Implements: FR-DM-1, FR-DM-3, NFR-DET-1.
func TestSpec_M2_Declaration_SubscribersFor_DeterministicOrder(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	// Subscribe in REVERSE handle order. SubscribersFor must still return
	// them sorted ascending.
	for _, h := range []core.FederateHandle{7, 3, 11, 1, 5} {
		if err := mgr.SubscribeObjectClassAttributes(ctx, "fed", h, 9, []core.AttributeHandle{2}); err != nil {
			t.Skipf("SubscribeObjectClassAttributes not yet implemented: %v", err)
		}
	}

	got := mgr.SubscribersFor(ctx, "fed", 9, []core.AttributeHandle{2})
	want := []core.FederateHandle{1, 3, 5, 7, 11}
	if len(got) != len(want) {
		t.Fatalf("SubscribersFor returned %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
		}
	}
}

// TestSpec_M2_Declaration_SubscribersFor_AnyMatchingAttribute: subscribing
// to attribute 2 should make the federate appear in SubscribersFor for ANY
// query that includes 2.
//
// Implements: FR-DM-1.
func TestSpec_M2_Declaration_SubscribersFor_AnyMatchingAttribute(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	_ = mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2})

	// A subsequent query that includes attribute 2 plus others must
	// return federate 5.
	got := mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{2, 3, 4})
	if len(got) != 1 || got[0] != 5 {
		t.Errorf("SubscribersFor with superset attrs = %v, want [5]", got)
	}

	// A query with attributes that don't include 2 should NOT return federate 5.
	got = mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{3, 4})
	if len(got) != 0 {
		t.Errorf("SubscribersFor with disjoint attrs = %v, want []", got)
	}
}

// TestSpec_M2_Declaration_Unsubscribe_Removes: after unsubscribe, the
// federate no longer appears in SubscribersFor.
//
// Implements: FR-DM-1.
func TestSpec_M2_Declaration_Unsubscribe_Removes(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	_ = mgr.SubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2})
	_ = mgr.UnsubscribeObjectClassAttributes(ctx, "fed", 5, 7, []core.AttributeHandle{2})

	got := mgr.SubscribersFor(ctx, "fed", 7, []core.AttributeHandle{2})
	if len(got) != 0 {
		t.Errorf("SubscribersFor after unsub = %v, want []", got)
	}
}

// TestSpec_M2_Declaration_InteractionPubSub: symmetric pub/sub for
// interaction classes works the same way as object classes, with the
// same deterministic-order guarantee.
//
// Implements: FR-DM-2, NFR-DET-1.
func TestSpec_M2_Declaration_InteractionPubSub(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	if err := mgr.PublishInteractionClass(ctx, "fed", 4, 11); err != nil {
		t.Skipf("PublishInteractionClass not yet implemented: %v", err)
	}
	for _, h := range []core.FederateHandle{8, 2, 6} {
		_ = mgr.SubscribeInteractionClass(ctx, "fed", h, 11)
	}

	pubs := mgr.InteractionPublishersFor(ctx, "fed", 11)
	if len(pubs) != 1 || pubs[0] != 4 {
		t.Errorf("InteractionPublishersFor = %v, want [4]", pubs)
	}

	got := mgr.InteractionSubscribersFor(ctx, "fed", 11)
	want := []core.FederateHandle{2, 6, 8}
	if len(got) != len(want) {
		t.Fatalf("InteractionSubscribersFor = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %d want %d", i, got[i], want[i])
		}
	}
}

// TestSpec_M2_Declaration_PerFederationIsolation: state in one federation
// must not leak into another.
//
// Implements: FR-DM-1.
func TestSpec_M2_Declaration_PerFederationIsolation(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	_ = mgr.SubscribeObjectClassAttributes(ctx, "fedA", 5, 7, []core.AttributeHandle{2})

	got := mgr.SubscribersFor(ctx, "fedB", 7, []core.AttributeHandle{2})
	if len(got) != 0 {
		t.Errorf("SubscribersFor(fedB) leaked from fedA: got %v", got)
	}
}

// TestSpec_M2_Declaration_Idempotent: repeated publish of the same
// (federate, class, attr) does not duplicate.
//
// Implements: FR-DM-1.
func TestSpec_M2_Declaration_Idempotent(t *testing.T) {
	mgr := declaration.New()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_ = mgr.PublishObjectClassAttributes(ctx, "fed", 1, 7, []core.AttributeHandle{2})
	}
	pubs := mgr.PublishersFor(ctx, "fed", 7, 2)
	// Sort defensively in case implementation returns unsorted on
	// single-element results — but assertion is uniqueness.
	sort.Slice(pubs, func(i, j int) bool { return pubs[i] < pubs[j] })
	if len(pubs) != 1 {
		t.Errorf("PublishersFor after 3 idempotent pubs = %v, want length 1", pubs)
	}
}
