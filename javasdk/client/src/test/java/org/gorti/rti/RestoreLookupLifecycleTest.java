package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.Status;
import io.grpc.stub.ServerCallStreamObserver;
import io.grpc.stub.StreamObserver;
import java.io.ByteArrayInputStream;
import java.io.DataInputStream;
import java.lang.reflect.Field;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.BooleanSupplier;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import rti.v1.Common;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.Savepoint;
import rti.v1.SavepointServiceGrpc;
import rti.v1.Stream;
import rti.v1.StreamServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;
import rti.v1.Time;
import rti.v1.TimeServiceGrpc;

@Timeout(30)
class RestoreLookupLifecycleTest {
    @Test void successResponseBeforeInitiateValidationCannotSendRpc() throws Exception {
        try (Fixture f = new Fixture()) {
            assertThrows(RestoreNotRequested.class, f.rti::federateRestoreComplete);
            f.stream.onNext(Stream.FederateEventBatch.newBuilder().addEvents(Stream.FederateEvent.newBuilder()
                    .setRestoreBegun(Stream.FederationRestoreBegun.getDefaultInstance())).build());
            await(f.rti, () -> f.events.begun == 1);
            assertThrows(RTIinternalError.class, f.rti::federateRestoreComplete);
            assertEquals(0, f.restoreCompletes.get());
            f.initiate(); f.rti.federateRestoreComplete();
            assertEquals(1, f.restoreCompletes.get());
        }
    }

    @Test void newRestoreBegunClearsPreviousValidatedReadiness() throws Exception {
        try (Fixture f = new Fixture()) {
            f.initiate();
            f.stream.onNext(Stream.FederateEventBatch.newBuilder().addEvents(Stream.FederateEvent.newBuilder()
                    .setRestoreBegun(Stream.FederationRestoreBegun.getDefaultInstance())).build());
            await(f.rti, () -> f.events.begun == 2);
            assertThrows(RTIinternalError.class, f.rti::federateRestoreComplete);
            assertEquals(0, f.restoreCompletes.get());
        }
    }

    @Test void explicitlyValidatedEmptyRuntimeCanCompleteButMalformedRuntimeCannot() throws Exception {
        try (Fixture f = new Fixture()) {
            DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
            f.privateState = new RuntimeCheckpoint(new ExchangeServices.State(new ExchangeKnowledge(), 0, 0), regions, Set.of(),
                    false, false, false, false, false, 0).encode("restore-lookups", 7, 41, "saved");
            f.initiate(); f.rti.federateRestoreComplete(); assertEquals(1, f.restoreCompletes.get());
        }
        try (Fixture f = new Fixture()) {
            f.privateState = new byte[]{1};
            assertThrows(RTIinternalError.class, f::initiate);
            assertThrows(RTIinternalError.class, f.rti::federateRestoreComplete);
            assertEquals(0, f.restoreCompletes.get());
        }
    }
    @Test void historicInitiateAndBegunNotificationsCannotInstallCheckpointOrOpenTransaction() throws Exception {
        try (Fixture f = new Fixture()) {
            f.initiate(); f.rti.federateRestoreComplete(); await(f.rti, () -> f.events.restored == 1);
            java.util.List<Stream.FederateEvent> controls = java.util.List.of(
                    Stream.FederateEvent.newBuilder().setSaveInitiate(Stream.InitiateFederateSave.newBuilder().setLabel("historic-save")).build(),
                    Stream.FederateEvent.newBuilder().setRestoreInitiate(Stream.InitiateFederateRestore.newBuilder().setLabel("historic-restore")
                            .setFederateHandle(99).setFederateName("other").setClientState(ByteString.copyFromUtf8("invalid-checkpoint"))).build(),
                    Stream.FederateEvent.newBuilder().setRestoreBegun(Stream.FederationRestoreBegun.getDefaultInstance()).build());
            Stream.FederateEventBatch.Builder batch = Stream.FederateEventBatch.newBuilder();
            int index = 1;
            for (Stream.FederateEvent body : controls) {
                byte[] receipt = new byte[32]; receipt[31] = (byte) index++;
                Stream.FederateEvent original = body.toBuilder().setCallbackReceipt(ByteString.copyFrom(receipt)).build();
                batch.addEvents(CallbackInvocationDeliveryTest.recovery(original, CallbackInvocationDelivery.identity(original), false));
            }
            f.stream.onNext(batch.build()); await(f.rti, () -> f.events.begun == 2);
            assertEquals(1, f.events.saving); assertEquals(2, f.events.initiated);
            assertEquals(6, f.reportedEntries.size());
            f.rti.timeAdvanceRequest(TimeValues.time(3)); assertEquals(1, f.advances.get());
            Field owner = GortiRtiAmbassador.class.getDeclaredField("services"); owner.setAccessible(true);
            Object services = owner.get(f.rti);
            Field label = AmbassadorServices.class.getDeclaredField("pendingSaveLabel"); label.setAccessible(true);
            assertNull(label.get(services));
        }
    }
    @Test void historicRestoredNotificationCannotRebindOrRestartLifecycle() throws Exception {
        try (Fixture f = new Fixture()) {
            f.initiate(); f.rti.federateRestoreComplete();
            await(f.rti, () -> f.events.restored == 1);
            Stream.FederateEvent original = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFrom(new byte[32]))
                    .setRestoreCompleted(Stream.FederationRestored.newBuilder().setLabel("historic")
                            .setFederateHandle(99).setCallbackEpoch(1)).build();
            Stream.FederateEvent recovered = CallbackInvocationDeliveryTest.recovery(original, CallbackInvocationDelivery.identity(original), false);
            f.stream.onNext(Stream.FederateEventBatch.newBuilder().addEvents(recovered).addEvents(recovered).build());
            await(f.rti, () -> f.events.restored == 2);
            f.rti.evokeMultipleCallbacks(0, 0.03);
            assertEquals(2, f.events.restored);
            assertEquals(java.util.List.of(41L, 41L), f.reportedHandles);
            assertEquals(java.util.List.of(true, false), f.reportedEntries);
            assertEquals(12, f.rti.getUpdateRateValue("fast"));
            assertEquals(0, f.wrongIdentity.get());
        }
    }
    @Test void v2JournalIsNeverReplayedAndOutcomeOnlyCannotMutateTimeOrInvokeBody() throws Exception {
        try (Fixture f = new Fixture()) {
            ExchangeKnowledge knowledge = new ExchangeKnowledge(); knowledge.discovered(10, 20, "saved");
            DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
            Stream.FederateEvent grant = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFrom(new byte[32]))
                    .setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(3)).build();
            String identity = CallbackInvocationDelivery.identity(grant);
            f.privateState = RuntimeJson.encode(new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 0), regions, Set.of(),
                    false, false, false, false, true, TimeValues.time(2), java.util.List.of(grant))
                    .withRecovery(java.util.List.of(new CallbackInvocationDelivery.Outcome(identity, true, ""))),
                    "restore-lookups", 7, (7L << 32) | 41, "saved");
            f.finalHandle = (7L << 32) | 41;
            f.initiate(); f.rti.federateRestoreComplete();
            await(f.rti, () -> f.events.restored == 1);
            Stream.FederateEvent recovered = CallbackInvocationDeliveryTest.recovery(grant, identity, true);
            f.stream.onNext(Stream.FederateEventBatch.newBuilder().addEvents(recovered).addEvents(recovered).build());
            await(f.rti, () -> f.reportedEntries.size() == 1);
            f.rti.evokeMultipleCallbacks(0, 0.03);
            assertEquals(java.util.List.of(false), f.reportedEntries);
            assertEquals(0, f.events.grants);
            Field owner = GortiRtiAmbassador.class.getDeclaredField("services"); owner.setAccessible(true);
            Object services = owner.get(f.rti);
            Field time = AmbassadorServices.class.getDeclaredField("logicalTime"); time.setAccessible(true);
            assertEquals(TimeValues.time(2), time.get(services));
        }
    }
    @Test
    void terminalReceiptEntryRetriesInstalledIdentityBeforeHydrationAndReportsOutcomeOnce() throws Exception {
        try (Fixture f = new Fixture()) {
            f.terminalReceipt = ByteString.copyFrom(new byte[32]);
            f.initiate(); f.rti.federateRestoreComplete();
            await(f.rti, () -> f.events.restored == 1);
            assertEquals(java.util.List.of(22L, 41L, 41L), f.reportedHandles);
            assertEquals(java.util.List.of(true, true, false), f.reportedEntries);
        }
    }
    @Test
    void failedRestoredBodyIsNotReinvokedAndDoesNotStrandSavedControls() throws Exception {
        try (Fixture f = new Fixture()) {
            ExchangeKnowledge knowledge = new ExchangeKnowledge(); knowledge.discovered(10, 20, "saved");
            DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
            Stream.FederateEvent grant = Stream.FederateEvent.newBuilder().setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(3)).build();
            f.privateState = new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 0), regions, Set.of(),
                    false, false, false, false, true, TimeValues.time(2), java.util.List.of(grant)).encode("restore-lookups", 7, 41, "saved");
            f.initiate(); f.events.failRestored = true; f.rti.federateRestoreComplete();
            awaitAdmittedRestore(f.rti, 1);
            assertThrows(RTIinternalError.class, () -> f.rti.evokeMultipleCallbacks(0.001, 0.02));
            await(f.rti, () -> f.events.grants == 1);
            assertEquals(1, f.events.restored);
            assertEquals(java.util.List.of("restored", "grant"), f.events.order);
        }
    }
    @Test
    void restoredReceiptlessControlsWaitForTerminalAndReceiptDataIsNotReplayedLocally() throws Exception {
        try (Fixture f = new Fixture()) {
            ExchangeKnowledge knowledge = new ExchangeKnowledge();
            knowledge.discovered(10, 20, "saved");
            DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
            Stream.FederateEvent grant = Stream.FederateEvent.newBuilder()
                    .setGrant(Stream.TimeAdvanceGrant.newBuilder().setLogicalTime(3)).build();
            Stream.FederateEvent data = discovery(19, "old-receipt-data").toBuilder()
                    .setCallbackReceipt(ByteString.copyFromUtf8("old-receipt")).build();
            f.privateState = new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 0), regions, Set.of(),
                    false, false, false, false, true, TimeValues.time(2), java.util.List.of(grant, data))
                    .encode("restore-lookups", 7, 41, "saved");
            f.initiate();
            f.rti.federateRestoreComplete();
            awaitAdmittedRestore(f.rti, 1);
            f.rti.disableCallbacks();
            assertEquals(Handles.objectInstance(10), f.rti.getObjectInstanceHandle("saved"));
            assertEquals(0, f.events.grants);
            assertEquals(0, f.events.restored);
            f.rti.enableCallbacks();
            await(f.rti, () -> f.events.restored == 1 && f.events.grants == 1 && f.events.discovered == 2);
            assertEquals(java.util.List.of("restored", "grant"), f.events.order);
            assertThrows(ObjectInstanceNotKnown.class, () -> f.rti.getObjectInstanceHandle("old-receipt-data"));
            f.rti.evokeMultipleCallbacks(0, 0.05);
            assertEquals(1, f.events.grants);
        }
    }

    @Test
    void translatedTargetHandleMayDifferFromOriginalInitiateHandle() throws Exception {
        try (Fixture f = new Fixture()) {
            f.announcedHandle = 8;
            f.initiate();
            f.rti.federateRestoreComplete();
            await(f.rti, () -> f.events.restored == 1);
            assertEquals(12, f.rti.getUpdateRateValue("fast"));
            assertEquals(0, f.wrongIdentity.get());
        }
    }

    @Test
    void legalLookupsHydrateRestoredIdentityWithoutInvokingCallbacksOrUnfencingMutations() throws Exception {
        try (Fixture f = new Fixture()) {
            f.initiate();
            f.rti.federateRestoreComplete();
            awaitAdmittedRestore(f.rti, 1);
            f.rti.disableCallbacks();

            assertEquals(12, f.rti.getUpdateRateValue("fast"));
            assertEquals(12, f.rti.getUpdateRateValueForAttribute(Handles.objectInstance(10), Handles.attribute(3)));
            assertEquals(ResignAction.NO_ACTION, f.rti.getAutomaticResignDirective());
            assertEquals(Handles.objectInstance(10), f.rti.getObjectInstanceHandle("saved"));
            assertEquals(Handles.objectClass(20), f.rti.getKnownObjectClassHandle(Handles.objectInstance(10)));
            assertThrows(ObjectInstanceNotKnown.class, () -> f.rti.getObjectInstanceHandle("discarded"));
            assertThrows(ObjectInstanceNotKnown.class, () -> f.rti.getObjectInstanceHandle("after-restore"));
            assertEquals(0, f.events.restored);
            assertEquals(1, f.events.discovered);
            assertThrows(RestoreInProgress.class, () -> f.rti.timeAdvanceRequest(TimeValues.time(3)));
            assertThrows(RestoreInProgress.class, f.rti::enableAttributeScopeAdvisorySwitch);
            assertEquals(0, f.advances.get());
            assertEquals(0, f.resigns.get());
            assertEquals(0, f.wrongIdentity.get());

            f.rti.enableCallbacks();
            await(f.rti, () -> f.events.restored == 1 && f.events.discovered == 2);
            assertEquals(Handles.objectInstance(11), f.rti.getObjectInstanceHandle("after-restore"));
            assertEquals(12, f.rti.getUpdateRateValue("fast"));
            assertEquals(Handles.objectInstance(11), f.rti.getObjectInstanceHandle("after-restore"), "Repeated queries must not reapply the checkpoint");
            f.rti.timeAdvanceRequest(TimeValues.time(3));
            assertEquals(1, f.advances.get());
        }
    }

    @Test
    void legalResignationDuringHydrationUsesRestoredIdentity() throws Exception {
        try (Fixture f = new Fixture()) {
            f.initiate();
            f.rti.federateRestoreComplete();
            awaitAdmittedRestore(f.rti, 1);
            f.rti.disableCallbacks();
            f.rti.resignFederationExecution(ResignAction.NO_ACTION);
            assertEquals(41, f.resignedHandle);
            assertEquals(0, f.events.restored);
            assertEquals(1, f.resigns.get());
        }
    }

    @Test
    void finalIdentityMustMatchTheAnnouncedPersistedCheckpoint() throws Exception {
        try (Fixture f = new Fixture()) {
            f.initiate();
            f.finalHandle = 99;
            f.rti.federateRestoreComplete();
            awaitAdmittedRestore(f.rti, 1);
            f.rti.disableCallbacks();
            assertThrows(RTIinternalError.class, () -> f.rti.getUpdateRateValue("fast"));
            assertEquals(0, f.queries.get(), "An inconsistent restored identity must not reach the server");
            assertThrows(RTIinternalError.class, () -> f.rti.timeAdvanceRequest(TimeValues.time(3)));
            assertEquals(0, f.advances.get());
        }
    }

    static void awaitAdmittedRestore(RTIambassador rti, long expected) throws Exception {
        Field owner = GortiRtiAmbassador.class.getDeclaredField("services");
        owner.setAccessible(true);
        Object services = owner.get(rti);
        Field admitted = AmbassadorServices.class.getDeclaredField("callbackEpoch");
        admitted.setAccessible(true);
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
        while (admitted.getLong(services) != expected && System.nanoTime() < deadline) Thread.sleep(1);
        assertEquals(expected, admitted.getLong(services), "Restore terminal was not admitted by transport");
    }

    static void await(RTIambassador rti, BooleanSupplier condition) throws Exception {
        long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10);
        while (!condition.getAsBoolean() && System.nanoTime() < deadline) rti.evokeMultipleCallbacks(0.001, 0.02);
        assertTrue(condition.getAsBoolean(), "Expected callback was not delivered");
    }

    private static byte[] checkpoint() throws Exception {
        ExchangeKnowledge knowledge = new ExchangeKnowledge();
        knowledge.discovered(10, 20, "saved");
        DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
        return new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 0, 0), regions, Set.of(),
                false, false, false, false, false, 2).encode("restore-lookups", 7, 41, "saved");
    }

    private static Stream.FederateEvent discovery(long handle, String name) {
        return Stream.FederateEvent.newBuilder().setDiscover(Stream.DiscoverObjectInstance.newBuilder()
                .setObjectHandle(handle).setObjectClassHandle(20).setObjectName(name)).build();
    }

    private static final class Events extends NullFederateAmbassador {
        int initiated, restored, discovered, grants, saving, begun;
        boolean failRestored;
        final java.util.List<String> order = new java.util.ArrayList<>();
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { initiated++; }
        @Override public void initiateFederateSave(String label) { saving++; }
        @Override public void federationRestoreBegun() { begun++; }
        @Override public void federationRestored() throws FederateInternalError {
            restored++; order.add("restored");
            if (failRestored) throw new FederateInternalError("Application failed after entering restored callback");
        }
        @Override public void timeAdvanceGrant(LogicalTime time) { grants++; order.add("grant"); }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { discovered++; }
    }

    private static final class Fixture implements AutoCloseable {
        final RTIambassador rti = new GortiRtiAmbassador();
        final Events events = new Events();
        final AtomicInteger queries = new AtomicInteger(), advances = new AtomicInteger(), resigns = new AtomicInteger(), wrongIdentity = new AtomicInteger();
        final AtomicInteger restoreCompletes = new AtomicInteger();
        final Server server;
        volatile StreamObserver<Stream.FederateEventBatch> stream;
        volatile long finalHandle = 41;
        long announcedHandle = 41;
        byte[] privateState;
        ByteString terminalReceipt = ByteString.EMPTY;
        final java.util.List<Long> reportedHandles = new java.util.ArrayList<>();
        final java.util.List<Boolean> reportedEntries = new java.util.ArrayList<>();
        volatile long resignedHandle;

        Fixture() throws Exception {
            server = ServerBuilder.forPort(0)
                    .addService(new FederationServiceGrpc.FederationServiceImplBase() {
                        @Override public void listFederations(Federation.ListFederationsRequest request, StreamObserver<Federation.ListFederationsResponse> response) {
                            reply(response, Federation.ListFederationsResponse.newBuilder().addFederations(
                                    Federation.FederationSummary.newBuilder().setName("restore-lookups").setFederationGeneration(7)).build());
                        }
                        @Override public void joinFederation(Federation.JoinFederationRequest request, StreamObserver<Federation.JoinFederationResponse> response) {
                            reply(response, Federation.JoinFederationResponse.newBuilder().setFederateHandle(22)
                                    .setCallbackInvocationEntrySupported(true)
                                    .setSessionToken(ByteString.copyFromUtf8("replacement-session"))
                                    .setTransportMode(Federation.TransportMode.TRANSPORT_MODE_GRPC).build());
                        }
                        @Override public void resignFederation(Federation.ResignFederationRequest request, StreamObserver<Common.Empty> response) {
                            resignedHandle = request.getFederateHandle();
                            resigns.incrementAndGet(); reply(response, Common.Empty.getDefaultInstance());
                        }
                        @Override public void getAutomaticResignDirective(Federation.GetAutomaticResignDirectiveRequest request, StreamObserver<Federation.GetAutomaticResignDirectiveResponse> response) {
                            if (identity(request.getFederateHandle(), response)) reply(response,
                                    Federation.GetAutomaticResignDirectiveResponse.newBuilder().setAction(Common.ResignAction.RESIGN_ACTION_NO_ACTION).build());
                        }
                    })
                    .addService(new StreamServiceGrpc.StreamServiceImplBase() {
                        @Override public void eventBatches(Stream.EventsRequest request, StreamObserver<Stream.FederateEventBatch> response) {
                            ((ServerCallStreamObserver<Stream.FederateEventBatch>) response).setOnCancelHandler(() -> { });
                            stream = response;
                            response.onNext(Stream.FederateEventBatch.newBuilder().setReady(true).build());
                        }
                    })
                    .addService(new SavepointServiceGrpc.SavepointServiceImplBase() {
                        @Override public void federateRestoreComplete(Savepoint.FederateRestoreResponseRequest request, StreamObserver<Common.Empty> response) {
                            restoreCompletes.incrementAndGet();
                            stream.onNext(Stream.FederateEventBatch.newBuilder().addEvents(Stream.FederateEvent.newBuilder()
                                    .setCallbackReceipt(terminalReceipt)
                                    .setRestoreCompleted(Stream.FederationRestored.newBuilder().setLabel("saved")
                                            .setFederateHandle(finalHandle).setCallbackEpoch(1)))
                                    .addEvents(discovery(11, "after-restore")).build());
                            reply(response, Common.Empty.getDefaultInstance());
                        }
                    })
                    .addService(new SupportServiceGrpc.SupportServiceImplBase() {
                        @Override public void reportCallbackInvocation(Support.ReportCallbackInvocationRequest request, StreamObserver<Common.Empty> response) {
                            reportedHandles.add(request.getFederateHandle()); reportedEntries.add(request.getInvocationEntry());
                            if (identity(request.getFederateHandle(), response)) reply(response, Common.Empty.getDefaultInstance());
                        }
                        @Override public void getUpdateRateValue(Support.GetUpdateRateValueRequest request, StreamObserver<Support.GetUpdateRateValueResponse> response) {
                            queries.incrementAndGet();
                            if (identity(request.getFederateHandle(), response)) reply(response,
                                    Support.GetUpdateRateValueResponse.newBuilder().setUpdateRate(12).build());
                        }
                        @Override public void getUpdateRateValueForAttribute(Support.GetUpdateRateValueForAttributeRequest request, StreamObserver<Support.GetUpdateRateValueForAttributeResponse> response) {
                            queries.incrementAndGet();
                            if (identity(request.getFederateHandle(), response)) reply(response,
                                    Support.GetUpdateRateValueForAttributeResponse.newBuilder().setUpdateRate(12).build());
                        }
                    })
                    .addService(new TimeServiceGrpc.TimeServiceImplBase() {
                        @Override public void timeAdvanceRequest(Time.TARRequest request, StreamObserver<Common.Empty> response) {
                            advances.incrementAndGet(); reply(response, Common.Empty.getDefaultInstance());
                        }
                    }).build().start();
            rti.connect(events, CallbackModel.HLA_EVOKED, "grpc://localhost:" + server.getPort());
            rti.joinFederationExecution("one", "test", "restore-lookups");
        }

        private boolean identity(long handle, StreamObserver<?> response) {
            if (handle == finalHandle) return true;
            wrongIdentity.incrementAndGet();
            response.onError(Status.PERMISSION_DENIED.withDescription("stale restored membership").asRuntimeException());
            return false;
        }

        void initiate() throws Exception {
            stream.onNext(Stream.FederateEventBatch.newBuilder().addEvents(discovery(12, "discarded"))
                    .addEvents(Stream.FederateEvent.newBuilder().setRestoreBegun(Stream.FederationRestoreBegun.getDefaultInstance()))
                    .addEvents(Stream.FederateEvent.newBuilder().setRestoreInitiate(Stream.InitiateFederateRestore.newBuilder()
                            .setLabel("saved").setFederateName("one").setFederateHandle(announcedHandle)
                            .setClientState(ByteString.copyFrom(privateState == null ? checkpoint() : privateState)))).build());
            await(rti, () -> events.initiated == 1);
        }

        @Override public void close() throws Exception {
            try {
                try { rti.enableCallbacks(); rti.evokeMultipleCallbacks(0, 0.05); } catch (RTIexception ignored) { }
                try { rti.resignFederationExecution(ResignAction.NO_ACTION); } catch (RTIexception ignored) { }
                try { rti.disconnect(); } catch (RTIexception ignored) { }
            } finally {
                server.shutdownNow();
                assertTrue(server.awaitTermination(5, TimeUnit.SECONDS));
            }
        }
    }

    private static <T> void reply(StreamObserver<T> response, T value) { response.onNext(value); response.onCompleted(); }
}
