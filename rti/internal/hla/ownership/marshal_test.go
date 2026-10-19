package ownership

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

// TestMarshalUnmarshal_RoundTripOwnership exercises M13 thread C
// (docs/srs.md §10.4): ownership state survives a Marshal → fresh
// manager → Unmarshal round-trip — including pending divests, owned
// attribute records, and the resulting Snapshot counters.
func TestMarshalUnmarshal_RoundTripOwnership(t *testing.T) {
	t.Parallel()
	src, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New src: %v", err)
	}
	src.RegisterInitialOwnership("fed", 1, 100, []core.AttributeHandle{1, 2, 3})
	src.RegisterInitialOwnership("fed", 2, 200, []core.AttributeHandle{1})
	if err := src.NegotiatedDivest(context.Background(), "fed", 1, 100,
		[]core.AttributeHandle{1}, []byte("divest-tag")); err != nil {
		t.Fatalf("NegotiatedDivest: %v", err)
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
	if srcSnap != dstSnap {
		t.Errorf("Snapshot mismatch: src=%+v dst=%+v", srcSnap, dstSnap)
	}
	// Spot-check ownership query through the API.
	if owner, ok := dst.QueryOwnership("fed", 100, 2); !ok || owner != 1 {
		t.Errorf("QueryOwnership(100, 2) = (%v, %v), want (1, true)", owner, ok)
	}
	if owner, ok := dst.QueryOwnership("fed", 200, 1); !ok || owner != 2 {
		t.Errorf("QueryOwnership(200, 1) = (%v, %v), want (2, true)", owner, ok)
	}

	// Determinism check.
	stateB := src.ExportState("fed")
	if !reflect.DeepEqual(stateA, stateB) {
		t.Errorf("ExportState not deterministic: %+v vs %+v", stateA, stateB)
	}
}

func TestOwnershipMarshal_UnknownFederation_ReturnsNilBytes(t *testing.T) {
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

func TestOwnershipUnmarshal_EmptyBytes_NoOp(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mgr.ImportState("nope", nil)
}
