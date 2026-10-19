package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.RTIexception;
import io.grpc.*;
import org.junit.jupiter.api.Test;
import rti.v1.Callback;
import rti.v1.Stream;

class OwnershipCallbackDeliveryTest {
    @Test void mandatoryReceiptsAcknowledgeEntryNotQueueAndRetryWithoutReinvoking() throws Exception {
        ExactTimeWireTest.RecordingChannel wire = new ExactTimeWireTest.RecordingChannel();
        OwnershipCallbackDelivery delivery = new OwnershipCallbackDelivery(context(wire));
        delivery.enable(); wire.requests.clear();
        Stream.FederateEvent event = terminal(1);
        delivery.observe(event); assertTrue(wire.requests.isEmpty());
        assertTrue(delivery.begin(event));
        assertThrows(RTIexception.class, () -> {
            try { throw new RTIexception("user handler failed"); }
            finally { wire.failure = Status.UNAVAILABLE; delivery.flush(); }
        });
        wire.failure = null;
        assertFalse(delivery.begin(terminal(2))); delivery.flush();
        Callback.ReportOwnershipCallbackInvocationRequest report = (Callback.ReportOwnershipCallbackInvocationRequest) wire.requests.get(wire.requests.size() - 1);
        assertEquals(2, report.getReceipt().getDeliveryAttempt());
    }

    @Test void localDeletionFiltersAdviceButNeverTerminalCallbacks() throws Exception {
        ExactTimeWireTest.RecordingChannel wire = new ExactTimeWireTest.RecordingChannel();
        OwnershipCallbackDelivery delivery = new OwnershipCallbackDelivery(context(wire)); delivery.enable();
        Stream.FederateEvent old = Stream.FederateEvent.newBuilder().setObjectKnowledgeEpoch(2)
                .setOwnershipAssumption(Stream.RequestAttributeOwnershipAssumption.newBuilder().setObjectHandle(31)).build();
        delivery.retire(31, 2); assertFalse(delivery.begin(old));
        Stream.FederateEvent rediscovery = Stream.FederateEvent.newBuilder().setObjectKnowledgeEpoch(3)
                .setDiscover(Stream.DiscoverObjectInstance.newBuilder().setObjectHandle(31)).build();
        delivery.observe(rediscovery); assertTrue(delivery.begin(rediscovery)); assertTrue(delivery.begin(terminal(1)));
    }

    private static Stream.FederateEvent terminal(long attempt) {
        return Stream.FederateEvent.newBuilder().setObjectKnowledgeEpoch(1)
                .setOwnershipUnavailable(Stream.AttributeOwnershipUnavailable.newBuilder().setObjectHandle(31).addAttributeHandles(2))
                .setOwnershipReceipt(Callback.OwnershipCallbackReceipt.newBuilder().setFederationGeneration(9).setCallbackId(4).setDeliveryAttempt(attempt)).build();
    }
    private static ServiceContext context(Channel wire) {
        return new ServiceContext() {
            public void requireMember() { }
            public Channel channel() { return wire; }
            public String federation() { return "fed"; }
            public long federate() { return 7; }
            public long generation() { return 9; }
            public boolean regulating() { return false; }
            public double logicalTime() { return 0; }
        };
    }
}
