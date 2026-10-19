package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/cbchoi/gorti/rti/internal/adapters/delivery"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/adapters/savebundle"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func checkpointReviewLedger(t *testing.T, capacity int) *CallbackReceipts {
	t.Helper()
	r, err := NewCallbackReceipts(CallbackReceiptOptions{
		Authenticate: func(context.Context, core.FederationName, core.FederateHandle) error { return nil },
		Observe: func(context.Context, core.FederationName, core.FederateHandle, *rtiv1.FederateEvent, bool, string) error {
			return nil
		},
		MaxPending: capacity,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func checkpointReviewData(seq uint64, marker byte) *rtiv1.FederateEvent {
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	return &rtiv1.FederateEvent{Seq: seq, Event: &rtiv1.FederateEvent_Receive{Receive: &rtiv1.ReceiveInteraction{InteractionClassHandle: 7, UserSuppliedTag: []byte{marker}, Metadata: &rtiv1.ObjectCallbackMetadata{ReceivedOrder: &order}}}}
}

func checkpointReviewImage(t *testing.T, recipient core.FederateHandle, events ...*rtiv1.FederateEvent) []byte {
	t.Helper()
	rows := make([]map[string]any, len(events))
	for i, event := range events {
		payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		rows[i] = map[string]any{"recipient": recipient, "order": i + 1, "event": payload}
	}
	data, err := json.Marshal(map[string]any{"version": 1, "deliveries": rows})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCallbackCheckpointReviewReconnectPreservesAdmissionNotSequenceOrder(t *testing.T) {
	r := checkpointReviewLedger(t, 8)
	data := checkpointReviewImage(t, 1, checkpointReviewData(90, 1), checkpointReviewData(1, 2), checkpointReviewData(90, 3))
	_, replay, err := r.PrepareCheckpointReplay(context.Background(), "f", data, []core.RestoreParticipant{{CurrentHandle: 7, SavedHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Release()
	replay.Admit()
	r.restoreCommitted("f", "saved", map[core.FederateHandle]core.FederateHandle{7: 1})
	if err := replay.Commit(); err != nil {
		t.Fatal(err)
	}
	r.markDisconnected("f", 1)
	retries := r.pendingRedelivery("f", 1)
	var got []byte
	for _, event := range retries {
		got = append(got, event.GetReceive().UserSuppliedTag[0])
	}
	if !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("reconnect reordered admitted data: tags=%v, want [1 2 3]", got)
	}
}

func TestCallbackCheckpointReviewRequiredScopeExcludesOptionalMeasurement(t *testing.T) {
	r := checkpointReviewLedger(t, 4)
	_, sent, err := r.Prepare(context.Background(), "f", 1, checkpointReviewData(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(image.Deliveries) != 0 {
		t.Fatal("optional measurement receipt became a required replay obligation")
	}
}

func TestCallbackCheckpointReviewDuplicateJSONRejectedWithoutLedgerMutation(t *testing.T) {
	valid := checkpointReviewImage(t, 1, checkpointReviewData(1, 1))
	for name, data := range map[string][]byte{
		"root-version": bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":99,"version":1`), 1),
		"recipient":    bytes.Replace(valid, []byte(`"recipient":1`), []byte(`"recipient":9,"recipient":1`), 1),
		"case-alias":   bytes.Replace(valid, []byte(`"version":1`), []byte(`"Version":99,"version":1`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			r := checkpointReviewLedger(t, 4)
			_, replay, err := r.PrepareCheckpointReplay(context.Background(), "f", data, []core.RestoreParticipant{{CurrentHandle: 1, SavedHandle: 1}})
			if err == nil {
				replay.Release()
				t.Fatal("ambiguous duplicate snapshot JSON accepted")
			}
			if r.pending != 0 || len(r.entries) != 0 || r.nextCheckpointOrder != 0 {
				t.Fatal("invalid candidate mutated receipt ledger")
			}
		})
	}
}

func TestCallbackCheckpointReviewHistoricalProducerReferencesAndExactRebind(t *testing.T) {
	const receiver core.FederateHandle = 1<<32 | 2
	const departed core.FederateHandle = 1<<32 | 9
	producer := uint64(departed)
	event := checkpointReviewData(9, 255)
	event.GetReceive().Metadata.ProducingFederate = &producer
	event.GetReceive().Metadata.ReceivedRegions = &rtiv1.ReceivedRegions{RegionHandles: []uint64{uint64(departed)}}
	data := checkpointReviewImage(t, receiver, event)
	generation := uint64(1)
	manifest := core.CheckpointManifest{FederationGeneration: &generation, Federates: []core.FederateHandle{receiver}, ManagerSnapshots: map[string][]byte{CallbackCheckpointKey: data}}
	graph := savebundle.RestoreGraph{Snapshots: map[string]core.CheckpointSnapshotter{CallbackCheckpointKey: CallbackCheckpoint{}}}
	refs, err := graph.References(manifest)
	if err != nil || !reflect.DeepEqual(refs, []core.FederateHandle{receiver, departed}) {
		t.Fatalf("historical producer references: %v %v", refs, err)
	}
	if _, err := graph.Rebind(manifest, 2, map[core.FederateHandle]core.FederateHandle{receiver: 2<<32 | 2}); err == nil {
		t.Fatal("unmapped historical producer accepted")
	}
	rebound, err := graph.Rebind(manifest, 2, map[core.FederateHandle]core.FederateHandle{receiver: 2<<32 | 2, departed: 2<<32 | 9})
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(rebound.ManagerSnapshots[CallbackCheckpointKey])
	if err != nil {
		t.Fatal(err)
	}
	got := &rtiv1.FederateEvent{}
	if err := proto.Unmarshal(image.Deliveries[0].Event, got); err != nil {
		t.Fatal(err)
	}
	if image.Deliveries[0].Recipient != 2<<32|2 || got.GetReceive().Metadata.GetProducingFederate() != 2<<32|9 || got.GetReceive().Metadata.GetReceivedRegions().RegionHandles[0] != uint64(departed) || got.GetReceive().InteractionClassHandle != 7 || got.Seq != 9 {
		t.Fatal("rebind changed non-federate handles or lost historical identity")
	}
	if !bytes.Equal(manifest.ManagerSnapshots[CallbackCheckpointKey], data) {
		t.Fatal("rebind changed source checkpoint")
	}
}

func TestCallbackCheckpointReviewConveyedLeaseRecipientAndActivity(t *testing.T) {
	event := checkpointReviewData(1, 1)
	event.GetReceive().Metadata.ReceivedRegions = &rtiv1.ReceivedRegions{RegionHandles: []uint64{77}}
	data := checkpointReviewImage(t, 1, event)
	for _, tc := range []struct {
		name      string
		recipient uint64
		active    bool
		handles   []uint64
		valid     bool
	}{
		{"pending-own", 1, false, []uint64{77}, true},
		{"foreign-recipient", 2, false, []uint64{77}, false},
		{"already-active", 1, true, []uint64{77}, false},
		{"missing-designator", 1, false, []uint64{78}, false},
		{"duplicate-designator", 1, false, []uint64{77, 77}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := proto.Clone(event).(*rtiv1.FederateEvent)
			candidate.GetReceive().Metadata.ReceivedRegions.RegionHandles = tc.handles
			data = checkpointReviewImage(t, 1, candidate)
			ddmState, err := proto.Marshal(&rtiv1.DDMManagerState{ConveyedRegions: []*rtiv1.DDMConveyedRegionEntry{{Designator: 77, SourceRegionHandle: 7, Recipients: []*rtiv1.DDMConveyedRegionRecipientEntry{{FederateHandle: tc.recipient, Active: tc.active}}}}})
			if err != nil {
				t.Fatal(err)
			}
			err = (CallbackCheckpoint{}).ValidateConveyedRegions(data, ddmState)
			if (err == nil) != tc.valid {
				t.Fatalf("lease validation: %v", err)
			}
		})
	}
}

func TestCallbackCheckpointReviewMandatoryOrdinalExhaustionRollsBack(t *testing.T) {
	r := checkpointReviewLedger(t, 2)
	raw := delivery.NewWithBatch(2, 1, 0)
	ch, cancel, err := raw.Subscribe(context.Background(), "f", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTransportCleanup(t, cancel)
	r.nextCheckpointOrder = ^uint64(0) - 1
	out := NewMandatoryCallbackOutbox(raw, func() *CallbackReceipts { return r })
	reservation, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: &requiredCallbackEvent{event: checkpointReviewData(1, 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Commit(); err == nil {
		t.Fatal("exhausted commit ordinal accepted")
	}
	reservation.Release()
	if len(ch) != 0 || r.pending != 0 || len(r.entries) != 0 {
		t.Fatal("failed mandatory publication leaked event or receipt")
	}
}

func TestCallbackCheckpointReviewReplayReleasePreservesOldReceipt(t *testing.T) {
	r := checkpointReviewLedger(t, 2)
	ctx := context.Background()
	old, sent, err := r.PrepareRequired(ctx, "f", 7, checkpointReviewData(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	sent(true)
	data := checkpointReviewImage(t, 1, checkpointReviewData(1, 1))
	pending, replay, err := r.PrepareCheckpointReplay(ctx, "f", data, []core.RestoreParticipant{{CurrentHandle: 7, SavedHandle: 1}})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := EncodeOutboundEvent(pending[1][0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(fresh.CallbackReceipt, old) {
		t.Fatal("replay reused saved capability")
	}
	replay.Admit()
	replay.Release()
	if err := r.ReportEntry(ctx, "f", 1, fresh.CallbackReceipt); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("rolled-back fresh receipt survived: %v", err)
	}
	if err := r.ReportEntry(ctx, "f", 7, old); err != nil {
		t.Fatal("rollback expired old receipt", err)
	}
	if err := r.Report(ctx, "f", 7, old, true, ""); err != nil {
		t.Fatal(err)
	}
	if r.pending != 0 {
		t.Fatal("rollback pending count leak")
	}
	_, _, err = r.PrepareCheckpointReplay(ctx, "f", data, nil)
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("unknown restore recipient admitted")
	}
}

func TestCallbackCheckpointReviewDiscoveryThenReflectionFreshTickets(t *testing.T) {
	const receiver core.FederateHandle = 1<<32 | 2
	const departed core.FederateHandle = 1<<32 | 9
	producer := uint64(departed)
	order := rtiv1.OrderType_ORDER_TYPE_RECEIVE
	discovery := &rtiv1.FederateEvent{Seq: 9, Event: &rtiv1.FederateEvent_Discover{Discover: &rtiv1.DiscoverObjectInstance{ObjectHandle: 11, ObjectClassHandle: 7, ObjectName: "review-object", ProducingFederate: &producer}}}
	reflection := &rtiv1.FederateEvent{Seq: 1, Event: &rtiv1.FederateEvent_Reflect{Reflect: &rtiv1.ReflectAttributeValues{ObjectHandle: 11, ObjectClassHandle: 7, Attributes: map[uint64][]byte{3: {0, 255}}, Metadata: &rtiv1.ObjectCallbackMetadata{ReceivedOrder: &order, ProducingFederate: &producer}}}}
	data := checkpointReviewImage(t, receiver, discovery, reflection)
	rebound, err := (CallbackCheckpoint{}).RewriteFederates(data, func(h core.FederateHandle) (core.FederateHandle, error) {
		return 2<<32 | h&0xffffffff, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := checkpointReviewLedger(t, 4)
	const target core.FederateHandle = 2<<32 | 2
	pending, replay, err := r.PrepareCheckpointReplay(context.Background(), "f", rebound, []core.RestoreParticipant{{CurrentHandle: 2<<32 | 5, SavedHandle: target}})
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Release()
	if len(pending[target]) != 2 {
		t.Fatal("discovery/reflection replay incomplete")
	}
	first, err := EncodeOutboundEvent(pending[target][0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeOutboundEvent(pending[target][1])
	if err != nil {
		t.Fatal(err)
	}
	if first.GetDiscover() == nil || second.GetReflect() == nil || first.GetDiscover().GetProducingFederate() != 2<<32|9 || second.GetReflect().Metadata.GetProducingFederate() != 2<<32|9 || !bytes.Equal(second.GetReflect().Attributes[3], []byte{0, 255}) {
		t.Fatal("discovery order, historical producer or reflection values lost")
	}
	if len(first.CallbackReceipt) != 32 || len(second.CallbackReceipt) != 32 || bytes.Equal(first.CallbackReceipt, second.CallbackReceipt) {
		t.Fatal("discovery/reflection need distinct fresh mandatory tickets")
	}
	replay.Admit()
	r.restoreCommitted("f", "saved", map[core.FederateHandle]core.FederateHandle{2<<32 | 5: target})
	if err := replay.Commit(); err != nil {
		t.Fatal(err)
	}
	r.markDisconnected("f", target)
	retries := r.pendingRedelivery("f", target)
	if len(retries) != 2 || retries[0].GetDiscover() == nil || retries[1].GetReflect() == nil {
		t.Fatal("reconnect delivers reflection before required discovery")
	}
}

func TestCallbackCheckpointReviewMandatoryOrderUsesCommitNotPreparation(t *testing.T) {
	r := checkpointReviewLedger(t, 4)
	raw := delivery.NewWithBatch(2, 1, 0)
	for _, h := range []core.FederateHandle{1, 2} {
		_, cancel, err := raw.Subscribe(context.Background(), "f", h)
		if err != nil {
			t.Fatal(err)
		}
		defer checkTransportCleanup(t, cancel)
	}
	out := NewMandatoryCallbackOutbox(raw, func() *CallbackReceipts { return r })
	first, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: &requiredCallbackEvent{event: checkpointReviewData(9, 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 2, Event: &requiredCallbackEvent{event: checkpointReviewData(1, 2)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
	if err != nil {
		t.Fatal(err)
	}
	image, err := decodeCallbackCheckpoint(data)
	if err != nil || len(image.Deliveries) != 2 || image.Deliveries[0].Recipient != 2 || image.Deliveries[1].Recipient != 1 {
		t.Fatalf("checkpoint order follows preparation rather than admission: %+v %v", image, err)
	}
}

func TestCallbackCheckpointReviewRequiredControlsPreservePhaseAndBody(t *testing.T) {
	for _, name := range []string{
		"sync_announced", "sync_synchronized", "sync_registration_succeeded", "sync_registration_failed",
		"ownership_assumption", "ownership_acquired", "ownership_divest_confirmed", "ownership_release_requested", "ownership_unavailable", "ownership_acquisition_cancelled",
		"grant", "provide_update", "time_regulation_enabled", "time_constrained_enabled", callbackTransportationConfirmed,
		"restore_initiate", "restore_completed", "restore_failed", "restore_request_succeeded", "restore_request_failed", "restore_begun",
	} {
		t.Run(name, func(t *testing.T) {
			r := checkpointReviewLedger(t, 4)
			event := &rtiv1.FederateEvent{}
			message := event.ProtoReflect()
			field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
			if field == nil {
				t.Fatal("review field does not exist")
			}
			message.Set(field, message.NewField(field))
			if name == callbackTransportationConfirmed {
				change := message.Get(field).Message()
				class := change.Descriptor().Fields().ByName("interaction_class_handle")
				change.Set(class, protoreflect.ValueOfUint64(7))
			}
			transportation := transportationCheckpointInventoryProjection(event)
			_, sent, err := r.PrepareRequired(context.Background(), "f", 1, event)
			if err != nil {
				t.Fatal(err)
			}
			sent(true)
			data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }, Transportation: transportation}).Marshal("f")
			if err != nil {
				t.Fatal(err)
			}
			image, err := decodeCallbackCheckpoint(data)
			if err != nil || image.Version != 2 || len(image.Deliveries) != 1 || image.Deliveries[0].Phase != callbackPhasePending {
				t.Fatalf("missing pending control: %+v %v", image, err)
			}
			restored := &rtiv1.FederateEvent{}
			expected := event
			if transportation != nil {
				expected, err = StripTransportationCheckpointEvent(event, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := proto.Unmarshal(image.Deliveries[0].Event, restored); err != nil || !proto.Equal(expected, restored) {
				t.Fatal("checkpoint changed control body", err)
			}
			if r.pending != 1 || len(r.entries) != 1 {
				t.Fatal("checkpoint capture changed live receipt obligations")
			}
		})
	}
}

func TestCallbackCheckpointReviewSaveLifecycleCapturedWithoutLiveMutation(t *testing.T) {
	for _, name := range []string{"save_initiate", "save_completed", "save_failed"} {
		t.Run(name, func(t *testing.T) {
			r := checkpointReviewLedger(t, 4)
			event := &rtiv1.FederateEvent{}
			message := event.ProtoReflect()
			field := message.Descriptor().Fields().ByName(protoreflect.Name(name))
			message.Set(field, message.NewField(field))
			_, sent, err := r.PrepareRequired(context.Background(), "f", 1, event)
			if err != nil {
				t.Fatal(err)
			}
			sent(true)
			data, err := (CallbackCheckpoint{Receipts: func() *CallbackReceipts { return r }}).Marshal("f")
			if err != nil {
				t.Fatal("save protocol bookkeeping blocked its own capture", err)
			}
			image, err := decodeCallbackCheckpoint(data)
			if err != nil || image.Version != 2 || len(image.Deliveries) != 1 || image.Deliveries[0].Phase != callbackPhasePending || r.pending != 1 || len(r.entries) != 1 {
				t.Fatalf("save callback lost or obligation changed: %+v %v", image, err)
			}
			restored := &rtiv1.FederateEvent{}
			if err := proto.Unmarshal(image.Deliveries[0].Event, restored); err != nil || !proto.Equal(event, restored) {
				t.Fatal("checkpoint changed save callback body", err)
			}
		})
	}
}

type checkpointReviewQueue struct {
	core.ReservableOutbox
	commit func() error
}

func (q checkpointReviewQueue) Reserve(context.Context, core.FederationName, []core.OutboxDelivery) (core.OutboxReservation, error) {
	return checkpointReviewQueueReservation{commit: q.commit}, nil
}

type checkpointReviewQueueReservation struct{ commit func() error }

func (q checkpointReviewQueueReservation) Commit() error { return q.commit() }
func (checkpointReviewQueueReservation) Release()        {}

func TestCallbackCheckpointReviewPublicationLockDoesNotHoldLedger(t *testing.T) {
	r := checkpointReviewLedger(t, 2)
	commits := 0
	queue := checkpointReviewQueue{commit: func() error {
		commits++
		if r.publicationMu.TryLock() {
			r.publicationMu.Unlock()
			return errors.New("publication serialization ended before raw queue commit")
		}
		if !r.mu.TryLock() {
			return errors.New("raw queue commit holds receipt ledger lock")
		}
		defer r.mu.Unlock()
		for _, entry := range r.entries {
			if !entry.admitted || entry.checkpointOrder == 0 {
				return errors.New("raw publication preceded receipt admission")
			}
		}
		return nil
	}}
	out := NewMandatoryCallbackOutbox(queue, func() *CallbackReceipts { return r })
	reservation, err := out.Reserve(context.Background(), "f", []core.OutboxDelivery{{Recipient: 1, Event: &requiredCallbackEvent{event: checkpointReviewData(1, 1)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	for i := 0; i < 2; i++ {
		if err := reservation.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if commits != 1 {
		t.Fatal("mandatory publication repeated")
	}
	if !r.publicationMu.TryLock() {
		t.Fatal("publication lock leaked after commit")
	}
	r.publicationMu.Unlock()
}
