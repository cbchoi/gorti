package ddm

import (
	"bytes"
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestMarshalUnmarshal_RoundTripDDM exercises M13 thread C
// (docs/srs.md §10.4): runtime DDM state survives a Marshal → fresh
// manager → Unmarshal round-trip. Setup-time configuration
// (routing-space + dimension declarations) is reconstructed by the
// FOM/permissive lookup at restore-bootstrap, so we exercise the same
// permissive FOM in both source + destination managers.
func TestMarshalUnmarshal_RoundTripDDM(t *testing.T) {
	t.Parallel()
	src, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New src: %v", err)
	}
	ctx := context.Background()
	rs, _ := src.LookupRoutingSpace("fed", "default")
	dim, _ := src.LookupDimension("fed", rs, "x")
	r1, err := src.CreateRegion(ctx, "fed", 1, rs, []DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion 1: %v", err)
	}
	r2, err := src.CreateRegion(ctx, "fed", 2, rs, []DimensionHandle{dim})
	if err != nil {
		t.Fatalf("CreateRegion 2: %v", err)
	}
	if err := src.SetRangeBounds("fed", 1, r1, dim, Range{Lower: 10, Upper: 20}); err != nil {
		t.Fatalf("SetRangeBounds: %v", err)
	}
	if err := src.CommitRegionModifications(ctx, "fed", 1, []RegionHandle{r1}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := src.SubscribeObjectClassAttributesWithRegions(ctx, "fed", 5, 10,
		[]core.AttributeHandle{1, 2}, []RegionHandle{r1, r2}); err != nil {
		t.Fatalf("SubscribeOCA: %v", err)
	}
	// Use only regions owned by federate 1 so the publisher
	// association is accepted (r1 was created by federate 1, r2 by
	// federate 2).
	if err := src.AssociateRegionsWithObjectInstance(ctx, "fed", 1, 100,
		map[core.AttributeHandle][]RegionHandle{
			1: {r1},
			2: {r1},
		}); err != nil {
		t.Fatalf("AssociateRegions: %v", err)
	}
	_ = r2

	bytesA, err := src.Marshal("fed")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(bytesA) == 0 {
		t.Fatal("Marshal returned empty bytes")
	}

	dst, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New dst: %v", err)
	}
	// Pre-populate dst's routing-space tables the same way src had
	// (in production, this happens via CreateFederation reaching the
	// FOM); we only need the permissive fallback here.
	_, _ = dst.LookupRoutingSpace("fed", "default")
	_, _ = dst.LookupDimension("fed", rs, "x")

	if err := dst.Unmarshal("fed", bytesA); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	srcSnap := src.Snapshot("fed")
	dstSnap := dst.Snapshot("fed")
	if srcSnap.RegionCount != dstSnap.RegionCount {
		t.Errorf("RegionCount mismatch: src=%d dst=%d",
			srcSnap.RegionCount, dstSnap.RegionCount)
	}
	// QueryBounds must reflect the committed bounds we set.
	bounds, ok := dst.QueryBounds("fed", r1, dim)
	if !ok {
		t.Errorf("QueryBounds: not ok")
	}
	if bounds.Lower != 10 || bounds.Upper != 20 {
		t.Errorf("Bounds = %+v, want {10, 20}", bounds)
	}

	// PublisherRegionsFor must reflect the associations.
	if rs := dst.PublisherRegionsFor("fed", 100, 1); len(rs) != 1 || rs[0] != r1 {
		t.Errorf("PublisherRegionsFor(100,1) = %v, want [%d]", rs, r1)
	}

	// Determinism check.
	bytesB, err := src.Marshal("fed")
	if err != nil {
		t.Fatalf("Marshal #2: %v", err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Errorf("Marshal not deterministic: %x vs %x", bytesA, bytesB)
	}
}

func TestDDMMarshal_UnknownFederation_ReturnsNilBytes(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := mgr.Marshal("nope")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got != nil {
		t.Errorf("Marshal unknown = %x, want nil", got)
	}
}

func TestDDMUnmarshal_EmptyBytes_NoOp(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("nope", nil); err != nil {
		t.Errorf("Unmarshal nil: %v", err)
	}
}

func TestDDMUnmarshal_InvalidBytes_ReturnsError(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("fed", []byte{0xff, 0xff, 0xff}); err == nil {
		t.Error("Unmarshal of garbage: want error, got nil")
	}
}
