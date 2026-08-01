package ddm

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
	"github.com/cbchoi/gorti/rti/pkg/fom/model"
)

// fakeOutbox satisfies core.Outbox with a thread-safe slice sink.
type fakeOutbox struct {
	mu   sync.Mutex
	sent int
}

func (o *fakeOutbox) Send(_ context.Context, _ core.FederationName, _ core.FederateHandle, _ core.OutboundEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent++
	return nil
}

// permissiveFOMRepo mirrors the spec-test stub: any Get / Load returns
// a valid handle that does NOT implement DimensionEnumerator, so the
// DDM manager runs in permissive mode.
type permissiveFOMRepo struct{}

func (permissiveFOMRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

func (permissiveFOMRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return permissiveFOMHandle{}, nil
}

type permissiveFOMHandle struct{}

func (permissiveFOMHandle) IsValid() bool { return true }
func (permissiveFOMHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (permissiveFOMHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}

// fomFromModel wraps a *model.FOM so the test can drive the FOM-aware
// (non-permissive) path.
type fomBackedRepo struct{ fom *model.FOM }

func (r fomBackedRepo) Load(context.Context, []core.FOMModule) (core.FOMHandle, error) {
	return fomBackedHandle{fom: r.fom}, nil
}
func (r fomBackedRepo) Get(context.Context, core.FederationName) (core.FOMHandle, error) {
	return fomBackedHandle{fom: r.fom}, nil
}

type fomBackedHandle struct{ fom *model.FOM }

func (h fomBackedHandle) IsValid() bool { return h.fom != nil }
func (h fomBackedHandle) LookupObjectClass(string) (core.ObjectClassHandle, bool) {
	return 1, true
}
func (h fomBackedHandle) LookupInteractionClass(string) (core.InteractionClassHandle, bool) {
	return 1, true
}
func (h fomBackedHandle) LookupAttribute(core.ObjectClassHandle, string) (core.AttributeHandle, bool) {
	return 1, true
}
func (h fomBackedHandle) LookupParameter(core.InteractionClassHandle, string) (core.ParameterHandle, bool) {
	return 1, true
}
func (h fomBackedHandle) Dimensions() []model.Dimension { return h.fom.Dimensions() }

func newPermissiveManager(t *testing.T) *Manager {
	t.Helper()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return mgr
}

// TestNew_RequiredOptions exercises the constructor's nil-checks.
func TestNew_RequiredOptions(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Errorf("New with empty Options: want error, got nil")
	}
	if _, err := New(Options{Outbox: &fakeOutbox{}}); err == nil {
		t.Errorf("New with nil FOMs: want error, got nil")
	}
}

// TestPermissiveLookup verifies the lookup-mints-handles fallback
// used by the spec tests' permissive FOM stub.
func TestPermissiveLookup(t *testing.T) {
	mgr := newPermissiveManager(t)
	const fed core.FederationName = "perm"
	s1, ok := mgr.LookupRoutingSpace(fed, "GeoSpace")
	if !ok || s1 == 0 {
		t.Fatalf("LookupRoutingSpace = (%d, %v), want non-zero, true", s1, ok)
	}
	s2, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	if s1 != s2 {
		t.Errorf("repeat LookupRoutingSpace: got %d then %d (want stable)", s1, s2)
	}
	d1, ok := mgr.LookupDimension(fed, s1, "X")
	if !ok || d1 == 0 {
		t.Fatalf("LookupDimension = (%d, %v), want non-zero, true", d1, ok)
	}
	d2, _ := mgr.LookupDimension(fed, s1, "X")
	if d1 != d2 {
		t.Errorf("repeat LookupDimension: got %d then %d", d1, d2)
	}
}

// TestFOMBackedLookup verifies the FOM-aware path: dimensions declared
// in the model are visible; undeclared ones are not.
func TestFOMBackedLookup(t *testing.T) {
	fom := model.NewFOMWithDimensions(nil, nil, nil, []model.Dimension{
		{Name: "X", UpperBound: 1000},
		{Name: "Y", UpperBound: 500},
	})
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: fomBackedRepo{fom: fom}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const fed core.FederationName = "fom"
	space, ok := mgr.LookupRoutingSpace(fed, DefaultRoutingSpace)
	if !ok {
		t.Fatalf("LookupRoutingSpace(default): want ok")
	}
	dx, ok := mgr.LookupDimension(fed, space, "X")
	if !ok {
		t.Fatalf("LookupDimension(X): want ok")
	}
	dy, ok := mgr.LookupDimension(fed, space, "Y")
	if !ok {
		t.Fatalf("LookupDimension(Y): want ok")
	}
	if dx == dy {
		t.Errorf("X and Y produced identical handles %d", dx)
	}
	if _, ok := mgr.LookupDimension(fed, space, "Undeclared"); ok {
		t.Errorf("LookupDimension(Undeclared): want false in non-permissive mode")
	}
}

// TestRegionLifecycle exercises Create → SetRangeBounds → Commit →
// Query → Delete.
func TestRegionLifecycle(t *testing.T) {
	mgr := newPermissiveManager(t)
	const fed core.FederationName = "lifecycle"
	const owner core.FederateHandle = 1
	ctx := context.Background()

	space, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	dim, _ := mgr.LookupDimension(fed, space, "X")

	rh, err := mgr.CreateRegion(ctx, fed, owner, space, []DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}
	// Pre-commit: pending bounds not visible via QueryBounds.
	if err := mgr.SetRangeBounds(fed, owner, rh, dim, Range{Lower: 5, Upper: 9}); err != nil {
		t.Fatalf("SetRangeBounds: %v", err)
	}
	got, ok := mgr.QueryBounds(fed, rh, dim)
	if !ok {
		t.Fatalf("QueryBounds: want ok (committed-default range present)")
	}
	if got.Lower == 5 && got.Upper == 9 {
		t.Errorf("pre-commit QueryBounds = %+v; pending changes should not be visible", got)
	}

	if err := mgr.CommitRegionModifications(ctx, fed, owner, []RegionHandle{rh}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, _ = mgr.QueryBounds(fed, rh, dim)
	if got.Lower != 5 || got.Upper != 9 {
		t.Errorf("post-commit QueryBounds = %+v, want [5, 9)", got)
	}

	if err := mgr.DeleteRegion(ctx, fed, owner, rh); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := mgr.QueryBounds(fed, rh, dim); ok {
		t.Errorf("QueryBounds after Delete: want false")
	}
}

// TestRegionOwnership: SetRangeBounds + Delete by a non-owner are
// rejected with ErrRegionNotOwnedByFederate.
func TestRegionOwnership(t *testing.T) {
	mgr := newPermissiveManager(t)
	const fed core.FederationName = "own"
	const owner core.FederateHandle = 1
	const intruder core.FederateHandle = 2
	ctx := context.Background()
	space, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	dim, _ := mgr.LookupDimension(fed, space, "X")
	rh, err := mgr.CreateRegion(ctx, fed, owner, space, []DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}
	if err := mgr.SetRangeBounds(fed, intruder, rh, dim, Range{Lower: 0, Upper: 1}); !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
		t.Errorf("SetRangeBounds by intruder: want ErrRegionNotOwnedByFederate, got %v", err)
	}
	if err := mgr.DeleteRegion(ctx, fed, intruder, rh); !errors.Is(err, core.ErrRegionNotOwnedByFederate) {
		t.Errorf("DeleteRegion by intruder: want ErrRegionNotOwnedByFederate, got %v", err)
	}
}

// TestDeleteRegionInUse: deleting a region that's still referenced by
// a subscription is rejected.
func TestDeleteRegionInUse(t *testing.T) {
	mgr := newPermissiveManager(t)
	const fed core.FederationName = "inuse"
	const owner core.FederateHandle = 1
	ctx := context.Background()
	space, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	dim, _ := mgr.LookupDimension(fed, space, "X")
	rh, err := mgr.CreateRegion(ctx, fed, owner, space, []DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}
	if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, owner, 1,
		[]core.AttributeHandle{1}, []RegionHandle{rh}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := mgr.DeleteRegion(ctx, fed, owner, rh); !errors.Is(err, core.ErrRegionInUse) {
		t.Errorf("DeleteRegion in-use: want ErrRegionInUse, got %v", err)
	}
}

// TestSubscribersForUpdate_ZeroCost: the empty-publisher-regions path
// returns nil immediately (no map lookups, FR-DDM-6 fast path).
func TestSubscribersForUpdate_ZeroCost(t *testing.T) {
	mgr := newPermissiveManager(t)
	got := mgr.SubscribersForUpdate("any", 1, 1, nil)
	if got != nil {
		t.Errorf("SubscribersForUpdate(nil regions): want nil, got %v", got)
	}
}

// TestPublisherRegionsFor_NoAssociation: no association → nil result.
func TestPublisherRegionsFor_NoAssociation(t *testing.T) {
	mgr := newPermissiveManager(t)
	if got := mgr.PublisherRegionsFor("any", 99, 1); got != nil {
		t.Errorf("PublisherRegionsFor uninitialized: want nil, got %v", got)
	}
	if mgr.HasObjectAssociations("any", 99) {
		t.Errorf("HasObjectAssociations uninitialized: want false")
	}
}

// TestAssociateAndQueryPublisherRegions: round-trip a publisher
// association.
func TestAssociateAndQueryPublisherRegions(t *testing.T) {
	mgr := newPermissiveManager(t)
	const fed core.FederationName = "assoc"
	const owner core.FederateHandle = 1
	ctx := context.Background()
	space, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	dim, _ := mgr.LookupDimension(fed, space, "X")
	rh, err := mgr.CreateRegion(ctx, fed, owner, space, []DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}
	if err := mgr.AssociateRegionsWithObjectInstance(ctx, fed, owner, 42,
		map[core.AttributeHandle][]RegionHandle{1: {rh}}); err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if !mgr.HasObjectAssociations(fed, 42) {
		t.Errorf("HasObjectAssociations after Associate: want true")
	}
	got := mgr.PublisherRegionsFor(fed, 42, 1)
	if len(got) != 1 || got[0] != rh {
		t.Errorf("PublisherRegionsFor = %v, want [%d]", got, rh)
	}
}

// TestSubscribeReplaceSemantics: a second SubscribeWithRegions on the
// same (sub, cls, attr) replaces the previous region set rather than
// appending.
func TestSubscribeReplaceSemantics(t *testing.T) {
	mgr := newPermissiveManager(t)
	const fed core.FederationName = "replace"
	const owner core.FederateHandle = 1
	ctx := context.Background()
	space, _ := mgr.LookupRoutingSpace(fed, "GeoSpace")
	dim, _ := mgr.LookupDimension(fed, space, "X")

	r1, _ := mgr.CreateRegion(ctx, fed, owner, space, []DimensionHandle{dim})
	r2, _ := mgr.CreateRegion(ctx, fed, owner, space, []DimensionHandle{dim})
	_ = mgr.SetRangeBounds(fed, owner, r1, dim, Range{Lower: 0, Upper: 5})
	_ = mgr.SetRangeBounds(fed, owner, r2, dim, Range{Lower: 100, Upper: 200})
	_ = mgr.CommitRegionModifications(ctx, fed, owner, []RegionHandle{r1, r2})

	if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, owner, 1, []core.AttributeHandle{1}, []RegionHandle{r1}); err != nil {
		t.Fatalf("first Subscribe: %v", err)
	}
	// Now replace with r2 only.
	if err := mgr.SubscribeObjectClassAttributesWithRegions(ctx, fed, owner, 1, []core.AttributeHandle{1}, []RegionHandle{r2}); err != nil {
		t.Fatalf("replace Subscribe: %v", err)
	}
	// r1 should NOT be in use anymore — DeleteRegion succeeds.
	if err := mgr.DeleteRegion(ctx, fed, owner, r1); err != nil {
		t.Errorf("DeleteRegion r1 after replace: %v (replace should drop the prior subscription)", err)
	}
	// r2 still in use.
	if err := mgr.DeleteRegion(ctx, fed, owner, r2); !errors.Is(err, core.ErrRegionInUse) {
		t.Errorf("DeleteRegion r2 after replace: want ErrRegionInUse, got %v", err)
	}
}

// TestRangeOverlap_Basics is a unit-test for the closed-open Overlap
// helper. (The rti/spec/M10/ddm_test.go covers the canonical cases;
// the lines below pin a few extras: empty range, max bounds.)
func TestRangeOverlap_Basics(t *testing.T) {
	cases := []struct {
		a, b Range
		want bool
	}{
		{Range{Lower: 0, Upper: 100}, Range{Lower: 50, Upper: 200}, true},
		{Range{Lower: 0, Upper: 100}, Range{Lower: 100, Upper: 200}, false}, // touch but don't overlap
		{Range{Lower: 0, Upper: 0}, Range{Lower: 0, Upper: 10}, false},      // empty range
		{Range{Lower: ^uint64(0) - 1, Upper: ^uint64(0)}, Range{Lower: 0, Upper: ^uint64(0)}, true},
	}
	for _, tc := range cases {
		if got := tc.a.Overlap(tc.b); got != tc.want {
			t.Errorf("%v.Overlap(%v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
