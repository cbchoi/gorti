package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederationExecutionInformation;
import hla.rti1516e.FederationExecutionInformationSet;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.exceptions.CouldNotCreateLogicalTimeFactory;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.time.HLAinteger64TimeFactory;
import io.grpc.StatusRuntimeException;
import java.net.URI;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.UUID;
import org.gorti.rti.transport.WireSession;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;

import static org.junit.jupiter.api.Assertions.*;

/** Real-server metadata checks; unsupported successful joins are covered by the loopback test. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires a server with logical-time selection metadata")
@Timeout(30)
class LiveFederationTimeSelectionTest {
    private static final String FLOAT64 = "HLAfloat64Time";

    @ParameterizedTest
    @ValueSource(strings = {"HLAfloat64Time", "", "HLAinteger64Time"})
    void sdkCreateListAndJoinUseTheSelectedMetadata(String requested) throws Exception {
        try (Fixture f = new Fixture()) {
            String expected = requested.isEmpty() ? FLOAT64 : requested;
            f.client.createFederationExecution(f.name, new URL[]{f.fom}, requested);
            f.created = true;
            Federation.FederationSummary summary = f.summary();
            assertEquals(expected, summary.getLogicalTimeImplementationName(),
                    "The actual server must report its selection, not rely on Java's legacy fallback");
            f.client.listFederationExecutions();
            assertNull(f.reported);
            f.client.evokeMultipleCallbacks(0, 0);
            assertNotNull(f.reported);
            assertEquals(expected, f.reported.logicalTimeImplementationName);
            f.client.joinFederationExecution("java", "time-selection", f.name);
            f.joined = true;
            if ("HLAinteger64Time".equals(expected)) {
                assertEquals(expected, assertInstanceOf(HLAinteger64TimeFactory.class, f.client.getTimeFactory()).getName());
                assertEquals(0L, assertInstanceOf(HLAinteger64Time.class, f.client.queryLogicalTime()).getValue());
            } else {
                assertEquals(expected, assertInstanceOf(HLAfloat64TimeFactory.class, f.client.getTimeFactory()).getName());
                assertEquals(0.0, assertInstanceOf(HLAfloat64Time.class, f.client.queryLogicalTime()).getValue());
            }
        }
    }

    @ParameterizedTest
    @ValueSource(strings = {"HLAfloat64Time", "", "HLAinteger64Time"})
    void serverCreateAndJoinResponsesExposeExplicitOrLegacyDefaultSelection(String requested) throws Exception {
        try (Fixture f = new Fixture()) {
            String expected = requested.isEmpty() ? FLOAT64 : requested;
            Federation.CreateFederationResponse created = f.stub().createFederation(f.request(f.name, requested));
            f.created = true;
            assertEquals(expected, created.getLogicalTimeImplementationName());
            assertEquals(expected, f.summary().getLogicalTimeImplementationName());
            Federation.JoinFederationResponse joined = f.stub().joinFederation(Federation.JoinFederationRequest.newBuilder()
                    .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(f.name)
                    .setFederateName("wire").setFederateType("time-selection")
                    .setExpectedFederationGeneration(created.getFederationGeneration()).build());
            f.wire.setMembership(f.name, joined.getFederateHandle(), created.getFederationGeneration(), joined.getSessionToken());
            f.rawHandle = joined.getFederateHandle();
            assertEquals(expected, joined.getLogicalTimeImplementationName());
        }
    }

    @Test
    void serverRejectsUnsupportedSelectorsWithTypedMetadataAndNoCreatedFederation() throws Exception {
        try (Fixture f = new Fixture()) {
            for (String unsupported : new String[]{"UnsupportedTime", "VendorExactTime"}) {
                StatusRuntimeException failure = assertThrows(StatusRuntimeException.class,
                        () -> f.stub().createFederation(f.request(f.name, unsupported)));
                assertInstanceOf(CouldNotCreateLogicalTimeFactory.class, WireErrors.translate(failure));
                assertTrue(f.stub().listFederations(Federation.ListFederationsRequest.newBuilder()
                        .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).build()).getFederationsList().stream()
                        .noneMatch(entry -> entry.getName().equals(f.name)));
                assertThrows(CouldNotCreateLogicalTimeFactory.class,
                        () -> f.client.createFederationExecution(f.name, new URL[]{f.fom}, unsupported));
            }
        }
    }

    private static final class Fixture implements AutoCloseable {
        final String name = "gorti-java-time-selection-" + UUID.randomUUID();
        final RTIambassador client = new GortiRtiFactory().getRtiAmbassador();
        final URL fom = LiveFederationTimeSelectionTest.class.getResource("/exchange-fom.xml");
        final WireSession wire;
        FederationExecutionInformation reported;
        boolean created;
        boolean joined;
        long rawHandle;

        Fixture() throws Exception {
            assertNotNull(fom);
            String endpoint = System.getProperty("gorti.endpoint");
            URI address = URI.create(endpoint);
            assertEquals("grpc", address.getScheme());
            wire = new WireSession(address.getRawAuthority(), true);
            client.connect(new NullFederateAmbassador() {
                @Override public void reportFederationExecutions(FederationExecutionInformationSet entries) {
                    for (FederationExecutionInformation entry : entries) {
                        if (entry.federationExecutionName.equals(name)) reported = entry;
                    }
                }
            }, CallbackModel.HLA_EVOKED, endpoint);
        }

        FederationServiceGrpc.FederationServiceBlockingStub stub() {
            return FederationServiceGrpc.newBlockingStub(wire.unauthenticatedChannel());
        }

        Federation.CreateFederationRequest request(String name, String time) throws Exception {
            return Federation.CreateFederationRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                    .setFederationName(name).setMode(Common.Mode.MODE_BEST_EFFORT).setLogicalTimeImplementationName(time)
                    .addFomModules(Common.FOMModule.newBuilder().setPath("exchange-fom.xml")
                            .setXml(ByteString.copyFrom(Files.readAllBytes(Path.of(fom.toURI()))))).build();
        }

        Federation.FederationSummary summary() {
            return stub().listFederations(Federation.ListFederationsRequest.newBuilder()
                    .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).build()).getFederationsList().stream()
                    .filter(entry -> entry.getName().equals(name)).findFirst().orElseThrow();
        }

        @Override public void close() throws Exception {
            try {
                if (rawHandle != 0) {
                    FederationServiceGrpc.newBlockingStub(wire.channel()).resignFederation(Federation.ResignFederationRequest.newBuilder()
                            .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(name).setFederateHandle(rawHandle)
                            .setAction(Common.ResignAction.RESIGN_ACTION_CANCEL_THEN_DELETE).build());
                    wire.clearMembership();
                }
                if (joined) client.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                if (created) client.destroyFederationExecution(name);
            } finally {
                try { client.disconnect(); }
                finally { wire.close(); }
            }
        }
    }
}
