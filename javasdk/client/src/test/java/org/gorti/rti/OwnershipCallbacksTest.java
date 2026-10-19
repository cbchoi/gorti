package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.FederateInternalError;
import java.util.List;
import java.util.Set;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.Stream;

class OwnershipCallbacksTest {
    @Test void allFiveOwnershipEventsSelectTheirStandardCallback() throws Exception {
        List<Stream.FederateEvent> events = events();
        for (int i = 0; i < events.size(); i++) {
            Recording callback = new Recording();
            assertTrue(OwnershipCallbacks.deliver(events.get(i), callback));
            assertEquals(List.of("assumption", "acquired", "confirm", "release", "unavailable").get(i), callback.kind);
            assertEquals(Handles.objectInstance(31), callback.object);
            assertEquals(Set.of(Handles.attribute(51), Handles.attribute(52)), callback.attributes);
            if (i == 0 || i == 1 || i == 3) assertArrayEquals(new byte[]{0, -1, 7}, callback.tag);
            else assertNull(callback.tag);
        }
    }

    @Test void callbackPayloadsAreFreshAndCannotMutateWireEvent() throws Exception {
        for (Stream.FederateEvent event : events()) {
            Recording first = new Recording();
            OwnershipCallbacks.deliver(event, first);
            first.attributes.clear();
            if (first.tag != null) first.tag[0] = 99;
            Recording next = new Recording();
            OwnershipCallbacks.deliver(event, next);
            assertEquals(2, next.attributes.size());
            if (next.tag != null) assertArrayEquals(new byte[]{0, -1, 7}, next.tag);
        }
    }

    @Test void unrelatedEventsRemainAvailableForOtherAdapters() throws Exception {
        Recording target = new Recording();
        assertFalse(OwnershipCallbacks.deliver(Stream.FederateEvent.getDefaultInstance(), target));
        assertFalse(OwnershipCallbacks.deliver(Stream.FederateEvent.newBuilder().setDiscover(
                Stream.DiscoverObjectInstance.newBuilder().setObjectHandle(31)).build(), target));
        assertNull(target.kind);
    }

    @Test void userCallbackFailuresAreNotSwallowedOrRetried() {
        FederateInternalError failure = new FederateInternalError("user callback failed");
        NullFederateAmbassador callback = new NullFederateAmbassador() {
            @Override public void requestDivestitureConfirmation(ObjectInstanceHandle object, AttributeHandleSet attributes) throws FederateInternalError { throw failure; }
        };
        assertSame(failure, assertThrows(FederateInternalError.class, () -> OwnershipCallbacks.deliver(events().get(2), callback)));
    }

    private static List<Stream.FederateEvent> events() {
        List<Long> attributes = List.of(51L, 52L);
        ByteString tag = ByteString.copyFrom(new byte[]{0, -1, 7});
        return List.of(
                Stream.FederateEvent.newBuilder().setOwnershipAssumption(Stream.RequestAttributeOwnershipAssumption.newBuilder().setObjectHandle(31).addAllAttributeHandles(attributes).setDivestingFederate(7).setTag(tag)).build(),
                Stream.FederateEvent.newBuilder().setOwnershipAcquired(Stream.AttributeOwnershipAcquisitionNotification.newBuilder().setObjectHandle(31).addAllAttributeHandles(attributes).setOwningFederate(8).setTag(tag)).build(),
                Stream.FederateEvent.newBuilder().setOwnershipDivestConfirmed(Stream.RequestDivestitureConfirmation.newBuilder().setObjectHandle(31).addAllAttributeHandles(attributes)).build(),
                Stream.FederateEvent.newBuilder().setOwnershipReleaseRequested(Stream.RequestAttributeOwnershipRelease.newBuilder().setObjectHandle(31).addAllAttributeHandles(attributes).setTag(tag)).build(),
                Stream.FederateEvent.newBuilder().setOwnershipUnavailable(Stream.AttributeOwnershipUnavailable.newBuilder().setObjectHandle(31).addAllAttributeHandles(attributes)).build());
    }

    private static final class Recording extends NullFederateAmbassador {
        String kind;
        ObjectInstanceHandle object;
        AttributeHandleSet attributes;
        byte[] tag;
        private void record(String kind, ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) {
            assertNull(this.kind);
            this.kind = kind; this.object = object; this.attributes = attributes; this.tag = tag;
        }
        @Override public void requestAttributeOwnershipAssumption(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) { record("assumption", object, attributes, tag); }
        @Override public void attributeOwnershipAcquisitionNotification(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) { record("acquired", object, attributes, tag); }
        @Override public void requestDivestitureConfirmation(ObjectInstanceHandle object, AttributeHandleSet attributes) { record("confirm", object, attributes, null); }
        @Override public void requestAttributeOwnershipRelease(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) { record("release", object, attributes, tag); }
        @Override public void attributeOwnershipUnavailable(ObjectInstanceHandle object, AttributeHandleSet attributes) { record("unavailable", object, attributes, null); }
    }
}
