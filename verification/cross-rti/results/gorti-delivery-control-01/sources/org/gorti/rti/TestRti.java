package org.gorti.rti;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.RtiFactoryFactory;

/** Provider and connection setup only; service assertions stay in the source tests. */
final class TestRti {
    private static int connections;

    static RTIambassador ambassador() throws Exception {
        if ("portico".equals(System.getProperty("cross.rti"))) {
            return RtiFactoryFactory.getRtiFactory().getRtiAmbassador();
        }
        return RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
    }

    static void connect(RTIambassador rti, FederateAmbassador callbacks) throws Exception {
        if ("portico".equals(System.getProperty("cross.rti"))) {
            int port = Integer.parseInt(System.getProperty("cross.port.base")) + connections++;
            System.setProperty("portico.jgroups.tcp.bindPort", Integer.toString(port));
            rti.connect(callbacks, CallbackModel.HLA_EVOKED);
        } else {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
        }
    }

    private TestRti() { }
}
