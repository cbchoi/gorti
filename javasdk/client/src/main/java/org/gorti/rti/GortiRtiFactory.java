package org.gorti.rti;

import hla.rti1516e.RTIambassador;
import hla.rti1516e.RtiFactory;
import hla.rti1516e.encoding.EncoderFactory;
import hla.rti1516e.exceptions.RTIinternalError;

public final class GortiRtiFactory implements RtiFactory {
    @Override public RTIambassador getRtiAmbassador() { return new GortiRtiAmbassador(); }
    @Override public EncoderFactory getEncoderFactory() throws RTIinternalError {
        return new org.gorti.rti.encoding.GortiEncoderFactory();
    }
    @Override public String rtiName() { return "gorti"; }
    @Override public String rtiVersion() { return "0.1.0-SNAPSHOT"; }
}
