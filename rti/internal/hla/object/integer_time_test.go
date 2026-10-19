package object

import (
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/hla/core"
	timepkg "github.com/cbchoi/gorti/rti/internal/hla/time"
)

func TestIntegerObjectExactMessageTimestamps(t *testing.T) {
	for _, timestamp := range []int64{0, 1, 1<<53 - 1, 1 << 53, 1<<53 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		for _, operation := range []string{"update", "update-wire", testInteraction, testInteractionWire, testDelete} {
			t.Run(operation+"/"+strconv.FormatInt(timestamp, 10), func(t *testing.T) {
				timestamp := timestamp
				log := &recordingEventLog{}
				legacy, declarations, out := newTestRegistry(t, log)
				r, err := NewOf[int64](OptionsOf[int64]{EventLog: log, Declarations: declarations, Outbox: out, FOMs: legacy.opts.FOMs, Clock: legacy.opts.Clock})
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				if err := declarations.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
					t.Fatal(err)
				}
				if err := declarations.SubscribeObjectClassAttributes(ctx, "fed", 2, 1, []core.AttributeHandle{1}); err != nil {
					t.Fatal(err)
				}
				if err := declarations.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
					t.Fatal(err)
				}
				if err := declarations.SubscribeInteractionClass(ctx, "fed", 2, 7); err != nil {
					t.Fatal(err)
				}
				reserveFixtureName(t, r, "fed", 1, "exact-integer")
				obj, _, err := r.Register(ctx, "fed", 1, 1, "exact-integer")
				if err != nil {
					t.Fatal(err)
				}
				tag := []byte{0, 255, 1}
				options := core.ObjectMessageOptions{Tag: tag}
				wantKind := CallbackReflect
				switch operation {
				case "update":
					err = r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &timestamp, options)
				case "update-wire":
					err = r.UpdateAttributesWire(ctx, "fed", 1, obj, map[uint64][]byte{1: {7}}, &timestamp)
				case testInteraction:
					wantKind = CallbackReceive
					err = r.SendInteractionWithMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {7}}, &timestamp, options)
				case testInteractionWire:
					wantKind = CallbackReceive
					err = r.SendInteractionWire(ctx, "fed", 1, 7, map[uint64][]byte{1: {7}}, &timestamp)
				case testDelete:
					wantKind = CallbackRemove
					err = r.DeleteWithMetadata(ctx, "fed", 1, obj, &timestamp, options)
				}
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, delivery := range out.Records() {
					event, ok := delivery.Event.(*CallbackEventOf[int64])
					if !ok || event.Kind != wantKind {
						continue
					}
					found = true
					if event.LogicalTime == nil || *event.LogicalTime != timestamp || event.Metadata == nil || event.Metadata.SentOrder != core.OrderTimestamp || event.Metadata.Producer != 1 {
						t.Fatalf("callback lost exact timestamp or metadata: %+v", event)
					}
				}
				if !found {
					t.Fatal("missing timestamped callback")
				}
				records := log.Records()
				event, ok := records[len(records)-1].Event.(*core.ObjectJournalRecordOf[int64])
				if !ok || event.LogicalTime == nil || *event.LogicalTime != timestamp {
					t.Fatalf("journal lost exact timestamp: %T %+v", records[len(records)-1].Event, records[len(records)-1].Event)
				}
				original := timestamp
				timestamp = 0
				if *event.LogicalTime != original {
					t.Fatal("journal retained caller timestamp pointer")
				}
			})
		}
	}
}

func integerTimeFixture(t *testing.T) (*IntegerRegistry, *timepkg.IntegerManager, *atomicUpdateOutbox, core.ObjectHandle) {
	t.Helper()
	legacy, declarations, _ := newTestRegistry(t, nil)
	out := &atomicUpdateOutbox{}
	r, err := NewOf[int64](OptionsOf[int64]{Declarations: declarations, Outbox: out, FOMs: legacy.opts.FOMs, Clock: legacy.opts.Clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := declarations.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	if err := declarations.PublishInteractionClass(ctx, "fed", 1, 7); err != nil {
		t.Fatal(err)
	}
	for _, recipient := range []core.FederateHandle{2, 3} {
		if err := declarations.SubscribeObjectClassAttributes(ctx, "fed", recipient, 1, []core.AttributeHandle{1}); err != nil {
			t.Fatal(err)
		}
		if err := declarations.SubscribeInteractionClass(ctx, "fed", recipient, 7); err != nil {
			t.Fatal(err)
		}
	}
	reserveFixtureName(t, r, "fed", 1, "integer-lifecycle")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "integer-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	tm, err := timepkg.NewOf[int64](timepkg.OptionsOf[int64]{Clock: r.opts.Clock, Outbox: out})
	if err != nil {
		t.Fatal(err)
	}
	tm.SetDeletionLifecycle(r)
	r.opts.TSOGate, r.opts.Retractions, r.opts.TSOValidator = tm, tm, tm
	for _, recipient := range []core.FederateHandle{1, 2, 3} {
		if err := tm.EnableConstrained(ctx, "fed", recipient); err != nil {
			t.Fatal(err)
		}
	}
	out.records = nil
	return r, tm, out, obj
}

func cloneIntegerTimeCallback(event core.OutboundEvent) (core.OutboundEvent, error) {
	callback, ok := event.(*CallbackEventOf[int64])
	if !ok {
		return nil, errors.New("unexpected callback type")
	}
	cloned := *callback
	cloned.LogicalTime = copiedLogicalTimeOf(callback.LogicalTime)
	cloned.UserSuppliedTag = slices.Clone(callback.UserSuppliedTag)
	cloned.Attributes = make(map[uint64][]byte, len(callback.Attributes))
	for attribute, value := range callback.Attributes {
		cloned.Attributes[attribute] = slices.Clone(value)
	}
	cloned.Parameters = make(map[uint64][]byte, len(callback.Parameters))
	for parameter, value := range callback.Parameters {
		cloned.Parameters[parameter] = slices.Clone(value)
	}
	if callback.Metadata != nil {
		metadata := *callback.Metadata
		metadata.Regions = slices.Clone(metadata.Regions)
		cloned.Metadata = &metadata
	}
	return &cloned, nil
}

func TestIntegerObjectRealQueueDeletionAndSnapshot(t *testing.T) {
	for _, finalTime := range []int64{1<<53 + 3, math.MaxInt64} {
		t.Run(strconv.FormatInt(finalTime, 10), func(t *testing.T) {
			r, tm, out, obj := integerTimeFixture(t)
			ctx := context.Background()
			updateTime, interactionTime, deleteTime := finalTime-2, finalTime-1, finalTime
			if err := r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &updateTime, core.ObjectMessageOptions{Tag: []byte("update"), RetractionHandle: 101}); err != nil {
				t.Fatal(err)
			}
			if err := r.SendInteractionWithMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {8}}, &interactionTime, core.ObjectMessageOptions{Tag: []byte(testInteraction), RetractionHandle: 102}); err != nil {
				t.Fatal(err)
			}
			if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &deleteTime, core.ObjectMessageOptions{Tag: []byte(testDelete), RetractionHandle: 103}); err != nil {
				t.Fatal(err)
			}
			deleteTime = 0
			saved := r.ExportState("fed")
			if len(saved.Deletions) != 1 || saved.Deletions[0].Timestamp != finalTime || saved.Deletions[0].Committed {
				t.Fatalf("pending deletion lost exact time: %+v", saved.Deletions)
			}
			queue, err := tm.ExportState("fed", cloneIntegerTimeCallback)
			if err != nil {
				t.Fatal(err)
			}
			for _, state := range queue.Federates {
				if state.Handle == 2 || state.Handle == 3 {
					var timestamps []int64
					for _, message := range state.Queue {
						timestamps = append(timestamps, message.Timestamp)
					}
					if !slices.Equal(timestamps, []int64{updateTime, interactionTime, finalTime}) {
						t.Fatalf("recipient %d queue timestamps=%v", state.Handle, timestamps)
					}
				}
			}
			restored, err := NewOf[int64](r.opts)
			if err != nil {
				t.Fatal(err)
			}
			commit, err := restored.PrepareImportState("fed", saved)
			if err != nil {
				t.Fatal(err)
			}
			commit()
			saved.Deletions[0].Timestamp = 0
			saved.Deletions[0].Tag[0] = 'x'
			if state := restored.ExportState("fed"); state.Deletions[0].Timestamp != finalTime || string(state.Deletions[0].Tag) != testDelete {
				t.Fatalf("snapshot import aliased or changed time: %+v", state.Deletions)
			}
			if len(out.records) != 0 {
				t.Fatal("future callbacks delivered before time advance")
			}
			if err := tm.TimeAdvanceRequest(ctx, "fed", 1, finalTime); err != nil {
				t.Fatal(err)
			}
			out.records = nil
			if err := tm.TimeAdvanceRequest(ctx, "fed", 2, finalTime); err != nil {
				t.Fatal(err)
			}
			var events []*CallbackEventOf[int64]
			var grant *timepkg.TimeAdvanceGrantOf[int64]
			for _, delivery := range out.records {
				if delivery.Federate != 2 {
					continue
				}
				if event, ok := delivery.Event.(*CallbackEventOf[int64]); ok {
					events = append(events, event)
				}
				if event, ok := delivery.Event.(*timepkg.TimeAdvanceGrantOf[int64]); ok {
					grant = event
				}
			}
			if len(events) != 3 || grant == nil || grant.Time != finalTime {
				t.Fatalf("queue delivery count=%d grant=%+v", len(events), grant)
			}
			for i, kind := range []CallbackKind{CallbackReflect, CallbackReceive, CallbackRemove} {
				event := events[i]
				if event.Kind != kind || event.LogicalTime == nil || *event.LogicalTime != finalTime-2+int64(i) || event.Metadata.ReceivedOrder != core.OrderTimestamp || event.Metadata.RetractionHandle != uint64(101+i) {
					t.Fatalf("ordered exact callback %d: %+v", i, event)
				}
			}
			if r.Snapshot("fed").InstanceCount != 0 {
				t.Fatal("deletion did not retire instance")
			}
			if _, _, known := r.LookupKnownObjectInstance("fed", 2, obj); known {
				t.Fatal("typed time snapshot failed to forget removal at grant time")
			}
			if _, _, known := r.LookupKnownObjectInstance("fed", 3, obj); !known {
				t.Fatal("removal forgot recipient before its grant")
			}
		})
	}
}

func TestIntegerObjectRealDeletionRetraction(t *testing.T) {
	r, tm, out, obj := integerTimeFixture(t)
	ctx := context.Background()
	if err := tm.EnableRegulation(ctx, "fed", 1, 1); err != nil {
		t.Fatal(err)
	}
	timestamp := int64(1<<53 + 1)
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &timestamp, core.ObjectMessageOptions{RetractionHandle: 201}); err != nil {
		t.Fatal(err)
	}
	if err := r.RetractWithError(ctx, "fed", 1, 201); err != nil {
		t.Fatal(err)
	}
	if state := r.ExportState("fed"); len(state.Deletions) != 0 || len(state.Objects) != 1 {
		t.Fatalf("retraction failed to preserve object: %+v", state)
	}
	if err := r.DeleteWithMetadata(ctx, "fed", 1, obj, &timestamp, core.ObjectMessageOptions{RetractionHandle: 201}); !errors.Is(err, core.ErrRetractionIDAlreadyUsed) {
		t.Fatalf("reused identity: %v", err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 1, timestamp); err != nil {
		t.Fatal(err)
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, timestamp); err != nil {
		t.Fatal(err)
	}
	for _, delivery := range out.records {
		if event, ok := delivery.Event.(*CallbackEventOf[int64]); ok && event.Kind == CallbackRemove {
			t.Fatal("retracted deletion remained queued")
		}
	}
}

func TestIntegerObjectReceiveOrderRetainsOriginalTime(t *testing.T) {
	r, tm, out, obj := integerTimeFixture(t)
	ctx := context.Background()
	r.opts.Orders = preferredOrderTable{attributes: map[core.AttributeHandle]Order{1: OrderReceive}, interaction: OrderReceive}
	timestamp := int64(math.MaxInt64)
	if err := r.UpdateAttributesWithMetadata(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &timestamp, core.ObjectMessageOptions{Tag: []byte("ro-update")}); err != nil {
		t.Fatal(err)
	}
	if err := r.SendInteractionWithMetadata(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {8}}, &timestamp, core.ObjectMessageOptions{Tag: []byte("ro-interaction")}); err != nil {
		t.Fatal(err)
	}
	queue, err := tm.ExportState("fed", cloneIntegerTimeCallback)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range queue.Federates {
		if state.Handle != 2 && state.Handle != 3 {
			continue
		}
		if len(state.Queue) != 0 || len(state.ROQueue) != 2 {
			t.Fatalf("RO messages misclassified: %+v", state)
		}
		for _, message := range state.ROQueue {
			event := message.Event.(*CallbackEventOf[int64])
			if message.Timestamp != 0 || !message.ReceiveOrder || event.LogicalTime == nil || *event.LogicalTime != math.MaxInt64 {
				t.Fatalf("RO callback lost original Integer64 timestamp: %+v", message)
			}
		}
	}
	if err := tm.TimeAdvanceRequest(ctx, "fed", 2, 1); err != nil {
		t.Fatal(err)
	}
	var callbacks int
	for _, delivery := range out.records {
		if delivery.Federate != 2 {
			continue
		}
		if event, ok := delivery.Event.(*CallbackEventOf[int64]); ok {
			callbacks++
			if event.LogicalTime == nil || *event.LogicalTime != math.MaxInt64 || event.Metadata.ReceivedOrder != core.OrderReceive {
				t.Fatalf("RO delivery lost exact original time: %+v", event)
			}
		}
	}
	if callbacks != 2 {
		t.Fatalf("RO delivered callbacks=%d", callbacks)
	}
}

func TestIntegerObjectRejectsNegativeBeforeEffects(t *testing.T) {
	log := &recordingEventLog{}
	legacy, declarations, out := newTestRegistry(t, log)
	r, err := NewOf[int64](OptionsOf[int64]{EventLog: log, Declarations: declarations, Outbox: out, FOMs: legacy.opts.FOMs, Clock: legacy.opts.Clock})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := declarations.PublishObjectClassAttributes(ctx, "fed", 1, 1, []core.AttributeHandle{1}); err != nil {
		t.Fatal(err)
	}
	reserveFixtureName(t, r, "fed", 1, "negative-integer")
	obj, _, err := r.Register(ctx, "fed", 1, 1, "negative-integer")
	if err != nil {
		t.Fatal(err)
	}
	beforeLog, beforeCallbacks := len(log.Records()), len(out.Records())
	timestamp := int64(-1)
	for _, err := range []error{
		r.UpdateAttributes(ctx, "fed", 1, obj, map[core.AttributeHandle][]byte{1: {7}}, &timestamp),
		r.SendInteraction(ctx, "fed", 1, 7, map[core.ParameterHandle][]byte{1: {7}}, &timestamp),
		r.Delete(ctx, "fed", 1, obj, &timestamp, nil),
	} {
		if !errors.Is(err, core.ErrTimeInvalidLogicalTime) {
			t.Fatalf("negative timestamp error: %v", err)
		}
	}
	if len(log.Records()) != beforeLog || len(out.Records()) != beforeCallbacks || r.Snapshot("fed").InstanceCount != 1 {
		t.Fatal("negative timestamp changed state or emitted events")
	}
}
