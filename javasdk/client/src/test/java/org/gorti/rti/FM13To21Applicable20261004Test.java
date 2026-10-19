package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.RTIambassador;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.function.Executable;
import rti.v1.SavepointServiceGrpc;
import rti.v1.SyncServiceGrpc;

class FM13To21Applicable20261004Test {
    @Test void disconnectedSaveAndSynchronizationServicesFailBeforeTransport() {
        RTIambassador rti = new GortiRtiFactory().getRtiAmbassador();
        List<Executable> calls = List.of(
                () -> rti.synchronizationPointAchieved("ready"),
                () -> rti.requestFederationSave("save"),
                rti::federateSaveBegun, rti::federateSaveComplete,
                rti::federateSaveNotComplete, rti::abortFederationSave);
        for (Executable call : calls) assertThrows(NotConnected.class, call);
    }

    @Test void internalFailureFromEachSaveAndSynchronizationRpcIsCheckedAndDoesNotConsumeAnnouncement() throws Exception {
        String name = InProcessServerBuilder.generateName();
        AtomicInteger requests = new AtomicInteger();
        ServerInterceptor failure = new ServerInterceptor() {
            @Override public <Q, R> ServerCall.Listener<Q> interceptCall(
                    ServerCall<Q, R> call, Metadata headers, ServerCallHandler<Q, R> next) {
                requests.incrementAndGet();
                call.close(Status.INTERNAL.withDescription("injected coordinator failure"), new Metadata());
                return new ServerCall.Listener<Q>() { };
            }
        };
        Server server = InProcessServerBuilder.forName(name).directExecutor().intercept(failure)
                .addService(new SavepointServiceGrpc.SavepointServiceImplBase() { })
                .addService(new SyncServiceGrpc.SyncServiceImplBase() { }).build().start();
        ManagedChannel channel = InProcessChannelBuilder.forName(name).directExecutor().build();
        try {
            ServiceContext context = new ServiceContext() {
                @Override public void requireMember() { }
                @Override public Channel channel() { return channel; }
                @Override public String federation() { return "fed"; }
                @Override public long federate() { return 1; }
                @Override public long generation() { return 1; }
                @Override public boolean regulating() { return false; }
                @Override public double logicalTime() { return 0; }
            };
            SaveRestoreServices saves = new SaveRestoreServices(context, callback -> fail("fabricated callback"));
            SynchronizationServices sync = new SynchronizationServices(context);
            sync.announced("ready");
            List<Executable> calls = List.of(
                    () -> sync.synchronizationPointAchieved("ready"),
                    () -> saves.requestFederationSave("save"),
                    saves::federateSaveBegun, saves::federateSaveComplete,
                    saves::federateSaveNotComplete, saves::abortFederationSave);
            for (Executable call : calls) {
                int before = requests.get();
                RTIinternalError actual = assertThrows(RTIinternalError.class, call);
                assertTrue(actual.getMessage().contains("injected coordinator failure"));
                assertEquals(before + 1, requests.get(), "failure must come from the selected RPC");
            }
            assertTrue(sync.checkpoint().contains("ready"), "failed achievement consumed announcement");
        } finally {
            channel.shutdownNow();
            server.shutdownNow();
            assertTrue(channel.awaitTermination(5, java.util.concurrent.TimeUnit.SECONDS));
            assertTrue(server.awaitTermination(5, java.util.concurrent.TimeUnit.SECONDS));
        }
    }
}
