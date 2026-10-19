package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import java.net.URL;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.*;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

/** Promoted from the independently authored October 2 callback contract probe. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
@Timeout(40)
class LiveCallbackGateContractTest {
    private RTIambassador rti;
    private Events events;
    private String federation;
    private boolean connected, created, joined;

    @BeforeEach void setup() throws Exception {
        rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        events = new Events();
        federation = "callback-gate-" + UUID.randomUUID();
        rti.connect(events, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
        connected = true;
        URL fom = getClass().getResource("/exchange-fom.xml");
        assertNotNull(fom);
        rti.createFederationExecution(federation, new URL[]{fom});
        created = true;
        rti.joinFederationExecution("callback-gate", "java-contract", federation);
        joined = true;
    }

    @AfterEach void cleanup() throws Exception {
        try {
            if (joined) {
                if (events.restore && !events.restored && !events.restoreFailed) {
                    rti.abortFederationRestore();
                    await(() -> events.restoreFailed);
                } else if (events.save && !events.saved && !events.saveFailed) {
                    rti.abortFederationSave();
                    await(() -> events.saveFailed);
                }
                rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            }
            if (created) rti.destroyFederationExecution(federation);
        } finally {
            if (connected) rti.disconnect();
        }
    }

    private void saveBegun() throws Exception {
        rti.requestFederationSave("gate-save");
        await(() -> events.save);
        rti.federateSaveBegun();
    }

    private void restoreBegun() throws Exception {
        saveBegun();
        rti.federateSaveComplete();
        await(() -> events.saved);
        rti.requestFederationRestore("gate-save");
        await(() -> events.restore);
    }

    @Test void enableDuringSaveThrowsSaveInProgress() throws Exception {
        saveBegun();
        assertThrows(SaveInProgress.class, rti::enableCallbacks);
    }

    @Test void disableDuringSaveThrowsSaveInProgressWithoutDisablingDelivery() throws Exception {
        saveBegun();
        assertThrows(SaveInProgress.class, rti::disableCallbacks);
        rti.federateSaveComplete();
        await(() -> events.saved);
        rti.disableCallbacks();
        rti.enableCallbacks();
    }

    @Test void enableDuringRestoreThrowsRestoreInProgress() throws Exception {
        restoreBegun();
        assertThrows(RestoreInProgress.class, rti::enableCallbacks);
    }

    @Test void disableDuringRestoreThrowsRestoreInProgressWithoutDisablingDelivery() throws Exception {
        restoreBegun();
        assertThrows(RestoreInProgress.class, rti::disableCallbacks);
        rti.federateRestoreComplete();
        await(() -> events.restored);
        rti.disableCallbacks();
        rti.enableCallbacks();
        assertThrows(RestoreNotRequested.class, rti::federateRestoreComplete);
        assertThrows(RestoreNotRequested.class, rti::federateRestoreNotComplete);
    }

    @Test void idleRestoreResponsesUseTheStandardException() throws Exception {
        assertThrows(RestoreNotRequested.class, rti::federateRestoreComplete);
        assertThrows(RestoreNotRequested.class, rti::federateRestoreNotComplete);
    }

    @Test void restoreResponsesDuringSaveUseTheServiceSpecificSaveGuard() throws Exception {
        saveBegun();
        assertThrows(SaveInProgress.class, rti::federateRestoreComplete);
        assertThrows(SaveInProgress.class, rti::federateRestoreNotComplete);
    }

    @Test void initiallyDisabledPeerSaveCanBeEnabledBeforeInitiateEntry() throws Exception {
        withPeer(false);
    }

    @Test void initiallyDisabledPeerRestoreCanBeEnabledBeforeInitiateEntry() throws Exception {
        withPeer(true);
    }

    private void withPeer(boolean restoring) throws Exception {
        RTIambassador peer = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        Events peerEvents = new Events();
        peer.connect(peerEvents, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
        peer.joinFederationExecution("peer", "java-contract", federation);
        try {
            if (restoring) {
                peer.requestFederationSave("peer-save");
                await(() -> events.save);
                await(peer, () -> peerEvents.save);
                rti.federateSaveBegun(); peer.federateSaveBegun();
                rti.federateSaveComplete(); peer.federateSaveComplete();
                await(() -> events.saved); await(peer, () -> peerEvents.saved);
            }
            rti.disableCallbacks();
            if (restoring) peer.requestFederationRestore("peer-save");
            else peer.requestFederationSave("peer-save");
            await(peer, () -> restoring ? peerEvents.restore : peerEvents.save);
            awaitPendingWhileDisabled();
            assertFalse(restoring ? events.restore : events.save, "Initiation has not entered the disabled federate");
            if (!restoring) {
                assertThrows(SaveInProgress.class, rti::federateRestoreComplete,
                        "Queued save initiation still governs restore-response validation");
                assertThrows(SaveInProgress.class, rti::federateRestoreNotComplete);
            }
            assertDoesNotThrow(rti::enableCallbacks, "Transport admission must not prevent initiation delivery");
            await(() -> restoring ? events.restore : events.save);
            if (restoring) {
                assertThrows(RestoreInProgress.class, rti::enableCallbacks);
                assertThrows(RestoreInProgress.class, rti::disableCallbacks);
                rti.federateRestoreComplete(); peer.federateRestoreComplete();
                await(() -> events.restored); await(peer, () -> peerEvents.restored);
            } else {
                assertThrows(SaveInProgress.class, rti::enableCallbacks);
                assertThrows(SaveInProgress.class, rti::disableCallbacks);
                rti.federateSaveBegun(); peer.federateSaveBegun();
                rti.federateSaveComplete(); peer.federateSaveComplete();
                await(() -> events.saved); await(peer, () -> peerEvents.saved);
            }
        } finally {
            try { peer.abortFederationSave(); } catch (RTIexception expected) { }
            try { peer.abortFederationRestore(); } catch (RTIexception expected) { }
            long deadline = System.nanoTime() + 10_000_000_000L;
            for (;;) {
                try { rti.enableCallbacks(); break; }
                catch (SaveInProgress | RestoreInProgress active) {
                    if (System.nanoTime() >= deadline) throw active;
                    Thread.sleep(1);
                }
            }
            rti.evokeMultipleCallbacks(0, 0.05);
            peer.evokeMultipleCallbacks(0, 0.05);
            peer.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            peer.disconnect();
        }
    }

    private void awaitPendingWhileDisabled() throws Exception {
        long deadline = System.nanoTime() + 10_000_000_000L;
        while (System.nanoTime() < deadline) {
            if (rti.evokeCallback(0)) return;
            Thread.sleep(1);
        }
        fail("Peer initiation must actually be admitted while callbacks are disabled");
    }

    @Test void switchesDeferDeliveryAndReconnectRestoresTheEnabledDefault() throws Exception {
        rti.disableCallbacks();
        rti.queryFederationSaveStatus();
        rti.evokeMultipleCallbacks(0.001, 0.02);
        assertFalse(events.status);
        rti.enableCallbacks();
        await(() -> events.status);
        rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
        joined = false;
        rti.disableCallbacks();
        rti.disconnect();
        connected = false;
        events.status = false;
        rti.connect(events, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
        connected = true;
        rti.joinFederationExecution("callback-gate", "java-contract", federation);
        joined = true;
        rti.queryFederationSaveStatus();
        await(() -> events.status);
    }

    private void await(BooleanSupplier ready) throws Exception {
        await(rti, ready);
    }

    private void await(RTIambassador target, BooleanSupplier ready) throws Exception {
        long deadline = System.nanoTime() + 10_000_000_000L;
        while (!ready.getAsBoolean() && System.nanoTime() < deadline) target.evokeMultipleCallbacks(0.001, 0.02);
        assertTrue(ready.getAsBoolean(), "Actual callback required before assertion");
    }

    private static class Events extends NullFederateAmbassador {
        boolean save, saved, saveFailed, restore, restored, restoreFailed, status;
        @Override public void initiateFederateSave(String label) { save = true; }
        @Override public void federationSaved() { saved = true; }
        @Override public void federationNotSaved(SaveFailureReason reason) { saveFailed = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { restore = true; }
        @Override public void federationRestored() { restored = true; }
        @Override public void federationNotRestored(RestoreFailureReason reason) { restoreFailed = true; }
        @Override public void federationSaveStatusResponse(FederateHandleSaveStatusPair[] values) { status = true; }
    }
}
