package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.*;
import java.util.*;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.TimeUnit;
import org.gorti.rti.transport.WireSession;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Common;
import rti.v1.Stream;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

class DeferredSynchronizationAdmission20261004Test {
    @ParameterizedTest
    @CsvSource({"false,false", "false,true", "true,false", "true,true"})
    void guardedConfirmationRetainsReceiptAndYieldsToCheckpointControls(boolean restore, boolean rejected) throws Exception {
        try (Fixture f = new Fixture(restore, rejected)) {
            f.enqueue(f.confirmation);
            f.enqueue(f.control);
            assertDoesNotThrow(() -> f.rti.evokeCallback(0), "A phase guard defers entry, not the control behind it");
            assertEquals(List.of("control"), f.bodies);
            assertEquals(1, f.entries(f.confirmation));
            assertEquals(0, f.completions(f.confirmation));
            assertTrue(f.pending().contains(f.confirmation), "The same receipt remains in the checkpoint journal");
            assertTrue(f.delivery().checkpointOutcomes().isEmpty(), "Denied entry is not a running or completed body");

            f.enqueue(f.confirmation);
            assertTrue(f.rti.evokeMultipleCallbacks(0, 0.05), "Deferred work remains pending");
            assertEquals(2, f.entries(f.confirmation), "Only one retry per evocation, not a busy loop");
            assertEquals(List.of("control"), f.bodies);

            f.enqueue(f.terminal);
            assertDoesNotThrow(() -> f.rti.evokeCallback(0));
            assertEquals(List.of("control", "terminal"), f.bodies);
            assertEquals(0, f.completions(f.confirmation));
            assertFalse(f.rti.evokeCallback(0));
            assertEquals(List.of("control", "terminal", "confirmation"), f.bodies);
            assertEquals(1, f.completions(f.confirmation));
            assertEquals(4, f.entries(f.confirmation));
            f.enqueue(f.confirmation);
            f.rti.evokeMultipleCallbacks(0, 0.05);
            assertEquals(1, Collections.frequency(f.bodies, "confirmation"));
            assertEquals(1, f.completions(f.confirmation));
            assertTrue(f.pending().isEmpty());
        }
    }

    @ParameterizedTest @ValueSource(strings = {"epoch", "callbackEpoch"})
    void obsoleteDeferredReceiptNeverEntersAfterLifecycleChange(String epoch) throws Exception {
        try (Fixture f = new Fixture(false, false)) {
            f.enqueue(f.confirmation);
            assertDoesNotThrow(() -> f.rti.evokeCallback(0));
            set(f.services, epoch, 2L);
            f.blocked = false;
            Stream.FederateEvent recovered = CallbackInvocationDeliveryTest.recovery(f.confirmation,
                    CallbackInvocationDelivery.identity(f.confirmation), false);
            f.enqueue(recovered);
            f.rti.evokeMultipleCallbacks(0, 0.05);
            assertEquals(1, f.entries(f.confirmation), "Old receipt must never be retried in the new epoch");
            assertEquals(0, f.completions(f.confirmation));
            assertEquals(1, f.entries(recovered));
            assertEquals(1, f.completions(recovered));
            assertEquals(List.of("confirmation"), f.bodies);
        }
    }

    @Test void callbackBodyFailureIsReportedOnceAndIsNotDeferred() throws Exception {
        try (Fixture f = new Fixture(false, false)) {
            f.blocked = false; f.failBody = true;
            f.enqueue(f.confirmation);
            assertThrows(RTIinternalError.class, () -> f.rti.evokeCallback(0));
            assertFalse(f.rti.evokeCallback(0));
            assertEquals(List.of("confirmation"), f.bodies);
            Support.ReportCallbackInvocationRequest outcome = f.reports.stream().filter(r -> !r.getInvocationEntry()).findFirst().orElseThrow();
            assertFalse(outcome.getSuccess());
            assertEquals("FederateInternalError", outcome.getException());
            assertTrue(f.pending().isEmpty());
        }
    }

    @Test void deferredEntryIsSerializedAsPendingNotCompletedOrRunning() throws Exception {
        try (Fixture f = new Fixture(false, false)) {
            f.enqueue(f.confirmation); f.enqueue(f.control);
            assertDoesNotThrow(() -> f.rti.evokeCallback(0));
            Method checkpoint = AmbassadorServices.class.getDeclaredMethod("checkpointBytes"); checkpoint.setAccessible(true);
            byte[] bytes = (byte[])checkpoint.invoke(f.services);
            RuntimeCheckpoint saved = RuntimeCheckpoint.decodeForRestore(bytes, "f", 3, "checkpoint");
            assertEquals(List.of(f.confirmation), saved.pendingCallbacks);
            assertTrue(saved.pendingOutcomes.isEmpty());
            assertTrue(saved.serverRecovery, "Server recovery must issue fresh receipts after restore");
            assertEquals(1, f.entries(f.confirmation));
            assertEquals(0, f.completions(f.confirmation));
        }
    }

    @Test void unrelatedEntryFailureStillReachesTheCaller() throws Exception {
        try (Fixture f = new Fixture(false, false)) {
            f.unrelated = true;
            f.enqueue(f.confirmation);
            assertThrows(RTIinternalError.class, () -> f.rti.evokeCallback(0));
            assertEquals(1, f.entries(f.confirmation));
            assertEquals(0, f.completions(f.confirmation));
            assertTrue(f.bodies.isEmpty());
        }
    }

    private static void set(java.lang.Object target, String name, java.lang.Object value) throws Exception {
        Field field = target.getClass().getDeclaredField(name); field.setAccessible(true); field.set(target, value);
    }
    private static java.lang.Object get(java.lang.Object target, String name) throws Exception {
        Field field = target.getClass().getDeclaredField(name); field.setAccessible(true); return field.get(target);
    }
    private static ByteString receipt(int value) { byte[] bytes = new byte[32]; bytes[0] = (byte)value; return ByteString.copyFrom(bytes); }

    private static final class Fixture implements AutoCloseable {
        final GortiRtiAmbassador rti = new GortiRtiAmbassador();
        final AmbassadorServices services;
        final Server server;
        final WireSession session;
        final List<String> bodies = new ArrayList<>();
        final List<Support.ReportCallbackInvocationRequest> reports = new ArrayList<>();
        final Stream.FederateEvent confirmation, control, terminal;
        final Thread evoker = Thread.currentThread();
        volatile boolean blocked = true, unrelated;
        boolean failBody;

        Fixture(boolean restore, boolean rejected) throws Exception {
            services = (AmbassadorServices)get(rti, "services");
            Stream.FederateEvent.Builder confirm = Stream.FederateEvent.newBuilder().setCallbackReceipt(receipt(1));
            if (rejected) confirm.setSyncRegistrationFailed(Stream.SyncRegistrationFailed.newBuilder().setLabel("queued")
                    .setReason(Stream.SyncPointFailureReason.SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE));
            else confirm.setSyncRegistrationSucceeded(Stream.SyncRegistrationSucceeded.newBuilder().setLabel("queued"));
            confirmation = confirm.build();
            control = restore ? Stream.FederateEvent.newBuilder().setCallbackReceipt(receipt(2)).setRestoreBegun(Stream.FederationRestoreBegun.getDefaultInstance()).build()
                    : Stream.FederateEvent.newBuilder().setCallbackReceipt(receipt(2)).setSaveInitiate(Stream.InitiateFederateSave.newBuilder().setLabel("checkpoint")).build();
            terminal = restore ? Stream.FederateEvent.newBuilder().setCallbackReceipt(receipt(3)).setRestoreFailed(Stream.FederationNotRestored.newBuilder().setLabel("checkpoint").setReason(5)).build()
                    : Stream.FederateEvent.newBuilder().setCallbackReceipt(receipt(3)).setSaveFailed(Stream.FederationNotSaved.newBuilder().setLabel("checkpoint").setReason(6)).build();
            server = ServerBuilder.forPort(0).addService(new SupportServiceGrpc.SupportServiceImplBase() {
                @Override public void reportCallbackInvocation(Support.ReportCallbackInvocationRequest request, StreamObserver<Common.Empty> response) {
                    reports.add(request);
                    if (request.getInvocationEntry() && request.getCallbackReceipt().equals(confirmation.getCallbackReceipt()) && blocked) {
                        Metadata trailers = new Metadata();
                        trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), restore ? "RestoreInProgress" : "SaveInProgress");
                        response.onError(unrelated ? Status.INTERNAL.asRuntimeException() : Status.FAILED_PRECONDITION.asRuntimeException(trailers));
                        return;
                    }
                    if (request.getInvocationEntry() && request.getCallbackReceipt().equals(terminal.getCallbackReceipt())) blocked = false;
                    response.onNext(Common.Empty.getDefaultInstance()); response.onCompleted();
                }
            }).build().start();
            session = new WireSession("127.0.0.1:" + server.getPort(), true);
            long participant = (3L << 32) | 7;
            session.setMembership("f", participant, 3, receipt(8));
            set(services, "session", session); set(services, "federation", "f"); set(services, "handle", participant);
            set(services, "epoch", 1L); set(services, "federationGeneration", 3L); set(services, "callbackEpoch", 1L);
            set(services, "appliedCallbackEpoch", 1L); set(services, "hydratedCallbackEpoch", 1L); set(services, "callbackEntrySupported", true);
            set(services, "ambassador", new NullFederateAmbassador() {
                private void body(String name) { assertSame(evoker, Thread.currentThread()); bodies.add(name); }
                @Override public void synchronizationPointRegistrationSucceeded(String label) throws FederateInternalError {
                    assertEquals("queued", label); body("confirmation"); if (failBody) throw new FederateInternalError("user callback");
                }
                @Override public void synchronizationPointRegistrationFailed(String label, SynchronizationPointFailureReason reason) {
                    assertEquals("queued", label); assertEquals(SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE, reason); body("confirmation");
                }
                @Override public void initiateFederateSave(String label) { body("control"); }
                @Override public void federationRestoreBegun() { body("control"); }
                @Override public void federationNotSaved(SaveFailureReason reason) { body("terminal"); }
                @Override public void federationNotRestored(RestoreFailureReason reason) { body("terminal"); }
            });
        }
        int entries(Stream.FederateEvent event) { return (int)reports.stream().filter(r -> r.getInvocationEntry() && r.getCallbackReceipt().equals(event.getCallbackReceipt())).count(); }
        int completions(Stream.FederateEvent event) { return (int)reports.stream().filter(r -> !r.getInvocationEntry() && r.getCallbackReceipt().equals(event.getCallbackReceipt())).count(); }
        CallbackInvocationDelivery delivery() throws Exception { return (CallbackInvocationDelivery)get(services, "callbackInvocation"); }
        @SuppressWarnings("unchecked") List<Stream.FederateEvent> pending() throws Exception {
            Method method = AmbassadorServices.class.getDeclaredMethod("pendingStreamCallbacks"); method.setAccessible(true);
            return (List<Stream.FederateEvent>)method.invoke(services);
        }
        @SuppressWarnings("unchecked") void enqueue(Stream.FederateEvent event) throws Exception {
            Class<?> callback = Class.forName("org.gorti.rti.AmbassadorServices$Callback");
            Method deliver = AmbassadorServices.class.getDeclaredMethod("deliver", Stream.FederateEvent.class, long.class); deliver.setAccessible(true);
            java.lang.Object queued = java.lang.reflect.Proxy.newProxyInstance(callback.getClassLoader(), new Class<?>[]{callback}, (proxy, method, args) -> {
                if (method.getName().equals("invoke")) {
                    try { deliver.invoke(services, event, 0L); } catch (InvocationTargetException failure) { throw failure.getCause(); }
                }
                if (method.getName().equals("checkpointEvent")) return event;
                if (method.getReturnType() == boolean.class) return false;
                return null;
            });
            assertTrue(((BlockingQueue<java.lang.Object>)get(services, "callbacks")).offer(queued));
        }
        @Override public void close() throws Exception {
            session.close(); server.shutdownNow(); assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
        }
    }
}
