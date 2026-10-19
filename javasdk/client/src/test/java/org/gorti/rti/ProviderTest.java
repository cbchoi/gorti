package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.Metadata;
import io.grpc.Status;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class ProviderTest {
    @Test void standardFactoryDiscovery() throws Exception {
        assertEquals("gorti", RtiFactoryFactory.getRtiFactory("gorti").rtiName());
        assertTrue(RtiFactoryFactory.getAvailableRtiFactories().stream().anyMatch(f -> f.rtiName().equals("gorti")));
        assertInstanceOf(GortiRtiAmbassador.class, RtiFactoryFactory.getRtiFactory().getRtiAmbassador());
        assertArrayEquals(new byte[] {0, 0, 0, 42}, RtiFactoryFactory.getRtiFactory("gorti")
                .getEncoderFactory().createHLAinteger32BE(42).toByteArray());
        assertEquals("HLAfloat64Time", LogicalTimeFactoryFactory.getLogicalTimeFactory("HLAfloat64Time").getName());
    }
    @Test void disconnectedServicesFailWithoutNetwork() {
        RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        assertThrows(NotConnected.class, rti::queryLogicalTime);
        assertThrows(NotConnected.class, rti::getTimeFactory);
        assertThrows(NotConnected.class, () -> rti.joinFederationExecution("type", "missing"));
        assertDoesNotThrow(rti::disconnect);
    }
    @Test void encoderFactoryDoesNotRequireConnection() throws Exception {
        GortiRtiFactory factory = new GortiRtiFactory();
        assertArrayEquals(new byte[] {0, 0, 0, 1, 0, 65},
                factory.getEncoderFactory().createHLAunicodeString("A").toByteArray());
    }
    @Test void connectRejectsUnsupportedModeAndAmbiguousSettings() {
        RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        assertThrows(UnsupportedCallbackModel.class,
                () -> rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_IMMEDIATE, "grpc://localhost:1"));
        assertThrows(InvalidLocalSettingsDesignator.class,
                () -> rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, "localhost:50051"));
        assertThrows(InvalidLocalSettingsDesignator.class,
                () -> rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, "grpc://user:secret@localhost:50051"));
    }
    @Test void callbackWaitRejectsInvalidIntervals() throws Exception {
        RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        assertThrows(RTIinternalError.class, () -> rti.evokeCallback(Double.NaN));
        assertThrows(RTIinternalError.class, () -> rti.evokeMultipleCallbacks(1, 0));
        assertFalse(rti.evokeCallback(0));
    }
    @Test void canonicalWireErrorsMapToStandardExceptionTypes() {
        Metadata trailers = new Metadata();
        trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), "InTimeAdvancingState");
        assertInstanceOf(InTimeAdvancingState.class,
                WireErrors.translate(Status.FAILED_PRECONDITION.withDescription("advance pending").asRuntimeException(trailers)));
        trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), "java.lang.Runtime");
        assertInstanceOf(RTIinternalError.class, WireErrors.translate(Status.UNKNOWN.asRuntimeException(trailers)));
    }
}
