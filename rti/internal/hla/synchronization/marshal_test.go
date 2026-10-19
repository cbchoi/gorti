package sync

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// TestMarshalUnmarshal_RoundTripSyncState exercises M13 thread C
// (docs/srs.md §10.4): a fresh manager that consumes the bundled
// state from Marshal+Unmarshal must reproduce the identical
// per-(federation, label) record set.
func TestMarshalUnmarshal_RoundTripSyncState(t *testing.T) {
	t.Parallel()
	src, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New src: %v", err)
	}
	ctx := context.Background()
	if err := src.Register(ctx, "fed", "sp1", []byte("tag-1"),
		[]core.FederateHandle{1, 2, 3}); err != nil {
		t.Fatalf("Register sp1: %v", err)
	}
	if err := src.Register(ctx, "fed", "sp2", nil,
		[]core.FederateHandle{4, 5}); err != nil {
		t.Fatalf("Register sp2: %v", err)
	}
	// Achieve sp1 partially so the achieved set is non-empty.
	if err := src.Achieve(ctx, "fed", core.FederateHandle(1), "sp1"); err != nil {
		t.Fatalf("Achieve: %v", err)
	}

	stateA := src.ExportState("fed")
	if stateA == nil {
		t.Fatal("ExportState returned nil for non-empty federation")
	}

	dst, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New dst: %v", err)
	}
	dst.ImportState("fed", stateA)

	srcSnap := src.Snapshot("fed")
	dstSnap := dst.Snapshot("fed")
	if !reflect.DeepEqual(srcSnap, dstSnap) {
		t.Errorf("snapshot mismatch:\n src=%+v\n dst=%+v", srcSnap, dstSnap)
	}

	// Determinism check: Marshal must return byte-identical bytes
	// across calls with the same in-memory state.
	stateB := src.ExportState("fed")
	if !reflect.DeepEqual(stateA, stateB) {
		t.Errorf("ExportState not deterministic: %+v vs %+v", stateA, stateB)
	}
}

// TestMarshal_UnknownFederation_ReturnsNilBytes asserts that the
// snapshot of an unknown / never-seen federation is the empty marker
// — Unmarshal of an empty / nil slice is a silent no-op so this is
// the round-trip identity for a never-touched federation.
func TestMarshal_UnknownFederation_ReturnsNilBytes(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := mgr.ExportState("nope")
	if got != nil {
		t.Errorf("ExportState unknown = %+v, want nil", got)
	}
}

func TestUnmarshal_EmptyBytes_NoOp(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mgr.ImportState("nope", nil)
}
