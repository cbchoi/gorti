package org.gorti.rti;

import com.google.gson.*;
import com.google.gson.stream.*;
import com.google.protobuf.*;
import com.google.protobuf.Descriptors.FieldDescriptor;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.time.HLAinteger64Time;
import java.io.*;
import java.math.BigInteger;
import java.nio.ByteBuffer;
import java.nio.charset.*;
import java.security.*;
import java.util.*;
import org.gorti.rti.types.TimeValues;
import rti.v1.Stream;

/** Shared GRS1 envelope. Binary conversion reuses the reviewed legacy state validator. */
final class RuntimeJson {
    private static final Gson JSON = new GsonBuilder().disableHtmlEscaping().create();
    private RuntimeJson() { }

    static byte[] encode(RuntimeCheckpoint checkpoint, String federation, long generation, long self, String label) throws IOException {
        if (self == 0 || (int) self == 0 || self >>> 32 != generation) throw new IOException("Invalid portable participant scope");
        byte[] binary = checkpoint.withLocalNotifications(List.of()).encode(federation, generation, self, label);
        DataInputStream input = new DataInputStream(new ByteArrayInputStream(binary));
        input.readInt(); CheckpointIO.string(input); input.readLong(); input.readLong(); CheckpointIO.string(input);
        JsonObject state = new JsonObject();
        state.addProperty("federation", federation); state.addProperty("label", label);
        JsonArray known = new JsonArray();
        for (int n = CheckpointIO.count(input); n > 0; n--) known.add(readObject(input));
        state.add("known_objects", known);
        JsonArray pending = new JsonArray();
        for (int n = CheckpointIO.count(input); n > 0; n--) {
            JsonObject value = new JsonObject(); number(value, "object_handle", input.readLong());
            value.add("logical_time_value", time(TimeWire.readCheckpoint(input, true))); pending.add(value);
        }
        state.add("pending_removals", pending);
        JsonArray deleted = new JsonArray();
        for (int n = CheckpointIO.count(input); n > 0; n--) {
            long producer = input.readLong(), retraction = input.readLong();
            JsonObject value = readObject(input);
            number(value, "sender_federate", producer); number(value, "retraction_handle", retraction);
            value.add("logical_time_value", time(TimeWire.readCheckpoint(input, true)));
            value.addProperty("expires_at_grant", input.readBoolean()); deleted.add(value);
        }
        state.add("deleted_objects", deleted);
        number(state, "cleared_retraction", input.readLong()); number(state, "last_issued_retraction", input.readLong());
        JsonArray regions = new JsonArray();
        for (int n = CheckpointIO.count(input); n > 0; n--) {
            JsonObject region = new JsonObject(); number(region, "region_handle", input.readLong());
            JsonArray dimensions = new JsonArray();
            for (int d = CheckpointIO.count(input); d > 0; d--) {
                JsonObject range = new JsonObject(); number(range, "dimension_handle", input.readLong());
                number(range, "lower", input.readLong()); number(range, "upper", input.readLong()); dimensions.add(range);
            }
            region.add("dimensions", dimensions); regions.add(region);
        }
        state.add("ddm_regions", regions);
        JsonArray synchronizations = new JsonArray();
        for (int n = CheckpointIO.count(input); n > 0; n--) synchronizations.add(CheckpointIO.string(input));
        state.add("synchronizations", synchronizations);
        state.addProperty("time_policy_flags", input.readUnsignedByte());
        state.add("logical_time_value", time(TimeWire.readCheckpoint(input, true)));
        JsonArray callbacks = new JsonArray();
        for (Stream.FederateEvent event : checkpoint.pendingCallbacks) {
            callbacks.add(message(event));
        }
        state.add("pending_callbacks", callbacks);
        state.addProperty("callback_recovery", "server-v2");
        JsonArray outcomes = new JsonArray();
        Set<String> identities = new HashSet<>();
        for (CallbackInvocationDelivery.Outcome outcome : checkpoint.pendingOutcomes) {
            if (!identities.add(outcome.identity)) throw new IOException("Duplicate pending outcome identity");
            JsonObject value = new JsonObject();
            value.addProperty("identity", outcome.identity); value.addProperty("success", outcome.success);
            value.addProperty("exception", outcome.exception); outcomes.add(value);
        }
        state.add("pending_outcomes", outcomes);
        if (!checkpoint.pendingLocalNotifications.isEmpty()) {
            JsonArray notifications = new JsonArray(); Set<String> localIds = new HashSet<>(); long previous = 0;
            for (LocalNotification notification : checkpoint.pendingLocalNotifications) {
                if (!localIds.add(notification.localId) || Long.compareUnsigned(notification.order, previous) <= 0)
                    throw new IOException("Duplicate or unordered local callback record");
                notifications.add(notification.encode(generation)); previous = notification.order;
            }
            state.add("pending_local_notifications", notifications);
        }
        String scalar = checkpoint.logicalTimeValue instanceof HLAinteger64Time ? TimeWire.INTEGER : TimeWire.FLOAT;
        checkpoint.validateTime(scalar);
        JsonObject envelope = new JsonObject();
        envelope.addProperty("magic", "GRS1"); envelope.addProperty("version", 1);
        envelope.addProperty("language", "java"); envelope.addProperty("scalar", scalar);
        number(envelope, "generation", generation); number(envelope, "self", self);
        envelope.add("state", state);
        envelope.addProperty("checksum", checksum(scalar, generation, self, JSON.toJson(state)));
        validateCollections(envelope, 0);
        byte[] encoded = JSON.toJson(envelope).getBytes(StandardCharsets.UTF_8);
        if (encoded.length > CheckpointIO.MAX_BYTES) throw new IOException("Portable checkpoint exceeds 1 MiB");
        return encoded;
    }

    static RuntimeCheckpoint decode(byte[] encoded, String federation, long generation, String label) throws IOException {
        if (encoded.length == 0 || encoded.length > CheckpointIO.MAX_BYTES) throw new IOException("Invalid portable checkpoint size");
        try {
            String source = StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(encoded)).toString();
            OneCharReader characters = new OneCharReader(source);
            JsonReader reader = new JsonReader(characters); reader.setStrictness(Strictness.STRICT); reader.setNestingLimit(64);
            JsonObject envelope = new JsonObject(); String rawState = null;
            reader.beginObject();
            while (reader.hasNext()) {
                String name = reader.nextName();
                if (envelope.has(name)) throw new IOException("Duplicate envelope field");
                if (name.equals("state")) {
                    if (reader.peek() != JsonToken.BEGIN_OBJECT) throw new IOException("State must be an object");
                    int start = characters.position - 1;
                    envelope.add(name, parse(reader));
                    rawState = compact(source.substring(start, characters.position));
                } else envelope.add(name, parse(reader));
            }
            reader.endObject();
            if (reader.peek() != JsonToken.END_DOCUMENT) throw new IOException("Trailing portable checkpoint data");
            fields(envelope, "magic", "version", "language", "scalar", "generation", "self", "state", "checksum");
            if (!string(envelope, "magic").equals("GRS1") || unsigned(envelope, "version") != 1
                    || !string(envelope, "language").equals("java") || unsigned(envelope, "generation") != generation)
                throw new IOException("Portable checkpoint format or identity mismatch");
            String scalar = string(envelope, "scalar");
            if (!scalar.equals(TimeWire.FLOAT) && !scalar.equals(TimeWire.INTEGER)) throw new IOException("Unsupported portable time factory");
            long self = unsigned(envelope, "self");
            if (self == 0 || (int) self == 0 || self >>> 32 != generation) throw new IOException("Invalid portable participant");
            if (!MessageDigest.isEqual(checksum(scalar, generation, self, rawState).getBytes(StandardCharsets.US_ASCII),
                    string(envelope, "checksum").getBytes(StandardCharsets.US_ASCII))) throw new IOException("Portable checksum mismatch");
            JsonObject state = envelope.getAsJsonObject("state");
            boolean recovery = state.has("callback_recovery") || state.has("pending_outcomes");
            Set<String> expected = new HashSet<>(Arrays.asList("federation", "label", "known_objects", "pending_removals", "deleted_objects", "cleared_retraction",
                    "last_issued_retraction", "ddm_regions", "synchronizations", "time_policy_flags", "logical_time_value", "pending_callbacks"));
            List<CallbackInvocationDelivery.Outcome> outcomes = new ArrayList<>();
            List<LocalNotification> notifications = new ArrayList<>();
            if (state.has("pending_local_notifications")) {
                if (!recovery) throw new IOException("Local notifications require server-v2 runtime");
                expected.add("pending_local_notifications"); Set<String> localIds = new HashSet<>(); long previous = 0;
                for (JsonElement item : array(state, "pending_local_notifications")) {
                    LocalNotification notification = LocalNotification.decode(item.getAsJsonObject(), generation);
                    if (!localIds.add(notification.localId) || Long.compareUnsigned(notification.order, previous) <= 0)
                        throw new IOException("Duplicate or unordered local callback record");
                    notifications.add(notification); previous = notification.order;
                }
            }
            if (recovery) {
                expected.add("callback_recovery"); expected.add("pending_outcomes");
                if (!string(state, "callback_recovery").equals("server-v2")) throw new IOException("Unsupported callback recovery authority");
                Set<String> identities = new HashSet<>();
                for (JsonElement item : array(state, "pending_outcomes")) {
                    JsonObject value = item.getAsJsonObject(); fields(value, "identity", "success", "exception");
                    CallbackInvocationDelivery.Outcome outcome = new CallbackInvocationDelivery.Outcome(string(value, "identity"), bool(value, "success"), string(value, "exception"));
                    if (!identities.add(outcome.identity)) throw new IOException("Duplicate pending outcome identity");
                    outcomes.add(outcome);
                }
            }
            if (!state.keySet().equals(expected)) throw new IOException("Unknown or missing portable runtime field");
            if (!string(state, "federation").equals(federation) || !string(state, "label").equals(label)) throw new IOException("Portable federation or label mismatch");
            ByteArrayOutputStream bytes = new ByteArrayOutputStream(); DataOutputStream output = CheckpointIO.output(bytes);
            JsonArray events = array(state, "pending_callbacks");
            output.writeInt(events.size() == 0 ? 0x47524a32 : 0x47524a33);
            CheckpointIO.string(output, federation); output.writeLong(generation); output.writeLong(self); CheckpointIO.string(output, label);
            JsonArray known = array(state, "known_objects"); output.writeInt(known.size());
            for (JsonElement value : known) { JsonObject object = value.getAsJsonObject(); fields(object, "object_handle", "class_handle", "name"); writeObject(output, object); }
            JsonArray pending = array(state, "pending_removals"); output.writeInt(pending.size());
            for (JsonElement value : pending) {
                JsonObject object = value.getAsJsonObject(); fields(object, "object_handle", "logical_time_value");
                output.writeLong(unsigned(object, "object_handle")); writeTime(output, object.getAsJsonObject("logical_time_value"), scalar);
            }
            JsonArray deleted = array(state, "deleted_objects"); output.writeInt(deleted.size());
            for (JsonElement value : deleted) {
                JsonObject object = value.getAsJsonObject(); fields(object, "sender_federate", "retraction_handle", "object_handle", "class_handle", "name", "logical_time_value", "expires_at_grant");
                output.writeLong(unsigned(object, "sender_federate")); output.writeLong(unsigned(object, "retraction_handle")); writeObject(output, object);
                writeTime(output, object.getAsJsonObject("logical_time_value"), scalar); output.writeBoolean(bool(object, "expires_at_grant"));
            }
            output.writeLong(unsigned(state, "cleared_retraction")); output.writeLong(unsigned(state, "last_issued_retraction"));
            JsonArray regions = array(state, "ddm_regions"); output.writeInt(regions.size());
            for (JsonElement value : regions) {
                JsonObject region = value.getAsJsonObject(); fields(region, "region_handle", "dimensions"); output.writeLong(unsigned(region, "region_handle"));
                JsonArray ranges = array(region, "dimensions"); output.writeInt(ranges.size());
                for (JsonElement element : ranges) {
                    JsonObject range = element.getAsJsonObject(); fields(range, "dimension_handle", "lower", "upper");
                    output.writeLong(unsigned(range, "dimension_handle")); output.writeLong(unsigned(range, "lower")); output.writeLong(unsigned(range, "upper"));
                }
            }
            JsonArray sync = array(state, "synchronizations"); output.writeInt(sync.size());
            for (JsonElement value : sync) { if (!value.isJsonPrimitive() || !value.getAsJsonPrimitive().isString()) throw new IOException("Invalid synchronization label"); CheckpointIO.string(output, value.getAsString()); }
            long flags = unsigned(state, "time_policy_flags"); if (Long.compareUnsigned(flags, 31) > 0) throw new IOException("Invalid time policy"); output.writeByte((int) flags);
            writeTime(output, state.getAsJsonObject("logical_time_value"), scalar);
            if (events.size() != 0) {
                output.writeInt(events.size());
                for (JsonElement value : events) {
                    Stream.FederateEvent.Builder event = Stream.FederateEvent.newBuilder(); readMessage(value.getAsJsonObject(), event);
                    byte[] eventBytes = event.build().toByteArray(); output.writeInt(eventBytes.length); output.write(eventBytes);
                }
            }
            output.flush(); output.write(digest(bytes.toByteArray())); output.flush();
            RuntimeCheckpoint result = RuntimeCheckpoint.decode(bytes.toByteArray(), federation, generation, self, label);
            result.validateTime(scalar); return recovery ? result.withRecovery(outcomes).withLocalNotifications(notifications) : result;
        } catch (RuntimeException invalid) {
            throw new IOException("Invalid portable Java runtime state", invalid);
        }
    }

    private static JsonObject readObject(DataInput input) throws IOException {
        JsonObject object = new JsonObject(); number(object, "object_handle", input.readLong()); number(object, "class_handle", input.readLong());
        object.addProperty("name", CheckpointIO.string(input)); return object;
    }
    private static void writeObject(DataOutput output, JsonObject object) throws IOException {
        output.writeLong(unsigned(object, "object_handle")); output.writeLong(unsigned(object, "class_handle")); CheckpointIO.string(output, string(object, "name"));
    }
    private static JsonObject time(LogicalTime value) {
        JsonObject time = new JsonObject();
        if (value instanceof HLAinteger64Time) time.addProperty("integer64_value", Long.toString(((HLAinteger64Time)value).getValue()));
        else time.addProperty("float64_value", TimeValues.value(value)); return time;
    }
    private static void writeTime(DataOutput output, JsonObject value, String scalar) throws IOException {
        if (scalar.equals(TimeWire.INTEGER)) {
            fields(value, "integer64_value"); String number = string(value, "integer64_value");
            if (!number.matches("0|[1-9][0-9]*")) throw new IOException("Invalid Integer64 time");
            long exact = Long.parseLong(number); output.writeByte(2); output.writeLong(exact);
        } else {
            fields(value, "float64_value"); JsonPrimitive number = value.getAsJsonPrimitive("float64_value");
            if (!number.isNumber()) throw new IOException("Invalid Float64 time");
            double exact = number.getAsDouble(); TimeValues.time(exact); output.writeByte(1); output.writeDouble(exact);
        }
    }
    private static void number(JsonObject target, String key, long value) { target.addProperty(key, new BigInteger(Long.toUnsignedString(value))); }
    static long unsigned(JsonObject object, String key) throws IOException {
        JsonPrimitive number = object.getAsJsonPrimitive(key);
        if (number == null || !number.isNumber() || !number.getAsString().matches("0|[1-9][0-9]*")) throw new IOException("Invalid unsigned field: " + key);
        return Long.parseUnsignedLong(number.getAsString());
    }
    static String string(JsonObject object, String key) throws IOException {
        JsonPrimitive value = object.getAsJsonPrimitive(key);
        if (value == null || !value.isString()) throw new IOException("Invalid string: " + key); return value.getAsString();
    }
    private static boolean bool(JsonObject object, String key) throws IOException {
        JsonPrimitive value = object.getAsJsonPrimitive(key);
        if (value == null || !value.isBoolean()) throw new IOException("Invalid boolean: " + key); return value.getAsBoolean();
    }
    static JsonArray array(JsonObject object, String key) throws IOException {
        JsonArray values = object.getAsJsonArray(key); if (values == null) throw new IOException("Missing array: " + key);
        CheckpointIO.count(values.size()); return values;
    }
    static void fields(JsonObject object, String... expected) throws IOException {
        if (object == null || !object.keySet().equals(new HashSet<>(Arrays.asList(expected)))) throw new IOException("Unknown or missing portable runtime field");
    }
    private static String checksum(String scalar, long generation, long self, String state) {
        byte[] hash = digest(("GRS1\n1\njava\n" + scalar + "\n" + Long.toUnsignedString(generation) + "\n" + Long.toUnsignedString(self) + "\n" + state).getBytes(StandardCharsets.UTF_8));
        StringBuilder result = new StringBuilder(64); for (byte value : hash) result.append(String.format(Locale.ROOT, "%02x", value & 255)); return result.toString();
    }
    private static byte[] digest(byte[] value) {
        try { return MessageDigest.getInstance("SHA-256").digest(value); }
        catch (NoSuchAlgorithmException impossible) { throw new IllegalStateException(impossible); }
    }
    private static JsonElement parse(JsonReader reader) throws IOException {
        switch (reader.peek()) {
            case BEGIN_OBJECT:
                JsonObject object = new JsonObject(); reader.beginObject();
                while (reader.hasNext()) { String name = reader.nextName(); if (object.has(name)) throw new IOException("Duplicate runtime field"); if (object.size() >= 8192) throw new IOException("Runtime object exceeds limit"); object.add(name, parse(reader)); }
                reader.endObject(); return object;
            case BEGIN_ARRAY:
                JsonArray array = new JsonArray(); reader.beginArray();
                while (reader.hasNext()) { if (array.size() >= 8192) throw new IOException("Runtime array exceeds limit"); array.add(parse(reader)); }
                reader.endArray(); return array;
            case STRING: return new JsonPrimitive(reader.nextString());
            case NUMBER: return JsonParser.parseString(reader.nextString());
            case BOOLEAN: return new JsonPrimitive(reader.nextBoolean());
            case NULL: reader.nextNull(); return JsonNull.INSTANCE;
            default: throw new IOException("Invalid runtime JSON token");
        }
    }
    private static void validateCollections(JsonElement value, int depth) throws IOException {
        if (depth > 64) throw new IOException("Portable checkpoint nesting limit");
        if (value.isJsonObject()) {
            if (value.getAsJsonObject().size() > 8192) throw new IOException("Portable object exceeds limit");
            for (JsonElement child : value.getAsJsonObject().asMap().values()) validateCollections(child, depth + 1);
        } else if (value.isJsonArray()) {
            if (value.getAsJsonArray().size() > 8192) throw new IOException("Portable array exceeds limit");
            for (JsonElement child : value.getAsJsonArray()) validateCollections(child, depth + 1);
        }
    }
    private static String compact(String source) {
        StringBuilder value = new StringBuilder(); boolean quoted = false, escaped = false;
        for (int i = 0; i < source.length(); i++) {
            char c = source.charAt(i);
            if (quoted || c != ' ' && c != '\t' && c != '\r' && c != '\n') value.append(c);
            if (escaped) escaped = false; else if (quoted && c == '\\') escaped = true; else if (c == '"') quoted = !quoted;
        }
        return value.toString();
    }
    private static final class OneCharReader extends Reader {
        final String source; int position;
        OneCharReader(String source) { this.source = source; }
        @Override public int read(char[] buffer, int offset, int length) {
            if (length == 0) return 0; if (position == source.length()) return -1;
            buffer[offset] = source.charAt(position++); return 1;
        }
        @Override public void close() { }
    }

    private static JsonObject message(Message message) throws IOException {
        if (!message.getUnknownFields().asMap().isEmpty()) throw new IOException("Unknown protobuf runtime fields");
        JsonObject json = new JsonObject();
        for (Map.Entry<FieldDescriptor, Object> entry : message.getAllFields().entrySet()) {
            FieldDescriptor field = entry.getKey();
            if (field.isMapField()) {
                JsonObject values = new JsonObject();
                FieldDescriptor key = field.getMessageType().findFieldByName("key"), value = field.getMessageType().findFieldByName("value");
                for (Object item : (List<?>)entry.getValue()) {
                    Message pair = (Message)item;
                    values.add(protoValue(key, pair.getField(key)).getAsString(), protoValue(value, pair.getField(value)));
                }
                json.add(field.getName(), values);
            } else if (field.isRepeated()) { JsonArray values = new JsonArray(); for (Object value : (List<?>)entry.getValue()) values.add(protoValue(field, value)); json.add(field.getName(), values); }
            else json.add(field.getName(), protoValue(field, entry.getValue()));
        }
        return json;
    }
    private static JsonElement protoValue(FieldDescriptor field, Object value) throws IOException {
        switch (field.getJavaType()) {
            case MESSAGE: return message((Message)value);
            case BYTE_STRING: return new JsonPrimitive(Base64.getEncoder().encodeToString(((ByteString)value).toByteArray()));
            case ENUM: return new JsonPrimitive(((Descriptors.EnumValueDescriptor)value).getName());
            case LONG:
                return new JsonPrimitive(field.getType() == FieldDescriptor.Type.UINT64 || field.getType() == FieldDescriptor.Type.FIXED64
                        ? Long.toUnsignedString((Long)value) : Long.toString((Long)value));
            case BOOLEAN: return new JsonPrimitive((Boolean)value);
            case STRING: return new JsonPrimitive((String)value);
            default: return new JsonPrimitive((Number)value);
        }
    }
    private static void readMessage(JsonObject json, Message.Builder builder) throws IOException {
        Set<Descriptors.OneofDescriptor> selected = new HashSet<>();
        for (Map.Entry<String, JsonElement> entry : json.entrySet()) {
            FieldDescriptor field = builder.getDescriptorForType().findFieldByName(entry.getKey());
            if (field == null || field.getContainingOneof() != null && !selected.add(field.getContainingOneof())) throw new IOException("Unknown or conflicting protobuf runtime field");
            if (field.isMapField()) {
                FieldDescriptor key = field.getMessageType().findFieldByName("key"), item = field.getMessageType().findFieldByName("value");
                JsonObject values = entry.getValue().getAsJsonObject(); CheckpointIO.count(values.size());
                for (Map.Entry<String, JsonElement> value : values.entrySet()) {
                    Message.Builder pair = builder.newBuilderForField(field);
                    pair.setField(key, readProtoValue(key, new JsonPrimitive(value.getKey()), pair));
                    pair.setField(item, readProtoValue(item, value.getValue(), pair)); builder.addRepeatedField(field, pair.build());
                }
            } else if (field.isRepeated()) {
                JsonArray values = entry.getValue().getAsJsonArray(); CheckpointIO.count(values.size());
                for (JsonElement value : values) builder.addRepeatedField(field, readProtoValue(field, value, builder));
            } else builder.setField(field, readProtoValue(field, entry.getValue(), builder));
        }
    }
    private static Object readProtoValue(FieldDescriptor field, JsonElement value, Message.Builder parent) throws IOException {
        switch (field.getJavaType()) {
            case MESSAGE: Message.Builder child = parent.newBuilderForField(field); readMessage(value.getAsJsonObject(), child); return child.build();
            case BYTE_STRING:
                if (!value.getAsJsonPrimitive().isString()) throw new IOException("Invalid protobuf bytes");
                return ByteString.copyFrom(Base64.getDecoder().decode(value.getAsString()));
            case ENUM:
                Descriptors.EnumValueDescriptor enumeration = field.getEnumType().findValueByName(value.getAsString());
                if (enumeration == null) throw new IOException("Unknown protobuf runtime enum"); return enumeration;
            case LONG:
                String integer = value.getAsString();
                return field.getType() == FieldDescriptor.Type.UINT64 || field.getType() == FieldDescriptor.Type.FIXED64
                        ? Long.parseUnsignedLong(integer) : Long.parseLong(integer);
            case INT: return value.getAsBigDecimal().intValueExact();
            case DOUBLE: return value.getAsDouble();
            case FLOAT: return value.getAsFloat();
            case BOOLEAN: if (!value.getAsJsonPrimitive().isBoolean()) throw new IOException("Invalid protobuf boolean"); return value.getAsBoolean();
            case STRING: if (!value.getAsJsonPrimitive().isString()) throw new IOException("Invalid protobuf string"); return value.getAsString();
            default: throw new IOException("Unsupported protobuf runtime field");
        }
    }
}
