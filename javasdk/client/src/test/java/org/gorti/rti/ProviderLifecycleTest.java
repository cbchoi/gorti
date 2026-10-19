package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.ObjectClassHandle;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.FederateRestoreStatus;
import hla.rti1516e.FederationExecutionInformationSet;
import hla.rti1516e.RestoreFailureReason;
import hla.rti1516e.exceptions.FederateNotExecutionMember;
import hla.rti1516e.exceptions.InTimeAdvancingState;
import hla.rti1516e.exceptions.NotConnected;
import hla.rti1516e.exceptions.ObjectInstanceNotKnown;
import hla.rti1516e.exceptions.RTIinternalError;
import hla.rti1516e.exceptions.RequestForTimeRegulationPending;
import hla.rti1516e.exceptions.RestoreInProgress;
import hla.rti1516e.exceptions.TimeRegulationIsNotEnabled;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.stub.ServerCallStreamObserver;
import io.grpc.stub.StreamObserver;
import java.util.List;
import java.util.Map;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.Executors;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Stream;
import rti.v1.Savepoint;
import rti.v1.SavepointServiceGrpc;
import rti.v1.StreamServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;
import rti.v1.Time;
import rti.v1.TimeServiceGrpc;

/** Client lifecycle contracts against a real loopback gRPC transport. */
@Timeout(30)
class ProviderLifecycleTest {
    private static final String FEDERATION = "java-lifecycle";
    private static final ByteString RECEIPT = ByteString.copyFrom(new byte[32]);
    private final AtomicLong joinedHandles = new AtomicLong();
    private final AtomicInteger advanceCalls = new AtomicInteger();
    private final AtomicInteger enableCalls = new AtomicInteger();
    private final AtomicInteger disableCalls = new AtomicInteger();
    private final Map<Long, Subscription> streams = new ConcurrentHashMap<>();
    private final BlockingQueue<Support.ReportCallbackInvocationRequest> receipts = new LinkedBlockingQueue<>();
    private final BlockingQueue<StreamObserver<Savepoint.QueryRestoreStateResponse>> restoreQueries = new LinkedBlockingQueue<>();
    private final Callbacks callbacks = new Callbacks();
    private volatile boolean includeReceipt;
    private Server server;
    private RTIambassador rti;
    private HLAfloat64TimeFactory time;
    private String endpoint;

    @BeforeEach
    void startLoopbackServerAndJoin() throws Exception {
        server = ServerBuilder.forPort(0)
                .addService(new FederationServiceGrpc.FederationServiceImplBase() {
                    @Override
                    public void listFederations(Federation.ListFederationsRequest request,
                            StreamObserver<Federation.ListFederationsResponse> response) {
                        reply(response, Federation.ListFederationsResponse.newBuilder()
                                .addFederations(Federation.FederationSummary.newBuilder()
                                        .setName(FEDERATION).setFederationGeneration(7)).build());
                    }

                    @Override
                    public void joinFederation(Federation.JoinFederationRequest request,
                            StreamObserver<Federation.JoinFederationResponse> response) {
                        long handle = joinedHandles.incrementAndGet();
                        reply(response, Federation.JoinFederationResponse.newBuilder()
                                .setFederateHandle(handle)
                                .setCallbackInvocationEntrySupported(true)
                                .setSessionToken(ByteString.copyFromUtf8("session-" + handle))
                                .setTransportMode(Federation.TransportMode.TRANSPORT_MODE_GRPC).build());
                    }

                    @Override
                    public void resignFederation(Federation.ResignFederationRequest request,
                            StreamObserver<Common.Empty> response) {
                        reply(response, Common.Empty.getDefaultInstance());
                    }
                })
                .addService(new StreamServiceGrpc.StreamServiceImplBase() {
                    @Override
                    public void eventBatches(Stream.EventsRequest request,
                            StreamObserver<Stream.FederateEventBatch> response) {
                        Subscription subscription = new Subscription(response);
                        ((ServerCallStreamObserver<Stream.FederateEventBatch>) response)
                                .setOnCancelHandler(subscription.cancelled::countDown);
                        streams.put(request.getFederateHandle(), subscription);
                        subscription.send(Stream.FederateEventBatch.newBuilder().setReady(true).build());
                    }
                })
                .addService(new TimeServiceGrpc.TimeServiceImplBase() {
                    @Override
                    public void timeAdvanceRequest(Time.TARRequest request, StreamObserver<Common.Empty> response) {
                        advanceCalls.incrementAndGet();
                        Stream.FederateEvent.Builder event = Stream.FederateEvent.newBuilder()
                                .setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(request.getLogicalTime()));
                        if (includeReceipt) event.setCallbackReceipt(RECEIPT);
                        stream(request.getFederateHandle()).event(event.build());
                        reply(response, Common.Empty.getDefaultInstance());
                    }

                    @Override
                    public void enableTimeRegulation(Time.EnableRegulationRequest request,
                            StreamObserver<Common.Empty> response) {
                        enableCalls.incrementAndGet();
                        stream(request.getFederateHandle()).event(Stream.FederateEvent.newBuilder()
                                .setTimeRegulationEnabled(Stream.TimeRegulationEnabled.newBuilder()
                                        .setLogicalTime(0)).build());
                        reply(response, Common.Empty.getDefaultInstance());
                    }

                    @Override
                    public void disableTimeRegulation(Time.DisableRegulationRequest request,
                            StreamObserver<Common.Empty> response) {
                        disableCalls.incrementAndGet();
                        reply(response, Common.Empty.getDefaultInstance());
                    }
                })
                .addService(new SavepointServiceGrpc.SavepointServiceImplBase() {
                    @Override public void queryRestoreState(Savepoint.QueryRestoreStateRequest request,
                            StreamObserver<Savepoint.QueryRestoreStateResponse> response) {
                        restoreQueries.add(response);
                    }
                    @Override public void abortFederationRestore(Savepoint.AbortFederationRestoreRequest request,
                            StreamObserver<Common.Empty> response) {
                        stream(joinedHandles.get()).event(Stream.FederateEvent.newBuilder().setRestoreFailed(
                                Stream.FederationNotRestored.newBuilder().setLabel("snapshot").setReason(5)).build());
                        reply(response, Common.Empty.getDefaultInstance());
                    }
                })
                .addService(new SupportServiceGrpc.SupportServiceImplBase() {
                    @Override
                    public void reportCallbackInvocation(Support.ReportCallbackInvocationRequest request,
                            StreamObserver<Common.Empty> response) {
                        receipts.add(request);
                        reply(response, Common.Empty.getDefaultInstance());
                    }
                }).build().start();
        endpoint = "grpc://127.0.0.1:" + server.getPort();
        rti = new GortiRtiFactory().getRtiAmbassador();
        rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint);
        rti.joinFederationExecution("participant", "lifecycle-test", FEDERATION);
        time = (HLAfloat64TimeFactory) rti.getTimeFactory();
    }

    @AfterEach
    void closeClientAndServer() throws Exception {
        try {
            if (rti != null) {
                try {
                    rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                } catch (NotConnected | FederateNotExecutionMember expected) {
                    // A terminal callback can already have cleared the membership.
                } finally {
                    rti.disconnect();
                }
            }
        } finally {
            if (server != null) {
                server.shutdownNow();
                assertTrue(server.awaitTermination(5, TimeUnit.SECONDS), "Loopback server did not terminate");
            }
        }
    }

    @Test
    void secondAdvanceBeforeGrantEvocationIsRejectedWithoutAnotherRpc() throws Exception {
        Thread invokingThread = Thread.currentThread();
        rti.timeAdvanceRequest(time.makeTime(1));
        assertEquals(0, callbacks.grants.get(), "RPC completion must not invoke an evoked callback");
        assertThrows(InTimeAdvancingState.class, () -> rti.timeAdvanceRequest(time.makeTime(2)));
        assertEquals(1, advanceCalls.get(), "The second advance must be rejected locally");
        evokeUntil(() -> callbacks.grants.get() == 1);
        rti.timeAdvanceRequest(time.makeTime(2));
        evokeUntil(() -> callbacks.grants.get() == 2);
        assertEquals(2, advanceCalls.get());
        assertThreads(invokingThread);
    }

    @Test
    void disableRegulationBeforeEnabledEvocationUsesDeclaredNotEnabledException() throws Exception {
        rti.enableTimeRegulation(time.makeInterval(0.5));
        assertEquals(0, callbacks.enabled.get());
        assertThrows(TimeRegulationIsNotEnabled.class, rti::disableTimeRegulation);
        assertEquals(0, disableCalls.get(), "Pending enable cannot be disabled remotely");
        evokeUntil(() -> callbacks.enabled.get() == 1);
        rti.disableTimeRegulation();
        assertEquals(1, disableCalls.get(), "Disable becomes valid after the callback");
    }

    @Test
    void duplicateEnableBeforeEnabledEvocationIsLocallyPending() throws Exception {
        rti.enableTimeRegulation(time.makeInterval(0.5));
        assertThrows(RequestForTimeRegulationPending.class,
                () -> rti.enableTimeRegulation(time.makeInterval(0.5)));
        assertEquals(1, enableCalls.get());
        assertEquals(0, callbacks.enabled.get());
        evokeUntil(() -> callbacks.enabled.get() == 1);
        assertThreads(Thread.currentThread());
    }

    @Test
    void terminalStreamFailureCanBeEvokedAndSameAmbassadorReconnected() throws Exception {
        Thread invokingThread = Thread.currentThread();
        stream(joinedHandles.get()).fail();
        evokeUntil(() -> callbacks.lost.get() == 1);
        assertThreads(invokingThread);
        assertDoesNotThrow(() -> rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint));
        assertDoesNotThrow(() -> rti.joinFederationExecution("participant-again", "lifecycle-test", FEDERATION));
        assertEquals(2, joinedHandles.get());
        time = (HLAfloat64TimeFactory) rti.getTimeFactory();
        rti.timeAdvanceRequest(time.makeTime(1));
        evokeUntil(() -> callbacks.grants.get() == 1);
        assertEquals(1, callbacks.lost.get(), "Old subscription failure must not leak into the new join");
        assertThreads(invokingThread);
    }

    @Test
    void disabledFullQueueStillSurfacesFailureAndEvokesConnectionLostOnCaller() throws Exception {
        Thread invokingThread = Thread.currentThread();
        rti.disableCallbacks();
        Subscription subscription = stream(joinedHandles.get());
        Stream.FederateEventBatch.Builder batch = Stream.FederateEventBatch.newBuilder();
        // Saturate transport admission; no callbacks are invoked while disabled.
        for (int i = 0; i < 16385; i++) {
            batch.addEvents(Stream.FederateEvent.newBuilder().setSeq(i + 1)
                    .setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(i + 1)));
        }
        subscription.send(batch.build());
        assertTrue(subscription.cancelled.await(10, TimeUnit.SECONDS), "Overflow must terminate the failed stream");
        assertEquals(0, callbacks.grants.get());
        assertEquals(0, callbacks.lost.get());
        assertThrows(RTIinternalError.class, () -> rti.evokeCallback(0),
                "Disabled callbacks must not hide terminal queue failure");
        rti.enableCallbacks();
        evokeUntil(() -> callbacks.lost.get() == 1);
        assertEquals(1, callbacks.lost.get());
        assertThreads(invokingThread);
    }

    @Test
    void assertionErrorInCallbackIsReportedAsFailedInvocation() throws Exception {
        includeReceipt = true;
        callbacks.throwFromGrant = true;
        rti.timeAdvanceRequest(time.makeTime(1));
        Throwable failure = assertThrows(Throwable.class, () -> evokeUntil(() -> callbacks.grants.get() == 1));
        assertTrue(failure instanceof AssertionError || failure instanceof RTIinternalError,
                "Callback failure should propagate or be translated to RTIinternalError");
        assertEquals(1, callbacks.grants.get(), "The failing callback must actually have run");
        Support.ReportCallbackInvocationRequest report = receipts.poll(5, TimeUnit.SECONDS);
        assertNotNull(report, "A supplied callback receipt must receive an invocation report");
        assertEquals(RECEIPT, report.getCallbackReceipt());
        assertTrue(report.getInvocationEntry(), "First report authenticates entry, not success of the body");
        assertTrue(report.getSuccess());
        assertEquals("", report.getException());
        report = receipts.poll(5, TimeUnit.SECONDS);
        assertNotNull(report, "Callback failure must receive a separate completion report");
        assertFalse(report.getInvocationEntry());
        assertEquals(RECEIPT, report.getCallbackReceipt());
        assertFalse(report.getSuccess(), "AssertionError is not a successful callback");
        assertFalse(report.getException().isEmpty());
        assertThreads(Thread.currentThread());
    }

    @Test
    void restoreDropsOldQueuedCallbacksButRetainsCheckpointBookkeepingAndNewEvents() throws Exception {
        Subscription stream = stream(joinedHandles.get());
        stream.event(discovery(1, "saved-object"));
        evokeUntil(() -> callbacks.discoveries.get() == 1);
        stream.event(Stream.FederateEvent.newBuilder().setSaveInitiate(
                Stream.InitiateFederateSave.newBuilder().setLabel("snapshot")).build());
        evokeUntil(() -> callbacks.saving.get() == 1);
        rti.listFederationExecutions();
        Stream.FederateEventBatch.Builder full = Stream.FederateEventBatch.newBuilder()
                .addEvents(Stream.FederateEvent.newBuilder().setSaveCompleted(
                        Stream.FederationSaved.newBuilder().setLabel("snapshot")))
                .addEvents(discovery(2, "discarded-object"))
                .addEvents(Stream.FederateEvent.newBuilder().setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(9)));
        for (int i = 4; i < 16384; i++) full.addEvents(Stream.FederateEvent.newBuilder()
                .setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(9)));
        full.addEvents(Stream.FederateEvent.newBuilder().setRestoreCompleted(Stream.FederationRestored.newBuilder()
                        .setLabel("snapshot").setFederateHandle(joinedHandles.get()).setCallbackEpoch(1).setNextRetractionHandle(10)))
                .addEvents(discovery(3, "new-object"));
        stream.send(full.build());
        awaitTransportEpoch(1);
        rti.disableCallbacks();
        assertThrows(RestoreInProgress.class, () -> rti.timeAdvanceRequest(time.makeTime(0.25)),
                "Requests must wait until the restored client state has been applied");
        assertEquals(0, advanceCalls.get());
        rti.enableCallbacks();
        evokeUntil(() -> callbacks.restored.get() == 1 && callbacks.discoveries.get() == 2
                && callbacks.federationReports.get() == 1);
        assertEquals(0, callbacks.saved.get(), "Old-epoch callbacks must not reach user code");
        assertEquals(0, callbacks.grants.get());
        assertNotNull(rti.getObjectInstanceHandle("saved-object"));
        assertNotNull(rti.getObjectInstanceHandle("new-object"));
        assertThrows(ObjectInstanceNotKnown.class, () -> rti.getObjectInstanceHandle("discarded-object"));
        assertEquals(1, stream.cancelled.getCount(), "Restore must not tear down the stream");
        rti.timeAdvanceRequest(time.makeTime(0.25));
        evokeUntil(() -> callbacks.grants.get() == 1);
    }

    @Test
    void restoreBegunBlocksOrdinaryServicesUntilFailureIsEvoked() throws Exception {
        stream(joinedHandles.get()).event(Stream.FederateEvent.newBuilder().setRestoreBegun(
                Stream.FederationRestoreBegun.newBuilder()).build());
        evokeUntil(() -> callbacks.restoreBegun.get() == 1);
        assertThrows(RestoreInProgress.class, () -> rti.timeAdvanceRequest(time.makeTime(1)));
        assertEquals(0, advanceCalls.get());
        rti.abortFederationRestore();
        assertThrows(RestoreInProgress.class, () -> rti.timeAdvanceRequest(time.makeTime(1)));
        evokeUntil(() -> callbacks.restoreFailed.get() == 1);
        rti.timeAdvanceRequest(time.makeTime(1));
        evokeUntil(() -> callbacks.grants.get() == 1);
    }

    @Test
    void invalidPersistedRuntimeStateCannotAcknowledgeRestoreAndCanBeAborted() throws Exception {
        Subscription stream = stream(joinedHandles.get());
        stream.event(Stream.FederateEvent.newBuilder().setRestoreBegun(Stream.FederationRestoreBegun.newBuilder()).build());
        evokeUntil(() -> callbacks.restoreBegun.get() == 1);
        stream.event(Stream.FederateEvent.newBuilder().setRestoreInitiate(Stream.InitiateFederateRestore.newBuilder()
                .setLabel("corrupt").setFederateHandle(joinedHandles.get()).setFederateName("one")
                .setClientState(ByteString.copyFromUtf8("corrupted-runtime"))).build());
        awaitQueuedCallbacks(1);
        assertThrows(RTIinternalError.class, () -> rti.evokeCallback(0));
        assertThrows(RTIinternalError.class, rti::federateRestoreComplete,
                "A failed runtime decode must never acknowledge restoration to the server");
        rti.abortFederationRestore();
        evokeUntil(() -> callbacks.restoreFailed.get() == 1);
        rti.timeAdvanceRequest(time.makeTime(1));
        evokeUntil(() -> callbacks.grants.get() == 1);
    }

    @Test
    void restoreStatusResponseKeepsItsRequestEpochAcrossDelayedCompletion() throws Exception {
        Subscription stream = stream(joinedHandles.get());
        stream.event(Stream.FederateEvent.newBuilder().setSaveInitiate(
                Stream.InitiateFederateSave.newBuilder().setLabel("snapshot")).build());
        evokeUntil(() -> callbacks.saving.get() == 1);
        stream.event(Stream.FederateEvent.newBuilder().setSaveCompleted(
                Stream.FederationSaved.newBuilder().setLabel("snapshot")).build());
        evokeUntil(() -> callbacks.saved.get() == 1);
        stream.event(Stream.FederateEvent.newBuilder().setRestoreBegun(
                Stream.FederationRestoreBegun.newBuilder()).build());
        evokeUntil(() -> callbacks.restoreBegun.get() == 1);
        java.util.concurrent.ExecutorService executor = Executors.newSingleThreadExecutor();
        try {
            java.util.concurrent.Future<?> query = executor.submit(() -> { rti.queryFederationRestoreStatus(); return null; });
            StreamObserver<Savepoint.QueryRestoreStateResponse> response = restoreQueries.poll(5, TimeUnit.SECONDS);
            assertNotNull(response);
            stream.event(Stream.FederateEvent.newBuilder().setRestoreCompleted(Stream.FederationRestored.newBuilder()
                    .setLabel("snapshot").setFederateHandle(joinedHandles.get()).setCallbackEpoch(1)).build());
            awaitTransportEpoch(1);
            reply(response, Savepoint.QueryRestoreStateResponse.getDefaultInstance());
            query.get(5, TimeUnit.SECONDS);
            evokeUntil(() -> callbacks.restored.get() == 1);
            rti.evokeMultipleCallbacks(0, 0.1);
            assertEquals(0, callbacks.restoreStatuses.get(), "Pre-restore status must not become a post-restore callback");
        } finally {
            executor.shutdownNow();
            assertTrue(executor.awaitTermination(5, TimeUnit.SECONDS));
        }
    }

    @Test
    void failedRestoreCannotClearALaterAdmittedRestore() throws Exception {
        Subscription stream = stream(joinedHandles.get());
        stream.event(Stream.FederateEvent.newBuilder().setRestoreBegun(Stream.FederationRestoreBegun.newBuilder()).build());
        evokeUntil(() -> callbacks.restoreBegun.get() == 1);
        stream.send(Stream.FederateEventBatch.newBuilder()
                .addEvents(Stream.FederateEvent.newBuilder().setRestoreFailed(
                        Stream.FederationNotRestored.newBuilder().setLabel("first").setReason(5)))
                .addEvents(Stream.FederateEvent.newBuilder().setRestoreBegun(Stream.FederationRestoreBegun.newBuilder())).build());
        // Waiting for both queue entries proves that the later begin was already admitted.
        awaitQueuedCallbacks(2);
        rti.evokeCallback(0);
        assertEquals(1, callbacks.restoreFailed.get());
        assertEquals(1, callbacks.restoreBegun.get());
        assertThrows(RestoreInProgress.class, () -> rti.timeAdvanceRequest(time.makeTime(1)));
        assertEquals(0, advanceCalls.get());
    }

    @Test
    void connectionScopedFederationReportSurvivesJoining() throws Exception {
        rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
        rti.listFederationExecutions();
        rti.joinFederationExecution("again", "lifecycle-test", FEDERATION);
        evokeUntil(() -> callbacks.federationReports.get() == 1);
    }

    private void awaitQueuedCallbacks(int expected) throws Exception {
        java.lang.reflect.Field serviceField = GortiRtiAmbassador.class.getDeclaredField("services");
        serviceField.setAccessible(true);
        Object service = serviceField.get(rti);
        java.lang.reflect.Field queueField = AmbassadorServices.class.getDeclaredField("callbacks");
        queueField.setAccessible(true);
        java.util.Queue<?> queue = (java.util.Queue<?>) queueField.get(service);
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5);
        while (queue.size() < expected && System.nanoTime() < deadline) Thread.sleep(1);
        assertTrue(queue.size() >= expected);
    }

    private static Stream.FederateEvent discovery(long object, String name) {
        return Stream.FederateEvent.newBuilder().setDiscover(Stream.DiscoverObjectInstance.newBuilder()
                .setObjectHandle(object).setObjectClassHandle(10).setObjectName(name)).build();
    }

    private void awaitTransportEpoch(long expected) throws Exception {
        java.lang.reflect.Field serviceField = GortiRtiAmbassador.class.getDeclaredField("services");
        serviceField.setAccessible(true);
        Object service = serviceField.get(rti);
        java.lang.reflect.Field epochField = AmbassadorServices.class.getDeclaredField("callbackEpoch");
        epochField.setAccessible(true);
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5);
        while (epochField.getLong(service) != expected && System.nanoTime() < deadline) Thread.sleep(1);
        assertEquals(expected, epochField.getLong(service));
    }

    private void evokeUntil(BooleanSupplier completed) throws Exception {
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
        while (!completed.getAsBoolean() && System.nanoTime() < deadline) {
            rti.evokeMultipleCallbacks(0.01, 0.05);
        }
        assertTrue(completed.getAsBoolean(), "Expected evoked callback was not delivered");
    }

    private void assertThreads(Thread expected) {
        assertFalse(callbacks.threads.isEmpty());
        for (Thread actual : callbacks.threads) assertSame(expected, actual, "HLA_EVOKED callback thread");
    }

    private Subscription stream(long handle) {
        Subscription subscription = streams.get(handle);
        if (subscription == null) throw new IllegalStateException("No ready stream for " + handle);
        return subscription;
    }

    private static <T> void reply(StreamObserver<T> response, T value) {
        response.onNext(value);
        response.onCompleted();
    }

    private static final class Subscription {
        final StreamObserver<Stream.FederateEventBatch> response;
        final CountDownLatch cancelled = new CountDownLatch(1);

        Subscription(StreamObserver<Stream.FederateEventBatch> response) { this.response = response; }
        synchronized void send(Stream.FederateEventBatch batch) { response.onNext(batch); }
        synchronized void event(Stream.FederateEvent event) {
            send(Stream.FederateEventBatch.newBuilder().addEvents(event).build());
        }
        synchronized void fail() { response.onError(Status.UNAVAILABLE.withDescription("test stream failure").asRuntimeException()); }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final AtomicInteger grants = new AtomicInteger();
        final AtomicInteger enabled = new AtomicInteger();
        final AtomicInteger lost = new AtomicInteger();
        final AtomicInteger discoveries = new AtomicInteger();
        final AtomicInteger saving = new AtomicInteger();
        final AtomicInteger saved = new AtomicInteger();
        final AtomicInteger restored = new AtomicInteger();
        final AtomicInteger restoreBegun = new AtomicInteger();
        final AtomicInteger restoreFailed = new AtomicInteger();
        final AtomicInteger restoreStatuses = new AtomicInteger();
        final AtomicInteger federationReports = new AtomicInteger();
        final List<Thread> threads = new CopyOnWriteArrayList<>();
        volatile boolean throwFromGrant;

        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { discoveries.incrementAndGet(); }
        @Override public void initiateFederateSave(String label) { saving.incrementAndGet(); }
        @Override public void federationSaved() { saved.incrementAndGet(); }
        @Override public void federationRestored() { restored.incrementAndGet(); }
        @Override public void federationRestoreBegun() { restoreBegun.incrementAndGet(); }
        @Override public void federationNotRestored(RestoreFailureReason reason) { restoreFailed.incrementAndGet(); }
        @Override public void federationRestoreStatusResponse(FederateRestoreStatus[] status) { restoreStatuses.incrementAndGet(); }
        @Override public void reportFederationExecutions(FederationExecutionInformationSet entries) { federationReports.incrementAndGet(); }

        @Override
        public void timeAdvanceGrant(LogicalTime logicalTime) {
            threads.add(Thread.currentThread());
            grants.incrementAndGet();
            if (throwFromGrant) throw new AssertionError("intentional callback failure");
        }

        @Override
        public void timeRegulationEnabled(LogicalTime logicalTime) {
            threads.add(Thread.currentThread());
            enabled.incrementAndGet();
        }

        @Override
        public void connectionLost(String description) {
            threads.add(Thread.currentThread());
            lost.incrementAndGet();
        }
    }
}
