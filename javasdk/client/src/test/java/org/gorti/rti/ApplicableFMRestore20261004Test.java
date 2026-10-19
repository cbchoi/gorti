package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.RTIambassador;
import hla.rti1516e.exceptions.NotConnected;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import io.grpc.Channel;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.Status;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.StreamObserver;
import java.util.ArrayDeque;
import java.util.List;
import java.util.Queue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import rti.v1.Common;
import rti.v1.Savepoint;
import rti.v1.SavepointServiceGrpc;

class ApplicableFMRestore20261004Test {
    @Test void disconnectedStandardRestoreServicesDoNotConnectOrAdmitState() throws Exception {
        RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        assertThrows(NotConnected.class, rti::queryFederationSaveStatus);
        assertThrows(NotConnected.class, () -> rti.requestFederationRestore("saved"));
        assertThrows(NotConnected.class, rti::federateRestoreComplete);
        assertThrows(NotConnected.class, rti::federateRestoreNotComplete);
        assertThrows(NotConnected.class, rti::abortFederationRestore);
        assertThrows(NotConnected.class, rti::queryFederationRestoreStatus);
        assertFalse(rti.evokeCallback(0));
        assertDoesNotThrow(rti::disconnect);
        assertThrows(NotConnected.class, rti::queryFederationSaveStatus);
    }

    @FunctionalInterface interface Call { void invoke(SaveRestoreServices services) throws RTIexception; }

    @Test void internalFailuresOfEveryRestoreServiceReachCallerWithoutCallbacks() throws Exception {
        AtomicInteger calls = new AtomicInteger();
        String endpoint = InProcessServerBuilder.generateName();
        Server server = InProcessServerBuilder.forName(endpoint).directExecutor()
                .addService(new SavepointServiceGrpc.SavepointServiceImplBase() {
                    private <T> void reject(StreamObserver<T> output) {
                        calls.incrementAndGet();
                        output.onError(Status.INTERNAL.withDescription("injected restore persistence failure").asRuntimeException());
                    }
                    @Override public void querySaveState(Savepoint.QuerySaveStateRequest r, StreamObserver<Savepoint.QuerySaveStateResponse> o) { reject(o); }
                    @Override public void requestFederationRestore(Savepoint.RequestFederationRestoreRequest r, StreamObserver<Common.Empty> o) { reject(o); }
                    @Override public void federateRestoreComplete(Savepoint.FederateRestoreResponseRequest r, StreamObserver<Common.Empty> o) { reject(o); }
                    @Override public void federateRestoreNotComplete(Savepoint.FederateRestoreResponseRequest r, StreamObserver<Common.Empty> o) { reject(o); }
                    @Override public void abortFederationRestore(Savepoint.AbortFederationRestoreRequest r, StreamObserver<Common.Empty> o) { reject(o); }
                    @Override public void queryRestoreState(Savepoint.QueryRestoreStateRequest r, StreamObserver<Savepoint.QueryRestoreStateResponse> o) { reject(o); }
                }).build().start();
        ManagedChannel channel = InProcessChannelBuilder.forName(endpoint).directExecutor().build();
        Queue<CallbackSink.Invocation> callbacks = new ArrayDeque<>();
        ServiceContext context = new ServiceContext() {
            @Override public void requireMember() { }
            @Override public Channel channel() { return channel; }
            @Override public String federation() { return "restore"; }
            @Override public long federate() { return 7; }
            @Override public long generation() { return 1; }
            @Override public boolean regulating() { return false; }
            @Override public double logicalTime() { return 0; }
        };
        try {
            SaveRestoreServices services = new SaveRestoreServices(context, callbacks::add);
            List<Call> operations = List.of(SaveRestoreServices::queryFederationSaveStatus,
                    s -> s.requestFederationRestore("saved"), SaveRestoreServices::federateRestoreComplete,
                    SaveRestoreServices::federateRestoreNotComplete, SaveRestoreServices::abortFederationRestore,
                    SaveRestoreServices::queryFederationRestoreStatus);
            for (int i = 0; i < operations.size(); i++) {
                Call call = operations.get(i);
                RTIinternalError error = assertThrows(RTIinternalError.class, () -> call.invoke(services));
                assertTrue(error.getMessage().contains("injected restore persistence failure"));
                assertEquals(i + 1, calls.get(), "Each failure must come from the matching RPC");
                assertTrue(callbacks.isEmpty(), "A failed service cannot fabricate an acknowledgment or status callback");
            }
        } finally {
            channel.shutdownNow();
            server.shutdownNow();
            assertTrue(channel.awaitTermination(5, TimeUnit.SECONDS));
            assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
        }
    }
}
