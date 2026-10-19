package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import java.net.URL;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

/** Receipt reporting must finish after restore completes inside its initiation callback. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external production RTI with save storage")
class LiveRestoreReceiptTest {
    @Test @Timeout(90)
    void completesRestoreInsideCallbackWithUnchangedHandles() throws Exception {
        exercise(false);
    }

    @Test @Timeout(90)
    void completesRestoreInsideCallbackAfterReplacementHandleRebinding() throws Exception {
        exercise(true);
    }

    private static void exercise(boolean replacement) throws Exception {
        try (Federation f = new Federation()) {
            List<FederateHandle> saved = List.of(f.a.handle, f.b.handle);
            f.a.rti.requestFederationSave("receipt-checkpoint");
            await(() -> f.a.callbacks.saving == 1 && f.b.callbacks.saving == 1, f);
            for (Participant p : List.of(f.a, f.b)) p.rti.federateSaveBegun();
            for (Participant p : List.of(f.a, f.b)) p.rti.federateSaveComplete();
            await(() -> f.a.callbacks.saved == 1 && f.b.callbacks.saved == 1, f);
            if (replacement) {
                f.a.close();
                f.b.close();
                // Reverse join order and fresh ambassadors rule out ordinal or local-cache reuse.
                f.b = new Participant("B", f.name);
                f.a = new Participant("A", f.name);
                assertNotEquals(saved.get(0), f.a.handle);
                assertNotEquals(saved.get(1), f.b.handle);
            }
            f.a.rti.requestFederationRestore("receipt-checkpoint");
            await(() -> f.a.callbacks.restored == 1 && f.b.callbacks.restored == 1, f);
            for (int i = 0; i < 2; i++) {
                Participant p = i == 0 ? f.a : f.b;
                assertEquals(1, p.callbacks.completedInsideInitiation);
                assertEquals(saved.get(i), p.callbacks.announcedHandle);
                assertEquals(saved.get(i), p.rti.getFederateHandle(p.name));
                assertEquals(p.name, p.rti.getFederateName(saved.get(i)));
                assertEquals(List.of("begun", "initiate", "completed-inside", "restored"), p.callbacks.order);
                assertNull(p.callbacks.connectionFailure);
                assertTrue(p.callbacks.failures.isEmpty());
                assertDoesNotThrow(p.rti::queryLogicalTime);
            }
        }
    }

    private static void await(BooleanSupplier done, Federation f) throws Exception {
        long deadline = System.nanoTime() + Duration.ofSeconds(15).toNanos();
        while (!done.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant p : List.of(f.a, f.b)) {
                // A receipt error from deliver's finally block fails this real public API call.
                p.rti.evokeMultipleCallbacks(0.001, 0.02);
                assertNull(p.callbacks.connectionFailure);
                assertTrue(p.callbacks.failures.isEmpty(), p.callbacks.failures.toString());
            }
        }
        assertTrue(done.getAsBoolean(), "Timed out waiting for save/restore callbacks");
    }

    private static final class Federation implements AutoCloseable {
        final String name = "java-restore-receipt-" + UUID.randomUUID();
        Participant a, b;
        final RTIambassador creator = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        boolean connected, created;

        Federation() throws Exception {
            try {
                creator.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
                connected = true;
                URL fom = LiveRestoreReceiptTest.class.getResource("/exchange-fom.xml");
                assertNotNull(fom);
                creator.createFederationExecution(name, new URL[]{fom});
                created = true;
                a = new Participant("A", name);
                b = new Participant("B", name);
            } catch (Exception | Error failure) {
                try { close(); } catch (Exception cleanup) { failure.addSuppressed(cleanup); }
                throw failure;
            }
        }

        @Override public void close() throws Exception {
            List<Exception> errors = new ArrayList<>();
            for (Participant p : new Participant[]{b, a}) if (p != null) {
                try { p.close(); } catch (Exception failure) { errors.add(failure); }
            }
            if (created) try { creator.destroyFederationExecution(name); } catch (Exception failure) { errors.add(failure); }
            if (connected) try { creator.disconnect(); } catch (Exception failure) { errors.add(failure); }
            if (!errors.isEmpty()) {
                Exception failure = new Exception("Restore receipt test cleanup failed");
                errors.forEach(failure::addSuppressed);
                throw failure;
            }
        }
    }

    private static final class Participant implements AutoCloseable {
        final String name;
        final RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        final Callbacks callbacks = new Callbacks(rti);
        FederateHandle handle;
        boolean connected, joined;

        Participant(String name, String federation) throws Exception {
            this.name = name;
            try {
                rti.connect(callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
                connected = true;
                handle = rti.joinFederationExecution(name, "restore-receipt", federation);
                joined = true;
            } catch (Exception | Error failure) {
                try { close(); } catch (Exception cleanup) { failure.addSuppressed(cleanup); }
                throw failure;
            }
        }

        @Override public void close() throws Exception {
            if (joined) {
                try { rti.abortFederationSave(); } catch (SaveNotInProgress expected) { }
                try { rti.abortFederationRestore(); } catch (RestoreNotInProgress expected) { }
                rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                joined = false;
            }
            if (connected) {
                rti.disconnect();
                connected = false;
            }
        }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final RTIambassador rti;
        final List<String> order = new ArrayList<>(), failures = new ArrayList<>();
        int saving, saved, completedInsideInitiation, restored;
        FederateHandle announcedHandle;
        String connectionFailure;

        Callbacks(RTIambassador rti) { this.rti = rti; }
        @Override public void initiateFederateSave(String label) { saving++; }
        @Override public void federationSaved() { saved++; }
        @Override public void federationRestoreBegun() { order.add("begun"); }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) throws FederateInternalError {
            order.add("initiate");
            announcedHandle = handle;
            try {
                rti.federateRestoreComplete();
                completedInsideInitiation++;
                order.add("completed-inside");
            } catch (RTIexception failure) {
                throw new FederateInternalError("Completing restore inside initiation failed", failure);
            }
        }
        @Override public void federationRestored() { order.add("restored"); restored++; }
        @Override public void federationNotSaved(SaveFailureReason reason) { failures.add("save: " + reason); }
        @Override public void federationNotRestored(RestoreFailureReason reason) { failures.add("restore: " + reason); }
        @Override public void connectionLost(String description) { connectionFailure = description; }
    }
}
