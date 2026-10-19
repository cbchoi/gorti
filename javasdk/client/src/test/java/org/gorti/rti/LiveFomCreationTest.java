package org.gorti.rti;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.exceptions.FederationExecutionDoesNotExist;
import hla.rti1516e.exceptions.RTIexception;
import java.net.URL;
import java.nio.file.Path;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

import static org.junit.jupiter.api.Assertions.*;

@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
@Timeout(30)
class LiveFomCreationTest {
    @Test
    void emptyCreationModulesAreRejectedWithoutCreatingAnExecution() throws Exception {
        RTIambassador rti = connected();
        String name = "empty-fom-contract-" + UUID.randomUUID();
        boolean created = false;
        try {
            RTIexception rejection = null;
            try {
                rti.createFederationExecution(name, new URL[0]);
                created = true;
            } catch (RTIexception failure) {
                rejection = failure;
            }
            assertNotNull(rejection, "Creation requires at least one FOM module; no particular exception identity is assumed");
            assertThrows(FederationExecutionDoesNotExist.class,
                    () -> rti.joinFederationExecution("probe", "test", name));
        } finally {
            try {
                if (created) rti.destroyFederationExecution(name);
            } finally {
                rti.disconnect();
            }
        }
    }

    @Test
    void validCreationAndEmptyAdditionalJoinModulesRetainDefaultMim() throws Exception {
        RTIambassador rti = connected();
        String name = "default-mim-contract-" + UUID.randomUUID();
        boolean created = false;
        boolean joined = false;
        try {
            URL fom = Path.of(System.getProperty("gorti.fom")).toUri().toURL();
            rti.createFederationExecution(name, new URL[]{fom});
            created = true;
            rti.joinFederationExecution("probe", "test", name, new URL[0]);
            joined = true;
            assertNotNull(rti.getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederate"));
            assertNotNull(rti.getInteractionClassHandle(
                    "HLAinteractionRoot.HLAmanager.HLAfederate.HLArequest.HLArequestPublications"));
        } finally {
            try {
                if (joined) rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            } finally {
                try {
                    if (created) rti.destroyFederationExecution(name);
                } finally {
                    rti.disconnect();
                }
            }
        }
    }

    private RTIambassador connected() throws Exception {
        RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED,
                System.getProperty("gorti.endpoint"));
        return rti;
    }
}
