package sync

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
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
	if !reflect.DeepEqual(srcSnap, dstSnap) {
		t.Errorf("snapshot mismatch:\n src=%+v\n dst=%+v", srcSnap, dstSnap)
	}

	// Determinism check: Marshal must return byte-identical bytes
	// across calls with the same in-memory state.
	bytesB, err := src.Marshal("fed")
	if err != nil {
		t.Fatalf("Marshal #2: %v", err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Errorf("Marshal not deterministic: %x vs %x", bytesA, bytesB)
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
	got, err := mgr.Marshal("nope")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got != nil {
		t.Errorf("Marshal unknown = %x, want nil", got)
	}
}

func TestUnmarshal_EmptyBytes_NoOp(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("nope", nil); err != nil {
		t.Errorf("Unmarshal nil: %v", err)
	}
}

func TestUnmarshal_InvalidBytes_ReturnsError(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("fed", []byte{0xff, 0xff, 0xff}); err == nil {
		t.Error("Unmarshal of garbage: want error, got nil")
	}
}
