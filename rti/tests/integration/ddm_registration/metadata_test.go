package ddm_registration_test

import (
	"bytes"
	"testing"

	rtiv1 "github.com/cbchoi/gorti/rti/internal/adapters/protobuf/rti/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProductionObjectMetadataAndOrderControls(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	f.reserve("metadata")
	r, err := f.register("metadata", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{f.region(0, 10, 20)}})
	check(t, err)
	tag := []byte{0, 255, 'm'}
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {7}}, UserSuppliedTag: tag})
	check(t, err)
	f.barrier()
	var got *rtiv1.ReflectAttributeValues
	for _, event := range f.callbacks[1].snapshot() {
		if reflect := event.GetReflect(); reflect != nil && reflect.ObjectHandle == r.ObjectHandle {
			got = reflect
		}
	}
	if got == nil || !bytes.Equal(got.UserSuppliedTag, tag) || got.Metadata == nil || got.Metadata.GetProducingFederate() != f.handles[0] || got.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE {
		t.Errorf("reflect metadata lost: %v", got)
	}
	_, err = f.objects.ChangeAttributeOrderType(f.ctx, &rtiv1.ChangeAttributeOrderRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, AttributeHandles: []uint64{f.position}, OrderType: rtiv1.OrderType_ORDER_TYPE_RECEIVE})
	check(t, err)
	ts := float64(50)
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name,
		FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {8}}, LogicalTime: &ts, UserSuppliedTag: tag})
	check(t, err)
	f.barrier()
	for _, event := range f.callbacks[1].snapshot() {
		if reflect := event.GetReflect(); reflect != nil && bytes.Equal(reflect.Attributes[f.position], []byte{8}) {
			if reflect.LogicalTime == nil || *reflect.LogicalTime != ts || reflect.Metadata == nil || reflect.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || reflect.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE {
				t.Errorf("receive order override not applied: %v", reflect)
			}
			return
		}
	}
	t.Fatal("missing receive-order update after order change")
}

func TestProductionMixedOrderAndRegionalMetadata(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	region := f.region(0, 10, 20)
	f.reserve("mixed-order")
	r, err := f.register("mixed-order", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{region}}, &rtiv1.AttributeRegions{AttributeHandle: f.velocity, RegionHandles: []uint64{region}})
	check(t, err)
	_, err = f.timeService.EnableTimeConstrained(f.ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1]})
	check(t, err)
	_, err = f.timeService.EnableTimeRegulation(f.ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], Lookahead: 1})
	check(t, err)
	_, err = f.timeService.EnableAsynchronousDelivery(f.ctx, &rtiv1.EnableAsynchronousDeliveryRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1]})
	check(t, err)
	for attr, order := range map[uint64]rtiv1.OrderType{f.position: rtiv1.OrderType_ORDER_TYPE_RECEIVE, f.velocity: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP} {
		_, err = f.objects.ChangeAttributeOrderType(f.ctx, &rtiv1.ChangeAttributeOrderRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, AttributeHandles: []uint64{attr}, OrderType: order})
		check(t, err)
	}
	ts := float64(10)
	_, err = f.objects.RequestAttributeTransportationTypeChange(f.ctx, &rtiv1.ChangeAttributeTransportRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, AttributeHandles: []uint64{f.velocity}, TransportType: rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT})
	check(t, err)
	tag := []byte{0, 255, 's'}
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {1}, f.velocity: {2}}, LogicalTime: &ts, UserSuppliedTag: tag, MessageRetractionHandle: 91})
	check(t, err)
	f.barrier()
	count := 0
	for _, event := range f.callbacks[1].snapshot() {
		v := event.GetReflect()
		if v == nil || v.ObjectHandle != r.ObjectHandle {
			continue
		}
		count++
		if len(v.Attributes) != 1 || len(v.Attributes[f.position]) != 1 || v.LogicalTime == nil || *v.LogicalTime != ts || v.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || v.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_RECEIVE || v.Metadata.MessageRetractionHandle != nil {
			t.Errorf("mixed RO callback=%v", v)
		}
	}
	if count != 1 {
		t.Fatalf("before advance reflects=%d want1", count)
	}
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], LogicalTime: 10})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], LogicalTime: 10})
	check(t, err)
	got := f.callbacks[1].wait(t, "timestamp reflection", func(e *rtiv1.FederateEvent) bool {
		return e.GetReflect() != nil && len(e.GetReflect().Attributes[f.velocity]) > 0
	}).GetReflect()
	if got.Metadata.GetTransportationType() != rtiv1.TransportationType_TRANSPORTATION_TYPE_BEST_EFFORT {
		t.Errorf("transport override ignored: %v", got)
	}
	if len(got.Attributes) != 1 || !bytes.Equal(got.UserSuppliedTag, tag) || got.Metadata.GetSentOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || got.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP || got.Metadata.GetProducingFederate() != f.handles[0] || got.Metadata.GetMessageRetractionHandle() != 91 {
		t.Errorf("timestamp metadata=%v", got)
	}
	f.expectCopiedRegion(1, got.Metadata.GetReceivedRegions(), region, 10, 20)

	cls, err := f.support.GetInteractionClassHandle(f.ctx, &rtiv1.GetInteractionClassHandleRequest{WireVersion: 1, FederationName: f.name, ClassName: "HLAinteractionRoot.RegionalSignal"})
	check(t, err)
	_, err = f.decl.PublishInteractionClass(f.ctx, &rtiv1.PubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	_, err = f.decl.SubscribeInteractionClass(f.ctx, &rtiv1.SubInterRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], InteractionClassHandle: cls.ClassHandle})
	check(t, err)
	_, err = f.objects.ChangeInteractionOrderType(f.ctx, &rtiv1.ChangeInteractionOrderRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, OrderType: rtiv1.OrderType_ORDER_TYPE_TIMESTAMP})
	check(t, err)
	ts = 20
	_, err = f.ddm.SendInteractionWithRegions(f.ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, RegionHandles: []uint64{region}, LogicalTime: &ts, UserSuppliedTag: tag, MessageRetractionHandle: 92})
	check(t, err)
	_, err = f.objects.Retract(f.ctx, &rtiv1.RetractRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], MessageRetractionHandle: 92})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], LogicalTime: 20})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], LogicalTime: 20})
	check(t, err)
	f.barrier()
	for _, event := range f.callbacks[1].snapshot() {
		if event.GetReceive() != nil && event.GetReceive().InteractionClassHandle == cls.ClassHandle {
			t.Fatal("retracted regional interaction delivered")
		}
	}
	ts = 30
	_, err = f.ddm.SendInteractionWithRegions(f.ctx, &rtiv1.SendInteractionWithRegionsRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], InteractionClassHandle: cls.ClassHandle, RegionHandles: []uint64{region}, LogicalTime: &ts, UserSuppliedTag: tag, MessageRetractionHandle: 93})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], LogicalTime: 30})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], LogicalTime: 30})
	check(t, err)
	interaction := f.callbacks[1].wait(t, "regional metadata", func(e *rtiv1.FederateEvent) bool {
		return e.GetReceive() != nil && e.GetReceive().InteractionClassHandle == cls.ClassHandle
	}).GetReceive()
	if !bytes.Equal(interaction.UserSuppliedTag, tag) || interaction.Metadata.GetMessageRetractionHandle() != 93 {
		t.Errorf("regional metadata=%v", interaction)
	}
	f.expectCopiedRegion(1, interaction.Metadata.GetReceivedRegions(), region, 10, 20)
}

func (f *federation) expectCopiedRegion(who int, received *rtiv1.ReceivedRegions, source, lower, upper uint64) {
	f.t.Helper()
	handles := received.GetRegionHandles()
	if len(handles) != 1 || handles[0] == 0 || handles[0] == source {
		f.t.Fatalf("expected one immutable copied designator, not source %d: %v", source, received)
	}
	log := f.callbacks[who]
	log.mu.Lock()
	bounds := log.copiedBounds[handles[0]]
	log.mu.Unlock()
	if bounds == nil || bounds.GetLower() != lower || bounds.GetUpper() != upper {
		f.t.Errorf("copied callback bounds = %v, want [%d,%d]", bounds, lower, upper)
	}
}

func TestProductionRetractableDeleteLifecycleAndDDMCleanup(t *testing.T) {
	f := startFederation(t)
	f.subscribe(1)
	region := f.region(0, 10, 20)
	f.reserve("retractable-delete")
	r, err := f.register("retractable-delete", &rtiv1.AttributeRegions{AttributeHandle: f.position, RegionHandles: []uint64{region}})
	check(t, err)
	_, err = f.timeService.EnableTimeConstrained(f.ctx, &rtiv1.EnableConstrainedRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1]})
	check(t, err)
	_, err = f.timeService.EnableTimeRegulation(f.ctx, &rtiv1.EnableRegulationRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], Lookahead: 1})
	check(t, err)
	ts := float64(10)
	_, err = f.objects.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, LogicalTime: &ts, UserSuppliedTag: []byte{0, 255}, MessageRetractionHandle: 99})
	check(t, err)
	earlier := float64(5)
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {7}}, LogicalTime: &earlier})
	check(t, err)
	_, err = f.objects.Retract(f.ctx, &rtiv1.RetractRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], MessageRetractionHandle: 99})
	check(t, err)
	_, err = f.objects.UpdateAttributeValues(f.ctx, &rtiv1.UpdateAttributeValuesRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, Attributes: map[uint64][]byte{f.position: {8}}})
	check(t, err)
	f.barrier()
	for _, event := range f.callbacks[1].snapshot() {
		if remove := event.GetRemove(); remove != nil && remove.ObjectHandle == r.ObjectHandle {
			t.Fatal("retracted future delete emitted removal")
		}
	}
	_, err = f.objects.DeleteObjectInstance(f.ctx, &rtiv1.DeleteObjectInstanceRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], ObjectHandle: r.ObjectHandle, LogicalTime: &ts, UserSuppliedTag: []byte{0, 255}, MessageRetractionHandle: 100})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], LogicalTime: 10})
	check(t, err)
	_, err = f.timeService.TimeAdvanceRequest(f.ctx, &rtiv1.TARRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[1], LogicalTime: 10})
	check(t, err)
	remove := f.callbacks[1].wait(t, "deferred object removal", func(e *rtiv1.FederateEvent) bool {
		return e.GetRemove() != nil && e.GetRemove().ObjectHandle == r.ObjectHandle
	}).GetRemove()
	if !bytes.Equal(remove.UserSuppliedTag, []byte{0, 255}) || remove.Metadata.GetMessageRetractionHandle() != 100 || remove.Metadata.GetProducingFederate() != f.handles[0] || remove.Metadata.GetReceivedOrder() != rtiv1.OrderType_ORDER_TYPE_TIMESTAMP {
		t.Fatalf("removal metadata=%v", remove)
	}
	_, err = f.objects.Retract(f.ctx, &rtiv1.RetractRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], MessageRetractionHandle: 100})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late deletion retract=%v", err)
	}
	f.lookup("retractable-delete", 0)
	_, err = f.ddm.DeleteRegion(f.ctx, &rtiv1.DeleteRegionRequest{WireVersion: 1, FederationName: f.name, FederateHandle: f.handles[0], RegionHandle: region})
	check(t, err)
}
