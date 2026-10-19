package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import hla.rti1516e.time.HLAfloat64Interval;
import java.net.URL;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

/** Two public Java ambassadors against an externally started production RTI. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external RTI with save storage and -Dgorti.endpoint")
class LiveSaveRestoreTest {
    @Test @Timeout(120)
    void replacementOfDifferentFederateTypeIsRejectedBeforeRestoreBegins() throws Exception {
        try (Federation f = new Federation()) {
            save("typed-census", f);
            f.replaceAmbassadors("different-simulator-type");
            assertThrows(RTIinternalError.class, () -> f.a.rti.requestFederationRestore("typed-census"));
            await("census rejection", () -> f.a.callbacks.restoreRejected.contains("typed-census"), f.a, f.b);
            assertTrue(f.a.callbacks.restoreLabels.isEmpty());
            assertTrue(f.b.callbacks.restoreLabels.isEmpty());
            assertEquals(0, TimeValues.value(f.a.rti.queryLogicalTime()));
        }
    }

    @Test @Timeout(120)
    void replacementAmbassadorsRecoverPersistedSdkKnowledgeAndTime() throws Exception {
        try (Federation f = new Federation()) {
            ObjectClassHandle type = f.a.rti.getObjectClassHandle("HLAobjectRoot.Item");
            AttributeHandle attribute = f.a.rti.getAttributeHandle(type, "value");
            AttributeHandleSet attributes = f.a.rti.getAttributeHandleSetFactory().create();
            attributes.add(attribute);
            f.a.rti.publishObjectClassAttributes(type, attributes);
            f.b.rti.subscribeObjectClassAttributes(type, attributes);
            ObjectInstanceHandle object = f.a.rti.registerObjectInstance(type);
            String name = f.a.rti.getObjectInstanceName(object);
            await("discovery before restart checkpoint", () -> f.b.callbacks.discovered.contains(object), f.a, f.b);
            for (Participant p : List.of(f.a, f.b)) p.rti.enableTimeRegulation(TimeValues.interval(0.5));
            await("regulation before replacement", () -> f.a.callbacks.regulating && f.b.callbacks.regulating, f.a, f.b);
            advance(2, f.a, f.b);
            save("replacement-runtime", f);
            FederateHandle oldA = f.a.handle, oldB = f.b.handle;
            f.replaceAmbassadors();
            assertNotEquals(oldA, f.a.handle);
            assertNotEquals(oldB, f.b.handle);
            assertThrows(ObjectInstanceNotKnown.class, () -> f.a.rti.getKnownObjectClassHandle(object));
            f.a.rti.requestFederationRestore("replacement-runtime");
            await("replacement restore initiated", () -> !f.a.callbacks.restoreLabels.isEmpty()
                    && !f.b.callbacks.restoreLabels.isEmpty(), f.a, f.b);
            assertEquals(oldA, f.a.callbacks.restoreHandle);
            assertEquals(oldB, f.b.callbacks.restoreHandle);
            for (Participant p : List.of(f.a, f.b)) p.callbacks.restoredProbe = () -> {
                assertEquals(type, p.rti.getKnownObjectClassHandle(object));
                assertEquals(object, p.rti.getObjectInstanceHandle(name));
                assertEquals(2, TimeValues.value(p.rti.queryLogicalTime()));
                assertThrows(TimeRegulationAlreadyEnabled.class, () -> p.rti.enableTimeRegulation(TimeValues.interval(0.5)));
            };
            f.a.rti.federateRestoreComplete();
            f.b.rti.federateRestoreComplete();
            await("replacement restore committed", () -> f.a.callbacks.restored == 1 && f.b.callbacks.restored == 1, f.a, f.b);
            for (Participant p : List.of(f.a, f.b)) assertNull(p.callbacks.probeFailure);
            AttributeHandleValueMap values = f.a.rti.getAttributeHandleValueMapFactory().create(1);
            values.put(attribute, new byte[]{5});
            f.a.rti.updateAttributeValues(object, values, new byte[]{6});
            await("restored ownership and subscription", () -> f.b.callbacks.reflections == 1, f.a, f.b);
            advance(3, f.a, f.b);
        }
    }

    @Test @Timeout(120)
    void restoresDeletedObjectAndLogicalTimeBeforeUserCallback() throws Exception {
        try (Federation f = new Federation()) {
            Participant a = f.a, b = f.b;
            ObjectClassHandle type = a.rti.getObjectClassHandle("HLAobjectRoot.Item");
            AttributeHandle attribute = a.rti.getAttributeHandle(type, "value");
            AttributeHandleSet attributes = a.rti.getAttributeHandleSetFactory().create();
            attributes.add(attribute);
            a.rti.publishObjectClassAttributes(type, attributes);
            b.rti.subscribeObjectClassAttributes(type, attributes);
            ObjectInstanceHandle object = a.rti.registerObjectInstance(type);
            await("object discovery", () -> b.callbacks.discovered.contains(object), a, b);
            for (Participant p : List.of(a, b)) {
                p.rti.enableTimeRegulation(TimeValues.interval(0.5));
                p.rti.enableTimeConstrained();
            }
            await("time policies enabled", () -> a.callbacks.regulating && a.callbacks.constrained
                    && b.callbacks.regulating && b.callbacks.constrained, a, b);
            advance(2, a, b);
            save("object-time", f);

            b.rti.enableAsynchronousDelivery();
            a.rti.deleteObjectInstance(object, new byte[]{7});
            await("post-save removal", () -> b.callbacks.removed.contains(object), a, b);
            for (Participant p : List.of(a, b)) {
                assertThrows(ObjectInstanceNotKnown.class, () -> p.rti.getKnownObjectClassHandle(object));
                p.rti.modifyLookahead(TimeValues.interval(1));
            }
            advance(4, a, b);
            for (Participant p : List.of(a, b)) {
                p.rti.disableTimeConstrained();
                p.callbacks.restoredProbe = () -> {
                    assertEquals(type, p.rti.getKnownObjectClassHandle(object));
                    assertEquals(2, TimeValues.value(p.rti.queryLogicalTime()));
                };
            }
            a.rti.requestFederationRestore("object-time");
            assertTrue(a.callbacks.restoreLabels.isEmpty(), "Request return is not callback delivery");
            await("restore initiated", () -> a.callbacks.restoreLabels.contains("object-time")
                    && b.callbacks.restoreLabels.contains("object-time"), a, b);
            assertEquals(List.of("accepted:object-time", "begun", "initiate:object-time"), a.callbacks.restoreOrder);
            assertEquals(List.of("begun", "initiate:object-time"), b.callbacks.restoreOrder);
            assertEquals(a.handle, a.callbacks.restoreHandle);
            assertEquals(b.handle, b.callbacks.restoreHandle);
            assertEquals("A", a.callbacks.restoreName);
            assertEquals("B", b.callbacks.restoreName);

            a.rti.federateRestoreComplete();
            a.rti.queryFederationRestoreStatus();
            await("restore progress", () -> a.callbacks.restoreStatus != null, a, b);
            assertRestoreStatus(a.callbacks.restoreStatus, a.handle, RestoreStatus.FEDERATE_WAITING_FOR_FEDERATION_TO_RESTORE);
            assertRestoreStatus(a.callbacks.restoreStatus, b.handle, RestoreStatus.FEDERATE_RESTORING);
            assertEquals(0, a.callbacks.restored);
            b.rti.federateRestoreComplete();
            assertEquals(0, b.callbacks.restored);
            await("successful restore", () -> a.callbacks.restored == 1 && b.callbacks.restored == 1, a, b);
            for (Participant p : List.of(a, b)) {
                assertNull(p.callbacks.probeFailure, "State must be committed before federationRestored");
                assertEquals(type, p.rti.getKnownObjectClassHandle(object));
                assertEquals(2, TimeValues.value(p.rti.queryLogicalTime()));
                assertEquals(0.5, ((HLAfloat64Interval) p.rti.queryLookahead()).getValue());
                assertThrows(TimeRegulationAlreadyEnabled.class, () -> p.rti.enableTimeRegulation(TimeValues.interval(0.5)));
                assertThrows(TimeConstrainedAlreadyEnabled.class, p.rti::enableTimeConstrained);
                p.rti.enableAsynchronousDelivery();
            }
            AttributeHandleValueMap values = a.rti.getAttributeHandleValueMapFactory().create(1);
            values.put(attribute, new byte[]{0, 0, 0, 9});
            a.rti.updateAttributeValues(object, values, new byte[]{8});
            await("restored object update on the existing stream", () -> b.callbacks.reflections == 1, a, b);
            advance(3, a, b);
        }
    }

    @Test @Timeout(120)
    void timedSaveDefersGrantsAndRestoreResumesTheSavedAdvance() throws Exception {
        try (Federation f = new Federation()) {
            Participant a = f.a, b = f.b;
            for (Participant p : List.of(a, b)) p.rti.enableTimeRegulation(TimeValues.interval(0.5));
            await("regulation enabled", () -> a.callbacks.regulating && b.callbacks.regulating, a, b);
            for (Participant p : List.of(a, b)) p.rti.enableTimeConstrained();
            await("constraints enabled", () -> a.callbacks.constrained && b.callbacks.constrained, a, b);

            a.rti.requestFederationSave("timed", TimeValues.time(2));
            assertTrue(a.callbacks.saveLabels.isEmpty());
            assertTrue(b.callbacks.saveLabels.isEmpty());
            a.rti.timeAdvanceRequest(TimeValues.time(3));
            long observation = System.nanoTime() + Duration.ofMillis(150).toNanos();
            do {
                a.rti.evokeMultipleCallbacks(0.001, 0.01);
                b.rti.evokeMultipleCallbacks(0.001, 0.01);
                assertTrue(a.callbacks.saveLabels.isEmpty(), "The peer has not reached the scheduled-save barrier");
                assertTrue(b.callbacks.saveLabels.isEmpty());
                assertTrue(a.callbacks.grants.isEmpty());
            } while (System.nanoTime() < observation);

            b.rti.timeAdvanceRequest(TimeValues.time(3));
            await("timed initiation", () -> a.callbacks.saveLabels.contains("timed")
                    && b.callbacks.saveLabels.contains("timed"), a, b);
            for (Participant p : List.of(a, b)) {
                assertEquals(List.of(2.0), p.callbacks.saveTimes, "Use the timed callback overload with the exact requested time");
                assertTrue(p.callbacks.grants.isEmpty(), "The original advance remains pending during save");
                assertThrows(SaveInProgress.class, p.rti::queryLogicalTime);
            }
            completeSave(f);
            await("original grants after save", () -> a.callbacks.grants.size() == 1 && b.callbacks.grants.size() == 1, a, b);
            for (Participant p : List.of(a, b)) {
                assertEquals(List.of(3.0), p.callbacks.grants);
                assertEquals(3, TimeValues.value(p.rti.queryLogicalTime()));
            }
            advance(5, a, b);

            for (Participant p : List.of(a, b)) p.callbacks.restoredProbe = () -> {
                assertEquals(0, TimeValues.value(p.rti.queryLogicalTime()), "Restore the last granted time, not the save timestamp");
                assertThrows(InTimeAdvancingState.class, () -> p.rti.timeAdvanceRequest(TimeValues.time(4)));
            };
            a.rti.requestFederationRestore("timed");
            await("timed restore initiated", () -> a.callbacks.restoreLabels.contains("timed")
                    && b.callbacks.restoreLabels.contains("timed"), a, b);
            a.rti.federateRestoreComplete();
            b.rti.federateRestoreComplete();
            await("restored pending grants", () -> a.callbacks.restored == 1 && b.callbacks.restored == 1
                    && a.callbacks.grants.size() == 3 && b.callbacks.grants.size() == 3, a, b);
            for (Participant p : List.of(a, b)) {
                assertNull(p.callbacks.probeFailure, "The restored state precedes both federationRestored and the resumed grant");
                assertEquals(List.of(3.0, 5.0, 3.0), p.callbacks.grants);
                assertEquals(3, TimeValues.value(p.rti.queryLogicalTime()));
            }
            advance(4, a, b);
        }
    }

    @Test @Timeout(120)
    void abortAndParticipantFailuresDeliverTheExactStandardReason() throws Exception {
        try (Federation f = new Federation()) {
            Participant a = f.a, b = f.b;
            assertThrows(SaveNotInProgress.class, a.rti::abortFederationSave);
            assertThrows(RestoreNotInProgress.class, a.rti::abortFederationRestore);
            assertThrows(SaveNotInitiated.class, a.rti::federateSaveBegun);
            assertThrows(FederateHasNotBegunSave.class, a.rti::federateSaveComplete);
            assertThrows(RestoreNotRequested.class, a.rti::federateRestoreComplete);

            initiateSave("abort", f);
            a.rti.abortFederationSave();
            awaitSaveFailure(SaveFailureReason.SAVE_ABORTED, 1, a, b);
            a.rti.queryFederationSaveStatus();
            await("idle save status", () -> a.callbacks.saveStatus != null, a, b);
            for (FederateHandleSaveStatusPair entry : a.callbacks.saveStatus) assertEquals(SaveStatus.NO_SAVE_IN_PROGRESS, entry.status);

            initiateSave("participant-failure", f);
            a.rti.federateSaveBegun();
            b.rti.federateSaveBegun();
            a.rti.federateSaveNotComplete();
            b.rti.federateSaveComplete();
            awaitSaveFailure(SaveFailureReason.FEDERATE_REPORTED_FAILURE_DURING_SAVE, 2, a, b);

            save("restorable", f);
            a.rti.requestFederationRestore("restorable");
            await("abortable restore", () -> a.callbacks.restoreLabels.size() == 1 && b.callbacks.restoreLabels.size() == 1, a, b);
            a.rti.abortFederationRestore();
            awaitRestoreFailure(RestoreFailureReason.RESTORE_ABORTED, 1, a, b);

            a.rti.requestFederationRestore("restorable");
            await("second restore", () -> a.callbacks.restoreLabels.size() == 2 && b.callbacks.restoreLabels.size() == 2, a, b);
            a.rti.federateRestoreNotComplete();
            b.rti.federateRestoreComplete();
            awaitRestoreFailure(RestoreFailureReason.FEDERATE_REPORTED_FAILURE_DURING_RESTORE, 2, a, b);
            assertEquals(0, a.callbacks.restored);
            assertEquals(0, b.callbacks.restored);
            assertThrows(RTIinternalError.class, () -> a.rti.requestFederationRestore("missing-label"));
            await("restore request rejected callback", () -> a.callbacks.restoreRejected.contains("missing-label"), a, b);
            assertTrue(b.callbacks.restoreRejected.isEmpty(), "Only the requester receives rejection");
        }
    }

    private static void initiateSave(String label, Federation f) throws Exception {
        f.a.rti.requestFederationSave(label);
        assertFalse(f.a.callbacks.saveLabels.contains(label));
        await("initiate save " + label, () -> f.a.callbacks.saveLabels.contains(label)
                && f.b.callbacks.saveLabels.contains(label), f.a, f.b);
    }

    private static void save(String label, Federation f) throws Exception {
        initiateSave(label, f);
        completeSave(f);
    }

    private static void completeSave(Federation f) throws Exception {
        int expected = f.a.callbacks.saved + 1;
        assertThrows(FederateHasNotBegunSave.class, f.a.rti::federateSaveComplete);
        f.a.rti.federateSaveBegun();
        f.b.rti.federateSaveBegun();
        f.a.rti.federateSaveComplete();
        f.a.callbacks.saveStatus = null;
        f.a.rti.queryFederationSaveStatus();
        assertNull(f.a.callbacks.saveStatus, "Status responses are evoked, never inline");
        await("save progress", () -> f.a.callbacks.saveStatus != null, f.a, f.b);
        assertSaveStatus(f.a.callbacks.saveStatus, f.a.handle, SaveStatus.FEDERATE_WAITING_FOR_FEDERATION_TO_SAVE);
        assertSaveStatus(f.a.callbacks.saveStatus, f.b.handle, SaveStatus.FEDERATE_SAVING);
        assertEquals(expected - 1, f.a.callbacks.saved);
        f.b.rti.federateSaveComplete();
        await("save complete", () -> f.a.callbacks.saved == expected && f.b.callbacks.saved == expected, f.a, f.b);
    }

    private static void advance(double time, Participant... participants) throws Exception {
        int[] counts = new int[participants.length];
        for (int i = 0; i < participants.length; i++) {
            counts[i] = participants[i].callbacks.grants.size();
            participants[i].rti.timeAdvanceRequest(TimeValues.time(time));
        }
        await("time advance " + time, () -> {
            for (int i = 0; i < participants.length; i++) if (participants[i].callbacks.grants.size() <= counts[i]) return false;
            return true;
        }, participants);
        for (Participant p : participants) assertEquals(time, TimeValues.value(p.rti.queryLogicalTime()));
    }

    private static void await(String description, BooleanSupplier done, Participant... participants) throws Exception {
        long deadline = System.nanoTime() + Duration.ofSeconds(12).toNanos();
        while (!done.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant p : participants) {
                p.rti.evokeMultipleCallbacks(0.001, 0.02);
                assertNull(p.callbacks.connectionFailure);
            }
        }
        assertTrue(done.getAsBoolean(), "Timed out awaiting " + description);
    }

    private static void awaitSaveFailure(SaveFailureReason reason, int count, Participant... participants) throws Exception {
        await("save failure " + reason, () -> {
            for (Participant p : participants) if (p.callbacks.saveFailures.size() < count) return false;
            return true;
        }, participants);
        for (Participant p : participants) assertEquals(reason, p.callbacks.saveFailures.get(count - 1));
    }

    private static void awaitRestoreFailure(RestoreFailureReason reason, int count, Participant... participants) throws Exception {
        await("restore failure " + reason, () -> {
            for (Participant p : participants) if (p.callbacks.restoreFailures.size() < count) return false;
            return true;
        }, participants);
        for (Participant p : participants) assertEquals(reason, p.callbacks.restoreFailures.get(count - 1));
    }

    private static void assertSaveStatus(FederateHandleSaveStatusPair[] values, FederateHandle handle, SaveStatus expected) {
        assertEquals(2, values.length);
        for (FederateHandleSaveStatusPair value : values) if (value.handle.equals(handle)) { assertEquals(expected, value.status); return; }
        fail("Missing save participant " + handle);
    }

    private static void assertRestoreStatus(FederateRestoreStatus[] values, FederateHandle handle, RestoreStatus expected) {
        assertEquals(2, values.length);
        for (FederateRestoreStatus value : values) if (value.preRestoreHandle.equals(handle)) {
            assertEquals(handle, value.postRestoreHandle);
            assertEquals(expected, value.status);
            return;
        }
        fail("Missing restore participant " + handle);
    }

    @FunctionalInterface private interface Probe { void run() throws Exception; }

    private static final class Federation implements AutoCloseable {
        final String name = "java-save-restore-" + UUID.randomUUID();
        Participant a = new Participant(), b = new Participant();
        boolean created;

        Federation() throws Exception {
            try {
                for (Participant p : List.of(a, b)) {
                    p.rti.connect(p.callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
                    p.connected = true;
                }
                URL fom = LiveSaveRestoreTest.class.getResource("/exchange-fom.xml");
                assertNotNull(fom);
                a.rti.createFederationExecution(name, new URL[]{fom});
                created = true;
                a.handle = a.rti.joinFederationExecution("A", "java-save-restore", name);
                a.joined = true;
                b.handle = b.rti.joinFederationExecution("B", "java-save-restore", name);
                b.joined = true;
                assertNotEquals(a.handle, b.handle);
            } catch (Exception | Error failure) {
                try { close(); } catch (Exception cleanup) { failure.addSuppressed(cleanup); }
                throw failure;
            }
        }

        void replaceAmbassadors() throws Exception {
            replaceAmbassadors("java-save-restore");
        }

        void replaceAmbassadors(String type) throws Exception {
            for (Participant p : List.of(b, a)) {
                p.rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                p.joined = false;
                p.rti.disconnect(); p.connected = false;
            }
            a = new Participant(); b = new Participant();
            for (Participant p : List.of(a, b)) {
                p.rti.connect(p.callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
                p.connected = true;
                p.handle = p.rti.joinFederationExecution(p == a ? "A" : "B", type, name);
                p.joined = true;
            }
        }

        @Override public void close() throws Exception {
            List<Exception> failures = new ArrayList<>();
            if (a.joined) {
                try { a.rti.abortFederationSave(); } catch (SaveNotInProgress expected) { } catch (Exception failure) { failures.add(failure); }
                try { a.rti.abortFederationRestore(); } catch (RestoreNotInProgress expected) { } catch (Exception failure) { failures.add(failure); }
            }
            for (Participant p : List.of(b, a)) if (p.joined) {
                try { p.rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); p.joined = false; }
                catch (Exception failure) { failures.add(failure); }
            }
            if (created) try { a.rti.destroyFederationExecution(name); } catch (Exception failure) { failures.add(failure); }
            for (Participant p : List.of(b, a)) if (p.connected) {
                try { p.rti.disconnect(); p.connected = false; } catch (Exception failure) { failures.add(failure); }
            }
            if (!failures.isEmpty()) {
                Exception failure = new Exception("Save/restore live cleanup failed");
                failures.forEach(failure::addSuppressed);
                throw failure;
            }
        }
    }

    private static final class Participant {
        final RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        final Callbacks callbacks = new Callbacks();
        FederateHandle handle;
        boolean connected, joined;
        Participant() throws Exception { }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final Thread callbackThread = Thread.currentThread();
        final List<String> saveLabels = new ArrayList<>(), restoreLabels = new ArrayList<>();
        final List<String> restoreOrder = new ArrayList<>(), restoreRejected = new ArrayList<>();
        final List<ObjectInstanceHandle> discovered = new ArrayList<>(), removed = new ArrayList<>();
        final List<Double> grants = new ArrayList<>();
        final List<Double> saveTimes = new ArrayList<>();
        final List<SaveFailureReason> saveFailures = new ArrayList<>();
        final List<RestoreFailureReason> restoreFailures = new ArrayList<>();
        FederateHandleSaveStatusPair[] saveStatus;
        FederateRestoreStatus[] restoreStatus;
        FederateHandle restoreHandle;
        String restoreName, connectionFailure;
        int saved, restored, reflections;
        boolean regulating, constrained;
        Probe restoredProbe;
        Throwable probeFailure;
        void thread() { assertSame(callbackThread, Thread.currentThread()); }
        @Override public void initiateFederateSave(String label) { thread(); saveLabels.add(label); }
        @Override public void initiateFederateSave(String label, LogicalTime time) { thread(); saveLabels.add(label); saveTimes.add(TimeValues.value(time)); }
        @Override public void federationSaved() { thread(); saved++; }
        @Override public void federationNotSaved(SaveFailureReason reason) { thread(); saveFailures.add(reason); }
        @Override public void federationNotRestored(RestoreFailureReason reason) { thread(); restoreFailures.add(reason); }
        @Override public void federationSaveStatusResponse(FederateHandleSaveStatusPair[] values) { thread(); saveStatus = values; }
        @Override public void federationRestoreStatusResponse(FederateRestoreStatus[] values) { thread(); restoreStatus = values; }
        @Override public void requestFederationRestoreSucceeded(String label) { thread(); restoreOrder.add("accepted:" + label); }
        @Override public void requestFederationRestoreFailed(String label) { thread(); restoreRejected.add(label); }
        @Override public void federationRestoreBegun() { thread(); restoreOrder.add("begun"); }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) {
            thread(); restoreLabels.add(label); restoreOrder.add("initiate:" + label); restoreHandle = handle; restoreName = name;
        }
        @Override public void federationRestored() {
            thread();
            if (restoredProbe != null) try { restoredProbe.run(); } catch (Throwable failure) { probeFailure = failure; }
            restored++;
        }
        @Override public void timeRegulationEnabled(LogicalTime time) { thread(); regulating = true; }
        @Override public void timeConstrainedEnabled(LogicalTime time) { thread(); constrained = true; }
        @Override public void timeAdvanceGrant(LogicalTime time) { thread(); grants.add(TimeValues.value(time)); }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name, FederateHandle producer) { thread(); discovered.add(object); }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { thread(); discovered.add(object); }
        @Override public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent, SupplementalRemoveInfo info) { thread(); removed.add(object); }
        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReflectInfo info) { thread(); reflections++; }
        @Override public void connectionLost(String description) { thread(); connectionFailure = description; }
    }
}
