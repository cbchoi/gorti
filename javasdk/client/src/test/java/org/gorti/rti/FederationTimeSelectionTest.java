package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederationExecutionInformation;
import hla.rti1516e.FederationExecutionInformationSet;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.exceptions.CouldNotCreateLogicalTimeFactory;
import hla.rti1516e.exceptions.FederateAlreadyExecutionMember;
import hla.rti1516e.exceptions.FederateNotExecutionMember;
import hla.rti1516e.exceptions.RTIinternalError;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.ServerInterceptors;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import java.net.URL;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Stream;
import rti.v1.StreamServiceGrpc;

import static org.junit.jupiter.api.Assertions.*;

@Timeout(20)
class FederationTimeSelectionTest {
    private static final String FLOAT64 = "HLAfloat64Time";
    private URL[] creationModules() {
        URL module = getClass().getResource("/exchange-fom.xml");
        assertNotNull(module, "FOM test resource is missing");
        return new URL[]{module};
    }

    @Test
    void creationSendsExplicitFloat64IncludingDefaultSelection() throws Exception {
        try (Fixture f = new Fixture()) {
            f.rti.createFederationExecution("explicit", creationModules(), FLOAT64);
            f.rti.createFederationExecution("default", creationModules(), "");
            f.rti.createFederationExecution("overload", creationModules());
            f.createTime = "";
            f.rti.createFederationExecution("legacy", creationModules());
            assertEquals(4, f.creates.size());
            for (Federation.CreateFederationRequest request : f.creates) {
                assertEquals(FLOAT64, request.getLogicalTimeImplementationName());
            }
        }
    }

    @ParameterizedTest
    @ValueSource(strings = {"HLAinteger64Time", "VendorExactTime"})
    void createReportsContradictoryServerSelectionWithoutDestroyingTheExecution(String selected) throws Exception {
        try (Fixture f = new Fixture()) {
            f.createTime = selected;
            RTIinternalError failure = assertThrows(RTIinternalError.class,
                    () -> f.rti.createFederationExecution("contradiction", creationModules(), FLOAT64));
            assertTrue(failure.getMessage().contains(selected));
            assertEquals(1, f.creates.size());
            assertEquals(0, f.destroys.get(), "A created execution may already be visible to other participants");
        }
    }

    @Test
    void unsupportedCreationRemainsLocalWithoutLossyIntegerConversion() throws Exception {
        try (Fixture f = new Fixture()) {
            for (String name : new String[]{"UnsupportedTime", "VendorExactTime", "HLAfloat64Time ", null}) {
                assertThrows(CouldNotCreateLogicalTimeFactory.class,
                        () -> f.rti.createFederationExecution("unsupported", creationModules(), name));
            }
            assertTrue(f.creates.isEmpty());
        }
    }

    @Test
    void listReportsServerNamesWithoutInventingFactorySupport() throws Exception {
        try (Fixture f = new Fixture()) {
            f.summaries = List.of(summary("float", FLOAT64), summary("legacy", ""),
                    summary("integer", "HLAinteger64Time"), summary("custom", "VendorExactTime"));
            f.rti.listFederationExecutions();
            assertTrue(f.reports.isEmpty(), "Reports must remain evoked callbacks");
            f.rti.evokeMultipleCallbacks(0, 0);
            assertEquals(1, f.reports.size());
            Map<String, String> expected = Map.of("float", FLOAT64, "legacy", FLOAT64,
                    "integer", "HLAinteger64Time", "custom", "VendorExactTime");
            assertEquals(expected.size(), f.reports.get(0).size());
            for (FederationExecutionInformation info : f.reports.get(0)) {
                assertEquals(expected.get(info.federationExecutionName), info.logicalTimeImplementationName);
            }
        }
    }

    @ParameterizedTest
    @ValueSource(strings = {"HLAfloat64Time", ""})
    void joinAdoptsSupportedOrLegacyFactory(String metadata) throws Exception {
        try (Fixture f = new Fixture()) {
            f.summaries = List.of(summary("f", metadata));
            f.joinTime = metadata;
            f.rti.joinFederationExecution("participant", "test", "f");
            HLAfloat64TimeFactory factory = assertInstanceOf(HLAfloat64TimeFactory.class, f.rti.getTimeFactory());
            assertEquals(FLOAT64, factory.getName());
            assertEquals(1, f.subscriptions.get());
            f.rti.resignFederationExecution(ResignAction.NO_ACTION);
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
        }
    }

    @Test
    void advertisedUnsupportedFactoryIsRejectedBeforeJoining() throws Exception {
        try (Fixture f = new Fixture()) {
            f.summaries = List.of(summary("f", "UnsupportedTime"));
            assertThrows(CouldNotCreateLogicalTimeFactory.class,
                    () -> f.rti.joinFederationExecution("participant", "test", "f"));
            assertEquals(0, f.joins.get());
            assertEquals(0, f.subscriptions.get());
            assertTrue(f.resigns.isEmpty());
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
        }
    }

    @Test
    void integerSelectionUsesAnIntegerFactoryWithoutFloatFallback() throws Exception {
        try (Fixture f = new Fixture()) {
            f.createTime = "HLAinteger64Time";
            f.rti.createFederationExecution("integer", creationModules(), "HLAinteger64Time");
            assertEquals("HLAinteger64Time", f.creates.get(0).getLogicalTimeImplementationName());
            f.summaries = List.of(summary("f", "HLAinteger64Time"));
            f.joinTime = "HLAinteger64Time";
            f.rti.joinFederationExecution("participant", "test", "f");
            assertInstanceOf(hla.rti1516e.time.HLAinteger64TimeFactory.class, f.rti.getTimeFactory());
            f.rti.resignFederationExecution(ResignAction.NO_ACTION);
        }
    }

    @ParameterizedTest
    @ValueSource(strings = {"UnsupportedTime", "VendorExactTime", "HLAfloat64Time "})
    void authoritativeUnsupportedJoinResponseIsCompensatedBeforeCallbacks(String unsupported) throws Exception {
        try (Fixture f = new Fixture()) {
            f.joinTime = unsupported;
            assertThrows(CouldNotCreateLogicalTimeFactory.class,
                    () -> f.rti.joinFederationExecution("participant", "test", "f"));
            assertEquals(1, f.resigns.size(), "Failed join must not leave an untracked remote member");
            Federation.ResignFederationRequest resign = f.resigns.get(0);
            assertEquals("f", resign.getFederationName());
            assertEquals(41, resign.getFederateHandle());
            assertEquals(Common.ResignAction.RESIGN_ACTION_CANCEL_THEN_DELETE, resign.getAction());
            assertArrayEquals(Fixture.TOKEN.toByteArray(), f.resignToken);
            assertEquals(0, f.subscriptions.get(), "Unsupported factories must fail before stream readiness");
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
            f.joinTime = FLOAT64;
            assertDoesNotThrow(() -> f.rti.joinFederationExecution("participant", "test", "f"));
            assertInstanceOf(HLAfloat64TimeFactory.class, f.rti.getTimeFactory());
            f.rti.resignFederationExecution(ResignAction.NO_ACTION);
        }
    }

    @Test
    void failedCompensationRetainsCleanupIdentityButDoesNotExposeAFloatFactory() throws Exception {
        try (Fixture f = new Fixture()) {
            f.joinTime = "UnsupportedTime";
            f.rejectResign = true;
            RTIinternalError failure = assertThrows(RTIinternalError.class,
                    () -> f.rti.joinFederationExecution("participant", "test", "f"));
            assertInstanceOf(CouldNotCreateLogicalTimeFactory.class, failure.getCause());
            assertEquals(1, failure.getCause().getSuppressed().length);
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
            assertThrows(RTIinternalError.class, f.rti::queryLogicalTime);
            assertDoesNotThrow(() -> f.rti.evokeCallback(0),
                    "Evocation must not discard the identity needed to retry failed join cleanup");
            assertThrows(FederateAlreadyExecutionMember.class,
                    () -> f.rti.joinFederationExecution("another", "test", "f"));
            assertEquals(0, f.subscriptions.get());
            f.rejectResign = false;
            f.rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            assertEquals(2, f.resigns.size());
            f.joinTime = "";
            f.rti.joinFederationExecution("participant", "test", "f");
            assertInstanceOf(HLAfloat64TimeFactory.class, f.rti.getTimeFactory());
            f.rti.resignFederationExecution(ResignAction.NO_ACTION);
        }
    }

    @Test
    void createPropagatesAuthoritativeFactoryExceptionMetadata() throws Exception {
        try (Fixture f = new Fixture()) {
            f.rejectCreate = true;
            assertThrows(CouldNotCreateLogicalTimeFactory.class,
                    () -> f.rti.createFederationExecution("rejected", creationModules(), FLOAT64));
            assertEquals(1, f.creates.size());
        }
    }

    @Test
    void emptyCreationArraysAreRejectedByEveryOverloadBeforeRpc() throws Exception {
        try (Fixture f = new Fixture()) {
            assertThrows(hla.rti1516e.exceptions.CouldNotOpenFDD.class,
                    () -> f.rti.createFederationExecution("empty", new URL[0]));
            assertThrows(hla.rti1516e.exceptions.CouldNotOpenFDD.class,
                    () -> f.rti.createFederationExecution("empty-time", new URL[0], FLOAT64));
            assertThrows(hla.rti1516e.exceptions.CouldNotOpenFDD.class,
                    () -> f.rti.createFederationExecution("empty-mim", new URL[0], (URL) null));
            assertThrows(hla.rti1516e.exceptions.CouldNotOpenFDD.class,
                    () -> f.rti.createFederationExecution("empty-all", new URL[0], null, FLOAT64));
            assertTrue(f.creates.isEmpty(), "Rejected input must not create remote execution state");
        }
    }

    private static Federation.FederationSummary summary(String name, String time) {
        return Federation.FederationSummary.newBuilder().setName(name).setFederationGeneration(7)
                .setLogicalTimeImplementationName(time).build();
    }

    private static <T> void reply(StreamObserver<T> response, T value) {
        response.onNext(value);
        response.onCompleted();
    }

    private static final class Fixture implements AutoCloseable {
        static final ByteString TOKEN = ByteString.copyFromUtf8("returned-membership-token");
        final List<Federation.CreateFederationRequest> creates = new CopyOnWriteArrayList<>();
        final List<Federation.ResignFederationRequest> resigns = new CopyOnWriteArrayList<>();
        final List<FederationExecutionInformationSet> reports = new ArrayList<>();
        final AtomicInteger joins = new AtomicInteger();
        final AtomicInteger subscriptions = new AtomicInteger();
        final AtomicInteger destroys = new AtomicInteger();
        volatile List<Federation.FederationSummary> summaries = List.of(summary("f", FLOAT64));
        volatile String joinTime = FLOAT64;
        volatile String createTime = FLOAT64;
        volatile boolean rejectResign;
        volatile boolean rejectCreate;
        volatile byte[] resignToken;
        final Server server;
        final RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();

        Fixture() throws Exception {
            FederationServiceGrpc.FederationServiceImplBase federationService = new FederationServiceGrpc.FederationServiceImplBase() {
                @Override public void listFederations(Federation.ListFederationsRequest request,
                        StreamObserver<Federation.ListFederationsResponse> response) {
                    reply(response, Federation.ListFederationsResponse.newBuilder().addAllFederations(summaries).build());
                }
                @Override public void createFederation(Federation.CreateFederationRequest request,
                        StreamObserver<Federation.CreateFederationResponse> response) {
                    creates.add(request);
                    if (rejectCreate) {
                        Metadata trailers = new Metadata();
                        trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER),
                                "CouldNotCreateLogicalTimeFactory");
                        response.onError(Status.INVALID_ARGUMENT.asRuntimeException(trailers));
                    } else {
                        reply(response, Federation.CreateFederationResponse.newBuilder()
                                .setLogicalTimeImplementationName(createTime).build());
                    }
                }
                @Override public void destroyFederation(Federation.DestroyFederationRequest request,
                        StreamObserver<Common.Empty> response) {
                    destroys.incrementAndGet();
                    reply(response, Common.Empty.getDefaultInstance());
                }
                @Override public void joinFederation(Federation.JoinFederationRequest request,
                        StreamObserver<Federation.JoinFederationResponse> response) {
                    reply(response, Federation.JoinFederationResponse.newBuilder()
                            .setFederateHandle(40 + joins.incrementAndGet()).setSessionToken(TOKEN)
                            .setTransportMode(Federation.TransportMode.TRANSPORT_MODE_GRPC)
                            .setLogicalTimeImplementationName(joinTime).build());
                }
                @Override public void resignFederation(Federation.ResignFederationRequest request,
                        StreamObserver<Common.Empty> response) {
                    resigns.add(request);
                    if (rejectResign) response.onError(Status.UNAVAILABLE.asRuntimeException());
                    else reply(response, Common.Empty.getDefaultInstance());
                }
            };
            server = ServerBuilder.forPort(0).addService(ServerInterceptors.intercept(federationService, new ServerInterceptor() {
                @Override public <Q, S> ServerCall.Listener<Q> interceptCall(ServerCall<Q, S> call,
                        Metadata headers, ServerCallHandler<Q, S> next) {
                    if (call.getMethodDescriptor().getBareMethodName().equals("ResignFederation")) {
                        resignToken = headers.get(Metadata.Key.of("gorti-session-token-bin", Metadata.BINARY_BYTE_MARSHALLER));
                    }
                    return next.startCall(call, headers);
                }
            })).addService(new StreamServiceGrpc.StreamServiceImplBase() {
                @Override public void eventBatches(Stream.EventsRequest request,
                        StreamObserver<Stream.FederateEventBatch> response) {
                    subscriptions.incrementAndGet();
                    response.onNext(Stream.FederateEventBatch.newBuilder().setReady(true).build());
                }
            }).build().start();
            rti.connect(new NullFederateAmbassador() {
                @Override public void reportFederationExecutions(FederationExecutionInformationSet entries) {
                    reports.add(entries);
                }
            }, CallbackModel.HLA_EVOKED, "grpc://127.0.0.1:" + server.getPort());
        }

        @Override public void close() throws Exception {
            try {
                rejectResign = false;
                try { rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); }
                catch (FederateNotExecutionMember expected) { }
                rti.disconnect();
            } finally {
                server.shutdownNow();
                assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
            }
        }
    }
}
