package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.InvalidFederateHandle;
import hla.rti1516e.exceptions.RestoreInProgress;
import java.net.URL;
import java.util.UUID;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
class LiveRestoreLookupTest {
    @Test @Timeout(60)
    void replacementCanQueryPersistedStateBeforeRestoredCallbackButCannotMutate() throws Exception {
        String federation = "java-restore-lookups-" + UUID.randomUUID();
        String endpoint = System.getProperty("gorti.endpoint");
        RTIambassador first = new GortiRtiAmbassador();
        Events savedEvents = new Events();
        FederateHandle savedHandle;
        first.connect(savedEvents, CallbackModel.HLA_EVOKED, endpoint);
        try {
            first.createFederationExecution(federation, new URL[]{getClass().getResource("/exchange-fom.xml")});
            savedHandle = first.joinFederationExecution("participant", "restore-lookups", federation);
            try {
                first.setAutomaticResignDirective(ResignAction.NO_ACTION);
                ObjectClassHandle type = first.getObjectClassHandle("HLAobjectRoot.Item");
                AttributeHandleSet attributes = first.getAttributeHandleSetFactory().create();
                attributes.add(first.getAttributeHandle(type, "value"));
                first.publishObjectClassAttributes(type, attributes);
                first.reserveObjectInstanceName("saved-item");
                RestoreLookupLifecycleTest.await(first, () -> savedEvents.reserved);
                first.registerObjectInstance(type, "saved-item");
                first.requestFederationSave("saved");
                RestoreLookupLifecycleTest.await(first, () -> savedEvents.saving);
                first.federateSaveBegun();
                first.federateSaveComplete();
                RestoreLookupLifecycleTest.await(first, () -> savedEvents.saved);
            } finally {
                first.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            }
        } finally {
            first.disconnect();
        }

        RTIambassador replacement = new GortiRtiAmbassador();
        Events events = new Events();
        replacement.connect(events, CallbackModel.HLA_EVOKED, endpoint);
        try {
            FederateHandle freshHandle = replacement.joinFederationExecution("participant", "restore-lookups", federation);
            try {
                assertNotEquals(savedHandle, freshHandle);
                replacement.requestFederationRestore("saved");
                RestoreLookupLifecycleTest.await(replacement, () -> events.restoring);
                assertThrows(RestoreInProgress.class, replacement::disableCallbacks);
                replacement.federateRestoreComplete();
                RestoreLookupLifecycleTest.awaitAdmittedRestore(replacement, 1);
                replacement.disableCallbacks();

                assertEquals(savedHandle, replacement.getFederateHandle("participant"));
                assertEquals("participant", replacement.getFederateName(savedHandle));
                assertThrows(InvalidFederateHandle.class, () -> replacement.getFederateName(freshHandle));
                assertEquals(ResignAction.NO_ACTION, replacement.getAutomaticResignDirective());
                ObjectInstanceHandle object = replacement.getObjectInstanceHandle("saved-item");
                assertEquals("saved-item", replacement.getObjectInstanceName(object));
                assertEquals(replacement.getObjectClassHandle("HLAobjectRoot.Item"), replacement.getKnownObjectClassHandle(object));
                assertDoesNotThrow(() -> replacement.normalizeServiceGroup(ServiceGroup.SUPPORT_SERVICES));
                assertFalse(events.restored);
                assertThrows(RestoreInProgress.class, () -> replacement.timeAdvanceRequest(TimeValues.time(1)));
                assertThrows(RestoreInProgress.class, replacement::enableAttributeScopeAdvisorySwitch);

                replacement.enableCallbacks();
                RestoreLookupLifecycleTest.await(replacement, () -> events.restored);
                AttributeHandleValueMap values = replacement.getAttributeHandleValueMapFactory().create(1);
                values.put(replacement.getAttributeHandle(replacement.getKnownObjectClassHandle(object), "value"), new byte[]{1});
                replacement.updateAttributeValues(object, values, new byte[0]);
            } finally {
                replacement.enableCallbacks();
                replacement.evokeMultipleCallbacks(0, 0.1);
                replacement.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            }
            replacement.destroyFederationExecution(federation);
        } finally {
            replacement.disconnect();
        }
    }

    private static final class Events extends NullFederateAmbassador {
        boolean reserved, saving, saved, restoring, restored;
        @Override public void objectInstanceNameReservationSucceeded(String name) { reserved = true; }
        @Override public void initiateFederateSave(String label) { saving = true; }
        @Override public void federationSaved() { saved = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { restoring = true; }
        @Override public void federationRestored() { restored = true; }
    }
}
