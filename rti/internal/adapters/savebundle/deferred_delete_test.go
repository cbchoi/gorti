package savebundle

import (
	"context"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"github.com/cbchoi/gorti/rti/internal/hla/object"
)

func TestDeferredDeletionSnapshotRestoresLifecycleAndMarker(t *testing.T) {
	for _, kind := range []object.CallbackKind{object.CallbackRemove, object.CallbackDeleteMarker} {
		obj := objectStateCodec(t)
		tm, _, out := newTimeStateCodec(t)
		tm.Manager.SetDeletionLifecycle(obj.Manager)
		state := codecObjectState()
		handle := state.Objects[0].Handle
		state.Deletions = []object.DeletionState{{Object: handle, Producer: 7, Timestamp: 10, RetractionHandle: 123}}
		commit, err := obj.Manager.PrepareImportState("fed", state)
		if err != nil {
			t.Fatal(err)
		}
		commit()
		ts := core.LogicalTime(10)
		event := &object.CallbackEvent{Kind: kind, Object: handle, LogicalTime: &ts, UserSuppliedTag: []byte{0, 255}, Metadata: &core.ObjectCallbackMetadata{Producer: 7, RetractionHandle: 123, SentOrder: core.OrderTimestamp}}
		ctx := context.Background()
		destination := core.FederateHandle(2)
		if kind == object.CallbackDeleteMarker {
			destination = 7
		}
		if err := tm.Manager.EnableConstrained(ctx, "fed", destination); err != nil {
			t.Fatal(err)
		}
		if err := tm.Manager.BufferTSOWithRetraction(ctx, "fed", destination, 10, event, 7, 123); err != nil {
			t.Fatal(err)
		}
		objectData, err := obj.Marshal("fed")
		if err != nil {
			t.Fatal(err)
		}
		timeData, err := tm.Marshal("fed")
		if err != nil {
			t.Fatal(err)
		}
		if err := tm.Manager.TimeAdvanceRequest(ctx, "fed", destination, 10); err != nil {
			t.Fatal(err)
		}
		wantLive := kind == object.CallbackRemove
		if (len(obj.Manager.ExportState("fed").Objects) != 0) != wantLive {
			t.Fatal("only the sender marker may finalize the instance")
		}
		if err := obj.Unmarshal("fed", objectData); err != nil {
			t.Fatal(err)
		}
		if err := tm.Unmarshal("fed", timeData); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(obj.Manager.ExportState("fed"), state) {
			t.Fatal("pending deletion snapshot changed")
		}
		out.sent = nil
		if err := tm.Manager.TimeAdvanceRequest(ctx, "fed", destination, 10); err != nil {
			t.Fatal(err)
		}
		got := obj.Manager.ExportState("fed")
		if (len(got.Objects) != 0) != wantLive || len(got.Deletions) != 1 || got.Deletions[0].Committed == wantLive {
			t.Fatalf("restored deletion did not finalize: %+v", got)
		}
		want := 2
		if kind == object.CallbackDeleteMarker {
			want = 1
		}
		if len(out.sent) != want {
			t.Fatalf("wire callbacks=%d want%d", len(out.sent), want)
		}
		if kind == object.CallbackRemove && out.sent[0].event.(*object.CallbackEvent).Metadata.RetractionHandle != 123 {
			t.Fatal("restored deletion lost retraction identity")
		}
	}
}

func TestDeferredDeletionSnapshotRejectsDanglingPendingAndClones(t *testing.T) {
	s := objectStateCodec(t)
	state := codecObjectState()
	state.Deletions = []object.DeletionState{{Object: state.Objects[0].Handle + 1, Producer: 7, Timestamp: 10, RetractionHandle: 1}}
	if commit, err := s.Manager.PrepareImportState("fed", state); err == nil || commit != nil {
		t.Fatal("dangling pending deletion accepted")
	}
	ts := core.LogicalTime(10)
	event := &object.CallbackEvent{Kind: object.CallbackDeleteMarker, Object: 1, LogicalTime: &ts, Metadata: &core.ObjectCallbackMetadata{Producer: 7, RetractionHandle: 1, SentOrder: core.OrderTimestamp}}
	cloned, err := cloneTimeEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	cloned.(*object.CallbackEvent).Metadata.RetractionHandle = 8
	if event.Metadata.RetractionHandle != 1 {
		t.Fatal("marker metadata aliased")
	}
}
