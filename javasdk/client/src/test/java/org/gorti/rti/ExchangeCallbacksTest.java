package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.RTIinternalError;
import hla.rti1516e.time.HLAfloat64Time;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Stream;

class ExchangeCallbacksTest {
    private static final ByteString DATA = ByteString.copyFrom(new byte[]{0, 1, -1});
    private static final ByteString TAG = ByteString.copyFrom(new byte[]{4, 0, 5});

    @Test void selectsAllNineObjectCallbackOverloads() throws Exception {
        for (String kind : List.of("reflect", "receive", "remove")) {
            for (int form = 0; form < 3; form++) {
                Recording callback = new Recording();
                deliver(event(kind, form), callback, 9);
                assertEquals(List.of(kind + form), callback.called);
                assertArrayEquals(TAG.toByteArray(), callback.tag);
                assertEquals(form == 0 ? OrderType.RECEIVE : OrderType.TIMESTAMP, callback.sent);
                if (form > 0) assertEquals(3.0, ((HLAfloat64Time) callback.time).getValue());
                if (form == 2) assertEquals(Handles.retraction("test", 9, 7, 12), callback.retraction);
                else assertNull(callback.retraction);
                assertEquals(Handles.federate(7), callback.producer);
            }
        }
    }

    @Test void copiesWirePayloadAndSentRegions() throws Exception {
        Recording callback = new Recording();
        Stream.FederateEvent event = event("reflect", 2);
        deliver(event, callback, 9);
        assertArrayEquals(DATA.toByteArray(), callback.values.get(Handles.attribute(4)));
        assertTrue(callback.reflectInfo.hasSentRegions());
        RegionHandleSet regions = callback.reflectInfo.getSentRegions();
        assertEquals(Set.of(Handles.region(11)), regions);
        regions.clear();
        assertEquals(Set.of(Handles.region(11)), callback.reflectInfo.getSentRegions());
        callback.values.get(Handles.attribute(4))[0] = 99;
        assertEquals(0, event.getReflect().getAttributesOrThrow(4).byteAt(0));
    }

    @Test void missingOrInvalidMetadataNeverInventsDeliveryFacts() {
        Stream.FederateEvent missing = Stream.FederateEvent.newBuilder().setReceive(
                Stream.ReceiveInteraction.newBuilder().setInteractionClassHandle(2)).build();
        assertThrows(RTIinternalError.class, () -> deliver(missing, new Recording(), 9));
        Stream.FederateEvent incomplete = event("receive", 2).toBuilder().setReceive(event("receive", 2).getReceive()
                .toBuilder().setMetadata(metadata(2).clearProducingFederate())).build();
        assertThrows(RTIinternalError.class, () -> deliver(incomplete, new Recording(), 9));
    }

    @Test void timestampedReceiveOrderDoesNotExposeRetractionHandle() throws Exception {
        Recording callback = new Recording();
        Stream.FederateEvent value = event("receive", 1);
        deliver(value.toBuilder().setReceive(value.getReceive().toBuilder()
                .setMetadata(metadata(1).setMessageRetractionHandle(88))).build(), callback, 9);
        assertEquals(List.of("receive1"), callback.called);
        assertNull(callback.retraction);
    }

    @Test void discoveryChoosesProducingFederateOverloadOnlyWhenPresent() throws Exception {
        List<String> calls = new ArrayList<>();
        NullFederateAmbassador callback = new NullFederateAmbassador() {
            @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { calls.add("plain"); }
            @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name, FederateHandle producer) { calls.add("producer"); }
        };
        Stream.DiscoverObjectInstance.Builder value = Stream.DiscoverObjectInstance.newBuilder().setObjectHandle(1).setObjectClassHandle(2).setObjectName("item");
        deliver(Stream.FederateEvent.newBuilder().setDiscover(value).build(), callback, 9);
        deliver(Stream.FederateEvent.newBuilder().setDiscover(value.setProducingFederate(7)).build(), callback, 9);
        assertEquals(List.of("plain", "producer"), calls);
    }

    @Test void synchronizationFailureAndReservationFailureKeepTheirFullPayload() throws Exception {
        List<String> calls = new ArrayList<>();
        NullFederateAmbassador callback = new NullFederateAmbassador() {
            @Override public void federationSynchronized(String label, FederateHandleSet failed) {
                assertEquals("ready", label); assertEquals(Set.of(Handles.federate(8)), failed); calls.add("sync");
            }
            @Override public void multipleObjectInstanceNameReservationFailed(Set<String> requested) {
                assertEquals(Set.of("a", "b"), requested); calls.add("names");
            }
        };
        deliver(Stream.FederateEvent.newBuilder().setSyncSynchronized(
                Stream.FederationSynchronized.newBuilder().setLabel("ready").addFailedToSync(8)).build(), callback, 9);
        deliver(Stream.FederateEvent.newBuilder().setReservationMultiFailed(
                Stream.MultipleObjectInstanceNameReservationFailed.newBuilder().addRequestedNames("a")
                        .addRequestedNames("b").addCollidingNames("a")).build(), callback, 9);
        assertEquals(List.of("sync", "names"), calls);
    }

    @Test void nativeTimestampedDeliveriesWithoutRetractionIdsStillReachAllThreeOverloads() throws Exception {
        Set<MessageRetractionHandle> identities = new java.util.HashSet<>();
        for (String kind : List.of("reflect", "receive", "remove")) {
            for (boolean absent : List.of(false, true)) {
                Stream.ObjectCallbackMetadata.Builder meta = metadata(2);
                if (absent) meta.clearMessageRetractionHandle(); else meta.setMessageRetractionHandle(0);
                Stream.FederateEvent.Builder value = event(kind, 2).toBuilder();
                if (kind.equals("reflect")) value.setReflect(value.getReflect().toBuilder().setMetadata(meta));
                else if (kind.equals("receive")) value.setReceive(value.getReceive().toBuilder().setMetadata(meta));
                else value.setRemove(value.getRemove().toBuilder().setMetadata(meta));
                Recording callback = new Recording();
                deliver(value.build(), callback, 9);
                assertEquals(List.of(kind + 2), callback.called);
                assertNotNull(callback.retraction);
                assertTrue(identities.add(callback.retraction));
                assertThrows(IllegalArgumentException.class, () -> Handles.value(callback.retraction));
            }
        }
    }

    @Test void removalDoesNotCommitBeforeSuccessfulDecodeButCommitsAfterThrowingUserCallback() throws Exception {
        int[] committed = {0};
        Stream.FederateEvent valid = event("remove", 0);
        Stream.FederateEvent invalid = valid.toBuilder().setRemove(valid.getRemove().toBuilder()
                .setMetadata(metadata(0).clearSentOrder())).build();
        assertThrows(RTIinternalError.class, () -> ExchangeCallbacks.deliver(invalid, new Recording(), "test", 9, () -> committed[0]++));
        assertEquals(0, committed[0]);
        AssertionError failure = new AssertionError("user callback failure");
        NullFederateAmbassador throwing = new NullFederateAmbassador() {
            @Override public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType order, SupplementalRemoveInfo info) {
                throw failure;
            }
        };
        assertSame(failure, assertThrows(AssertionError.class, () ->
                ExchangeCallbacks.deliver(valid, throwing, "test", 9, () -> committed[0]++)));
        assertEquals(1, committed[0]);
    }

    private static boolean deliver(Stream.FederateEvent event, FederateAmbassador target, long generation) throws hla.rti1516e.exceptions.RTIexception {
        return ExchangeCallbacks.deliver(event, target, "test", generation, () -> { });
    }

    private static Stream.ObjectCallbackMetadata.Builder metadata(int form) {
        return Stream.ObjectCallbackMetadata.newBuilder()
                .setSentOrder(form == 0 ? rti.v1.Object.OrderType.ORDER_TYPE_RECEIVE : rti.v1.Object.OrderType.ORDER_TYPE_TIMESTAMP)
                .setReceivedOrder(form == 2 ? rti.v1.Object.OrderType.ORDER_TYPE_TIMESTAMP : rti.v1.Object.OrderType.ORDER_TYPE_RECEIVE)
                .setTransportationType(Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE).setProducingFederate(7)
                .setMessageRetractionHandle(12).setReceivedRegions(Stream.ReceivedRegions.newBuilder().addRegionHandles(11));
    }
    private static Stream.FederateEvent event(String kind, int form) {
        Stream.FederateEvent.Builder event = Stream.FederateEvent.newBuilder();
        if (kind.equals("reflect")) {
            Stream.ReflectAttributeValues.Builder value = Stream.ReflectAttributeValues.newBuilder().setObjectHandle(1)
                    .putAttributes(4, DATA).setUserSuppliedTag(TAG).setMetadata(metadata(form));
            if (form > 0) value.setLogicalTime(3);
            event.setReflect(value);
        } else if (kind.equals("receive")) {
            Stream.ReceiveInteraction.Builder value = Stream.ReceiveInteraction.newBuilder().setInteractionClassHandle(2)
                    .putParameters(4, DATA).setUserSuppliedTag(TAG).setMetadata(metadata(form));
            if (form > 0) value.setLogicalTime(3);
            event.setReceive(value);
        } else {
            Stream.RemoveObjectInstance.Builder value = Stream.RemoveObjectInstance.newBuilder().setObjectHandle(1)
                    .setUserSuppliedTag(TAG).setMetadata(metadata(form));
            if (form > 0) value.setLogicalTime(3);
            event.setRemove(value);
        }
        return event.build();
    }

    private static final class Recording extends NullFederateAmbassador {
        final List<String> called = new ArrayList<>();
        byte[] tag; OrderType sent; LogicalTime time; MessageRetractionHandle retraction; FederateHandle producer;
        AttributeHandleValueMap values; SupplementalReflectInfo reflectInfo;
        private void record(String kind, int form, byte[] tag, OrderType sent, LogicalTime time, MessageRetractionHandle retraction, FederateHandle producer) {
            called.add(kind + form); this.tag = tag; this.sent = sent; this.time = time; this.retraction = retraction; this.producer = producer;
        }
        @Override public void reflectAttributeValues(ObjectInstanceHandle o, AttributeHandleValueMap v, byte[] t, OrderType s, TransportationTypeHandle tr, SupplementalReflectInfo i) { values=v; reflectInfo=i; record("reflect",0,t,s,null,null,i.getProducingFederate()); }
        @Override public void reflectAttributeValues(ObjectInstanceHandle o, AttributeHandleValueMap v, byte[] t, OrderType s, TransportationTypeHandle tr, LogicalTime time, OrderType r, SupplementalReflectInfo i) { values=v; reflectInfo=i; record("reflect",1,t,s,time,null,i.getProducingFederate()); assertEquals(OrderType.RECEIVE,r); }
        @Override public void reflectAttributeValues(ObjectInstanceHandle o, AttributeHandleValueMap v, byte[] t, OrderType s, TransportationTypeHandle tr, LogicalTime time, OrderType r, MessageRetractionHandle h, SupplementalReflectInfo i) { values=v; reflectInfo=i; record("reflect",2,t,s,time,h,i.getProducingFederate()); assertEquals(OrderType.TIMESTAMP,r); }
        @Override public void receiveInteraction(InteractionClassHandle c, ParameterHandleValueMap p, byte[] t, OrderType s, TransportationTypeHandle tr, SupplementalReceiveInfo i) { record("receive",0,t,s,null,null,i.getProducingFederate()); }
        @Override public void receiveInteraction(InteractionClassHandle c, ParameterHandleValueMap p, byte[] t, OrderType s, TransportationTypeHandle tr, LogicalTime time, OrderType r, SupplementalReceiveInfo i) { record("receive",1,t,s,time,null,i.getProducingFederate()); assertEquals(OrderType.RECEIVE,r); }
        @Override public void receiveInteraction(InteractionClassHandle c, ParameterHandleValueMap p, byte[] t, OrderType s, TransportationTypeHandle tr, LogicalTime time, OrderType r, MessageRetractionHandle h, SupplementalReceiveInfo i) { record("receive",2,t,s,time,h,i.getProducingFederate()); assertEquals(OrderType.TIMESTAMP,r); }
        @Override public void removeObjectInstance(ObjectInstanceHandle o, byte[] t, OrderType s, SupplementalRemoveInfo i) { record("remove",0,t,s,null,null,i.getProducingFederate()); }
        @Override public void removeObjectInstance(ObjectInstanceHandle o, byte[] t, OrderType s, LogicalTime time, OrderType r, SupplementalRemoveInfo i) { record("remove",1,t,s,time,null,i.getProducingFederate()); assertEquals(OrderType.RECEIVE,r); }
        @Override public void removeObjectInstance(ObjectInstanceHandle o, byte[] t, OrderType s, LogicalTime time, OrderType r, MessageRetractionHandle h, SupplementalRemoveInfo i) { record("remove",2,t,s,time,h,i.getProducingFederate()); assertEquals(OrderType.TIMESTAMP,r); }
    }
}
