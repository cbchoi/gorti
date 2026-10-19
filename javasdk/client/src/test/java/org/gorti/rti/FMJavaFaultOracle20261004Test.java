package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederationExecutionInformationSet;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.exceptions.*;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.Field;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.gorti.rti.transport.WireSession;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.io.TempDir;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Stream;
import rti.v1.StreamServiceGrpc;

@Timeout(25)
class FMJavaFaultOracle20261004Test {
    @TempDir Path directory;

    @Test void suppliedModuleUrlsAndBytesAreExactInCreateAndJoinRequests() throws Exception {
        Path first = Files.createDirectories(directory.resolve("first branch")).resolve("model %.xml");
        Path second = Files.createDirectories(directory.resolve("second branch")).resolve("model %.xml");
        byte[] xml = "<objectModel/>".getBytes(StandardCharsets.UTF_8);
        URL one = Files.write(first, xml).toUri().toURL();
        URL two = Files.write(second, xml).toUri().toURL();
        URL mim = Files.write(directory.resolve("selected MIM.xml"), xml).toUri().toURL();
        try (Fixture f = new Fixture()) {
            f.connect();
            f.rti.createFederationExecution("f", new URL[]{one, two}, mim);
            assertNotNull(f.created);
            assertEquals(2, f.created.getFomModulesCount());
            assertModule(f.created.getFomModules(0), one, xml);
            assertModule(f.created.getFomModules(1), two, xml);
            assertModule(f.created.getMimModule(), mim, xml);
            f.rti.joinFederationExecution("member", "type", "f", new URL[]{two, one});
            assertNotNull(f.joined);
            assertEquals(2, f.joined.getAdditionalFomModulesCount());
            assertModule(f.joined.getAdditionalFomModules(0), two, xml);
            assertModule(f.joined.getAdditionalFomModules(1), one, xml);
        }
    }

    @Test void connectInternalFailureIsDistinctAndDoesNotCommitConnection() throws Exception {
        try (Fixture f = new Fixture()) {
            f.listFailure = Status.INTERNAL.withDescription("injected federation catalog read failure");
            RTIinternalError error = assertThrows(RTIinternalError.class, f::connect);
            assertWireFailure(error, Status.Code.INTERNAL, "catalog read failure");
            assertEquals(1, f.lists.get());
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertEquals(1, f.lists.get(), "Failed connect must not admit subsequent service traffic");
            assertNull(field(services(f.rti), "session"));
            f.listFailure = null;
            f.connect();
            f.rti.listFederationExecutions();
            assertFalse(f.rti.evokeCallback(0));
            assertEquals(List.of("f"), f.callbacks.reports);
            assertFalse(f.rti.evokeCallback(0));
        }
    }

    @Test void connectUnavailableRemainsConnectionFailedNotInternalError() throws Exception {
        try (Fixture f = new Fixture()) {
            f.listFailure = Status.UNAVAILABLE.withDescription("injected connection admission outage");
            ConnectionFailed error = assertThrows(ConnectionFailed.class, f::connect);
            assertWireFailure(error, Status.Code.UNAVAILABLE, "admission outage");
            assertEquals(1, f.lists.get());
            assertNull(field(services(f.rti), "session"));
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertEquals(1, f.lists.get());
            f.listFailure = null;
            f.connect();
            assertThrows(FederateNotExecutionMember.class, f.rti::getTimeFactory);
        }
    }

    @Test void listInternalFailurePreservesConnectionAndAlreadyQueuedReport() throws Exception {
        try (Fixture f = new Fixture()) {
            f.connect();
            f.reportName = "before";
            f.rti.listFederationExecutions();
            Object originalSession = field(services(f.rti), "session");
            f.listFailure = Status.INTERNAL.withDescription("injected enumeration snapshot failure");
            RTIinternalError error = assertThrows(RTIinternalError.class, f.rti::listFederationExecutions);
            assertWireFailure(error, Status.Code.INTERNAL, "snapshot failure");
            assertSame(originalSession, field(services(f.rti), "session"));
            assertEquals(3, f.lists.get());
            assertFalse(f.rti.evokeCallback(0));
            assertEquals(List.of("before"), f.callbacks.reports);
            assertFalse(f.rti.evokeCallback(0), "Failed enumeration must not enqueue a success callback");
            f.listFailure = null;
            f.reportName = "after";
            f.rti.listFederationExecutions();
            assertFalse(f.rti.evokeCallback(0));
            assertEquals(List.of("before", "after"), f.callbacks.reports);
            assertFalse(f.rti.evokeCallback(0));
            assertSame(originalSession, field(services(f.rti), "session"));
        }
    }

    @Test void disconnectInterruptedTransportTeardownIsTypedAndRetiresConnection() throws Exception {
        try (Fixture f = new Fixture()) {
            f.connect();
            f.rti.listFederationExecutions();
            WireSession old = (WireSession) field(services(f.rti), "session");
            ManagedChannel channel = (ManagedChannel) field(old, "transport");
            RTIinternalError error;
            try {
                // Inject at the real blocking teardown boundary, not at an RPC or mapper.
                Thread.currentThread().interrupt();
                error = assertThrows(RTIinternalError.class, f.rti::disconnect);
                assertTrue(Thread.currentThread().isInterrupted(), "Shutdown must retain interruption");
            } finally {
                Thread.interrupted();
            }
            assertInstanceOf(IllegalStateException.class, error.getCause());
            assertInstanceOf(InterruptedException.class, error.getCause().getCause());
            assertTrue(channel.isShutdown());
            assertTrue(channel.awaitTermination(5, TimeUnit.SECONDS));
            assertNull(field(services(f.rti), "session"));
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertEquals(2, f.lists.get());
            f.rti.disconnect();
            f.connect();
            assertFalse(f.rti.evokeCallback(0), "Old queued report must not survive failed teardown");
            assertTrue(f.callbacks.reports.isEmpty());
            f.rti.listFederationExecutions();
            assertFalse(f.rti.evokeCallback(0));
            assertEquals(List.of("f"), f.callbacks.reports);
        }
    }

    @Test void disconnectedDisconnectDocumentsCanonicalSignatureAndObservedIdempotence() throws Exception {
        Set<Class<?>> declared = Set.of(RTIambassador.class.getMethod("disconnect").getExceptionTypes());
        assertEquals(Set.of(FederateIsExecutionMember.class, CallNotAllowedFromWithinCallback.class,
                RTIinternalError.class), declared);
        assertFalse(declared.contains(NotConnected.class));
        assertTrue(RTIexception.class.isAssignableFrom(NotConnected.class));
        assertFalse(RuntimeException.class.isAssignableFrom(NotConnected.class));
        assertEquals(declared, Set.of(GortiRtiAmbassador.class.getMethod("disconnect").getExceptionTypes()));
        try (Fixture f = new Fixture()) {
            assertDoesNotThrow(f.rti::disconnect);
            assertDoesNotThrow(f.rti::disconnect);
            assertEquals(0, f.lists.get());
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            f.connect();
            f.rti.disconnect();
            assertDoesNotThrow(f.rti::disconnect);
            assertThrows(NotConnected.class, f.rti::listFederationExecutions);
            assertEquals(1, f.lists.get());
            f.connect();
        }
    }

    private static void assertModule(Common.FOMModule module, URL url, byte[] xml) {
        assertEquals(url.toExternalForm(), module.getPath());
        assertArrayEquals(xml, module.getXml().toByteArray());
    }

    private static void assertWireFailure(Exception error, Status.Code code, String detail) {
        StatusRuntimeException wire = assertInstanceOf(StatusRuntimeException.class, error.getCause());
        assertEquals(code, wire.getStatus().getCode());
        assertTrue(wire.getStatus().getDescription().contains(detail));
    }

    private static Object field(Object owner, String name) throws Exception {
        Field field = owner.getClass().getDeclaredField(name);
        field.setAccessible(true);
        return field.get(owner);
    }

    private static Object services(RTIambassador rti) throws Exception { return field(rti, "services"); }

    private static <T> void reply(StreamObserver<T> observer, T value) {
        observer.onNext(value);
        observer.onCompleted();
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final List<String> reports = new ArrayList<>();
        @Override public void reportFederationExecutions(FederationExecutionInformationSet executions) {
            assertEquals(1, executions.size());
            reports.add(executions.iterator().next().federationExecutionName);
        }
    }

    private static final class Fixture implements AutoCloseable {
        final RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        final Callbacks callbacks = new Callbacks();
        final AtomicInteger lists = new AtomicInteger();
        final Server server;
        volatile Status listFailure;
        volatile String reportName = "f";
        volatile Federation.CreateFederationRequest created;
        volatile Federation.JoinFederationRequest joined;

        Fixture() throws Exception {
            server = ServerBuilder.forPort(0).addService(new FederationServiceGrpc.FederationServiceImplBase() {
                @Override public void listFederations(Federation.ListFederationsRequest request,
                        StreamObserver<Federation.ListFederationsResponse> response) {
                    lists.incrementAndGet();
                    if (listFailure != null) {
                        response.onError(listFailure.asRuntimeException());
                        return;
                    }
                    reply(response, Federation.ListFederationsResponse.newBuilder().addFederations(
                            Federation.FederationSummary.newBuilder().setName(reportName)
                                    .setLogicalTimeImplementationName("HLAfloat64Time")).build());
                }
                @Override public void createFederation(Federation.CreateFederationRequest request,
                        StreamObserver<Federation.CreateFederationResponse> response) {
                    created = request;
                    reply(response, Federation.CreateFederationResponse.newBuilder()
                            .setLogicalTimeImplementationName("HLAfloat64Time").build());
                }
                @Override public void joinFederation(Federation.JoinFederationRequest request,
                        StreamObserver<Federation.JoinFederationResponse> response) {
                    joined = request;
                    reply(response, Federation.JoinFederationResponse.newBuilder().setFederateHandle(73)
                            .setSessionToken(ByteString.copyFromUtf8("fm-java-fault"))
                            .setLogicalTimeImplementationName("HLAfloat64Time")
                            .setTransportMode(Federation.TransportMode.TRANSPORT_MODE_GRPC).build());
                }
                @Override public void resignFederation(Federation.ResignFederationRequest request,
                        StreamObserver<Common.Empty> response) { reply(response, Common.Empty.getDefaultInstance()); }
            }).addService(new StreamServiceGrpc.StreamServiceImplBase() {
                @Override public void eventBatches(Stream.EventsRequest request,
                        StreamObserver<Stream.FederateEventBatch> response) {
                    response.onNext(Stream.FederateEventBatch.newBuilder().setReady(true).build());
                }
            }).build().start();
        }

        void connect() throws Exception {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, "grpc://127.0.0.1:" + server.getPort());
        }

        @Override public void close() throws Exception {
            try {
                try { rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); }
                catch (NotConnected | FederateNotExecutionMember expected) { }
                rti.disconnect();
            } finally {
                server.shutdownNow();
                assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
            }
        }
    }
}
