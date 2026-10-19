package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import com.google.gson.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

/** Independent Java decoding of the central Go adapter's translated bytes. */
@EnabledIfSystemProperty(named = "gorti.central.runtime.fixtures", matches = ".+")
class CentralRuntimeFixtureExchangeTest {
    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    @EnabledIfSystemProperty(named = "gorti.central.local.fixtures", matches = ".+")
    void centralLocalRebindChangesOnlyLiveParticipantReferences(String scalar) throws Exception {
        Path directory = Path.of(System.getProperty("gorti.central.local.fixtures"));
        JsonObject source = JsonParser.parseString(Files.readString(directory.resolve("java-local-" + scalar + ".json"))).getAsJsonObject();
        byte[] translated = Files.readAllBytes(directory.resolve("central-java-local-" + scalar + ".json"));
        long target = (9L << 32) | 41;
        RuntimeCheckpoint decoded = RuntimeCheckpoint.decodeForRestore(translated, "fed", 9, "saved");
        assertEquals(target, decoded.persistedHandle); assertTrue(decoded.serverRecovery);
        assertEquals(10, decoded.pendingLocalNotifications.size());
        JsonObject expected = source.getAsJsonObject("state");
        for (JsonElement item : expected.getAsJsonArray("pending_local_notifications")) {
            JsonObject row = item.getAsJsonObject(), payload = row.getAsJsonObject("payload");
            switch (row.get("kind").getAsString()) {
                case "informAttributeOwnership": payload.addProperty("owner_federate", target); break;
                case "reportInteractionTransportationType": payload.addProperty("federate_handle", target); break;
                case "federationSaveStatusResponse": payload.getAsJsonArray("statuses").get(0).getAsJsonObject().addProperty("federate_handle", target); break;
                case "federationRestoreStatusResponse": payload.getAsJsonArray("statuses").get(0).getAsJsonObject().addProperty("pre_restore_handle", target); break;
                default: break;
            }
        }
        JsonObject actual = JsonParser.parseString(new String(RuntimeJson.encode(decoded, "fed", 9, target, "saved"), StandardCharsets.UTF_8)).getAsJsonObject();
        assertEquals(expected, actual.getAsJsonObject("state"));
    }
    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    @EnabledIfSystemProperty(named = "gorti.runtime.v2.fixtures", matches = ".+")
    void centralV2RebindPreservesKnownOutcomeAndStableIdentity(String scalar) throws Exception {
        Path directory = Path.of(System.getProperty("gorti.runtime.v2.fixtures"));
        JsonObject source = JsonParser.parseString(Files.readString(directory.resolve("java-v2-" + scalar + ".json"))).getAsJsonObject();
        byte[] translated = Files.readAllBytes(directory.resolve("central-java-v2-" + scalar + ".json"));
        long target = (9L << 32) | 41;
        RuntimeCheckpoint decoded = RuntimeCheckpoint.decodeForRestore(translated, "fed", 9, "saved");
        assertTrue(decoded.serverRecovery); assertEquals(target, decoded.persistedHandle);
        assertEquals(1, decoded.pendingOutcomes.size()); assertFalse(decoded.pendingOutcomes.get(0).success);
        assertEquals("IllegalStateException", decoded.pendingOutcomes.get(0).exception);
        assertEquals(CallbackInvocationDelivery.identity(decoded.pendingCallbacks.get(0)), decoded.pendingOutcomes.get(0).identity);
        JsonObject expected = source.getAsJsonObject("state");
        expected.getAsJsonArray("deleted_objects").get(0).getAsJsonObject().addProperty("sender_federate", target);
        JsonObject actual = JsonParser.parseString(new String(RuntimeJson.encode(decoded, "fed", 9, target, "saved"), StandardCharsets.UTF_8)).getAsJsonObject();
        assertEquals(expected, actual.getAsJsonObject("state"));
    }
    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    void centralRebindPreservesExactPrivateStateAndChangesOnlyFederateIdentity(String scalar) throws Exception {
        Path directory = Path.of(System.getProperty("gorti.central.runtime.fixtures"));
        JsonObject source = JsonParser.parseString(Files.readString(directory.resolve("java-" + scalar + ".json"))).getAsJsonObject();
        byte[] translated = Files.readAllBytes(directory.resolve("central-java-" + scalar + ".json"));
        long target = (9L << 32) | 41;
        RuntimeCheckpoint decoded = RuntimeCheckpoint.decodeForRestore(translated, "fed", 9, "saved");
        assertEquals(target, decoded.persistedHandle);
        decoded.validateTime(scalar);
        JsonObject roundTrip = JsonParser.parseString(new String(RuntimeJson.encode(decoded, "fed", 9, target, "saved"), StandardCharsets.UTF_8)).getAsJsonObject();
        JsonObject expected = source.getAsJsonObject("state");
        if (!expected.has("callback_recovery")) {
            expected.addProperty("callback_recovery", "server-v2"); expected.add("pending_outcomes", new JsonArray());
        }
        expected.getAsJsonArray("deleted_objects").get(0).getAsJsonObject().addProperty("sender_federate", target);
        assertEquals(expected, roundTrip.get("state"));
        JsonObject range = roundTrip.getAsJsonObject("state").getAsJsonArray("ddm_regions").get(0).getAsJsonObject()
                .getAsJsonArray("dimensions").get(0).getAsJsonObject();
        assertEquals("9223372036854775808", range.get("lower").getAsString());
        assertEquals("18446744073709551615", range.get("upper").getAsString());
        if (scalar.equals(TimeWire.INTEGER)) assertEquals("9007199254740993", roundTrip.getAsJsonObject("state")
                .getAsJsonObject("logical_time_value").get("integer64_value").getAsString());
        assertEquals(2, decoded.pendingCallbacks.stream().filter(event -> !event.getCallbackReceipt().isEmpty()).count());
        assertEquals(1, decoded.pendingCallbacks.stream().filter(event -> event.getCallbackReceipt().isEmpty()).count());
        assertThrows(java.io.IOException.class, () -> RuntimeCheckpoint.decodeForRestore(translated, "fed", 7, "saved"));
    }
}
