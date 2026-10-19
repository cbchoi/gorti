package org.gorti.rti.transport;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import io.grpc.Channel;
import io.grpc.Context;
import io.grpc.Metadata;
import io.grpc.Server;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerInterceptor;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.ServerCallStreamObserver;
import io.grpc.stub.StreamObserver;
import java.time.Duration;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Stream;
import rti.v1.StreamServiceGrpc;

class WireSessionTest {
    private static final ByteString TOKEN = ByteString.copyFrom(new byte[] {0, -1, 3, 8});
    private static final Metadata.Key<byte[]> TOKEN_HEADER =
            Metadata.Key.of("gorti-session-token-bin", Metadata.BINARY_BYTE_MARSHALLER);
    private static final Duration SECOND = Duration.ofSeconds(1);
    private final Probe probe = new Probe();
    private final List<Metadata> headers = new CopyOnWriteArrayList<>();
    private final List<Throwable> failures = new CopyOnWriteArrayList<>();
    private final List<Stream.FederateEvent> events = new CopyOnWriteArrayList<>();
    private final AtomicReference<Federation.DestroyFederationRequest> destroyed = new AtomicReference<>();
    private volatile boolean hangUnary;
    private volatile long observedDeadlineNanos;
    private Server server;
    private WireSession session;

    private WireSession open(Duration rpcTimeout, Duration closeTimeout) throws Exception {
        String name = InProcessServerBuilder.generateName();
        server = InProcessServerBuilder.forName(name).directExecutor()
                .intercept(new ServerInterceptor() {
                    @Override
                    public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(
                            ServerCall<ReqT, RespT> call, Metadata incoming,
                            ServerCallHandler<ReqT, RespT> next) {
                        Metadata copy = new Metadata();
                        copy.merge(incoming);
                        headers.add(copy);
                        return next.startCall(call, incoming);
                    }
                })
                .addService(probe)
                .addService(new FederationServiceGrpc.FederationServiceImplBase() {
                    @Override
                    public void listFederations(Federation.ListFederationsRequest request,
                            StreamObserver<Federation.ListFederationsResponse> response) {
                        observedDeadlineNanos = Context.current().getDeadline().timeRemaining(TimeUnit.NANOSECONDS);
                        if (!hangUnary) {
                            response.onNext(Federation.ListFederationsResponse.getDefaultInstance());
                            response.onCompleted();
                        }
                    }

                    @Override
                    public void destroyFederation(Federation.DestroyFederationRequest request,
                            StreamObserver<Common.Empty> response) {
                        destroyed.set(request);
                        response.onNext(Common.Empty.getDefaultInstance());
                        response.onCompleted();
                    }
                }).build().start();
        session = new WireSession(InProcessChannelBuilder.forName(name).build(), rpcTimeout, closeTimeout);
        return session;
    }

    private void joined() throws Exception {
        open(SECOND, SECOND).setMembership("fed", 7, 19, TOKEN);
    }

    @Test
    void restoredIdentityRetainsItsAuthenticatedStream() throws Exception {
        joined();
        probe.onOpen = probe::ready;
        CompletableFuture<Stream.FederateEvent> delivered = new CompletableFuture<>();
        EventSubscription subscription = session.subscribe(delivered::complete, failures::add);
        session.rebindMembership(12);
        assertFalse(subscription.isTerminated());
        assertEquals(1, probe.cancelled.getCount());
        FederationServiceGrpc.newBlockingStub(session.channel()).destroyFederation(
                Federation.DestroyFederationRequest.newBuilder().setFederationName("fed").build());
        assertEquals(19, destroyed.get().getExpectedFederationGeneration());
        assertArrayEquals(TOKEN.toByteArray(), headers.get(headers.size() - 1).get(TOKEN_HEADER));
        probe.batch(grant(1));
        assertEquals(1, delivered.get(1, TimeUnit.SECONDS).getSeq());
        assertThrows(IllegalArgumentException.class, () -> session.rebindMembership(0));
        assertFalse(subscription.isTerminated());
    }

    @AfterEach
    void cleanup() throws Exception {
        try {
            if (session != null) {
                session.close();
            }
        } finally {
            if (server != null) {
                server.shutdownNow();
                assertTrue(server.awaitTermination(2, TimeUnit.SECONDS));
            }
        }
    }

    @Test
    void authenticatedChannelsCaptureTokenAndGenerationIncludingUnsignedZero() throws Exception {
        open(SECOND, SECOND);
        Channel first = session.setMembership("fed", 7, 0, TOKEN);
        ByteString secondToken = ByteString.copyFromUtf8("replacement");
        Channel second = session.setMembership("fed", 8, -1L, secondToken);
        Federation.DestroyFederationRequest request = Federation.DestroyFederationRequest.newBuilder()
                .setFederationName("fed").build();
        FederationServiceGrpc.newBlockingStub(first).destroyFederation(request);
        assertArrayEquals(TOKEN.toByteArray(), headers.get(0).get(TOKEN_HEADER));
        assertTrue(destroyed.get().hasExpectedFederationGeneration());
        assertEquals(0, destroyed.get().getExpectedFederationGeneration());
        assertFalse(request.hasExpectedFederationGeneration());
        FederationServiceGrpc.newBlockingStub(second).destroyFederation(request);
        assertArrayEquals(secondToken.toByteArray(), headers.get(1).get(TOKEN_HEADER));
        assertEquals(-1L, destroyed.get().getExpectedFederationGeneration());
        FederationServiceGrpc.newBlockingStub(session.unauthenticatedChannel())
                .listFederations(Federation.ListFederationsRequest.getDefaultInstance());
        assertNull(headers.get(2).get(TOKEN_HEADER));
        assertTrue(headers.stream().noneMatch(h -> h.keys().stream().anyMatch(k -> k.contains("generation"))));
    }

    @Test
    void sessionRejectsAbsentMembershipAndUseAfterClose() throws Exception {
        open(SECOND, SECOND);
        assertThrows(IllegalStateException.class, session::channel);
        assertThrows(IllegalStateException.class, () -> session.subscribe(events::add, failures::add));
        session.setMembership("fed", 7, 0, TOKEN);
        session.clearMembership();
        assertThrows(IllegalStateException.class, session::channel);
        session.close();
        session.close();
        assertThrows(IllegalStateException.class, session::unauthenticatedChannel);
        assertThrows(IllegalStateException.class, () -> session.setMembership("fed", 7, 0, TOKEN));
    }

    @Test
    void unaryCallsHaveBoundedDeadlinesAndRetainShorterCallerDeadline() throws Exception {
        open(Duration.ofMillis(300), SECOND);
        FederationServiceGrpc.newBlockingStub(session.unauthenticatedChannel())
                .listFederations(Federation.ListFederationsRequest.getDefaultInstance());
        assertTrue(observedDeadlineNanos > 0 && observedDeadlineNanos <= TimeUnit.MILLISECONDS.toNanos(300));
        hangUnary = true;
        StatusRuntimeException failure = assertThrows(StatusRuntimeException.class,
                () -> FederationServiceGrpc.newBlockingStub(session.unauthenticatedChannel())
                        .withDeadlineAfter(50, TimeUnit.MILLISECONDS)
                        .listFederations(Federation.ListFederationsRequest.getDefaultInstance()));
        assertEquals(Status.Code.DEADLINE_EXCEEDED, failure.getStatus().getCode());
        assertTrue(observedDeadlineNanos <= TimeUnit.MILLISECONDS.toNanos(50));
    }

    @Test
    void explicitReadyHandshakeFencesMembershipAndForwardsOnlyActualEventsInOrder() throws Exception {
        joined();
        CompletableFuture<EventSubscription> opening = CompletableFuture.supplyAsync(
                () -> session.subscribe(events::add, failures::add));
        assertTrue(probe.subscribed.await(1, TimeUnit.SECONDS));
        assertFalse(opening.isDone());
        assertEquals("fed", probe.request.getFederationName());
        assertEquals(7, probe.request.getFederateHandle());
        assertTrue(probe.request.hasExpectedFederationGeneration());
        assertEquals(19, probe.request.getExpectedFederationGeneration());
        assertEquals(Common.WireVersion.WIRE_VERSION_V1, probe.request.getWireVersion());
        assertArrayEquals(TOKEN.toByteArray(), headers.get(0).get(TOKEN_HEADER));
        probe.ready();
        EventSubscription subscription = opening.get(1, TimeUnit.SECONDS);
        assertTrue(events.isEmpty());
        CountDownLatch delivered = new CountDownLatch(1);
        probe.batch(Stream.FederateEvent.newBuilder().setSeq(2)
                .setTimeRegulationEnabled(Stream.TimeRegulationEnabled.newBuilder().setLogicalTime(4)).build(),
                Stream.FederateEvent.newBuilder().setSeq(3)
                        .setTimeConstrainedEnabled(Stream.TimeConstrainedEnabled.newBuilder().setLogicalTime(5)).build());
        // A terminal notification follows all messages on the same stream.
        subscription.completion().whenComplete((ignored, failure) -> delivered.countDown());
        probe.error(Status.UNAVAILABLE);
        assertTrue(delivered.await(1, TimeUnit.SECONDS));
        assertEquals(List.of(2L, 3L), List.of(events.get(0).getSeq(), events.get(1).getSeq()));
        assertEquals(4, events.get(0).getTimeRegulationEnabled().getLogicalTime());
        assertEquals(5, events.get(1).getTimeConstrainedEnabled().getLogicalTime());
    }

    @Test
    void errorBeforeReadyFailsSubscribeWithServerStatus() throws Exception {
        joined();
        probe.onOpen = () -> probe.error(Status.PERMISSION_DENIED);
        StatusRuntimeException failure = assertThrows(StatusRuntimeException.class,
                () -> session.subscribe(events::add, failures::add));
        assertEquals(Status.Code.PERMISSION_DENIED, failure.getStatus().getCode());
        assertTrue(events.isEmpty());
        assertEquals(1, failures.size());
    }

    @Test
    void errorsAfterReadyAreObservableAndPreserveTrailers() throws Exception {
        joined();
        probe.onOpen = probe::ready;
        CompletableFuture<Throwable> reported = new CompletableFuture<>();
        EventSubscription subscription = session.subscribe(events::add, reported::complete);
        Metadata trailers = new Metadata();
        Metadata.Key<String> marker = Metadata.Key.of("failure-kind", Metadata.ASCII_STRING_MARSHALLER);
        trailers.put(marker, "late");
        probe.response.onError(Status.ABORTED.asRuntimeException(trailers));
        Throwable error = reported.get(1, TimeUnit.SECONDS);
        assertEquals(Status.Code.ABORTED, Status.fromThrowable(error).getCode());
        assertEquals("late", Status.trailersFromThrowable(error).get(marker));
        assertThrows(ExecutionException.class, () -> subscription.completion().toCompletableFuture().get());
    }

    @ParameterizedTest
    @ValueSource(booleans = {false, true})
    void malformedReadyBatchesFailBeforeDispatch(boolean readyWithData) throws Exception {
        joined();
        probe.onOpen = () -> probe.response.onNext(Stream.FederateEventBatch.newBuilder()
                .setReady(readyWithData).addEvents(grant(1)).build());
        StatusRuntimeException failure = assertThrows(StatusRuntimeException.class,
                () -> session.subscribe(events::add, failures::add));
        assertEquals(Status.Code.DATA_LOSS, failure.getStatus().getCode());
        assertTrue(events.isEmpty());
        assertTrue(probe.cancelled.await(1, TimeUnit.SECONDS));
    }

    @Test
    void duplicateReadyAndUnexpectedCleanEofAreErrors() throws Exception {
        joined();
        probe.onOpen = probe::ready;
        CompletableFuture<Throwable> reported = new CompletableFuture<>();
        EventSubscription subscription = session.subscribe(events::add, reported::complete);
        probe.ready();
        assertEquals(Status.Code.DATA_LOSS, Status.fromThrowable(reported.get(1, TimeUnit.SECONDS)).getCode());
        subscription.close();
        CompletableFuture<Throwable> secondReported = new CompletableFuture<>();
        session.subscribe(events::add, secondReported::complete);
        probe.response.onCompleted();
        assertEquals(Status.Code.UNAVAILABLE,
                Status.fromThrowable(secondReported.get(1, TimeUnit.SECONDS)).getCode());
    }

    @Test
    void readinessTimeoutCancelsServerSubscription() throws Exception {
        joined();
        StatusRuntimeException failure = assertThrows(StatusRuntimeException.class,
                () -> session.subscribe(events::add, failures::add, Duration.ofMillis(80)));
        assertEquals(Status.Code.DEADLINE_EXCEEDED, failure.getStatus().getCode());
        assertTrue(probe.cancelled.await(1, TimeUnit.SECONDS));
        assertTrue(failures.isEmpty());
    }

    @Test
    void readyStreamDoesNotInheritUnaryLifetimeDeadline() throws Exception {
        open(Duration.ofMillis(100), SECOND).setMembership("fed", 7, 19, TOKEN);
        probe.onOpen = probe::ready;
        EventSubscription subscription = session.subscribe(events::add, failures::add, SECOND);
        assertFalse(probe.cancelled.await(250, TimeUnit.MILLISECONDS));
        assertFalse(subscription.isTerminated());
        subscription.close();
        assertTrue(subscription.isTerminated());
        assertTrue(failures.isEmpty());
    }

    @Test
    void clearMembershipCancelsAndWaitsForStreamTeardown() throws Exception {
        joined();
        probe.onOpen = probe::ready;
        EventSubscription subscription = session.subscribe(events::add, failures::add);
        session.clearMembership();
        assertTrue(subscription.isTerminated());
        assertTrue(probe.cancelled.await(1, TimeUnit.SECONDS));
        subscription.completion().toCompletableFuture().get(1, TimeUnit.SECONDS);
        assertThrows(IllegalStateException.class, session::channel);
        assertTrue(failures.isEmpty());
    }

    @Test
    void replacingMembershipCancelsPreviousCallbacks() throws Exception {
        joined();
        probe.onOpen = probe::ready;
        EventSubscription old = session.subscribe(events::add, failures::add);
        session.setMembership("other", 12, 20, ByteString.copyFromUtf8("new-token"));
        assertTrue(old.isTerminated());
        session.subscribe(events::add, failures::add);
        assertEquals("other", probe.request.getFederationName());
        assertEquals(12, probe.request.getFederateHandle());
        assertEquals(20, probe.request.getExpectedFederationGeneration());
        assertThrows(IllegalStateException.class, () -> session.subscribe(events::add, failures::add));
    }

    @Test
    void closeFromInsideCallbackDoesNotDeadlockOrDispatchRemainingBatch() throws Exception {
        joined();
        probe.onOpen = probe::ready;
        CountDownLatch closed = new CountDownLatch(1);
        session.subscribe(event -> {
            events.add(event);
            session.close();
            closed.countDown();
        }, failures::add);
        probe.batch(grant(1), grant(2));
        assertTrue(closed.await(1, TimeUnit.SECONDS));
        assertTrue(probe.cancelled.await(1, TimeUnit.SECONDS));
        assertEquals(1, events.size());
        assertTrue(failures.isEmpty());
    }

    @ParameterizedTest
    @ValueSource(booleans = {false, true})
    void listenerFailureCancelsStreamAndIsReportedOnce(boolean assertionError) throws Exception {
        joined();
        probe.onOpen = probe::ready;
        Throwable expected = assertionError ? new AssertionError("callback failure")
                : new IllegalArgumentException("callback failure");
        CompletableFuture<Throwable> reported = new CompletableFuture<>();
        EventSubscription subscription = session.subscribe(event -> {
            if (expected instanceof Error) {
                throw (Error) expected;
            }
            throw (RuntimeException) expected;
        }, error -> {
            failures.add(error);
            reported.complete(error);
        });
        probe.batch(grant(1), grant(2));
        assertSame(expected, reported.get(1, TimeUnit.SECONDS));
        subscription.close();
        assertEquals(1, failures.size());
        assertTrue(probe.cancelled.await(1, TimeUnit.SECONDS));
    }

    @Test
    void blockedApplicationCallbackCannotMakeCloseHangForever() throws Exception {
        open(SECOND, Duration.ofMillis(80)).setMembership("fed", 7, 19, TOKEN);
        probe.onOpen = probe::ready;
        CountDownLatch entered = new CountDownLatch(1);
        CountDownLatch release = new CountDownLatch(1);
        EventSubscription subscription = session.subscribe(event -> {
            entered.countDown();
            try {
                release.await(2, TimeUnit.SECONDS);
            } catch (InterruptedException interrupted) {
                Thread.currentThread().interrupt();
            }
        }, failures::add);
        probe.batch(grant(1));
        assertTrue(entered.await(1, TimeUnit.SECONDS));
        try {
            assertTimeout(Duration.ofSeconds(1), () -> assertThrows(IllegalStateException.class, subscription::close));
        } finally {
            release.countDown();
        }
        subscription.close();
        assertTrue(subscription.isTerminated());
    }

    private static Stream.FederateEvent grant(long seq) {
        return Stream.FederateEvent.newBuilder().setSeq(seq)
                .setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(seq)).build();
    }

    private static final class Probe extends StreamServiceGrpc.StreamServiceImplBase {
        final CountDownLatch subscribed = new CountDownLatch(1);
        final CountDownLatch cancelled = new CountDownLatch(1);
        volatile Stream.EventsRequest request;
        volatile StreamObserver<Stream.FederateEventBatch> response;
        volatile Runnable onOpen = () -> {};

        @Override
        public void eventBatches(Stream.EventsRequest request, StreamObserver<Stream.FederateEventBatch> response) {
            this.request = request;
            this.response = response;
            ((ServerCallStreamObserver<Stream.FederateEventBatch>) response).setOnCancelHandler(cancelled::countDown);
            subscribed.countDown();
            onOpen.run();
        }

        void ready() {
            response.onNext(Stream.FederateEventBatch.newBuilder().setReady(true).build());
        }

        void batch(Stream.FederateEvent... events) {
            response.onNext(Stream.FederateEventBatch.newBuilder().addAllEvents(List.of(events)).build());
        }

        void error(Status status) {
            response.onError(status.asRuntimeException());
        }
    }
}
