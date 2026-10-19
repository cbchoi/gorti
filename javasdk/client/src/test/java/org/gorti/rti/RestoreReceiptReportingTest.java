package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.exceptions.FederateInternalError;
import hla.rti1516e.exceptions.RTIexception;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;
import org.gorti.rti.transport.WireSession;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import rti.v1.Common;
import rti.v1.Stream;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

/** Isolates deliver's reporting finally block; live coverage exercises public restore APIs. */
@Timeout(20)
class RestoreReceiptReportingTest {
    @Test
    void retriesOnlyAnnouncedRestoreHandleForMembershipRejections() throws Exception {
        for (Status rejection : List.of(Status.FAILED_PRECONDITION, Status.PERMISSION_DENIED)) {
            try (Fixture f = new Fixture(rejection, false, false)) {
                assertDoesNotThrow(() -> f.deliver(f.initiation(7)));
                assertEquals(List.of(44L, 7L, 44L, 7L), f.reportedHandles());
                assertEquals(List.of(true, true, false, false), f.reports.stream()
                        .map(Support.ReportCallbackInvocationRequest::getInvocationEntry).collect(Collectors.toList()));
                assertEquals(1, f.invocations);
                for (Support.ReportCallbackInvocationRequest report : f.reports) {
                    assertTrue(report.getSuccess());
                    assertEquals("", report.getException());
                    assertEquals(f.receipt, report.getCallbackReceipt());
                }
                assertEquals(44L, field("handle").getLong(f.services),
                        "Reporting must not apply SDK membership before federationRestored");
            }
        }
    }

    @Test
    void earlierParticipantReportsBeforeRestoreCommitWithoutTryingSavedHandle() throws Exception {
        try (Fixture f = new Fixture(null, false, false)) {
            assertDoesNotThrow(() -> f.deliver(f.initiation(7)));
            assertEquals(List.of(44L, 44L), f.reportedHandles());
        }
    }

    @Test
    void doesNotMaskRejectedSavedReceiptOrRetryOrdinaryCallbacks() throws Exception {
        try (Fixture f = new Fixture(Status.FAILED_PRECONDITION, true, false)) {
            assertThrows(RTIexception.class, () -> f.deliver(f.initiation(7)));
            assertEquals(List.of(44L, 7L), f.reportedHandles());
            f.reports.clear();
            Stream.FederateEvent ordinary = Stream.FederateEvent.newBuilder().setCallbackReceipt(f.receipt)
                    .setGrant(Stream.TimeAdvanceGrant.newBuilder()).build();
            assertThrows(RTIexception.class, () -> f.deliver(ordinary));
            assertEquals(List.of(44L), f.reportedHandles());
        }
    }

    @Test
    void doesNotRetryUnrelatedTransportFailureOrUnchangedHandle() throws Exception {
        try (Fixture f = new Fixture(Status.INTERNAL, false, false)) {
            assertThrows(RTIexception.class, () -> f.deliver(f.initiation(7)));
            assertEquals(List.of(44L), f.reportedHandles());
        }
        try (Fixture f = new Fixture(Status.FAILED_PRECONDITION, false, false)) {
            assertThrows(RTIexception.class, () -> f.deliver(f.initiation(44)));
            assertEquals(List.of(44L), f.reportedHandles());
        }
    }

    @Test
    void reportsActualCallbackFailureAcrossRebindingWithoutInventingSuccess() throws Exception {
        try (Fixture f = new Fixture(Status.FAILED_PRECONDITION, false, true)) {
            FederateInternalError failure = assertThrows(FederateInternalError.class, () -> f.deliver(f.initiation(7)));
            assertEquals("application callback failed", failure.getMessage());
            assertEquals(0, failure.getSuppressed().length);
            assertEquals(List.of(44L, 7L, 44L, 7L), f.reportedHandles());
            for (Support.ReportCallbackInvocationRequest report : f.reports) {
                if (report.getInvocationEntry()) {
                    assertTrue(report.getSuccess());
                    assertEquals("", report.getException());
                } else {
                    assertFalse(report.getSuccess());
                    assertEquals("FederateInternalError", report.getException());
                }
            }
        }
    }

    private static Field field(String name) throws Exception {
        Field field = AmbassadorServices.class.getDeclaredField(name);
        field.setAccessible(true);
        return field;
    }

    private static final class Fixture implements AutoCloseable {
        final AmbassadorServices services = new AmbassadorServices();
        final ByteString receipt = ByteString.copyFrom(new byte[32]);
        final List<Support.ReportCallbackInvocationRequest> reports = new CopyOnWriteArrayList<>();
        final Server server;
        final WireSession session;
        int invocations;

        Fixture(Status rejection, boolean rejectSaved, boolean failCallback) throws Exception {
            server = ServerBuilder.forPort(0).addService(new SupportServiceGrpc.SupportServiceImplBase() {
                @Override public void reportCallbackInvocation(Support.ReportCallbackInvocationRequest request,
                        StreamObserver<Common.Empty> response) {
                    reports.add(request);
                    if (rejection != null && (request.getFederateHandle() == 44 || rejectSaved)) {
                        response.onError(rejection.withDescription("test receipt rejection").asRuntimeException());
                    } else {
                        response.onNext(Common.Empty.getDefaultInstance());
                        response.onCompleted();
                    }
                }
            }).build().start();
            session = new WireSession("127.0.0.1:" + server.getPort(), true);
            session.setMembership("receipt", 44, 1, ByteString.copyFromUtf8("same-authenticated-session"));
            field("session").set(services, session);
            field("federation").set(services, "receipt");
            field("federationGeneration").setLong(services, 1);
            field("callbackEntrySupported").setBoolean(services, true);
            field("handle").setLong(services, 44);
            field("ambassador").set(services, new NullFederateAmbassador() {
                @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) throws FederateInternalError {
                    invocations++;
                    if (failCallback) throw new FederateInternalError("application callback failed");
                }
            });
        }

        Stream.FederateEvent initiation(long restoredHandle) throws Exception {
            RuntimeCheckpoint checkpoint = new RuntimeCheckpoint(services.exchange.checkpoint(), services.ddm.checkpoint(),
                    services.synchronization.checkpoint(), false, false, false, false, false, 0);
            return Stream.FederateEvent.newBuilder().setCallbackReceipt(receipt)
                    .setRestoreInitiate(Stream.InitiateFederateRestore.newBuilder().setLabel("saved")
                            .setFederateName("member").setFederateHandle(restoredHandle)
                            .setClientState(ByteString.copyFrom(checkpoint.encode("receipt", 1, restoredHandle, "saved")))).build();
        }

        void deliver(Stream.FederateEvent event) throws Throwable {
            Method deliver = AmbassadorServices.class.getDeclaredMethod("deliver", Stream.FederateEvent.class, long.class);
            deliver.setAccessible(true);
            try { deliver.invoke(services, event, 1L); }
            catch (InvocationTargetException failure) { throw failure.getCause(); }
        }

        List<Long> reportedHandles() {
            return reports.stream().map(Support.ReportCallbackInvocationRequest::getFederateHandle).collect(Collectors.toList());
        }

        @Override public void close() throws Exception {
            session.close();
            server.shutdownNow();
            assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
        }
    }
}
