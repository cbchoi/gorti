package ownership

import (
	"bytes"
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
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

	bytesA, err := src.Marshal("fed")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(bytesA) == 0 {
		t.Fatal("Marshal returned empty bytes for non-empty federation")
	}

	dst, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New dst: %v", err)
	}
	if err := dst.Unmarshal("fed", bytesA); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

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
	bytesB, err := src.Marshal("fed")
	if err != nil {
		t.Fatalf("Marshal #2: %v", err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Errorf("Marshal not deterministic: %x vs %x", bytesA, bytesB)
	}
}

func TestOwnershipMarshal_UnknownFederation_ReturnsNilBytes(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
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

func TestOwnershipUnmarshal_EmptyBytes_NoOp(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("nope", nil); err != nil {
		t.Errorf("Unmarshal nil: %v", err)
	}
}

func TestOwnershipUnmarshal_InvalidBytes_ReturnsError(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("fed", []byte{0xff, 0xff, 0xff}); err == nil {
		t.Error("Unmarshal of garbage: want error, got nil")
	}
}
