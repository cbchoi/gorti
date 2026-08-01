package ddm

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

func TestManager_Snapshot_RegionCount(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	rs, ok := mgr.LookupRoutingSpace("demo", "default")
	if !ok {
		t.Fatalf("LookupRoutingSpace: not ok")
	}
	dim, ok := mgr.LookupDimension("demo", rs, "x")
	if !ok {
		t.Fatalf("LookupDimension: not ok")
	}
	if _, err := mgr.CreateRegion(ctx, "demo", 1, rs, []DimensionHandle{dim}); err != nil {
		t.Fatalf("CreateRegion: %v", err)
	}
	if _, err := mgr.CreateRegion(ctx, "demo", 2, rs, []DimensionHandle{dim}); err != nil {
		t.Fatalf("CreateRegion 2: %v", err)
	}
	snap := mgr.Snapshot("demo")
	if snap.RegionCount != 2 {
		t.Errorf("RegionCount = %d, want 2", snap.RegionCount)
	}
}

func TestManager_Snapshot_UnknownFederation_ReturnsZero(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: &fakeOutbox{}, FOMs: permissiveFOMRepo{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap := mgr.Snapshot("nope")
	if snap.RegionCount != 0 {
		t.Errorf("Snapshot unknown.RegionCount = %d, want 0", snap.RegionCount)
	}
	_ = core.DDMSnapshot{}
}
