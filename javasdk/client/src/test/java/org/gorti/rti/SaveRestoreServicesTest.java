package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.Message;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import hla.rti1516e.time.HLAfloat64Time;
import io.grpc.*;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.MetadataUtils;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.Proxy;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;
import java.util.Queue;
import java.util.stream.Stream;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.Arguments;
import org.junit.jupiter.params.provider.MethodSource;
import rti.v1.Common;
import rti.v1.Savepoint;
import rti.v1.SavepointServiceGrpc;

class SaveRestoreServicesTest {
    @Test void restoreValidationPrecedesTheCompletionRpcButNotFailureReporting() throws Exception {
        try (Fixture f = new Fixture()) {
            AmbassadorServices state = new AmbassadorServices();
            java.lang.reflect.Field gate = AmbassadorServices.class.getDeclaredField("restoreGate");
            gate.setAccessible(true);
            ((java.util.concurrent.atomic.AtomicLong) gate.get(state)).set(1);
            java.lang.reflect.Field active = AmbassadorServices.class.getDeclaredField("callbackRestoreInProgress");
            active.setAccessible(true);
            active.set(state, true);
            SaveRestoreServices guarded = new SaveRestoreServices(f, f.callbacks::add,
                    () -> new byte[0], () -> {
                        try { CallbackGateTest.readiness(state); }
                        catch (RTIexception failure) { throw failure; }
                        catch (Exception failure) { throw new RTIinternalError("Test readiness invocation failed", failure); }
                    });
            assertThrows(RTIinternalError.class, guarded::federateRestoreComplete);
            assertTrue(f.calls.isEmpty(), "Unvalidated SDK state must not reach server completion");
            guarded.federateRestoreNotComplete();
            assertEquals(List.of("restoreNotComplete"), f.calls);
            java.lang.reflect.Field validated = AmbassadorServices.class.getDeclaredField("pendingRestoreValidated");
            validated.setAccessible(true);
            validated.set(state, true);
            guarded.federateRestoreComplete();
            assertEquals(List.of("restoreNotComplete", "restoreComplete"), f.calls);
        }
    }

    @Test void immediateAndTimedSavesKeepOptionalTimeAndCurrentIdentity() throws Exception {
        try (Fixture f = new Fixture()) {
            f.services.requestFederationSave("immediate");
            Savepoint.RequestFederationSaveRequest now = (Savepoint.RequestFederationSaveRequest) f.request;
            assertFalse(now.hasSaveTime());
            assertEquals("immediate", now.getLabel());
            f.handle = 23;
            f.services.requestFederationSave("zero", TimeValues.time(0));
            Savepoint.RequestFederationSaveRequest timed = (Savepoint.RequestFederationSaveRequest) f.request;
            assertTrue(timed.hasSaveTime());
            assertEquals(0, timed.getSaveTime());
            assertEquals(23, timed.getFederateHandle());
            f.services.requestFederationSave("future", TimeValues.time(12.5));
            assertEquals(12.5, ((Savepoint.RequestFederationSaveRequest) f.request).getSaveTime());
            assertTrue(f.callbacks.isEmpty(), "A successful save request is not an initiate callback");
        }
    }

    @Test void allParticipantTransitionsAreIndependentAuthoritativeCalls() throws Exception {
        try (Fixture f = new Fixture()) {
            f.services.federateSaveBegun();
            f.services.federateSaveComplete();
            f.services.federateSaveNotComplete();
            f.services.abortFederationSave();
            f.services.requestFederationRestore("saved");
            Savepoint.RequestFederationRestoreRequest restore = (Savepoint.RequestFederationRestoreRequest) f.request;
            assertEquals("saved", restore.getLabel());
            assertFalse(restore.hasSavedFederationGeneration());
            f.services.federateRestoreComplete();
            f.services.federateRestoreNotComplete();
            f.services.abortFederationRestore();
            assertEquals(List.of("begun", "saveComplete", "saveNotComplete", "abortSave", "restore",
                    "restoreComplete", "restoreNotComplete", "abortRestore"), f.calls);
            assertTrue(((Savepoint.AbortFederationRestoreRequest) f.request).hasFederateHandle());
            assertTrue(f.callbacks.isEmpty(), "Transitions must not fabricate callbacks");
        }
    }

    @Test void membershipAndRepresentabilityFailuresDoNotSendRpc() throws Exception {
        try (Fixture f = new Fixture()) {
            f.member = false;
            for (Operation operation : operations()) {
                assertThrows(FederateNotExecutionMember.class, () -> operation.invoke(f.services));
            }
            assertTrue(f.calls.isEmpty());
            f.member = true;
            assertThrows(RTIinternalError.class, () -> f.services.requestFederationSave(null));
            assertThrows(RTIinternalError.class, () -> f.services.requestFederationRestore(null));
            assertThrows(InvalidLogicalTime.class, () -> f.services.requestFederationSave("bad", null));
            for (double value : new double[]{Double.NaN, Double.POSITIVE_INFINITY, -1}) {
                HLAfloat64Time foreign = (HLAfloat64Time) Proxy.newProxyInstance(
                        getClass().getClassLoader(), new Class<?>[]{HLAfloat64Time.class},
                        (proxy, method, arguments) -> value);
                assertThrows(InvalidLogicalTime.class, () -> f.services.requestFederationSave("bad", foreign));
            }
            assertTrue(f.calls.isEmpty());
        }
    }

    @ParameterizedTest(name = "{0}")
    @MethodSource("wireFailures")
    void preservesExactCheckedWireExceptions(String exception, Operation operation) throws Exception {
        try (Fixture f = new Fixture()) {
            f.failure = exception;
            RTIexception actual = assertThrows(RTIexception.class, () -> operation.invoke(f.services));
            assertEquals("hla.rti1516e.exceptions." + exception, actual.getClass().getName());
            assertTrue(f.callbacks.isEmpty());
        }
    }

    static Stream<Arguments> wireFailures() {
        return Stream.of(
                Arguments.of("SaveInProgress", (Operation) s -> s.requestFederationSave("x")),
                Arguments.of("RestoreInProgress", (Operation) s -> s.requestFederationSave("x")),
                Arguments.of("LogicalTimeAlreadyPassed", (Operation) s -> s.requestFederationSave("x", TimeValues.time(1))),
                Arguments.of("FederateUnableToUseTime", (Operation) s -> s.requestFederationSave("x", TimeValues.time(1))),
                Arguments.of("InvalidLogicalTime", (Operation) s -> s.requestFederationSave("x", TimeValues.time(1))),
                Arguments.of("SaveNotInitiated", (Operation) SaveRestoreServices::federateSaveBegun),
                Arguments.of("FederateHasNotBegunSave", (Operation) SaveRestoreServices::federateSaveComplete),
                Arguments.of("FederateHasNotBegunSave", (Operation) SaveRestoreServices::federateSaveNotComplete),
                Arguments.of("SaveNotInProgress", (Operation) SaveRestoreServices::abortFederationSave),
                Arguments.of("RestoreInProgress", (Operation) SaveRestoreServices::queryFederationSaveStatus),
                Arguments.of("SaveInProgress", (Operation) s -> s.requestFederationRestore("x")),
                Arguments.of("RestoreNotRequested", (Operation) SaveRestoreServices::federateRestoreComplete),
                Arguments.of("RestoreNotRequested", (Operation) SaveRestoreServices::federateRestoreNotComplete),
                Arguments.of("RestoreNotInProgress", (Operation) SaveRestoreServices::abortFederationRestore),
                Arguments.of("SaveInProgress", (Operation) SaveRestoreServices::queryFederationRestoreStatus),
                Arguments.of("FederateNotExecutionMember", (Operation) SaveRestoreServices::queryFederationSaveStatus));
    }

    @Test void saveStatusIsAuthenticatedDeferredAndUsesEveryAuthoritativeProgressValue() throws Exception {
        try (Fixture f = new Fixture()) {
            Savepoint.QuerySaveStateResponse.Builder response = Savepoint.QuerySaveStateResponse.newBuilder();
            for (int i = 1; i <= 4; i++) response.addFederateStatuses(Savepoint.FederateSaveStatus.newBuilder()
                    .setFederateHandle(100 + i).setStatusValue(i));
            f.saveStatus = response.build();
            f.services.queryFederationSaveStatus();
            assertEquals("", ((Savepoint.QuerySaveStateRequest) f.request).getLabel());
            assertEquals(1, f.callbacks.size());
            f.saveStatus = Savepoint.QuerySaveStateResponse.getDefaultInstance();
            final List<FederateHandleSaveStatusPair[]> delivered = new ArrayList<>();
            f.callbacks.remove().invoke(new NullFederateAmbassador() {
                @Override public void federationSaveStatusResponse(FederateHandleSaveStatusPair[] values) {
                    delivered.add(values);
                }
            });
            assertEquals(1, delivered.size());
            assertEquals(4, delivered.get(0).length);
            for (int i = 0; i < 4; i++) {
                assertEquals(Handles.federate(101 + i), delivered.get(0)[i].handle);
                assertEquals(SaveStatus.values()[i], delivered.get(0)[i].status);
            }
        }
    }

    @Test void restoreStatusKeepsPrePostIdentityAndUnassignedPostHandle() throws Exception {
        try (Fixture f = new Fixture()) {
            Savepoint.QueryRestoreStateResponse.Builder response = Savepoint.QueryRestoreStateResponse.newBuilder();
            for (int i = 1; i <= 6; i++) response.addFederateStatuses(Savepoint.FederateRestoreStatus.newBuilder()
                    .setPreRestoreHandle(100 + i).setPostRestoreHandle(i == 1 ? 0 : 200 + i).setStatusValue(i));
            f.restoreStatus = response.build();
            f.services.queryFederationRestoreStatus();
            assertEquals("", ((Savepoint.QueryRestoreStateRequest) f.request).getLabel());
            assertEquals(1, f.callbacks.size());
            f.callbacks.remove().invoke(new NullFederateAmbassador() {
                @Override public void federationRestoreStatusResponse(FederateRestoreStatus[] values) {
                    assertEquals(6, values.length);
                    assertNull(values[0].postRestoreHandle);
                    for (int i = 0; i < 6; i++) {
                        assertEquals(Handles.federate(101 + i), values[i].preRestoreHandle);
                        if (i > 0) assertEquals(Handles.federate(201 + i), values[i].postRestoreHandle);
                        assertEquals(RestoreStatus.values()[i], values[i].status);
                    }
                }
            });
        }
    }

    @Test void invalidStatusValuesAndHandlesDoNotQueuePartialResponses() throws Exception {
        try (Fixture f = new Fixture()) {
            for (int value : new int[]{0, 99}) {
                f.saveStatus = Savepoint.QuerySaveStateResponse.newBuilder().addFederateStatuses(
                        Savepoint.FederateSaveStatus.newBuilder().setFederateHandle(7).setStatusValue(value)).build();
                assertThrows(RTIinternalError.class, f.services::queryFederationSaveStatus);
                f.restoreStatus = Savepoint.QueryRestoreStateResponse.newBuilder().addFederateStatuses(
                        Savepoint.FederateRestoreStatus.newBuilder().setPreRestoreHandle(7).setStatusValue(value)).build();
                assertThrows(RTIinternalError.class, f.services::queryFederationRestoreStatus);
            }
            f.saveStatus = Savepoint.QuerySaveStateResponse.newBuilder().addFederateStatuses(
                    Savepoint.FederateSaveStatus.newBuilder().setStatusValue(1)).build();
            assertThrows(RTIinternalError.class, f.services::queryFederationSaveStatus);
            f.restoreStatus = Savepoint.QueryRestoreStateResponse.newBuilder().addFederateStatuses(
                    Savepoint.FederateRestoreStatus.newBuilder().setStatusValue(1)).build();
            assertThrows(RTIinternalError.class, f.services::queryFederationRestoreStatus);
            assertTrue(f.callbacks.isEmpty());
        }
    }

    @Test void emptyStatusVectorsAreStillAuthoritativeCallbacks() throws Exception {
        try (Fixture f = new Fixture()) {
            f.services.queryFederationSaveStatus();
            f.services.queryFederationRestoreStatus();
            assertEquals(2, f.callbacks.size());
            NullFederateAmbassador target = new NullFederateAmbassador() {
                @Override public void federationSaveStatusResponse(FederateHandleSaveStatusPair[] values) { assertEquals(0, values.length); }
                @Override public void federationRestoreStatusResponse(FederateRestoreStatus[] values) { assertEquals(0, values.length); }
            };
            while (!f.callbacks.isEmpty()) f.callbacks.remove().invoke(target);
        }
    }

    @Test void decoderSelectsStandardSaveAndRestoreCallbacksWithoutChangingState() throws Exception {
        List<String> calls = new ArrayList<>();
        NullFederateAmbassador target = new NullFederateAmbassador() {
            @Override public void initiateFederateSave(String label) { calls.add("save:" + label); }
            @Override public void initiateFederateSave(String label, LogicalTime time) { calls.add("timed:" + label + ":" + TimeValues.value(time)); }
            @Override public void federationSaved() { calls.add("saved"); }
            @Override public void requestFederationRestoreSucceeded(String label) { calls.add("accepted:" + label); }
            @Override public void requestFederationRestoreFailed(String label) { calls.add("rejected:" + label); }
            @Override public void federationRestoreBegun() { calls.add("begun"); }
            @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) {
                calls.add("restore:" + label + ":" + name + ":" + Handles.value(handle));
            }
            @Override public void federationRestored() { calls.add("restored"); }
        };
        for (rti.v1.Stream.FederateEvent event : List.of(
                rti.v1.Stream.FederateEvent.newBuilder().setSaveInitiate(rti.v1.Stream.InitiateFederateSave.newBuilder().setLabel("now")).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setSaveInitiate(rti.v1.Stream.InitiateFederateSave.newBuilder().setLabel("pin").setSaveTime(0)).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setSaveCompleted(rti.v1.Stream.FederationSaved.getDefaultInstance()).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreRequestSucceeded(rti.v1.Stream.RequestFederationRestoreSucceeded.newBuilder().setLabel("old")).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreRequestFailed(rti.v1.Stream.RequestFederationRestoreFailed.newBuilder().setLabel("missing").setReason("storage failure")).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreBegun(rti.v1.Stream.FederationRestoreBegun.getDefaultInstance()).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreInitiate(rti.v1.Stream.InitiateFederateRestore.newBuilder().setLabel("old").setFederateName("A").setFederateHandle(73)).build(),
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreCompleted(rti.v1.Stream.FederationRestored.newBuilder().setLabel("old").setFederateHandle(73).setCallbackEpoch(9).setNextRetractionHandle(80)).build())) {
            assertTrue(SaveRestoreCallbacks.deliver(event, target));
        }
        assertEquals(List.of("save:now", "timed:pin:0.0", "saved", "accepted:old", "rejected:missing", "begun", "restore:old:A:73", "restored"), calls);
        assertFalse(SaveRestoreCallbacks.deliver(rti.v1.Stream.FederateEvent.getDefaultInstance(), target));
    }

    @Test void reasonlessFailureTerminalsCannotInventStandardFailureReasons() {
        NullFederateAmbassador target = new NullFederateAmbassador() {
            @Override public void federationNotSaved(SaveFailureReason reason) { fail("Absent wire reason must not be guessed"); }
            @Override public void federationNotRestored(RestoreFailureReason reason) { fail("Absent wire reason must not be guessed"); }
        };
        RTIinternalError save = assertThrows(RTIinternalError.class, () -> SaveRestoreCallbacks.deliver(
                rti.v1.Stream.FederateEvent.newBuilder().setSaveFailed(rti.v1.Stream.FederationNotSaved.newBuilder().setLabel("failed")).build(), target));
        assertTrue(save.getMessage().contains("failure reason"));
        RTIinternalError restore = assertThrows(RTIinternalError.class, () -> SaveRestoreCallbacks.deliver(
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreFailed(rti.v1.Stream.FederationNotRestored.newBuilder().setLabel("failed")).build(), target));
        assertTrue(restore.getMessage().contains("failure reason"));
    }

    @ParameterizedTest @MethodSource("saveFailureReasons")
    void mapsEverySaveFailureReason(int wire, SaveFailureReason expected) throws Exception {
        List<SaveFailureReason> delivered = new ArrayList<>();
        assertTrue(SaveRestoreCallbacks.deliver(rti.v1.Stream.FederateEvent.newBuilder()
                .setSaveFailed(rti.v1.Stream.FederationNotSaved.newBuilder().setReason(wire)).build(),
                new NullFederateAmbassador() {
                    @Override public void federationNotSaved(SaveFailureReason value) { delivered.add(value); }
                }));
        assertEquals(List.of(expected), delivered);
    }

    static Stream<Arguments> saveFailureReasons() {
        return Stream.of(Arguments.of(1, SaveFailureReason.RTI_UNABLE_TO_SAVE),
                Arguments.of(2, SaveFailureReason.FEDERATE_REPORTED_FAILURE_DURING_SAVE),
                Arguments.of(3, SaveFailureReason.FEDERATE_RESIGNED_DURING_SAVE),
                Arguments.of(4, SaveFailureReason.RTI_DETECTED_FAILURE_DURING_SAVE),
                Arguments.of(5, SaveFailureReason.SAVE_TIME_CANNOT_BE_HONORED),
                Arguments.of(6, SaveFailureReason.SAVE_ABORTED));
    }

    @ParameterizedTest @MethodSource("restoreFailureReasons")
    void mapsEveryRestoreFailureReason(int wire, RestoreFailureReason expected) throws Exception {
        List<RestoreFailureReason> delivered = new ArrayList<>();
        assertTrue(SaveRestoreCallbacks.deliver(rti.v1.Stream.FederateEvent.newBuilder()
                .setRestoreFailed(rti.v1.Stream.FederationNotRestored.newBuilder().setReason(wire)).build(),
                new NullFederateAmbassador() {
                    @Override public void federationNotRestored(RestoreFailureReason value) { delivered.add(value); }
                }));
        assertEquals(List.of(expected), delivered);
    }

    static Stream<Arguments> restoreFailureReasons() {
        return Stream.of(Arguments.of(1, RestoreFailureReason.RTI_UNABLE_TO_RESTORE),
                Arguments.of(2, RestoreFailureReason.FEDERATE_REPORTED_FAILURE_DURING_RESTORE),
                Arguments.of(3, RestoreFailureReason.FEDERATE_RESIGNED_DURING_RESTORE),
                Arguments.of(4, RestoreFailureReason.RTI_DETECTED_FAILURE_DURING_RESTORE),
                Arguments.of(5, RestoreFailureReason.RESTORE_ABORTED));
    }

    @Test void rejectsNoneAndUnknownFailureReasonValues() {
        NullFederateAmbassador target = new NullFederateAmbassador() {
            @Override public void federationNotSaved(SaveFailureReason reason) { fail("Invalid reason"); }
            @Override public void federationNotRestored(RestoreFailureReason reason) { fail("Invalid reason"); }
        };
        for (int reason : new int[]{0, -1, 99}) {
            assertThrows(RTIinternalError.class, () -> SaveRestoreCallbacks.deliver(rti.v1.Stream.FederateEvent.newBuilder()
                    .setSaveFailed(rti.v1.Stream.FederationNotSaved.newBuilder().setReason(reason)).build(), target));
            assertThrows(RTIinternalError.class, () -> SaveRestoreCallbacks.deliver(rti.v1.Stream.FederateEvent.newBuilder()
                    .setRestoreFailed(rti.v1.Stream.FederationNotRestored.newBuilder().setReason(reason)).build(), target));
        }
    }

    @Test void invalidCallbackTimeAndRestoreIdentityAreCheckedFailures() {
        assertThrows(RTIinternalError.class, () -> SaveRestoreCallbacks.deliver(
                rti.v1.Stream.FederateEvent.newBuilder().setSaveInitiate(rti.v1.Stream.InitiateFederateSave.newBuilder().setSaveTime(Double.NaN)).build(), new NullFederateAmbassador()));
        assertThrows(RTIinternalError.class, () -> SaveRestoreCallbacks.deliver(
                rti.v1.Stream.FederateEvent.newBuilder().setRestoreInitiate(rti.v1.Stream.InitiateFederateRestore.getDefaultInstance()).build(), new NullFederateAmbassador()));
    }

    private static List<Operation> operations() {
        return List.of(s -> s.requestFederationSave("x"), s -> s.requestFederationSave("x", TimeValues.time(1)),
                SaveRestoreServices::federateSaveBegun, SaveRestoreServices::federateSaveComplete,
                SaveRestoreServices::federateSaveNotComplete, SaveRestoreServices::abortFederationSave,
                SaveRestoreServices::queryFederationSaveStatus, s -> s.requestFederationRestore("x"),
                SaveRestoreServices::federateRestoreComplete, SaveRestoreServices::federateRestoreNotComplete,
                SaveRestoreServices::abortFederationRestore, SaveRestoreServices::queryFederationRestoreStatus);
    }

    @FunctionalInterface interface Operation { void invoke(SaveRestoreServices services) throws RTIexception; }

    private static final class Fixture implements AutoCloseable, ServiceContext {
        final Queue<CallbackSink.Invocation> callbacks = new ArrayDeque<>();
        final List<String> calls = new ArrayList<>();
        final ManagedChannel transport;
        final Channel authenticated;
        final Server server;
        final SaveRestoreServices services;
        Message request;
        String failure;
        long handle = 7;
        boolean member = true;
        Savepoint.QuerySaveStateResponse saveStatus = Savepoint.QuerySaveStateResponse.getDefaultInstance();
        Savepoint.QueryRestoreStateResponse restoreStatus = Savepoint.QueryRestoreStateResponse.getDefaultInstance();

        Fixture() throws Exception {
            String name = InProcessServerBuilder.generateName();
            Metadata.Key<byte[]> token = Metadata.Key.of("gorti-session-token-bin", Metadata.BINARY_BYTE_MARSHALLER);
            server = InProcessServerBuilder.forName(name).directExecutor().intercept(new ServerInterceptor() {
                @Override public <ReqT, RespT> ServerCall.Listener<ReqT> interceptCall(ServerCall<ReqT, RespT> call, Metadata headers, ServerCallHandler<ReqT, RespT> next) {
                    assertArrayEquals(new byte[]{4, 2}, headers.get(token), "Queries and transitions must use the authenticated context channel");
                    return next.startCall(call, headers);
                }
            }).addService(new SavepointServiceGrpc.SavepointServiceImplBase() {
                @Override public void requestFederationSave(Savepoint.RequestFederationSaveRequest r, StreamObserver<Common.Empty> o) { empty("save", r, o); }
                @Override public void federateSaveBegun(Savepoint.FederateSaveResponseRequest r, StreamObserver<Common.Empty> o) { empty("begun", r, o); }
                @Override public void federateSaveComplete(Savepoint.FederateSaveResponseRequest r, StreamObserver<Common.Empty> o) { empty("saveComplete", r, o); }
                @Override public void federateSaveNotComplete(Savepoint.FederateSaveResponseRequest r, StreamObserver<Common.Empty> o) { empty("saveNotComplete", r, o); }
                @Override public void abortFederationSave(Savepoint.AbortFederationSaveRequest r, StreamObserver<Common.Empty> o) { empty("abortSave", r, o); }
                @Override public void querySaveState(Savepoint.QuerySaveStateRequest r, StreamObserver<Savepoint.QuerySaveStateResponse> o) { reply("querySave", r, saveStatus, o); }
                @Override public void requestFederationRestore(Savepoint.RequestFederationRestoreRequest r, StreamObserver<Common.Empty> o) { empty("restore", r, o); }
                @Override public void federateRestoreComplete(Savepoint.FederateRestoreResponseRequest r, StreamObserver<Common.Empty> o) { empty("restoreComplete", r, o); }
                @Override public void federateRestoreNotComplete(Savepoint.FederateRestoreResponseRequest r, StreamObserver<Common.Empty> o) { empty("restoreNotComplete", r, o); }
                @Override public void abortFederationRestore(Savepoint.AbortFederationRestoreRequest r, StreamObserver<Common.Empty> o) { empty("abortRestore", r, o); }
                @Override public void queryRestoreState(Savepoint.QueryRestoreStateRequest r, StreamObserver<Savepoint.QueryRestoreStateResponse> o) { reply("queryRestore", r, restoreStatus, o); }
            }).build().start();
            transport = InProcessChannelBuilder.forName(name).directExecutor().build();
            Metadata headers = new Metadata();
            headers.put(token, new byte[]{4, 2});
            authenticated = ClientInterceptors.intercept(transport, MetadataUtils.newAttachHeadersInterceptor(headers));
            services = new SaveRestoreServices(this, callbacks::add);
        }

        void empty(String name, Message r, StreamObserver<Common.Empty> o) { reply(name, r, Common.Empty.getDefaultInstance(), o); }
        <T> void reply(String name, Message r, T response, StreamObserver<T> observer) {
            request = r;
            calls.add(name);
            assertEquals("demo", r.getField(r.getDescriptorForType().findFieldByName("federation_name")));
            assertEquals(Common.WireVersion.WIRE_VERSION_V1.getValueDescriptor(), r.getField(r.getDescriptorForType().findFieldByName("wire_version")));
            com.google.protobuf.Descriptors.FieldDescriptor identity = r.getDescriptorForType().findFieldByName("federate_handle");
            if (identity != null) assertEquals(handle, r.getField(identity));
            if (failure != null) {
                Metadata trailers = new Metadata();
                trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), failure);
                observer.onError(Status.FAILED_PRECONDITION.withDescription("authoritative rejection").asRuntimeException(trailers));
            } else {
                observer.onNext(response);
                observer.onCompleted();
            }
        }
        @Override public void close() { transport.shutdownNow(); server.shutdownNow(); }
        @Override public void requireMember() throws RTIexception { if (!member) throw new FederateNotExecutionMember("not joined"); }
        @Override public Channel channel() { return authenticated; }
        @Override public String federation() { return "demo"; }
        @Override public long federate() { return handle; }
        @Override public long generation() { return 4; }
        @Override public boolean regulating() { return false; }
        @Override public double logicalTime() { return 0; }
    }
}
