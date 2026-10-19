package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.exceptions.*;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.concurrent.atomic.AtomicLong;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Stream;

class CallbackGateTest {
    private AmbassadorServices services;
    private io.grpc.Server server;

    @BeforeEach void connectWithoutJoining() throws Exception {
        server = io.grpc.ServerBuilder.forPort(0).addService(new rti.v1.FederationServiceGrpc.FederationServiceImplBase() {
            @Override public void listFederations(rti.v1.Federation.ListFederationsRequest request,
                    io.grpc.stub.StreamObserver<rti.v1.Federation.ListFederationsResponse> response) {
                response.onNext(rti.v1.Federation.ListFederationsResponse.getDefaultInstance());
                response.onCompleted();
            }
        }).build().start();
        services = new AmbassadorServices();
        services.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, "grpc://127.0.0.1:" + server.getPort());
    }

    @AfterEach void closeConnection() throws Exception {
        try { services.disconnect(); }
        finally {
            server.shutdownNow();
            assertTrue(server.awaitTermination(5, java.util.concurrent.TimeUnit.SECONDS));
        }
    }

    @ParameterizedTest @ValueSource(booleans = {true, false})
    void disconnectedSwitchesRejectWithoutMutation(boolean enabled) throws Exception {
        AmbassadorServices disconnected = new AmbassadorServices();
        set(disconnected, "callbacksEnabled", enabled);
        assertThrows(RTIinternalError.class, disconnected::enableCallbacks);
        assertEquals(enabled, get(disconnected, "callbacksEnabled"));
        assertThrows(RTIinternalError.class, disconnected::disableCallbacks);
        assertEquals(enabled, get(disconnected, "callbacksEnabled"));
    }

    @Test void switchesRemainLocalAndDisabledCallbacksStayQueued() throws Exception {
        int[] delivered = {0};
        services.evokeCallback(0);
        enqueue(services, target -> delivered[0]++);
        services.disableCallbacks();
        assertTrue(services.evokeCallback(0));
        assertEquals(0, delivered[0]);
        services.enableCallbacks();
        assertFalse(services.evokeCallback(0));
        assertEquals(1, delivered[0]);
        // A connected federate need not have joined to use either switch.
        services.disableCallbacks();
        services.enableCallbacks();
    }

    @ParameterizedTest @ValueSource(booleans = {true, false})
    void saveRejectionsPreserveBothSwitchStates(boolean enabled) throws Exception {
        set(services, "callbacksEnabled", enabled);
        set(services, "callbackSaveLabel", "saving");
        assertThrows(SaveInProgress.class, services::enableCallbacks);
        assertEquals(enabled, get(services, "callbacksEnabled"));
        assertThrows(SaveInProgress.class, services::disableCallbacks);
        assertEquals(enabled, get(services, "callbacksEnabled"));
    }

    @ParameterizedTest @ValueSource(booleans = {true, false})
    void restoreRejectionsPreserveBothSwitchStates(boolean enabled) throws Exception {
        set(services, "callbacksEnabled", enabled);
        gate(services).set(1);
        set(services, "callbackRestoreInProgress", true);
        assertThrows(RestoreInProgress.class, services::enableCallbacks);
        assertEquals(enabled, get(services, "callbacksEnabled"));
        assertThrows(RestoreInProgress.class, services::disableCallbacks);
        assertEquals(enabled, get(services, "callbacksEnabled"));
    }

    @ParameterizedTest @ValueSource(booleans = {true, false})
    void saveTerminalReleasesTheGuardBeforeTheUserCallback(boolean success) throws Exception {
        set(services, "pendingSaveLabel", "saving");
        set(services, "callbackSaveLabel", "saving");
        set(services, "ambassador", new NullFederateAmbassador());
        Stream.FederateEvent event = success ? Stream.FederateEvent.newBuilder().setSaveCompleted(
                Stream.FederationSaved.newBuilder().setLabel("saving")).build()
                : Stream.FederateEvent.newBuilder().setSaveFailed(
                Stream.FederationNotSaved.newBuilder().setLabel("saving").setReason(6)).build();
        observe(services, event);
        services.disableCallbacks();
        services.enableCallbacks();
        invoke(services, "deliver", new Class<?>[]{Stream.FederateEvent.class, long.class}, event, 0L);
        services.disableCallbacks();
        services.enableCallbacks();
    }

    @Test void failedRestoreAndMembershipResetReleaseTheGuard() throws Exception {
        gate(services).set(1);
        set(services, "callbackRestoreInProgress", true);
        set(services, "ambassador", new NullFederateAmbassador());
        Stream.FederateEvent failed = Stream.FederateEvent.newBuilder().setRestoreFailed(
                Stream.FederationNotRestored.newBuilder().setLabel("restore").setReason(5)).build();
        observe(services, failed);
        services.disableCallbacks();
        services.enableCallbacks();
        invoke(services, "deliver", new Class<?>[]{Stream.FederateEvent.class, long.class}, failed, 1L);
        services.disableCallbacks();
        services.enableCallbacks();
        gate(services).set(3);
        set(services, "callbackRestoreInProgress", true);
        set(services, "callbackSaveLabel", "saving");
        invoke(services, "resetTime", new Class<?>[0]);
        services.disableCallbacks();
        services.enableCallbacks();
    }

    @Test void idleCompletionPrecedesStaleValidationFailure() throws Exception {
        assertThrows(RestoreNotRequested.class, () -> readiness(services));
        set(services, "pendingRestoreFailure", new RTIinternalError("old failure"));
        set(services, "pendingRestoreValidated", true);
        assertThrows(RestoreNotRequested.class, () -> readiness(services));
    }

    @Test void activeCompletionStillRequiresSdkValidation() throws Exception {
        gate(services).set(1);
        set(services, "callbackRestoreInProgress", true);
        assertThrows(RTIinternalError.class, () -> readiness(services));
        RTIinternalError failure = new RTIinternalError("invalid checkpoint");
        set(services, "pendingRestoreFailure", failure);
        assertSame(failure, assertThrows(RTIinternalError.class, () -> readiness(services)));
        set(services, "pendingRestoreFailure", null);
        set(services, "pendingRestoreValidated", true);
        readiness(services);
    }

    static void readiness(AmbassadorServices services) throws Exception {
        invoke(services, "restoreReady", new Class<?>[0]);
    }

    private static void observe(AmbassadorServices services, Stream.FederateEvent event) throws Exception {
        invoke(services, "observeCallbackPhase", new Class<?>[]{Stream.FederateEvent.class}, event);
    }

    private static void enqueue(AmbassadorServices services, CallbackSink.Invocation callback) throws Exception {
        invoke(services, "enqueueFederateCallback", new Class<?>[]{CallbackSink.Invocation.class, long.class, long.class},
                callback, get(services, "epoch"), get(services, "callbackEpoch"));
    }

    private static AtomicLong gate(AmbassadorServices services) throws Exception {
        return (AtomicLong) get(services, "restoreGate");
    }

    private static Object get(Object owner, String name) throws Exception {
        Field field = owner.getClass().getDeclaredField(name);
        field.setAccessible(true);
        return field.get(owner);
    }

    private static void set(Object owner, String name, Object value) throws Exception {
        Field field = owner.getClass().getDeclaredField(name);
        field.setAccessible(true);
        field.set(owner, value);
    }

    private static void invoke(Object owner, String name, Class<?>[] types, Object... arguments) throws Exception {
        Method method = owner.getClass().getDeclaredMethod(name, types);
        method.setAccessible(true);
        try { method.invoke(owner, arguments); }
        catch (InvocationTargetException failure) { throw (Exception) failure.getCause(); }
    }
}
