package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import com.google.gson.*;
import com.google.protobuf.ByteString;
import hla.rti1516e.LogicalTime;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.MessageDigest;
import java.util.*;
import org.gorti.rti.types.Integer64TimeFactory;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;
import rti.v1.Stream;

class RuntimeJsonTest {
    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    void v2ControlsAndFinishedOutcomesRoundTripWhileLegacyRemainsReadable(String scalar) throws Exception {
        Stream.FederateEvent control = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFrom(new byte[32]))
                .setSyncAnnounced(Stream.SynchronizationPointAnnounced.newBuilder().setLabel("control")).build();
        RuntimeCheckpoint base = sample(scalar);
        RuntimeCheckpoint checkpoint = new RuntimeCheckpoint(base.exchangeState, base.ddmState, base.synchronizations,
                base.regulating, base.constrained, base.regulationPending, base.constrainedPending, base.advancePending,
                base.logicalTimeValue, List.of(control)).withRecovery(List.of(
                    new CallbackInvocationDelivery.Outcome(CallbackInvocationDelivery.identity(control), false, "IllegalStateException")));
        byte[] bytes = RuntimeJson.encode(checkpoint, "fed", 7, SELF, "saved");
        RuntimeCheckpoint copy = RuntimeJson.decode(bytes, "fed", 7, "saved");
        assertTrue(copy.serverRecovery); assertEquals(List.of(control), copy.pendingCallbacks);
        assertEquals(checkpoint.pendingOutcomes.get(0).identity, copy.pendingOutcomes.get(0).identity);
        assertFalse(copy.pendingOutcomes.get(0).success);
        String fixtureDirectory = System.getProperty("gorti.runtime.v2.fixtures");
        if (fixtureDirectory != null) {
            Path directory = Path.of(fixtureDirectory); Files.createDirectories(directory);
            Files.write(directory.resolve("java-v2-" + scalar + ".json"), bytes);
        }
        JsonObject envelope = JsonParser.parseString(new String(bytes, StandardCharsets.UTF_8)).getAsJsonObject();
        envelope.getAsJsonObject("state").remove("callback_recovery");
        envelope.getAsJsonObject("state").remove("pending_outcomes"); resign(envelope);
        assertFalse(RuntimeJson.decode(envelope.toString().getBytes(StandardCharsets.UTF_8), "fed", 7, "saved").serverRecovery);
    }

    @Test void invalidOutcomeFieldsAndUnsupportedCapabilityAreRejectedAtomically() throws Exception {
        for (String mutation : List.of("capability", "missing", "identity", "success", "duplicate", "unknown")) {
            JsonObject envelope = JsonParser.parseString(new String(RuntimeJson.encode(sample(TimeWire.FLOAT), "fed", 7, SELF, "saved"), StandardCharsets.UTF_8)).getAsJsonObject();
            JsonObject state = envelope.getAsJsonObject("state");
            JsonObject outcome = new JsonObject(); outcome.addProperty("identity", "0".repeat(64));
            outcome.addProperty("success", true); outcome.addProperty("exception", "");
            state.getAsJsonArray("pending_outcomes").add(outcome);
            switch (mutation) {
                case "capability": state.addProperty("callback_recovery", "server-v3"); break;
                case "missing": state.remove("pending_outcomes"); break;
                case "identity": outcome.addProperty("identity", "A".repeat(64)); break;
                case "success": outcome.addProperty("exception", "unexpected"); break;
                case "duplicate": state.getAsJsonArray("pending_outcomes").add(outcome.deepCopy()); break;
                case "unknown": outcome.addProperty("extra", true); break;
                default: throw new AssertionError(mutation);
            }
            resign(envelope); assertInvalid(envelope.toString());
        }
    }

    @Test void malformedLocalRowsFailAsCheckedPortableStagingErrors() throws Exception {
        for (String mutation : List.of("null_array", "object_array", "null_row", "scalar_row", "array_row", "null_payload", "array_payload", "scalar_payload", "duplicate", "order", "legacy")) {
            RuntimeCheckpoint checkpoint = sample(TimeWire.FLOAT).withLocalNotifications(List.of(LocalNotificationCheckpointTest.inventory().get(0).ordered(1)));
            JsonObject envelope = JsonParser.parseString(new String(RuntimeJson.encode(checkpoint, "fed", 7, SELF, "saved"), StandardCharsets.UTF_8)).getAsJsonObject();
            JsonObject state = envelope.getAsJsonObject("state");
            JsonArray rows = state.getAsJsonArray("pending_local_notifications");
            switch (mutation) {
                case "null_array": state.add("pending_local_notifications", JsonNull.INSTANCE); break;
                case "object_array": state.add("pending_local_notifications", new JsonObject()); break;
                case "null_row": rows.set(0, JsonNull.INSTANCE); break;
                case "scalar_row": rows.set(0, new JsonPrimitive(1)); break;
                case "array_row": rows.set(0, new JsonArray()); break;
                case "null_payload": rows.get(0).getAsJsonObject().add("payload", JsonNull.INSTANCE); break;
                case "array_payload": rows.get(0).getAsJsonObject().add("payload", new JsonArray()); break;
                case "scalar_payload": rows.get(0).getAsJsonObject().addProperty("payload", 1); break;
                case "duplicate": rows.add(rows.get(0).deepCopy()); break;
                case "order": rows.get(0).getAsJsonObject().addProperty("order", 0); break;
                case "legacy": state.remove("callback_recovery"); state.remove("pending_outcomes"); break;
                default: throw new AssertionError(mutation);
            }
            resign(envelope); assertInvalid(envelope.toString());
        }
    }
    private static final long SELF = (7L << 32) | 8;
    private RuntimeCheckpoint sample(String scalar) throws Exception {
        LogicalTime time = scalar.equals(TimeWire.INTEGER) ? new Integer64TimeFactory().makeTime((1L << 53) + 1) : TimeValues.time(2.5);
        ExchangeKnowledge knowledge = new ExchangeKnowledge();
        knowledge.discovered(10, 20, "known"); knowledge.discovered(11, 20, "removed");
        knowledge.deleted(11, SELF, 3, time);
        ByteArrayOutputStream ranges = new ByteArrayOutputStream(); DataOutputStream output = new DataOutputStream(ranges);
        output.writeInt(1); output.writeLong(30); output.writeInt(1); output.writeLong(40); output.writeLong(Long.MIN_VALUE); output.writeLong(-1);
        DdmServices.State ddm = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(ranges.toByteArray())));
        Stream.FederateEvent grant = Stream.FederateEvent.newBuilder()
                .setGrant(TimeWire.write(Stream.TimeAdvanceGrant.newBuilder(), "logical_time", time, scalar)).build();
        Stream.FederateEvent data = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFromUtf8("obsolete-ticket"))
                .setDiscover(Stream.DiscoverObjectInstance.newBuilder().setObjectHandle(12).setObjectClassHandle(20).setObjectName("data")).build();
        Stream.FederateEvent reflection = Stream.FederateEvent.newBuilder().setCallbackReceipt(ByteString.copyFromUtf8("old-map-ticket"))
                .setReflect(Stream.ReflectAttributeValues.newBuilder().setObjectHandle(10).putAttributes(-1L, ByteString.copyFrom(new byte[]{0, 1, 2}))).build();
        return new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 1, 3), ddm, Set.of("sync"),
                true, false, false, false, true, time, List.of(grant, data, reflection));
    }

    @ParameterizedTest @ValueSource(strings = {TimeWire.FLOAT, TimeWire.INTEGER})
    void sharedEnvelopeRoundTripsPrivateStateAndProducesCentralAdapterFixtures(String scalar) throws Exception {
        RuntimeCheckpoint state = sample(scalar);
        byte[] encoded = RuntimeJson.encode(state, "fed", 7, SELF, "saved");
        RuntimeCheckpoint copy = RuntimeCheckpoint.decodeForRestore(encoded, "fed", 7, "saved");
        assertEquals(state.logicalTimeValue, copy.logicalTimeValue);
        assertArrayEquals(state.encode("fed", 7, SELF, "saved"), copy.encode("fed", 7, SELF, "saved"));
        assertEquals(SELF, copy.persistedHandle);
        String fixtureDirectory = System.getProperty("gorti.runtime.fixtures");
        if (fixtureDirectory != null) {
            Path directory = Path.of(fixtureDirectory); Files.createDirectories(directory);
            Files.write(directory.resolve("java-" + scalar + ".json"), encoded);
        }
    }

    @Test void rejectsUnknownDuplicateChecksumAndWrongIdentityBeforeReturningState() throws Exception {
        byte[] encoded = RuntimeJson.encode(sample(TimeWire.INTEGER), "fed", 7, SELF, "saved");
        String json = new String(encoded, StandardCharsets.UTF_8);
        assertInvalid(json.replace("\"version\":1", "\"version\":1,\"version\":1"));
        assertInvalid(json.replace("\"language\":\"java\"", "\"language\":\"other\""));
        assertInvalid(json.replace("\"known\"", "\"corrupt\""));
        assertInvalid(json + "{}");
        JsonObject envelope = JsonParser.parseString(json).getAsJsonObject();
        envelope.getAsJsonObject("state").addProperty("unexpected", true); resign(envelope);
        assertInvalid(envelope.toString());
        assertThrows(IOException.class, () -> RuntimeJson.decode(encoded, "fed", 9, "saved"));
        assertThrows(IOException.class, () -> RuntimeJson.decode(encoded, "other", 7, "saved"));
        assertThrows(IOException.class, () -> RuntimeJson.decode(encoded, "fed", 7, "other"));
    }

    @Test void rejectsMixedScalarsAndFractionalOrOverflowingIdentifiersWithValidChecksum() throws Exception {
        for (String mutation : List.of("mixed", "fractional", "overflow", "policy", "oneof")) {
            JsonObject envelope = JsonParser.parseString(new String(RuntimeJson.encode(sample(TimeWire.INTEGER), "fed", 7, SELF, "saved"), StandardCharsets.UTF_8)).getAsJsonObject();
            JsonObject state = envelope.getAsJsonObject("state");
            switch (mutation) {
                case "mixed": state.add("logical_time_value", JsonParser.parseString("{\"float64_value\":2}")); break;
                case "fractional": state.addProperty("last_issued_retraction", 3.5); break;
                case "overflow": state.addProperty("last_issued_retraction", new java.math.BigInteger("18446744073709551616")); break;
                case "policy": state.addProperty("time_policy_flags", new java.math.BigInteger("9223372036854775808")); break;
                case "oneof": state.getAsJsonArray("pending_callbacks").get(0).getAsJsonObject().add("discover", new JsonObject()); break;
                default: throw new AssertionError(mutation);
            }
            resign(envelope); assertInvalid(envelope.toString());
        }
    }

    @Test void checksumUsesOriginalStateEscapesAndIgnoresOnlyInsignificantWhitespace() throws Exception {
        String json = new String(RuntimeJson.encode(sample(TimeWire.FLOAT), "fed", 7, SELF, "saved"), StandardCharsets.UTF_8);
        JsonObject envelope = JsonParser.parseString(json).getAsJsonObject();
        String state = envelope.get("state").toString().replace("\"known\"", "\"\\u006bnown\"");
        envelope.addProperty("checksum", hash(header(envelope) + state));
        String escaped = envelope.toString();
        int start = escaped.indexOf("\"state\":") + 8, end = escaped.lastIndexOf(",\"checksum\":");
        escaped = escaped.substring(0, start) + state + escaped.substring(end);
        assertEquals(TimeValues.time(2.5), RuntimeJson.decode(escaped.getBytes(StandardCharsets.UTF_8), "fed", 7, "saved").logicalTimeValue);
        String pretty = escaped.replace("\"state\":{", "\"state\": {\n ").replace("\"known_objects\":", "\n\"known_objects\": ");
        assertEquals(TimeValues.time(2.5), RuntimeJson.decode(pretty.getBytes(StandardCharsets.UTF_8), "fed", 7, "saved").logicalTimeValue);
    }

    private void assertInvalid(String json) { assertThrows(IOException.class, () -> RuntimeJson.decode(json.getBytes(StandardCharsets.UTF_8), "fed", 7, "saved")); }
    private void resign(JsonObject envelope) throws Exception { envelope.addProperty("checksum", hash(header(envelope) + envelope.get("state"))); }
    private String header(JsonObject envelope) { return "GRS1\n1\njava\n" + envelope.get("scalar").getAsString() + "\n7\n" + SELF + "\n"; }
    private String hash(String source) throws Exception {
        StringBuilder result = new StringBuilder();
        for (byte value : MessageDigest.getInstance("SHA-256").digest(source.getBytes(StandardCharsets.UTF_8))) result.append(String.format("%02x", value & 255));
        return result.toString();
    }
}
