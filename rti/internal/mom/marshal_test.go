package mom

import (
	"bytes"
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/core"
)

// TestMarshalUnmarshal_RoundTripMOM exercises M13 thread C
// (docs/srs.md §10.4): MOM state survives a Marshal → fresh manager →
// Unmarshal round-trip. Federation singleton + per-federate counters
// + federateType + time-state are all preserved.
func TestMarshalUnmarshal_RoundTripMOM(t *testing.T) {
	t.Parallel()
	src, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New src: %v", err)
	}
	ctx := context.Background()
	if err := src.FederationCreated(ctx, "demo", []core.FOMModule{
		{Path: "a.xml"},
		{Path: "b.xml"},
	}); err != nil {
		t.Fatalf("FederationCreated: %v", err)
	}
	if err := src.FederateJoined(ctx, "demo", 1, "alpha", "Sensor"); err != nil {
		t.Fatalf("FederateJoined alpha: %v", err)
	}
	if err := src.FederateJoined(ctx, "demo", 2, "beta", "Actuator"); err != nil {
		t.Fatalf("FederateJoined beta: %v", err)
	}
	src.IncrementUpdatesSent("demo", 1)
	src.IncrementUpdatesSent("demo", 1)
	src.IncrementInteractionsSent("demo", 2)
	if err := src.TimeStateChanged(ctx, "demo", 1, true, false, 0.5, 7.25); err != nil {
		t.Fatalf("TimeStateChanged: %v", err)
	}

	bytesA, err := src.Marshal("demo")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if len(bytesA) == 0 {
		t.Fatal("Marshal returned empty bytes")
	}

	dst, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New dst: %v", err)
	}
	if err := dst.Unmarshal("demo", bytesA); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	srcSnap := src.Snapshot("demo")
	dstSnap := dst.Snapshot("demo")
	if len(srcSnap.PerFederate) != len(dstSnap.PerFederate) {
		t.Fatalf("PerFederate len mismatch: src=%d dst=%d",
			len(srcSnap.PerFederate), len(dstSnap.PerFederate))
	}
	for h, srcCounters := range srcSnap.PerFederate {
		dstCounters, ok := dstSnap.PerFederate[h]
		if !ok {
			t.Fatalf("dst missing federate %d", h)
		}
		if srcCounters != dstCounters {
			t.Errorf("counters mismatch for federate %d: src=%+v dst=%+v",
				h, srcCounters, dstCounters)
		}
	}

	// QueryFederate must reflect the restored federate-type + time-state.
	srcAttrs, _ := src.QueryFederate("demo", 1)
	dstAttrs, _ := dst.QueryFederate("demo", 1)
	if srcAttrs != dstAttrs {
		t.Errorf("QueryFederate(1) mismatch: src=%+v dst=%+v", srcAttrs, dstAttrs)
	}

	// HLAfederation singleton attributes preserved.
	srcFed, _ := src.QueryFederation("demo")
	dstFed, _ := dst.QueryFederation("demo")
	if srcFed.Name != dstFed.Name {
		t.Errorf("HLAfederation name mismatch: src=%q dst=%q", srcFed.Name, dstFed.Name)
	}
	if len(srcFed.FederateHandles) != len(dstFed.FederateHandles) {
		t.Errorf("FederateHandles len mismatch: src=%v dst=%v",
			srcFed.FederateHandles, dstFed.FederateHandles)
	}
	if len(srcFed.FOMModuleNames) != len(dstFed.FOMModuleNames) {
		t.Errorf("FOMModuleNames mismatch: src=%v dst=%v",
			srcFed.FOMModuleNames, dstFed.FOMModuleNames)
	}

	// Determinism check.
	bytesB, err := src.Marshal("demo")
	if err != nil {
		t.Fatalf("Marshal #2: %v", err)
	}
	if !bytes.Equal(bytesA, bytesB) {
		t.Errorf("Marshal not deterministic: %x vs %x", bytesA, bytesB)
	}
}

func TestMOMMarshal_UnknownFederation_ReturnsNilBytes(t *testing.T) {
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

func TestMOMUnmarshal_EmptyBytes_NoOp(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("nope", nil); err != nil {
		t.Errorf("Unmarshal nil: %v", err)
	}
}

func TestMOMUnmarshal_InvalidBytes_ReturnsError(t *testing.T) {
	t.Parallel()
	mgr, err := New(Options{Outbox: snapFakeOutbox{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := mgr.Unmarshal("fed", []byte{0xff, 0xff, 0xff}); err == nil {
		t.Error("Unmarshal of garbage: want error, got nil")
	}
}
