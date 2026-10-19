package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import hla.rti1516e.exceptions.RTIinternalError;
import java.io.*;
import java.lang.reflect.*;
import java.util.Set;
import java.util.List;
import java.util.ArrayList;
import com.google.gson.*;
import hla.rti1516e.*;
import org.gorti.rti.types.TimeValues;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Common;
import rti.v1.Savepoint;
import org.junit.jupiter.api.Test;

class LocalNotificationCheckpointTest {
    private static final long SELF = (7L << 32) | 8;

    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    void pureTypedResultsPreserveMultiplicityIdentityAndHistoricalPostHandle(String scalar) throws Exception {
        AmbassadorServices services = savingServices();
        if (scalar.equals(TimeWire.INTEGER)) set(services, "logicalTime", new org.gorti.rti.types.Integer64TimeFactory().makeTime((1L << 53) + 1));
        List<LocalNotification> notifications = inventory();
        for (LocalNotification notification : notifications) enqueue(services, notification);
        byte[] bytes = export(services);
        RuntimeCheckpoint decoded = RuntimeCheckpoint.decodeForRestore(bytes, "fed", 7, "saved");
        assertEquals(notifications.size(), decoded.pendingLocalNotifications.size());
        for (int i = 0; i < notifications.size(); i++) {
            assertEquals(notifications.get(i).localId, decoded.pendingLocalNotifications.get(i).localId);
            assertEquals(i + 1, decoded.pendingLocalNotifications.get(i).order);
        }
        assertNotEquals(notifications.get(1).localId, notifications.get(2).localId);
        JsonObject state = JsonParser.parseString(new String(bytes, StandardCharsets.UTF_8)).getAsJsonObject().getAsJsonObject("state");
        assertEquals("123", state.getAsJsonArray("pending_local_notifications").get(7).getAsJsonObject()
                .getAsJsonObject("payload").getAsJsonArray("statuses").get(0).getAsJsonObject().get("post_restore_handle").getAsString());
        assertThrows(IOException.class, () -> decoded.encode("fed", 7, SELF, "saved"));
        String directory = System.getProperty("gorti.local.fixtures");
        if (directory != null) { Files.createDirectories(Path.of(directory)); Files.write(Path.of(directory).resolve("java-local-" + scalar + ".json"), bytes); }
    }

    @Test void typedResultsInvokeOnlyUserNotificationsInSavedOrder() throws Exception {
        List<String> trace = new ArrayList<>();
        FederateAmbassador target = (FederateAmbassador) Proxy.newProxyInstance(getClass().getClassLoader(), new Class<?>[]{FederateAmbassador.class}, (proxy, method, args) -> { trace.add(method.getName()); return null; });
        for (LocalNotification notification : inventory()) notification.invoke(target);
        assertEquals(List.of("informAttributeOwnership", "attributeIsNotOwned", "attributeIsNotOwned", "attributeIsOwnedByRTI",
                "confirmAttributeOwnershipAcquisitionCancellation", "reportAttributeTransportationType", "reportInteractionTransportationType",
                "federationRestoreStatusResponse", "federationSaveStatusResponse", "reportFederationExecutions"), trace);
    }

    @Test void completedQueriesDoNotAccumulateHistoryBeyondQueueCapacity() throws Exception {
        AmbassadorServices services = savingServices();
        int[] invoked = {0};
        set(services, "ambassador", target((method, args) -> invoked[0]++));
        for (int i = 0; i < 9000; i++) {
            enqueue(services, LocalNotification.ownership("attributeIsNotOwned", 10, 20, 0));
            services.evokeCallback(0);
        }
        assertEquals(9000, invoked[0]);
        int retained = 0;
        for (Field field : AmbassadorServices.class.getDeclaredFields()) {
            if (field.getName().contains("Local") && java.util.Collection.class.isAssignableFrom(field.getType())) {
                field.setAccessible(true); retained += ((java.util.Collection<?>) field.get(services)).size();
            }
        }
        assertEquals(0, retained, "Completed local queries must not retain ID history");
        assertTrue(RuntimeCheckpoint.decodeForRestore(export(services), "fed", 7, "saved").pendingLocalNotifications.isEmpty());
    }

    @Test void queuedCopiesArePurgedEvenWhenBodyFailsWithoutLosingMultiplicity() throws Exception {
        AmbassadorServices services = savingServices();
        List<String> trace = new ArrayList<>();
        set(services, "ambassador", target((method, args) -> {
            trace.add(method);
            if (trace.size() == 1) throw new hla.rti1516e.exceptions.FederateInternalError("Finished local failure");
        }));
        LocalNotification first = LocalNotification.ownership("attributeIsNotOwned", 10, 20, 0);
        LocalNotification second = LocalNotification.ownership("attributeIsNotOwned", 10, 20, 0);
        enqueue(services, first); enqueue(services, first); enqueue(services, second);
        assertThrows(RTIinternalError.class, () -> services.evokeCallback(0));
        RuntimeCheckpoint pending = RuntimeCheckpoint.decodeForRestore(export(services), "fed", 7, "saved");
        assertEquals(1, pending.pendingLocalNotifications.size());
        assertEquals(second.localId, pending.pendingLocalNotifications.get(0).localId);
        assertFalse(services.evokeCallback(0));
        assertEquals(List.of("attributeIsNotOwned", "attributeIsNotOwned"), trace);
    }

    @Test void payloadAndCallbackCollectionsCannotMutateSavedResult() throws Exception {
        JsonObject row = inventory().get(4).ordered(1).encode(7);
        LocalNotification notification = LocalNotification.decode(row, 7);
        JsonObject before = notification.encode(7);
        row.getAsJsonObject("payload").getAsJsonArray("attribute_handles").add(999);
        notification.encode(7).getAsJsonObject("payload").getAsJsonArray("attribute_handles").add(998);
        notification.invoke(target((method, args) -> ((AttributeHandleSet) args[1]).clear()));
        assertEquals(before, notification.encode(7));
    }

    @Test void restoredLocalsWaitForTerminalAndPurgeRetainedConnectionCopy() throws Exception {
        AmbassadorServices services = savingServices();
        List<String> trace = new ArrayList<>();
        set(services, "ambassador", target((method, args) -> trace.add(method)));
        LocalNotification notification = inventory().get(9).ordered(1);
        Field restored = AmbassadorServices.class.getDeclaredField("restoredLocalNotifications"); restored.setAccessible(true);
        @SuppressWarnings("unchecked") java.util.Deque<LocalNotification> queue = (java.util.Deque<LocalNotification>) restored.get(services);
        queue.add(notification);
        services.evokeCallback(0); assertTrue(trace.isEmpty());
        enqueue(services, notification);
        set(services, "restoredControlsReady", true);
        assertFalse(services.evokeCallback(0));
        assertEquals(List.of("reportFederationExecutions"), trace);
        assertTrue(RuntimeCheckpoint.decodeForRestore(export(services), "fed", 7, "saved").pendingLocalNotifications.isEmpty());
    }

    @FunctionalInterface interface NotificationBody { void call(String method, Object[] args) throws Throwable; }
    private static FederateAmbassador target(NotificationBody body) {
        return (FederateAmbassador) Proxy.newProxyInstance(LocalNotificationCheckpointTest.class.getClassLoader(),
                new Class<?>[]{FederateAmbassador.class}, (proxy, method, args) -> { body.call(method.getName(), args); return null; });
    }

    @Test void strictLocalRecordsRejectUnknownKindAuthorityNullAndDuplicateParticipant() throws Exception {
        JsonObject good = inventory().get(0).ordered(1).encode(7);
        for (String mutation : List.of("kind", "ticket", "identity", "order", "scope", "null", "foreign", "fractional", "null_payload", "array_payload", "scalar_payload")) {
            JsonObject bad = good.deepCopy(); JsonObject payload = bad.getAsJsonObject("payload");
            switch (mutation) {
                case "kind": bad.addProperty("kind", "activateTransportation"); break;
                case "ticket": payload.addProperty("ticket", "old-authority"); break;
                case "identity": bad.addProperty("local_id", "A".repeat(64)); break;
                case "order": bad.addProperty("order", 0); break;
                case "scope": bad.addProperty("scope", "connection"); break;
                case "null": payload.add("owner_federate", JsonNull.INSTANCE); break;
                case "foreign": payload.addProperty("owner_federate", 123); break;
                case "fractional": payload.addProperty("attribute_handle", 1.5); break;
                case "null_payload": bad.add("payload", JsonNull.INSTANCE); break;
                case "array_payload": bad.add("payload", new JsonArray()); break;
                case "scalar_payload": bad.addProperty("payload", 1); break;
                default: throw new AssertionError(mutation);
            }
            assertThrows(IOException.class, () -> LocalNotification.decode(bad, 7), mutation);
        }
        JsonObject status = inventory().get(8).ordered(1).encode(7);
        JsonArray rows = status.getAsJsonObject("payload").getAsJsonArray("statuses"); rows.add(rows.get(0).deepCopy());
        assertThrows(IOException.class, () -> LocalNotification.decode(status, 7));
        JsonObject restore = inventory().get(7).ordered(1).encode(7);
        restore.getAsJsonObject("payload").getAsJsonArray("statuses").get(0).getAsJsonObject().addProperty("post_restore_handle", 1L << 32);
        assertThrows(IOException.class, () -> LocalNotification.decode(restore, 7));
        for (String key : List.of("name", "logical_time_implementation_name")) {
            JsonObject execution = inventory().get(9).ordered(1).encode(7);
            execution.getAsJsonObject("payload").getAsJsonArray("executions").get(0).getAsJsonObject().addProperty(key, "");
            assertThrows(IOException.class, () -> LocalNotification.decode(execution, 7));
        }
    }

    static List<LocalNotification> inventory() throws Exception {
        return List.of(LocalNotification.ownership("informAttributeOwnership", 10, 20, SELF),
                LocalNotification.ownership("attributeIsNotOwned", 10, 20, 0), LocalNotification.ownership("attributeIsNotOwned", 10, 20, 0),
                LocalNotification.ownership("attributeIsOwnedByRTI", 10, 20, 0), LocalNotification.cancelled(10, List.of(20L, 21L)),
                LocalNotification.transport("reportAttributeTransportationType", 10, 20, 0, 0, Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE),
                LocalNotification.transport("reportInteractionTransportationType", 0, 0, SELF, 30, Common.TransportationType.TRANSPORTATION_TYPE_BEST_EFFORT),
                LocalNotification.restoreStatus(Savepoint.QueryRestoreStateResponse.newBuilder().addFederateStatuses(Savepoint.FederateRestoreStatus.newBuilder()
                        .setPreRestoreHandle(SELF).setPostRestoreHandle(123).setStatusValue(1)).build()),
                LocalNotification.saveStatus(Savepoint.QuerySaveStateResponse.newBuilder().addFederateStatuses(Savepoint.FederateSaveStatus.newBuilder().setFederateHandle(SELF).setStatusValue(1)).build()),
                LocalNotification.executions(List.of(new FederationExecutionInformation("fed", TimeWire.FLOAT))));
    }

    static void enqueue(AmbassadorServices services, CallbackSink.Invocation callback) throws Exception {
        Method method = AmbassadorServices.class.getDeclaredMethod("enqueueFederateCallback", CallbackSink.Invocation.class, long.class, long.class);
        method.setAccessible(true); method.invoke(services, callback, 0L, 0L);
    }
    @Test void arbitraryLocalClosureMustFailExportInsteadOfDisappearing() throws Exception {
        AmbassadorServices services = savingServices();
        Method enqueue = AmbassadorServices.class.getDeclaredMethod("enqueueFederateCallback", CallbackSink.Invocation.class, long.class, long.class);
        enqueue.setAccessible(true); enqueue.invoke(services, (CallbackSink.Invocation) target -> { }, 0L, 0L);
        InvocationTargetException failure = assertThrows(InvocationTargetException.class, () -> export(services));
        assertInstanceOf(RTIinternalError.class, failure.getCause());
        assertTrue(failure.getCause().getMessage().contains("Uncheckpointable"));
    }

    @Test void preparedTransportationActivationMustFailExportWithoutExecutingRpc() throws Exception {
        AmbassadorServices services = savingServices();
        Method activation = TransportationServices.class.getDeclaredMethod("enqueueActivation", CallbackSink.class, Object.class,
                rti.v1.ObjectServiceGrpc.ObjectServiceBlockingStub.class, String.class, long.class, rti.v1.Object.TransportationChange.class);
        activation.setAccessible(true);
        activation.invoke(services.transportation, (CallbackSink) callback -> {
            try { enqueue(services, callback); } catch (Exception failure) { throw new AssertionError(failure); }
        }, new Object(), null, "fed", SELF, rti.v1.Object.TransportationChange.newBuilder()
                .setTicket(com.google.protobuf.ByteString.copyFrom(new byte[32])).build());
        InvocationTargetException failure = assertThrows(InvocationTargetException.class, () -> export(services));
        assertInstanceOf(RTIinternalError.class, failure.getCause());
        assertTrue(failure.getCause().getMessage().contains("Uncheckpointable"));
    }

    static AmbassadorServices savingServices() throws Exception {
        AmbassadorServices services = new AmbassadorServices();
        DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
        RuntimeCheckpoint checkpoint = new RuntimeCheckpoint(new ExchangeServices.State(new ExchangeKnowledge(), 0, 0),
                regions, Set.of(), false, false, false, false, false, 0);
        set(services, "pendingCheckpoint", checkpoint); set(services, "federation", "fed");
        set(services, "federationGeneration", 7L); set(services, "handle", (7L << 32) | 8);
        set(services, "pendingSaveLabel", "saved");
        return services;
    }

    static byte[] export(AmbassadorServices services) throws Exception {
        Method method = AmbassadorServices.class.getDeclaredMethod("checkpointBytes"); method.setAccessible(true);
        return (byte[]) method.invoke(services);
    }
    static void set(Object owner, String name, Object value) throws Exception {
        Field field = owner.getClass().getDeclaredField(name); field.setAccessible(true); field.set(owner, value);
    }
}
