package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.exceptions.AlreadyConnected;
import hla.rti1516e.exceptions.ConnectionFailed;
import hla.rti1516e.exceptions.CouldNotOpenFDD;
import hla.rti1516e.exceptions.CouldNotOpenMIM;
import hla.rti1516e.exceptions.FederateAlreadyExecutionMember;
import hla.rti1516e.exceptions.FederateIsExecutionMember;
import hla.rti1516e.exceptions.FederateNotExecutionMember;
import hla.rti1516e.exceptions.NotConnected;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.io.TempDir;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Stream;
import rti.v1.StreamServiceGrpc;

@Timeout(20)
class FMEarlyOracle20261004Test {
    @TempDir Path directory;

    @Test void connectRejectsSecondConnectionAndLeavesAnUnjoinedUsableSession() throws Exception {
        try (Fixture f = new Fixture()) {
            f.connect();
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
            int calls = f.lists.get();
            assertThrows(AlreadyConnected.class, f::connect);
            assertEquals(calls, f.lists.get(), "Second connect must not replace the session");
            f.rti.listFederationExecutions();
            assertEquals(calls + 1, f.lists.get());
            f.rti.disconnect();
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertEquals(calls + 1, f.lists.get());
            f.connect();
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
        }
    }

    @Test void failedHandshakeLeavesNoConnectionAndSameAmbassadorCanRetry() throws Exception {
        try (Fixture f = new Fixture()) {
            f.rejectHandshake = true;
            assertThrows(ConnectionFailed.class, f::connect);
            assertEquals(1, f.lists.get());
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertThrows(NotConnected.class, () -> f.rti.joinFederationExecution("type", "f"));
            assertEquals(0, f.joins.get());
            f.rejectHandshake = false;
            f.connect();
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
            f.join();
            assertNotNull(f.rti.getTimeFactory());
            assertEquals(1, f.joins.get());
        }
    }

    @Test void disconnectedEarlyServicesRejectBeforeAnyRpc() throws Exception {
        try (Fixture f = new Fixture()) {
            assertThrows(NotConnected.class, () -> f.rti.destroyFederationExecution("f"));
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertThrows(NotConnected.class,
                    () -> f.rti.resignFederationExecution(ResignAction.NO_ACTION));
            assertThrows(NotConnected.class,
                    () -> f.rti.registerFederationSynchronizationPoint("p", new byte[]{1}));
            assertEquals(0, f.lists.get());
            assertEquals(0, f.resigns.get());
            assertEquals(0, f.destroys.get());
            f.connect();
            f.join();
            assertNotNull(f.rti.getTimeFactory());
        }
    }

    @Test void joinedDisconnectAndSecondJoinPreserveOriginalMembership() throws Exception {
        try (Fixture f = new Fixture()) {
            f.connect();
            f.join();
            Object factory = f.rti.getTimeFactory();
            assertThrows(FederateIsExecutionMember.class, f.rti::disconnect);
            assertThrows(FederateAlreadyExecutionMember.class,
                    () -> f.rti.joinFederationExecution("replacement", "type", "f"));
            assertSame(factory, f.rti.getTimeFactory());
            assertEquals(1, f.joins.get());
            assertEquals(0, f.resigns.get());
            f.rti.resignFederationExecution(ResignAction.NO_ACTION);
            assertEquals(1, f.resigns.get());
            assertEquals(41, f.lastResigned);
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
            f.rti.disconnect();
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
        }
    }

    @Test void missingCreateAndJoinModulesHaveTypedErrorsWithoutAdmission() throws Exception {
        URL missing = directory.resolve("missing.xml").toUri().toURL();
        URL valid = Files.writeString(directory.resolve("valid.xml"), "<objectModel/>").toUri().toURL();
        try (Fixture f = new Fixture()) {
            f.connect();
            assertThrows(CouldNotOpenFDD.class,
                    () -> f.rti.createFederationExecution("bad-fom", new URL[]{missing}));
            assertThrows(CouldNotOpenMIM.class,
                    () -> f.rti.createFederationExecution("bad-mim", new URL[]{valid}, missing));
            assertThrows(CouldNotOpenFDD.class,
                    () -> f.rti.joinFederationExecution("candidate", "type", "f", new URL[]{missing}));
            assertEquals(0, f.creates.get());
            assertEquals(0, f.joins.get());
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
            f.join();
            assertNotNull(f.rti.getTimeFactory());
            assertEquals(1, f.joins.get());
        }
    }

    private static <T> void reply(StreamObserver<T> observer, T value) {
        observer.onNext(value);
        observer.onCompleted();
    }

    private static final class Fixture implements AutoCloseable {
        final AtomicInteger lists = new AtomicInteger();
        final AtomicInteger joins = new AtomicInteger();
        final AtomicInteger creates = new AtomicInteger();
        final AtomicInteger resigns = new AtomicInteger();
        final AtomicInteger destroys = new AtomicInteger();
        final RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        final Server server;
        volatile boolean rejectHandshake;
        volatile long lastResigned;

        Fixture() throws Exception {
            server = ServerBuilder.forPort(0)
                    .addService(new FederationServiceGrpc.FederationServiceImplBase() {
                        @Override public void listFederations(Federation.ListFederationsRequest request,
                                StreamObserver<Federation.ListFederationsResponse> response) {
                            lists.incrementAndGet();
                            if (rejectHandshake) {
                                response.onError(Status.UNAVAILABLE.withDescription("handshake unavailable").asRuntimeException());
                                return;
                            }
                            reply(response, Federation.ListFederationsResponse.newBuilder().addFederations(
                                    Federation.FederationSummary.newBuilder().setName("f")
                                            .setLogicalTimeImplementationName("HLAfloat64Time")).build());
                        }
                        @Override public void joinFederation(Federation.JoinFederationRequest request,
                                StreamObserver<Federation.JoinFederationResponse> response) {
                            joins.incrementAndGet();
                            reply(response, Federation.JoinFederationResponse.newBuilder().setFederateHandle(41)
                                    .setLogicalTimeImplementationName("HLAfloat64Time")
                                    .setSessionToken(ByteString.copyFromUtf8("fm-early-member"))
                                    .setTransportMode(Federation.TransportMode.TRANSPORT_MODE_GRPC).build());
                        }
                        @Override public void createFederation(Federation.CreateFederationRequest request,
                                StreamObserver<Federation.CreateFederationResponse> response) {
                            creates.incrementAndGet();
                            reply(response, Federation.CreateFederationResponse.newBuilder()
                                    .setLogicalTimeImplementationName("HLAfloat64Time").build());
                        }
                        @Override public void resignFederation(Federation.ResignFederationRequest request,
                                StreamObserver<Common.Empty> response) {
                            resigns.incrementAndGet();
                            lastResigned = request.getFederateHandle();
                            reply(response, Common.Empty.getDefaultInstance());
                        }
                        @Override public void destroyFederation(Federation.DestroyFederationRequest request,
                                StreamObserver<Common.Empty> response) {
                            destroys.incrementAndGet();
                            reply(response, Common.Empty.getDefaultInstance());
                        }
                    }).addService(new StreamServiceGrpc.StreamServiceImplBase() {
                        @Override public void eventBatches(Stream.EventsRequest request,
                                StreamObserver<Stream.FederateEventBatch> response) {
                            response.onNext(Stream.FederateEventBatch.newBuilder().setReady(true).build());
                        }
                    }).build().start();
        }

        void connect() throws Exception {
            rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED,
                    "grpc://127.0.0.1:" + server.getPort());
        }

        void join() throws Exception {
            rti.joinFederationExecution("member", "type", "f");
        }

        @Override public void close() throws Exception {
            try {
                try {
                    rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                } catch (NotConnected | FederateNotExecutionMember expected) {
                    // Failed connection or unjoined fixture.
                }
                rti.disconnect();
            } finally {
                server.shutdownNow();
                assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
            }
        }
    }
}
