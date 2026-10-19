package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.RTIexception;
import io.grpc.*;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.*;
import java.util.*;
import java.util.concurrent.TimeUnit;
import org.gorti.rti.transport.WireSession;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import rti.v1.*;

class TransportationStreamCallbackTest {
    @org.junit.jupiter.params.ParameterizedTest
    @org.junit.jupiter.params.provider.CsvSource({"false,true", "false,false", "true,true", "true,false"})
    void outcomeOnlyTransportationHasNoAuthorityAndNeverActivatesOrEnters(boolean interaction, boolean success) throws Throwable {
        try (Fixture f = new Fixture(interaction)) {
            Field field = AmbassadorServices.class.getDeclaredField("callbackInvocation"); field.setAccessible(true);
            CallbackInvocationDelivery delivery = (CallbackInvocationDelivery) field.get(f.services);
            String identity = CallbackInvocationDelivery.identity(f.event);
            String exception = success ? "" : "FederateInternalError";
            delivery.restoreOutcomes(List.of(new CallbackInvocationDelivery.Outcome(identity, success, exception)));
            Stream.FederateEvent body = f.event.toBuilder().setTransportationChangeConfirmed(f.event.getTransportationChangeConfirmed().toBuilder()
                    .clearTicket().clearFederationGeneration().clearCallbackEpoch()).build();
            Stream.FederateEvent recovered = CallbackInvocationDeliveryTest.recovery(body, identity, true);
            f.deliver(recovered); f.deliver(recovered);
            assertEquals(List.of("completion"), f.trace);
            assertEquals(1, f.reports.size());
            Support.ReportCallbackInvocationRequest report = f.reports.get(0);
            assertFalse(report.getInvocationEntry()); assertEquals(success, report.getSuccess());
            assertEquals(exception, report.getException()); assertEquals(recovered.getCallbackReceipt(), report.getCallbackReceipt());
            assertNotEquals(f.event.getCallbackReceipt(), report.getCallbackReceipt());
            assertTrue(delivery.checkpointOutcomes().isEmpty());
        }
    }
    @Test void entryActivationBodyCompletionAndDeduplication() throws Throwable {
        for (boolean interaction : List.of(false, true)) {
            try (Fixture f = new Fixture(interaction)) {
                f.deliver(); f.deliver();
                assertEquals(List.of("activate", "entry", "body", "completion"), f.trace);
            }
        }
    }

    @Test void failedActivationNeverCompletesOrInvokesAndCanRetry() throws Throwable {
        try (Fixture f = new Fixture(true)) {
            f.rejectActivation = true;
            assertThrows(RTIexception.class, f::deliver);
            assertEquals(List.of("activate"), f.trace);
            f.rejectActivation = false;
            f.deliver(); f.deliver();
            assertEquals(List.of("activate", "activate", "entry", "body", "completion"), f.trace);
        }
    }

    @Test void activatedTicketRetriesEntryFailureWithoutDuplicateBody() throws Throwable {
        try (Fixture f = new Fixture(true)) {
            f.rejectEntry = true;
            assertThrows(RTIexception.class, f::deliver);
            assertEquals(List.of("activate", "entry"), f.trace);
            f.rejectEntry = false;
            f.deliver(); f.deliver();
            assertEquals(List.of("activate", "entry", "activate", "entry", "body", "completion"), f.trace);
        }
    }

    @Test void failedAdmissionIsRetainedForStandardEvocation() throws Throwable {
        for (boolean entry : List.of(false, true)) {
            try (Fixture f = new Fixture(true)) {
                f.rejectActivation = !entry;
                f.rejectEntry = entry;
                assertThrows(RTIexception.class, f::deliver);
                assertFalse(f.trace.contains("body"));
                assertFalse(f.trace.contains("completion"));
                f.rejectActivation = false;
                f.rejectEntry = false;
                f.services.evokeCallback(0);
                assertEquals(List.of("activate", "entry", "body", "completion"),
                        f.trace.subList(f.trace.size() - 4, f.trace.size()));
                f.services.evokeCallback(0);
                assertEquals(1, Collections.frequency(f.trace, "body"));
                assertEquals(1, Collections.frequency(f.trace, "completion"));
            }
        }
    }

    @Test void retainedAdmissionIsFencedByLifecycleEpoch() throws Throwable {
        try (Fixture f = new Fixture(true)) {
            f.rejectActivation = true;
            assertThrows(RTIexception.class, f::deliver);
            set(f.services, "callbackEpoch", 2L);
            f.rejectActivation = false;
            f.services.evokeCallback(0);
            assertEquals(List.of("activate"), f.trace);
        }
    }

    private static void set(AmbassadorServices services, String name, java.lang.Object value) throws Exception {
        Field field = AmbassadorServices.class.getDeclaredField(name); field.setAccessible(true); field.set(services, value);
    }

    private static final class Fixture implements AutoCloseable {
        final AmbassadorServices services = new AmbassadorServices();
        final List<String> trace = new ArrayList<>();
        final List<Support.ReportCallbackInvocationRequest> reports = new ArrayList<>();
        final Server server;
        final WireSession session;
        final Stream.FederateEvent event;
        boolean rejectActivation;
        boolean rejectEntry;

        Fixture(boolean interaction) throws Exception {
            rti.v1.Object.TransportationChange.Builder change = rti.v1.Object.TransportationChange.newBuilder()
                    .setTicket(ByteString.copyFrom(new byte[32])).setFederationGeneration(3).setCallbackEpoch(1)
                    .setTransportType(Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE);
            if (interaction) change.setInteractionClassHandle(9); else change.setObjectHandle(4).addAttributeHandles(5).addAttributeHandles(6);
            event = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFrom(new byte[32]))
                    .setTransportationChangeConfirmed(change).build();
            server = ServerBuilder.forPort(0).addService(new SupportServiceGrpc.SupportServiceImplBase() {
                @Override public void reportCallbackInvocation(Support.ReportCallbackInvocationRequest request, StreamObserver<Common.Empty> response) {
                    assertEquals(7, request.getFederateHandle());
                    reports.add(request);
                    trace.add(request.getInvocationEntry() ? "entry" : "completion");
                    if (request.getInvocationEntry() && rejectEntry) {response.onError(Status.UNAVAILABLE.asRuntimeException());return;}
                    response.onNext(Common.Empty.getDefaultInstance()); response.onCompleted();
                }
            }).addService(new ObjectServiceGrpc.ObjectServiceImplBase() {
                @Override public void activateTransportationTypeChange(rti.v1.Object.ActivateTransportationTypeChangeRequest request,
                        StreamObserver<rti.v1.Object.TransportationChange> response) {
                    assertEquals(7, request.getFederateHandle()); assertEquals(3, request.getExpectedFederationGeneration());
                    assertEquals(1, request.getExpectedCallbackEpoch()); assertEquals(change.getTicket(), request.getTicket());
                    trace.add("activate");
                    if (rejectActivation) response.onError(Status.UNAVAILABLE.asRuntimeException());
                    else { response.onNext(change.clone().clearAttributeHandles().build()); response.onCompleted(); }
                }
            }).build().start();
            session = new WireSession("127.0.0.1:" + server.getPort(), true);
            session.setMembership("f", 7, 3, ByteString.copyFrom(new byte[32]));
            set(services, "session", session); set(services, "federation", "f"); set(services, "handle", 7L);
            set(services, "federationGeneration", 3L); set(services, "callbackEpoch", 1L); set(services, "callbackEntrySupported", true);
            set(services, "appliedCallbackEpoch", 1L); set(services, "hydratedCallbackEpoch", 1L);
            set(services, "ambassador", new NullFederateAmbassador() {
                @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle object, AttributeHandleSet attrs, TransportationTypeHandle kind) {
                    assertEquals(Handles.objectInstance(4), object); assertTrue(attrs.isEmpty()); assertEquals(Handles.transportation(1), kind); trace.add("body");
                }
                @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle interaction, TransportationTypeHandle kind) {
                    assertEquals(Handles.interactionClass(9), interaction); assertEquals(Handles.transportation(1), kind); trace.add("body");
                }
            });
        }

        void deliver() throws Throwable {
            deliver(event);
        }
        void deliver(Stream.FederateEvent event) throws Throwable {
            Method method = AmbassadorServices.class.getDeclaredMethod("deliver", Stream.FederateEvent.class, long.class);
            method.setAccessible(true);
            try { method.invoke(services, event, 0L); } catch (InvocationTargetException failure) { throw failure.getCause(); }
        }

        @Override public void close() throws Exception {
            session.close(); server.shutdownNow(); assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
        }
    }
}
