package object

import (
	"context"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
)

func TestIEEE2010ImmediateROPreservesTSORetractionMetadata(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "attribute", true: testRemove}[remove], func(t *testing.T) {
			ctx := context.Background()
			r, tm, out, obj := liveDeletionFixture(t, 2)
			if err := tm.DisableConstrained(ctx, "fed", 2); err != nil {
				t.Fatal(err)
			}
			ts := core.LogicalTime(10)
			opts := core.ObjectMessageOptions{RetractionHandle: 401, Tag: []byte{0, 255}}
			wantKind := CallbackReflect
			var err error
			if remove {
				wantKind = CallbackRemove
				err = r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, opts)
			} else {
				err = r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &ts, opts)
			}
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, record := range out.records {
				event, ok := record.Event.(*CallbackEvent)
				if !ok || event.Kind != wantKind {
					continue
				}
				found++
				if event.Metadata == nil || event.Metadata.RetractionHandle != 401 || event.Metadata.ReceivedOrder != core.OrderReceive || event.Metadata.SentOrder != core.OrderTimestamp {
					t.Fatalf("TSO sent/RO received metadata=%+v", event.Metadata)
				}
			}
			if found != 1 {
				t.Fatalf("received %d callbacks, want one", found)
			}
		})
	}
}

func TestIEEE2010RegulatingOnlyDeleteMarkerRetiresAtGrant(t *testing.T) {
	ctx := context.Background()
	r, tm, out, obj := liveDeletionFixture(t)
	if err := tm.DisableConstrained(ctx, "fed", 1); err != nil {
		t.Fatal(err)
	}
	ts := core.LogicalTime(10)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &ts, core.ObjectMessageOptions{RetractionHandle: 402}); err != nil {
		t.Fatal(err)
	}
	if r.stateFor("fed").instances[obj] == nil {
		t.Fatal("object retired before the deletion timestamp")
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, ts); err != nil {
		t.Fatal(err)
	}
	if r.stateFor("fed").instances[obj] != nil {
		t.Fatal("object still live after grant at its deletion timestamp")
	}
	if len(out.records) != 1 {
		t.Fatalf("internal delete marker leaked as a callback: %v", out.records)
	}
}
