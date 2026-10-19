package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.exceptions.RTIinternalError;
import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;
import rti.v1.Stream;
import static org.junit.jupiter.api.Assertions.*;

class CallbackInvocationDeliveryTest {
    @Test void oneFailedOutcomeDoesNotPreventOtherFinishedOutcomesBeingReported() throws Exception {
        java.util.Set<ByteString> accepted = new java.util.HashSet<>();
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            if (!entry && event.getSeq() == 0) throw new RTIinternalError("offline");
            if (!entry) accepted.add(event.getCallbackReceipt());
        }, () -> true);
        Stream.FederateEvent first = event();
        Stream.FederateEvent second = first.toBuilder().setSeq(1).setCallbackReceipt(ByteString.copyFrom(new byte[31]).concat(ByteString.copyFrom(new byte[]{2}))).build();
        delivery.begin(first); assertThrows(RTIinternalError.class, () -> delivery.complete(first, null));
        delivery.begin(second); assertThrows(RTIinternalError.class, () -> delivery.complete(second, null));
        assertEquals(java.util.Set.of(second.getCallbackReceipt()), accepted);
        assertEquals(1, delivery.checkpointOutcomes().size());
    }
    @Test void runningBodyCannotBeCheckpointedButFinishedFailedReportCan() throws Exception {
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            if (!entry) throw new RTIinternalError("offline");
        }, () -> true);
        Stream.FederateEvent event = event();
        assertTrue(delivery.begin(event));
        assertThrows(java.io.IOException.class, delivery::checkpointOutcomes);
        assertThrows(RTIinternalError.class, () -> delivery.complete(event, new IllegalArgumentException()));
        List<CallbackInvocationDelivery.Outcome> outcomes = delivery.checkpointOutcomes();
        assertEquals(1, outcomes.size());
        assertFalse(outcomes.get(0).success);
        assertEquals("IllegalArgumentException", outcomes.get(0).exception);
        assertEquals(64, outcomes.get(0).identity.length());
    }

    @Test void recoveryOutcomeUsesFreshReceiptWithoutEntryOrAdmission() throws Exception {
        List<String> trace = new ArrayList<>();
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            assertFalse(entry); assertFalse(success); assertEquals("IllegalArgumentException", exception);
            trace.add("outcome");
        }, () -> true);
        String identity = CallbackInvocationDelivery.identity(event());
        delivery.restoreOutcomes(List.of(new CallbackInvocationDelivery.Outcome(identity, false, "IllegalArgumentException")));
        Stream.FederateEvent recovered = recovery(event(), identity, true);
        assertFalse(delivery.begin(recovered, () -> fail("body admission")));
        assertFalse(delivery.begin(recovered));
        assertEquals(List.of("outcome"), trace);
        assertTrue(delivery.checkpointOutcomes().isEmpty());
    }

    @Test void recoveryWithoutKnownOutcomeFailsClosed() throws Exception {
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> fail("invented outcome"), () -> true);
        assertThrows(RTIinternalError.class, () -> delivery.begin(recovery(event(), CallbackInvocationDelivery.identity(event()), true)));
    }

    @Test void completedMarkerOnlyReconcilesAndDuplicatesAreHarmless() throws Exception {
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> fail("marker RPC"), () -> true);
        String identity = CallbackInvocationDelivery.identity(event());
        delivery.restoreOutcomes(List.of(new CallbackInvocationDelivery.Outcome(identity, true, "")));
        Stream.FederateEvent.Builder marker = recovery(event(), identity, true).toBuilder().clearEvent().clearCallbackReceipt();
        com.google.protobuf.Descriptors.FieldDescriptor field = marker.getDescriptorForType().findFieldByName("callback_recovery");
        com.google.protobuf.Message.Builder metadata = ((com.google.protobuf.Message) marker.getField(field)).toBuilder();
        metadata.setField(field.getMessageType().findFieldByName("completed"), true);
        Stream.FederateEvent completed = marker.setField(field, metadata.build()).build();
        assertFalse(delivery.begin(completed, () -> fail("marker admission")));
        assertFalse(delivery.begin(completed));
        assertTrue(delivery.checkpointOutcomes().isEmpty());
        assertThrows(RTIinternalError.class, () -> delivery.begin(completed.toBuilder().setCallbackReceipt(event().getCallbackReceipt()).build()));
    }

    @Test void restoredPendingCarrierPreservesIdentityAcrossFailedOutcomeAndSecondRestore() throws Exception {
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            if (!entry) throw new RTIinternalError("offline");
        }, () -> true);
        String identity = CallbackInvocationDelivery.identity(event());
        Stream.FederateEvent recovered = recovery(event(), identity, false);
        assertTrue(delivery.begin(recovered));
        assertThrows(RTIinternalError.class, () -> delivery.complete(recovered, null));
        assertEquals(identity, delivery.checkpointOutcomes().get(0).identity);
        List<CallbackInvocationDelivery.Outcome> saved = delivery.checkpointOutcomes();
        delivery.restoreEpoch(); delivery.restoreOutcomes(saved);
        assertThrows(RTIinternalError.class, () -> delivery.begin(recovery(event(), identity, true)));
        assertEquals(identity, delivery.checkpointOutcomes().get(0).identity);
    }

    static Stream.FederateEvent recovery(Stream.FederateEvent original, String identity, boolean outcomeOnly) {
        com.google.protobuf.Descriptors.FieldDescriptor field = original.getDescriptorForType().findFieldByName("callback_recovery");
        assertNotNull(field, "Parent callback_recovery schema must be generated");
        byte[] fresh = original.getCallbackReceipt().toByteArray(); fresh[0] ^= 0x7f;
        Stream.FederateEvent.Builder event = original.toBuilder().setCallbackReceipt(ByteString.copyFrom(fresh));
        com.google.protobuf.Message.Builder metadata = event.newBuilderForField(field);
        byte[] bytes = new byte[32];
        for (int i = 0; i < bytes.length; i++) bytes[i] = (byte) Integer.parseInt(identity.substring(i * 2, i * 2 + 2), 16);
        metadata.setField(field.getMessageType().findFieldByName("invocation_identity"), ByteString.copyFrom(bytes));
        metadata.setField(field.getMessageType().findFieldByName("outcome_only"), outcomeOnly);
        return event.setField(field, metadata.build()).build();
    }

    @Test void malformedRecoveryMetadataNeverReportsOrAdmitsBody() throws Exception {
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> fail("malformed RPC"), () -> true);
        Stream.FederateEvent.Builder event = recovery(event(), CallbackInvocationDelivery.identity(event()), false).toBuilder();
        com.google.protobuf.Descriptors.FieldDescriptor field = event.getDescriptorForType().findFieldByName("callback_recovery");
        com.google.protobuf.Message.Builder metadata = ((com.google.protobuf.Message) event.getField(field)).toBuilder();
        metadata.setField(field.getMessageType().findFieldByName("invocation_identity"), ByteString.copyFrom(new byte[31]));
        assertThrows(RTIinternalError.class, () -> delivery.begin(event.setField(field, metadata.build()).build(), () -> fail("admission")));
    }
    @Test void entryIsAcceptedBeforeBodyAndDuplicateNeverReenters() throws Exception {
        List<String> trace = new ArrayList<>();
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) ->
                trace.add(entry ? "entry" : "completion"), () -> true);
        Stream.FederateEvent event = event();
        assertTrue(delivery.begin(event));
        assertEquals(List.of("entry"), trace);
        trace.add("body");
        delivery.complete(event, null);
        assertFalse(delivery.begin(event));
        assertEquals(List.of("entry", "body", "completion"), trace);
    }

    @Test void failedEntryAllowsRetryAndFailedCompletionOnlyRetriesReport() throws Exception {
        boolean[] failures = {true, false};
        List<Boolean> phases = new ArrayList<>();
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            phases.add(entry);
            if (failures[entry ? 0 : 1]) throw new RTIinternalError("report failed");
        }, () -> true);
        Stream.FederateEvent event = event();
        assertThrows(RTIinternalError.class, () -> delivery.begin(event));
        failures[0] = false;
        assertTrue(delivery.begin(event));
        failures[1] = true;
        assertThrows(RTIinternalError.class, () -> delivery.complete(event, new AssertionError("user failed")));
        failures[1] = false;
        assertFalse(delivery.begin(event));
        assertEquals(List.of(true, true, false, false), phases);
    }

    @Test void noReceiptPreservesLegacyInvocationWithoutReports() throws Exception {
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> fail("legacy receipt invented"), () -> false);
        Stream.FederateEvent legacy = Stream.FederateEvent.getDefaultInstance();
        assertTrue(delivery.begin(legacy));
        delivery.complete(legacy, null);
        assertTrue(delivery.begin(legacy));
    }

    @Test void inFlightDuplicateIsSuppressedBeforeEntryReturns() throws Exception {
        CallbackInvocationDelivery[] reference = new CallbackInvocationDelivery[1];
        reference[0] = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            if (entry) assertFalse(reference[0].begin(event));
        }, () -> true);
        assertTrue(reference[0].begin(event()));
    }

    private static Stream.FederateEvent event() {
        return Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFrom(new byte[32]))
                .setSyncAnnounced(Stream.SynchronizationPointAnnounced.newBuilder().setLabel("ready")).build();
    }

    @Test void legacyServerNeverReceivesEntryAsPrematureCompletion() throws Exception {
        List<String> trace = new ArrayList<>();
        CallbackInvocationDelivery delivery = new CallbackInvocationDelivery((event, success, exception, entry) -> {
            assertFalse(entry);
            assertEquals(List.of("body"), trace);
            trace.add("completion");
        }, () -> false);
        Stream.FederateEvent event = event();
        assertTrue(delivery.begin(event));
        assertTrue(trace.isEmpty());
        trace.add("body");
        delivery.complete(event, null);
        assertEquals(List.of("body", "completion"), trace);
    }
}
