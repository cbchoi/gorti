package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	grpcadapter "github.com/cbchoi/gorti/rti/internal/adapters/grpc"
	wire "github.com/cbchoi/gorti/rti/internal/adapters/protobuf"
	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"github.com/cbchoi/gorti/rti/internal/hla/core"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func typedSaveWireTime[T core.LogicalTimeScalar](t *testing.T, at T) *rtiv1.LogicalTimeValue {
	t.Helper()
	value, err := core.ScalarTimeValue(at)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := wire.EncodeLogicalTimePair(core.ScalarRepresentation[T](), value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestProductionTimedSaveMessageOrderingAndRetentionBothFamilies(t *testing.T) {
	for _, mode := range []string{"TAR", "TARA", testNextMessageRequest, testNextMessageRequestAvailable, testFlushQueueRequest} {
		t.Run("Float64/"+mode, func(t *testing.T) { productionTimedParticipants[core.LogicalTime](t, mode, 10, true) })
		t.Run("Integer64/"+mode, func(t *testing.T) { productionTimedParticipants[int64](t, mode, 1<<53+1, true) })
	}
}

func TestProductionTimedSaveIndividualParticipantsBothFamilies(t *testing.T) {
	for _, mode := range []string{"TAR", "TARA", testNextMessageRequest, testNextMessageRequestAvailable, testFlushQueueRequest} {
		t.Run("Float64/"+mode, func(t *testing.T) { productionTimedParticipants[core.LogicalTime](t, mode, 10) })
		t.Run("Integer64/"+mode, func(t *testing.T) { productionTimedParticipants[int64](t, mode, 1<<53+1) })
	}
}

func productionTimedParticipants[T core.LogicalTimeScalar](t *testing.T, mode string, target T, messages ...bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	runtime, conn, _ := integerProductionServer(t, t.TempDir(), 191)
	const fed = "timed-participants"
	created, err := rtiv1.NewFederationServiceClient(conn).CreateFederation(ctx, &rtiv1.CreateFederationRequest{WireVersion: 1, FederationName: fed, LogicalTimeImplementationName: core.ScalarRepresentation[T]().String(), FomModules: []*rtiv1.FOMModule{momCompletionReviewModule(t, "Item")}})
	if err != nil {
		t.Fatal(err)
	}
	var handles []uint64
	channels := map[uint64]<-chan []core.OutboundEvent{}
	for i := 0; i < 4; i++ {
		joined := momCompletionReviewJoin(ctx, t, conn, fed, fmt.Sprint("participant-", i), "", created.FederationGeneration)
		handles = append(handles, joined.FederateHandle)
		channel, release, err := runtime.outbox.Subscribe(ctx, fed, core.FederateHandle(joined.FederateHandle))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		channels[joined.FederateHandle] = channel
	}
	saves, times := rtiv1.NewSavepointServiceClient(conn), rtiv1.NewTimeServiceClient(conn)
	requestSave := func(label string, at T, caller uint64) error {
		_, err := saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: caller, Label: label, SaveTimeValue: typedSaveWireTime(t, at)})
		return err
	}
	var trailer metadata.MD
	_, err = saves.RequestFederationSave(ctx, &rtiv1.RequestFederationSaveRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], Label: "no-galt", SaveTimeValue: typedSaveWireTime(t, target)}, grpcgo.Trailer(&trailer))
	if status.Code(err) != codes.FailedPrecondition || len(trailer.Get("rti-spec-exception")) != 1 || trailer.Get("rti-spec-exception")[0] != "FederateUnableToUseTime" {
		t.Fatalf("undefined requester GALT accepted/wrong exception: %v trailer=%v", err, trailer)
	}
	interval, err := core.ScalarIntervalValue(T(1))
	if err != nil {
		t.Fatal(err)
	}
	_, ahead, err := wire.EncodeLogicalTimeIntervalPair(core.ScalarRepresentation[T](), interval)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = times.EnableTimeRegulation(ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], LookaheadValue: ahead}); err != nil {
		t.Fatal(err)
	}
	for _, h := range handles[1:3] {
		if _, err = times.EnableTimeConstrained(ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}); err != nil {
			t.Fatal(err)
		}
	}
	withMessages := len(messages) > 0 && messages[0]
	objects := rtiv1.NewObjectServiceClient(conn)
	var objectHandle, attribute uint64
	if withMessages {
		model, err := runtime.foms.Get(ctx, fed)
		if err != nil {
			t.Fatal(err)
		}
		class, ok := model.LookupObjectClass("HLAobjectRoot.Item.Leaf")
		if !ok {
			t.Fatal("missing class")
		}
		attr, ok := model.LookupAttribute(class, "Local")
		if !ok {
			t.Fatal("missing attribute")
		}
		attribute = uint64(attr)
		declarations := rtiv1.NewDeclarationServiceClient(conn)
		if _, err = declarations.PublishObjectClassAttributes(ctx, &rtiv1.PubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], ObjectClassHandle: uint64(class), AttributeHandles: []uint64{attribute}}); err != nil {
			t.Fatal(err)
		}
		if _, err = declarations.SubscribeObjectClassAttributes(ctx, &rtiv1.SubObjAttrsRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[1], ObjectClassHandle: uint64(class), AttributeHandles: []uint64{attribute}}); err != nil {
			t.Fatal(err)
		}
		registered, err := objects.RegisterObjectInstance(ctx, &rtiv1.RegisterObjectRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], ObjectClassHandle: uint64(class)})
		if err != nil {
			t.Fatal(err)
		}
		objectHandle = registered.ObjectHandle
	}
	for _, channel := range channels {
		drainScheduledEvents(channel)
	}
	if err = requestSave("old", target, handles[0]); err != nil {
		t.Fatal(err)
	}
	if err = requestSave("bad", T(0), handles[0]); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	if err = requestSave("equal-galt", T(1), handles[1]); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nonregulating equality to GALT accepted: %v", err)
	}
	if err = requestSave("selected", target, handles[1]); err != nil {
		t.Fatalf("nonregulating requester above defined GALT rejected: %v", err)
	}
	if withMessages {
		if _, err = objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], ObjectHandle: objectHandle, Attributes: map[uint64][]byte{attribute: []byte("earlier")}, LogicalTimeValue: typedSaveWireTime(t, target-1)}); err != nil {
			t.Fatal(err)
		}
	}
	advance := func(h uint64, at T) error {
		r := &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: typedSaveWireTime(t, at)}
		switch mode {
		case "TARA":
			_, err = times.TimeAdvanceRequestAvailable(ctx, &rtiv1.TARARequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: r.LogicalTimeValue})
		case testNextMessageRequest:
			_, err = times.NextMessageRequest(ctx, &rtiv1.NERRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: r.LogicalTimeValue})
		case testNextMessageRequestAvailable:
			_, err = times.NextMessageRequestAvailable(ctx, &rtiv1.NMRARequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: r.LogicalTimeValue})
		case testFlushQueueRequest:
			_, err = times.FlushQueueRequest(ctx, &rtiv1.FQRRequest{WireVersion: 1, FederationName: fed, FederateHandle: h, LogicalTimeValue: r.LogicalTimeValue})
		default:
			_, err = times.TimeAdvanceRequest(ctx, r)
		}
		return err
	}
	if _, err = times.TimeAdvanceRequest(ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], LogicalTimeValue: typedSaveWireTime(t, target+10)}); err != nil {
		t.Fatal(err)
	}
	if err = advance(handles[1], target); err != nil {
		t.Fatal(err)
	}
	seen := map[uint64]bool{}
	earlier := false
	decodeEvent := func(event core.OutboundEvent) *rtiv1.FederateEvent {
		t.Helper()
		encoded, err := grpcadapter.EncodeOutboundEvent(event)
		if err != nil || encoded == nil {
			t.Fatalf("encode callback: %v", err)
		}
		return encoded
	}
	checkTime := func(name string, legacy *float64, exact *rtiv1.LogicalTimeValue, expected T) {
		t.Helper()
		value, err := wire.DecodeOptionalLogicalTimePair(core.ScalarRepresentation[T](), legacy, exact)
		if err != nil || value == nil {
			t.Fatalf("%s time missing/invalid: %v", name, err)
		}
		actual, err := core.ScalarFromTimeValue[T](*value)
		if err != nil || actual != expected {
			t.Fatalf("%s time changed: got %v want %v: %v", name, actual, expected, err)
		}
	}
	consume := func() {
		for h, channel := range channels {
			for _, event := range drainScheduledEvents(channel) {
				encoded := decodeEvent(event)
				if e := encoded.GetReflect(); e != nil {
					if len(encoded.GetCallbackReceipt()) == 0 {
						t.Fatal("data reflection bypassed mandatory callback receipt")
					}
					if string(e.Attributes[attribute]) == "during-save" {
						t.Fatal("message delivered to saving federate before federationSaved")
					}
					if string(e.Attributes[attribute]) == "earlier" {
						if h != handles[1] || e.ObjectHandle != objectHandle {
							t.Fatal("earlier TSO callback changed object or recipient")
						}
						checkTime("earlier TSO", e.LogicalTime, e.LogicalTimeValue, target-1)
						earlier = true
					}
				}
				if e := encoded.GetSaveInitiate(); e != nil {
					if e.Label != "selected" {
						t.Fatalf("wrong replacement/time: %v", e)
					}
					checkTime("initiate save", e.SaveTime, e.SaveTimeValue, target)
					if seen[h] {
						t.Fatal("duplicate initiate")
					}
					if withMessages && h == handles[1] && !earlier {
						t.Fatal("initiation overtook earlier TSO callback")
					}
					seen[h] = true
				}
			}
		}
	}
	consume()
	if withMessages && (mode == testNextMessageRequest || mode == testNextMessageRequestAvailable || mode == testFlushQueueRequest) {
		if len(seen) != 0 || !earlier {
			t.Fatal("next-message/flush save skipped earlier next grant")
		}
		if err = advance(handles[1], target); err != nil {
			t.Fatal(err)
		}
		consume()
	}
	if mode == "TARA" || mode == testNextMessageRequestAvailable || mode == testFlushQueueRequest {
		if len(seen) != 0 {
			t.Fatal("available grant equality initiated save")
		}
		if err = advance(handles[1], target+1); err != nil {
			t.Fatal(err)
		}
		consume()
	}
	if len(seen) != 1 || !seen[handles[1]] {
		t.Fatalf("ready federate waited for slower peer: %v", seen)
	}
	if err = advance(handles[1], target+2); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("saving participant mutated time: %v", err)
	}
	response := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[1]}
	if _, err = saves.FederateSaveBegun(ctx, response); err != nil {
		t.Fatal(err)
	}
	if _, err = saves.FederateSaveComplete(ctx, response); err != nil {
		t.Fatal(err)
	}
	if withMessages {
		if _, err = objects.UpdateAttributeValues(ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: fed, FederateHandle: handles[0], ObjectHandle: objectHandle, Attributes: map[uint64][]byte{attribute: []byte("during-save")}}); err != nil {
			t.Fatal(err)
		}
		consume()
	}
	if err = requestSave("active-replacement", target+20, handles[0]); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("active save replaced: %v", err)
	}
	if err = advance(handles[2], target+1); err != nil {
		t.Fatalf("waiting federate blocked by other save participant: %v", err)
	}
	consume()
	if len(seen) != 4 {
		t.Fatalf("remaining/nonconstrained initiation: %v", seen)
	}
	for _, h := range []uint64{handles[0], handles[2], handles[3]} {
		r := &rtiv1.FederateSaveResponseRequest{WireVersion: 1, FederationName: fed, FederateHandle: h}
		if _, err = saves.FederateSaveBegun(ctx, r); err != nil {
			t.Fatal(err)
		}
		if _, err = saves.FederateSaveComplete(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	for h, channel := range channels {
		saved, retained := false, false
		for _, event := range drainScheduledEvents(channel) {
			encoded := decodeEvent(event)
			if e := encoded.GetSaveCompleted(); e != nil && e.Label == "selected" {
				saved = true
			}
			if e := encoded.GetSaveFailed(); e != nil {
				t.Fatalf("save failed: %v", e.Reason)
			}
			if e := encoded.GetReflect(); e != nil && string(e.Attributes[attribute]) == "during-save" {
				if !saved {
					t.Fatal("retained callback preceded federationSaved")
				}
				if h != handles[1] || e.ObjectHandle != objectHandle || e.LogicalTime != nil || e.LogicalTimeValue != nil {
					t.Fatal("retained RO callback changed object, recipient or order")
				}
				if len(encoded.GetCallbackReceipt()) == 0 {
					t.Fatal("retained callback bypassed mandatory callback receipt")
				}
				retained = true
			}
		}
		if !saved {
			t.Fatalf("federate %d lacks actual federationSaved", h)
		}
		if withMessages && h == handles[1] && !retained {
			t.Fatal("saving recipient lost retained RO callback")
		}
	}
}
