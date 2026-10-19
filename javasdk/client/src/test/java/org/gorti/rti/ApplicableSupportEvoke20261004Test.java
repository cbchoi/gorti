package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.NullFederateAmbassador;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.locks.LockSupport;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.AfterEach;

class ApplicableSupportEvoke20261004Test {
    private io.grpc.Server server;
    private AmbassadorServices services;

    @BeforeEach
    void connectFixture() throws Exception {
        server = io.grpc.ServerBuilder.forPort(0).addService(
                new rti.v1.FederationServiceGrpc.FederationServiceImplBase() {
                    @Override public void listFederations(rti.v1.Federation.ListFederationsRequest request,
                            io.grpc.stub.StreamObserver<rti.v1.Federation.ListFederationsResponse> response) {
                        response.onNext(rti.v1.Federation.ListFederationsResponse.getDefaultInstance());
                        response.onCompleted();
                    }
                }).build().start();
        services = new AmbassadorServices();
        services.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, "grpc://127.0.0.1:" + server.getPort());
    }

    @AfterEach
    void closeFixture() throws Exception {
        try { if (services != null) services.disconnect(); }
        finally { if (server != null) { server.shutdownNow(); assertTrue(server.awaitTermination(2, TimeUnit.SECONDS)); } }
    }

    @Test @Timeout(5)
    void multipleWaitsForMinimumAndStopsBacklogAtMaximum() throws Exception {
            long start = System.nanoTime();
            assertFalse(services.evokeMultipleCallbacks(0.020, 0.060));
            long elapsed = System.nanoTime() - start;
            assertTrue(elapsed >= TimeUnit.MILLISECONDS.toNanos(19), "empty queue returned before millisecond minimum");
            AtomicInteger delivered = new AtomicInteger();
            Method enqueue = AmbassadorServices.class.getDeclaredMethod("enqueueFederateCallback", CallbackSink.Invocation.class, long.class, long.class);
            enqueue.setAccessible(true);
            Field epoch = AmbassadorServices.class.getDeclaredField("epoch"); epoch.setAccessible(true);
            Field callbacks = AmbassadorServices.class.getDeclaredField("callbackEpoch"); callbacks.setAccessible(true);
            for (int i = 0; i < 1000; i++) {
                enqueue.invoke(services, (CallbackSink.Invocation) target -> {
                    delivered.incrementAndGet();
                    LockSupport.parkNanos(TimeUnit.MILLISECONDS.toNanos(2));
                }, epoch.getLong(services), callbacks.getLong(services));
            }
            start = System.nanoTime();
            assertTrue(services.evokeMultipleCallbacks(0.020, 0.045), "continuous backlog must remain pending");
            elapsed = System.nanoTime() - start;
            assertTrue(delivered.get() > 1 && delivered.get() < 1000, "must process multiple callbacks without draining continuous backlog");
            assertTrue(elapsed >= TimeUnit.MILLISECONDS.toNanos(40), "stopped at minimum despite available callbacks");
            assertTrue(elapsed < TimeUnit.MILLISECONDS.toNanos(500), "ignored maximum duration");
    }
}
