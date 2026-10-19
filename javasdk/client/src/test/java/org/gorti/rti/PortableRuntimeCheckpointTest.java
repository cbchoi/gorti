package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import com.google.protobuf.ByteString;
import java.io.*;
import java.util.List;
import java.util.Set;
import hla.rti1516e.LogicalTime;
import org.gorti.rti.types.Integer64TimeFactory;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import rti.v1.Stream;

class PortableRuntimeCheckpointTest {
    private RuntimeCheckpoint checkpoint(LogicalTime time, List<Stream.FederateEvent> events) throws Exception {
        ExchangeKnowledge knowledge = new ExchangeKnowledge();
        knowledge.discovered(10, 20, "known");
        DdmServices.State ddm = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
        return new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 3), ddm, Set.of("sync"),
                true, false, false, false, true, time, events);
    }

    @Test void journalPreservesExactTimePayloadAndReceiptForServerReconciliation() throws Exception {
        long exact = (1L << 53) + 1;
        LogicalTime time = new Integer64TimeFactory().makeTime(exact);
        Stream.FederateEvent event = Stream.FederateEvent.newBuilder()
                .setCallbackReceipt(ByteString.copyFromUtf8("old-ticket"))
                .setGrant(TimeWire.write(Stream.TimeAdvanceGrant.newBuilder(), "logical_time", time, TimeWire.INTEGER)).build();
        RuntimeCheckpoint source = checkpoint(time, List.of(event));
        byte[] encoded = source.encode("fed", 7, 8, "saved");
        assertEquals(0x47524a33, new DataInputStream(new ByteArrayInputStream(encoded)).readInt());
        RuntimeCheckpoint restored = RuntimeCheckpoint.decode(encoded, "fed", 7, 8, "saved");
        restored.validateTime(TimeWire.INTEGER);
        assertEquals(time, restored.logicalTimeValue);
        assertEquals(List.of(event), restored.pendingCallbacks);
        assertThrows(UnsupportedOperationException.class, () -> restored.pendingCallbacks.clear());
    }

    @Test void emptyJournalRetainsGrj2EncodingAndReader() throws Exception {
        byte[] encoded = checkpoint(TimeValues.time(2), List.of()).encode("fed", 7, 8, "saved");
        assertEquals(0x47524a32, new DataInputStream(new ByteArrayInputStream(encoded)).readInt());
        assertTrue(RuntimeCheckpoint.decode(encoded, "fed", 7, 8, "saved").pendingCallbacks.isEmpty());
    }

    @Test void restoreDecoderUsesValidatedTargetIdentityWithoutAssumingAnnouncedSourceHandle() throws Exception {
        byte[] encoded = checkpoint(TimeValues.time(2), List.of()).encode("fed", 9, 41, "saved");
        assertEquals(41, RuntimeCheckpoint.decodeForRestore(encoded, "fed", 9, "saved").persistedHandle);
        assertThrows(IOException.class, () -> RuntimeCheckpoint.decodeForRestore(encoded, "fed", 7, "saved"));
        assertThrows(IOException.class, () -> RuntimeCheckpoint.decodeForRestore(encoded, "other", 9, "saved"));
        assertThrows(IOException.class, () -> RuntimeCheckpoint.decodeForRestore(encoded, "fed", 9, "other"));
    }

    @Test void timeFactoryValidationIncludesRetractableRemovalTimes() throws Exception {
        ExchangeKnowledge knowledge = new ExchangeKnowledge();
        knowledge.discovered(10, 20, "known");
        knowledge.deleted(10, 8, 3, TimeValues.time(4));
        DdmServices.State ddm = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
        RuntimeCheckpoint state = new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 3), ddm, Set.of(),
                true, false, false, false, false, new Integer64TimeFactory().makeTime(2), List.of());
        assertThrows(IOException.class, () -> state.validateTime(TimeWire.INTEGER));
    }

    @Test void journalRejectsRestoreControlAndUnknownEvents() throws Exception {
        Stream.FederateEvent terminal = Stream.FederateEvent.newBuilder()
                .setRestoreCompleted(Stream.FederationRestored.newBuilder().setLabel("saved")).build();
        assertThrows(IOException.class, () -> checkpoint(TimeValues.time(2), List.of(terminal)).encode("fed", 7, 8, "saved"));
        assertThrows(IOException.class, () -> checkpoint(TimeValues.time(2), List.of(Stream.FederateEvent.getDefaultInstance()))
                .encode("fed", 7, 8, "saved"));
    }

    @Test void restoredCallbackEpochDiscardsOldOutcomeRetryWithoutReportingIt() throws Exception {
        java.util.ArrayList<String> reports = new java.util.ArrayList<>();
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            reports.add(entry ? "entry" : "outcome");
            if (!entry) throw new hla.rti1516e.exceptions.RTIinternalError("temporarily unavailable");
        }, () -> true);
        Stream.FederateEvent event = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFrom(new byte[32]))
                .setDiscover(Stream.DiscoverObjectInstance.newBuilder().setObjectHandle(10).setObjectClassHandle(20).setObjectName("known")).build();
        assertTrue(delivery.begin(event));
        assertThrows(hla.rti1516e.exceptions.RTIinternalError.class, () -> delivery.complete(event, null));
        delivery.restoreEpoch();
        delivery.complete(event, null);
        delivery.flush();
        assertEquals(List.of("entry", "outcome"), reports);
    }
}
