package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import java.lang.reflect.*;
import java.net.URL;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.BlockingQueue;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.CsvSource;
import rti.v1.Stream;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
@Timeout(45)
class LiveDeferredSynchronization20261004Test {
    @ParameterizedTest
    @CsvSource({"HLAfloat64Time,false,false", "HLAfloat64Time,false,true", "HLAfloat64Time,true,false", "HLAfloat64Time,true,true",
            "HLAinteger64Time,false,false", "HLAinteger64Time,false,true", "HLAinteger64Time,true,false", "HLAinteger64Time,true,true"})
    void actualPhaseGuardDefersConfirmationUntilTerminalEntry(String scalar, boolean restore, boolean rejected) throws Exception {
        try (Fixture f = new Fixture(scalar)) {
            if (restore) {
                f.rti.requestFederationSave("baseline"); f.await(() -> f.events.saveInitiated == 1);
                f.rti.federateSaveBegun(); f.rti.federateSaveComplete(); f.await(() -> f.events.saved == 1);
            }
            if (rejected) {
                f.rti.registerFederationSynchronizationPoint("queued", new byte[]{0, (byte)255});
                f.await(() -> f.events.succeeded == 1 && f.events.announced == 1);
            }
            int successes = f.events.succeeded, failures = f.events.failed;
            f.rti.registerFederationSynchronizationPoint("queued", new byte[]{0, (byte)255});
            java.lang.Object held = f.holdConfirmation(rejected);
            Stream.FederateEvent original = f.event(held);
            assertEquals(32, original.getCallbackReceipt().size(), "Actual server-issued admission capability");

            if (restore) {
                f.rti.requestFederationRestore("baseline"); f.await(() -> f.events.restoreInitiated == 1);
                assertEquals(1, f.events.restoreBegun);
            } else {
                f.rti.requestFederationSave("active"); f.await(() -> f.events.saveInitiated == 1);
            }
            f.enqueue(held);
            assertTrue(f.rti.evokeCallback(0), "Rejected confirmation remains pending after actual phase entry");
            assertTrue(f.pending().contains(original), "Actual typed phase rejection must retain the original receipt");
            assertEquals(successes, f.events.succeeded); assertEquals(failures, f.events.failed);
            assertTimeout(Duration.ofSeconds(2), () -> assertTrue(f.rti.evokeMultipleCallbacks(0, 0.02)),
                    "No internal phase-retry busy loop");
            assertEquals(successes, f.events.succeeded); assertEquals(failures, f.events.failed);

            if (restore) f.rti.federateRestoreNotComplete();
            else { f.rti.federateSaveBegun(); f.rti.federateSaveNotComplete(); }
            f.await(() -> restore ? f.events.notRestored == 1 : f.events.notSaved == 1);
            f.await(() -> rejected ? f.events.failed == failures + 1 : f.events.succeeded == successes + 1);
            assertTrue(f.events.trace.lastIndexOf("terminal") < f.events.trace.lastIndexOf("confirmation"));
            if (restore) assertEquals(RestoreFailureReason.FEDERATE_REPORTED_FAILURE_DURING_RESTORE, f.events.restoreReason);
            else assertEquals(SaveFailureReason.FEDERATE_REPORTED_FAILURE_DURING_SAVE, f.events.saveReason);
            f.enqueue(held);
            f.rti.evokeMultipleCallbacks(0, 0.05);
            assertEquals(successes + (rejected ? 0 : 1), f.events.succeeded);
            assertEquals(failures + (rejected ? 1 : 0), f.events.failed);
            assertFalse(f.pending().contains(original), "Completed receipt must leave the deferred journal");
            System.out.println("DEFERRED_SYNC_OK scalar=" + scalar + " phase=" + (restore ? "restore" : "save")
                    + " rejected=" + rejected + " sameReceipt=true terminalBeforeConfirmation=true duplicateBody=false");
        }
    }

    private static final class Events extends NullFederateAmbassador {
        final Thread evoker = Thread.currentThread();
        final List<String> trace = new ArrayList<>();
        int saveInitiated, saved, restoreBegun, restoreInitiated, notSaved, notRestored, succeeded, failed, announced;
        SaveFailureReason saveReason;
        RestoreFailureReason restoreReason;
        private void entered(String kind) { assertSame(evoker, Thread.currentThread()); trace.add(kind); }
        @Override public void synchronizationPointRegistrationSucceeded(String label) { assertEquals("queued", label); entered("confirmation"); succeeded++; }
        @Override public void synchronizationPointRegistrationFailed(String label, SynchronizationPointFailureReason reason) {
            assertEquals("queued", label); assertEquals(SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE, reason);
            entered("confirmation"); failed++;
        }
        @Override public void announceSynchronizationPoint(String label, byte[] tag) { assertEquals("queued", label); assertArrayEquals(new byte[]{0, (byte)255}, tag); entered("announcement"); announced++; }
        @Override public void initiateFederateSave(String label) { entered("save"); saveInitiated++; }
        @Override public void federationSaved() { entered("saved"); saved++; }
        @Override public void federationRestoreBegun() { entered("restore-begun"); restoreBegun++; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { entered("restore"); restoreInitiated++; }
        @Override public void federationNotSaved(SaveFailureReason reason) { entered("terminal"); saveReason = reason; notSaved++; }
        @Override public void federationNotRestored(RestoreFailureReason reason) { entered("terminal"); restoreReason = reason; notRestored++; }
    }

    private static final class Fixture implements AutoCloseable {
        final RTIambassador rti = new GortiRtiAmbassador();
        final Events events = new Events();
        final String federation = "deferred-sync-" + UUID.randomUUID();
        final AmbassadorServices services;
        final BlockingQueue<java.lang.Object> queue;
        final Method checkpointEvent;
        @SuppressWarnings("unchecked") Fixture(String scalar) throws Exception {
            rti.connect(events, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
            rti.createFederationExecution(federation, new URL[]{getClass().getResource("/exchange-fom.xml")}, scalar);
            rti.joinFederationExecution("member", "deferred-sync", federation);
            Field field = GortiRtiAmbassador.class.getDeclaredField("services"); field.setAccessible(true);
            services = (AmbassadorServices)field.get(rti);
            field = AmbassadorServices.class.getDeclaredField("callbacks"); field.setAccessible(true);
            queue = (BlockingQueue<java.lang.Object>)field.get(services);
            checkpointEvent = Class.forName("org.gorti.rti.AmbassadorServices$Callback").getDeclaredMethod("checkpointEvent");
            checkpointEvent.setAccessible(true);
        }
        Stream.FederateEvent event(java.lang.Object callback) throws Exception { return (Stream.FederateEvent)checkpointEvent.invoke(callback); }
        java.lang.Object holdConfirmation(boolean rejected) throws Exception {
            // Control callback ordering only: retain the real server carrier, never fabricate a receipt or RPC result.
            long deadline = System.nanoTime() + 10_000_000_000L;
            while (System.nanoTime() < deadline) {
                for (java.lang.Object callback : queue) {
                    Stream.FederateEvent event = event(callback);
                    if (event != null && (rejected ? event.hasSyncRegistrationFailed() : event.hasSyncRegistrationSucceeded()) && queue.remove(callback)) return callback;
                }
                Thread.sleep(1);
            }
            throw new AssertionError("Actual synchronization confirmation was not admitted");
        }
        void enqueue(java.lang.Object callback) { assertTrue(queue.offer(callback)); }
        @SuppressWarnings("unchecked") List<Stream.FederateEvent> pending() throws Exception {
            Method method = AmbassadorServices.class.getDeclaredMethod("pendingStreamCallbacks"); method.setAccessible(true);
            return (List<Stream.FederateEvent>)method.invoke(services);
        }
        void await(BooleanSupplier condition) throws Exception {
            long deadline = System.nanoTime() + 10_000_000_000L;
            while (!condition.getAsBoolean() && System.nanoTime() < deadline) rti.evokeMultipleCallbacks(0.001, 0.02);
            assertTrue(condition.getAsBoolean(), "Actual callback required");
        }
        @Override public void close() throws Exception {
            try { rti.abortFederationSave(); } catch (RTIexception inactive) { }
            try { rti.abortFederationRestore(); } catch (RTIexception inactive) { }
            try { rti.evokeMultipleCallbacks(0, 0.05); }
            finally {
                try { rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); }
                finally { try { rti.destroyFederationExecution(federation); } finally { rti.disconnect(); } }
            }
        }
    }
}
