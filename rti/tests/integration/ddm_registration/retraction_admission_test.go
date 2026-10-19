package ddm_registration_test

import (
	"bytes"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProductionRetractionIDsSharedAcrossOperations(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	f.subscribe(2)
	f.reserve("retraction-ids")
	obj, err := f.register("retraction-ids")
	check(t, err)
	// The shared FOM defaults to Receive; retraction requires a TSO origin.
	_, err = f.objects.ChangeAttributeOrderType(f.ctx, &rtiv1.ChangeAttributeOrderRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: obj.ObjectHandle, AttributeHandles: []uint64{f.position}, OrderType: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP})
	check(t, err)
	cls, err := f.support.GetInteractionClassHandle(f.ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAinteractionRoot.RegionalSignal"})
	check(t, err)
	_, err = f.decl.PublishInteractionClass(f.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	_, err = f.objects.ChangeInteractionOrderType(f.ctx, &rtiv1.ChangeInteractionOrderRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, OrderType: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP})
	check(t, err)
	_, err = f.timeService.EnableTimeRegulation(f.ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], Lookahead: 1})
	check(t, err)
	for _, h := range f.handles[1:] {
		_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: h, InteractionClassHandle: cls.ClassHandle})
		check(t, err)
	}
	ts := float64(10)
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: obj.ObjectHandle, Attributes: map[uint64][]byte{f.position: {1}}, LogicalTime: &ts, MessageRetractionHandle: 80, UserSuppliedTag: []byte("accepted-update")})
	check(t, err)
	region := f.region(0, 10, 20)
	_, err = f.ddm.SendInteractionWithRegions(f.ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, RegionHandles: []uint64{region}, LogicalTime: &ts, MessageRetractionHandle: 80, UserSuppliedTag: []byte("duplicate")})
	if status.Code(err) != codes.FailedPrecondition {
		t.Error("regional send reused an ID accepted by an attribute update")
	}
	// An unused lower ID is valid: uniqueness is not a high-watermark policy.
	_, err = f.objects.SendInteraction(f.ctx, &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, LogicalTime: &ts, MessageRetractionHandle: 5, UserSuppliedTag: []byte("accepted-interaction")})
	check(t, err)
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: obj.ObjectHandle, Attributes: map[uint64][]byte{f.position: {2}}, LogicalTime: &ts, MessageRetractionHandle: 5, UserSuppliedTag: []byte("duplicate")})
	if status.Code(err) != codes.FailedPrecondition {
		t.Error("attribute update reused an ID accepted by an interaction")
	}
	f.barrier()
	for _, events := range f.callbacks[1:3] {
		updates, interactions := 0, 0
		for _, event := range events.snapshot() {
			if v := event.GetReflect(); v != nil {
				if bytes.Equal(v.UserSuppliedTag, []byte("duplicate")) {
					t.Error("rejected update emitted a callback")
				}
				if bytes.Equal(v.UserSuppliedTag, []byte("accepted-update")) {
					updates++
					if v.LogicalTime == nil || *v.LogicalTime != ts || v.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || v.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || v.Metadata.GetMessageRetractionHandle() != 80 {
						t.Errorf("accepted TSO update lost retraction metadata on RO receipt: %v", v)
					}
				}
			}
			if v := event.GetReceive(); v != nil {
				if bytes.Equal(v.UserSuppliedTag, []byte("duplicate")) {
					t.Error("rejected interaction emitted a callback")
				}
				if bytes.Equal(v.UserSuppliedTag, []byte("accepted-interaction")) {
					interactions++
					if v.LogicalTime == nil || *v.LogicalTime != ts || v.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || v.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || v.Metadata.GetMessageRetractionHandle() != 5 {
						t.Errorf("accepted TSO interaction lost retraction metadata on RO receipt: %v", v)
					}
				}
			}
		}
		if updates != 1 || interactions != 1 {
			t.Errorf("accepted fanout counts=(%d,%d), want (1,1)", updates, interactions)
		}
	}
}

func TestProductionRetractionIDRecordedWithoutRecipients(t *testing.T) {
	f := startFederation(t)
	cls, err := f.support.GetInteractionClassHandle(f.ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAinteractionRoot.RegionalSignal"})
	check(t, err)
	_, err = f.decl.PublishInteractionClass(f.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	// Consume an ID for a TSO send even when its fanout is empty.
	_, err = f.objects.ChangeInteractionOrderType(f.ctx, &rtiv1.ChangeInteractionOrderRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, OrderType: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP})
	check(t, err)
	_, err = f.timeService.EnableTimeRegulation(f.ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], Lookahead: 1})
	check(t, err)
	ts := float64(10)
	req := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, LogicalTime: &ts, MessageRetractionHandle: 40}
	_, err = f.objects.SendInteraction(f.ctx, req)
	check(t, err)
	_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	req.UserSuppliedTag = []byte("duplicate-empty-fanout")
	_, err = f.objects.SendInteraction(f.ctx, req)
	if status.Code(err) != codes.FailedPrecondition {
		t.Error("accepted zero-recipient send did not consume its retraction ID")
	}
	f.barrier()
	for _, event := range f.callbacks[1].snapshot() {
		if v := event.GetReceive(); v != nil && bytes.Equal(v.UserSuppliedTag, req.UserSuppliedTag) {
			t.Error("reused empty-fanout ID produced a callback")
		}
	}
}

func TestProductionRetractionIDSurvivesRetractAndDrain(t *testing.T) {
	f := startFederation(t)
	cls, err := f.support.GetInteractionClassHandle(f.ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAinteractionRoot.RegionalSignal"})
	check(t, err)
	_, err = f.decl.PublishInteractionClass(f.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	// Retraction and drain exercise TSO, not the FOM's Receive default.
	_, err = f.objects.ChangeInteractionOrderType(f.ctx, &rtiv1.ChangeInteractionOrderRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, OrderType: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP})
	check(t, err)
	_, err = f.timeService.EnableTimeRegulation(f.ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], Lookahead: 1})
	check(t, err)
	_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	_, err = f.timeService.EnableTimeConstrained(f.ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1]})
	check(t, err)
	ts := float64(10)
	req := &rtiv1.SendInteractionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, LogicalTime: &ts, MessageRetractionHandle: 70, UserSuppliedTag: []byte("retracted")}
	_, err = f.objects.SendInteraction(f.ctx, req)
	check(t, err)
	_, err = f.objects.Retract(f.ctx, &rtiv1.RetractRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], MessageRetractionHandle: 70})
	check(t, err)
	if _, err := f.objects.SendInteraction(f.ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reuse after retract=%v", err)
	}
	req.MessageRetractionHandle, req.UserSuppliedTag = 71, []byte("drained")
	_, err = f.objects.SendInteraction(f.ctx, req)
	check(t, err)
	// Move the regulating sender's bound beyond the queued timestamp.
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], LogicalTime: 10})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], LogicalTime: 10})
	check(t, err)
	drained := f.callbacks[1].wait(t, "drained interaction", func(e *rtiv1.FederateEvent) bool {
		return e.GetReceive() != nil && bytes.Equal(e.GetReceive().UserSuppliedTag, []byte("drained"))
	}).GetReceive()
	if drained.LogicalTime == nil || *drained.LogicalTime != ts || drained.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || drained.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || drained.Metadata.GetMessageRetractionHandle() != 71 {
		t.Fatalf("drained TSO interaction metadata=%v", drained)
	}
	ts = 20
	if _, err := f.objects.SendInteraction(f.ctx, req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reuse after drain=%v", err)
	}
	f.barrier()
	for _, e := range f.callbacks[1].snapshot() {
		if e.GetReceive() != nil && bytes.Equal(e.GetReceive().UserSuppliedTag, []byte("retracted")) {
			t.Fatal("retracted origin was delivered")
		}
	}
}
