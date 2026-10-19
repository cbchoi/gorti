package org.gorti.rti;

import java.io.*;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;
import java.util.TreeSet;
import java.util.List;
import java.util.ArrayList;
import com.google.protobuf.Message;
import com.google.protobuf.Descriptors.FieldDescriptor;
import rti.v1.Stream;
import hla.rti1516e.LogicalTime;
import org.gorti.rti.types.TimeValues;

/** Versioned SDK state; the server binds this data to the federation save transaction. */
final class RuntimeCheckpoint {
    private static final int LEGACY_FORMAT = 0x47524a31;
    private static final int FORMAT = 0x47524a32; // GRJ2 preserves the logical-time representation.
    private static final int JOURNAL_FORMAT = 0x47524a33;
    final ExchangeServices.State exchangeState;
    final DdmServices.State ddmState;
    final Set<String> synchronizations;
    final boolean regulating, constrained, regulationPending, constrainedPending, advancePending;
    final double logicalTime;
    final LogicalTime logicalTimeValue;
    final List<Stream.FederateEvent> pendingCallbacks;
    final long persistedHandle;
    final boolean serverRecovery;
    final List<CallbackInvocationDelivery.Outcome> pendingOutcomes;
    final List<LocalNotification> pendingLocalNotifications;

    RuntimeCheckpoint(ExchangeServices.State exchangeState, DdmServices.State ddmState, Set<String> synchronizations,
            boolean regulating, boolean constrained, boolean regulationPending, boolean constrainedPending,
            boolean advancePending, double logicalTime) {
        this(exchangeState, ddmState, synchronizations, regulating, constrained, regulationPending,
                constrainedPending, advancePending, TimeValues.time(logicalTime));
    }

    RuntimeCheckpoint(ExchangeServices.State exchangeState, DdmServices.State ddmState, Set<String> synchronizations,
            boolean regulating, boolean constrained, boolean regulationPending, boolean constrainedPending,
            boolean advancePending, LogicalTime logicalTime) {
        this(exchangeState, ddmState, synchronizations, regulating, constrained, regulationPending,
                constrainedPending, advancePending, logicalTime, List.of());
    }

    RuntimeCheckpoint(ExchangeServices.State exchangeState, DdmServices.State ddmState, Set<String> synchronizations,
            boolean regulating, boolean constrained, boolean regulationPending, boolean constrainedPending,
            boolean advancePending, LogicalTime logicalTime, List<Stream.FederateEvent> pendingCallbacks) {
        this.exchangeState = exchangeState; this.ddmState = ddmState;
        this.synchronizations = new HashSet<>(synchronizations);
        this.regulating = regulating; this.constrained = constrained;
        this.regulationPending = regulationPending; this.constrainedPending = constrainedPending;
        this.advancePending = advancePending; this.logicalTimeValue = logicalTime;
        this.logicalTime = logicalTime instanceof hla.rti1516e.time.HLAinteger64Time ? Double.NaN : TimeValues.value(logicalTime);
        this.pendingCallbacks = List.copyOf(pendingCallbacks);
        this.persistedHandle = 0;
        this.serverRecovery = false;
        this.pendingOutcomes = List.of();
        this.pendingLocalNotifications = List.of();
    }

    private RuntimeCheckpoint(RuntimeCheckpoint source, List<CallbackInvocationDelivery.Outcome> outcomes, List<LocalNotification> notifications) {
        exchangeState = source.exchangeState; ddmState = source.ddmState;
        synchronizations = source.synchronizations;
        regulating = source.regulating; constrained = source.constrained;
        regulationPending = source.regulationPending; constrainedPending = source.constrainedPending;
        advancePending = source.advancePending; logicalTime = source.logicalTime;
        logicalTimeValue = source.logicalTimeValue; pendingCallbacks = source.pendingCallbacks;
        persistedHandle = source.persistedHandle; serverRecovery = true;
        pendingOutcomes = List.copyOf(outcomes);
        pendingLocalNotifications = List.copyOf(notifications);
    }

    RuntimeCheckpoint withRecovery(List<CallbackInvocationDelivery.Outcome> outcomes) {
        return new RuntimeCheckpoint(this, outcomes, pendingLocalNotifications);
    }

    RuntimeCheckpoint withLocalNotifications(List<LocalNotification> notifications) {
        return new RuntimeCheckpoint(this, pendingOutcomes, notifications);
    }

    private RuntimeCheckpoint(DataInputStream input, boolean exact, boolean journal, long persistedHandle) throws IOException {
        this.persistedHandle = persistedHandle;
        this.serverRecovery = false;
        this.pendingOutcomes = List.of();
        this.pendingLocalNotifications = List.of();
        exchangeState = ExchangeServices.State.readFrom(input, exact);
        ddmState = DdmServices.State.readFrom(input);
        synchronizations = new HashSet<>();
        int count = CheckpointIO.count(input);
        for (int i = 0; i < count; i++) if (!synchronizations.add(CheckpointIO.string(input))) throw new IOException("Duplicate synchronization label");
        int flags = input.readUnsignedByte();
        if ((flags & ~31) != 0) throw new IOException("Invalid time-policy flags");
        regulating = (flags & 1) != 0; constrained = (flags & 2) != 0;
        regulationPending = (flags & 4) != 0; constrainedPending = (flags & 8) != 0; advancePending = (flags & 16) != 0;
        if (regulating && regulationPending || constrained && constrainedPending) throw new IOException("Conflicting time-policy flags");
        logicalTimeValue = TimeWire.readCheckpoint(input, exact);
        logicalTime = logicalTimeValue instanceof hla.rti1516e.time.HLAinteger64Time ? Double.NaN : TimeValues.value(logicalTimeValue);
        List<Stream.FederateEvent> events = new ArrayList<>();
        if (journal) {
            int eventsCount = CheckpointIO.count(input);
            if (eventsCount > 16384) throw new IOException("Callback journal exceeds queue capacity");
            for (int i = 0; i < eventsCount; i++) {
                int size = input.readInt();
                if (size <= 0 || size > input.available()) throw new IOException("Invalid callback journal event length");
                byte[] bytes = new byte[size];
                input.readFully(bytes);
                Stream.FederateEvent event = Stream.FederateEvent.parseFrom(bytes);
                validateEvent(event);
                events.add(event);
            }
        }
        pendingCallbacks = List.copyOf(events);
    }

    byte[] encode(String federation, long generation, long handle, String label) throws IOException {
        if (!pendingLocalNotifications.isEmpty()) throw new IOException("Local notifications require portable JSON, not a legacy binary checkpoint");
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutputStream output = CheckpointIO.output(bytes);
        output.writeInt(pendingCallbacks.isEmpty() ? FORMAT : JOURNAL_FORMAT);
        CheckpointIO.string(output, federation); output.writeLong(generation); output.writeLong(handle);
        CheckpointIO.string(output, label);
        exchangeState.writeTo(output, true); ddmState.writeTo(output);
        output.writeInt(CheckpointIO.count(synchronizations.size()));
        for (String sync : new TreeSet<>(synchronizations)) CheckpointIO.string(output, sync);
        output.writeByte((regulating ? 1 : 0) | (constrained ? 2 : 0) | (regulationPending ? 4 : 0)
                | (constrainedPending ? 8 : 0) | (advancePending ? 16 : 0));
        TimeWire.writeCheckpoint(output, logicalTimeValue, true);
        if (!pendingCallbacks.isEmpty()) {
            if (pendingCallbacks.size() > 16384) throw new IOException("Callback journal exceeds queue capacity");
            output.writeInt(pendingCallbacks.size());
            for (Stream.FederateEvent event : pendingCallbacks) {
                validateEvent(event);
                byte[] value = event.toByteArray();
                output.writeInt(value.length); output.write(value);
            }
        }
        output.flush();
        output.write(digest(bytes.toByteArray())); output.flush();
        return bytes.toByteArray();
    }

    static RuntimeCheckpoint decode(byte[] encoded, String federation, long generation, long savedHandle, String label) throws IOException {
        return decode(encoded, federation, generation, Long.valueOf(savedHandle), label);
    }

    static RuntimeCheckpoint decodeForRestore(byte[] encoded, String federation, long generation, String label) throws IOException {
        int start = 0;
        while (start < encoded.length && (encoded[start] == ' ' || encoded[start] == '\n' || encoded[start] == '\r' || encoded[start] == '\t')) start++;
        if (start < encoded.length && encoded[start] == '{') return RuntimeJson.decode(encoded, federation, generation, label);
        return decode(encoded, federation, generation, null, label);
    }

    private static RuntimeCheckpoint decode(byte[] encoded, String federation, long generation, Long savedHandle, String label) throws IOException {
        if (encoded.length < 36 || encoded.length > CheckpointIO.MAX_BYTES) throw new IOException("Invalid Java checkpoint length");
        byte[] body = Arrays.copyOf(encoded, encoded.length - 32);
        if (!MessageDigest.isEqual(digest(body), Arrays.copyOfRange(encoded, body.length, encoded.length))) throw new IOException("Java checkpoint checksum mismatch");
        DataInputStream input = new DataInputStream(new ByteArrayInputStream(body));
        int format = input.readInt();
        if ((format != FORMAT && format != LEGACY_FORMAT && format != JOURNAL_FORMAT) || !federation.equals(CheckpointIO.string(input))
                || input.readLong() != generation)
            throw new IOException("Java checkpoint version or membership identity mismatch");
        long identity = CheckpointIO.handle(input);
        if (savedHandle != null && identity != savedHandle || !label.equals(CheckpointIO.string(input)))
            throw new IOException("Java checkpoint participant or label mismatch");
        RuntimeCheckpoint state = new RuntimeCheckpoint(input, format != LEGACY_FORMAT, format == JOURNAL_FORMAT, identity);
        if (input.available() != 0) throw new IOException("Trailing Java checkpoint data");
        return state;
    }

    void validateTime(String selected) throws IOException {
        try {
            TimeWire.checked(logicalTimeValue, selected);
            exchangeState.validateTime(selected);
            for (Stream.FederateEvent event : pendingCallbacks) validateEventTimes(event, selected);
        } catch (IllegalArgumentException invalid) {
            throw new IOException("Checkpoint logical-time factory mismatch", invalid);
        }
    }

    static boolean journalEvent(Stream.FederateEvent event) {
        if (!event.getCallbackReceipt().isEmpty() && !event.hasLocalValidationBarrier() && !event.hasHalted()
                && event.getEventCase() != Stream.FederateEvent.EventCase.EVENT_NOT_SET) return true;
        switch (event.getEventCase()) {
            case SAVE_INITIATE: case SAVE_COMPLETED: case SAVE_FAILED:
            case RESTORE_REQUEST_SUCCEEDED: case RESTORE_REQUEST_FAILED: case RESTORE_BEGUN:
            case RESTORE_INITIATE: case RESTORE_COMPLETED: case RESTORE_FAILED:
            case LOCAL_VALIDATION_BARRIER: case HALTED: case EVENT_NOT_SET: return false;
            default: return true;
        }
    }

    private static void validateEvent(Stream.FederateEvent event) throws IOException {
        if (!journalEvent(event)) throw new IOException("Invalid callback journal event");
        validateKnownFields(event);
    }

    private static void validateKnownFields(Message message) throws IOException {
        if (!message.getUnknownFields().asMap().isEmpty()) throw new IOException("Unknown callback journal field");
        for (java.util.Map.Entry<FieldDescriptor, Object> field : message.getAllFields().entrySet()) {
            if (field.getKey().getJavaType() != FieldDescriptor.JavaType.MESSAGE) continue;
            if (field.getKey().isRepeated()) {
                for (Object value : (List<?>) field.getValue()) validateKnownFields((Message) value);
            } else validateKnownFields((Message) field.getValue());
        }
    }

    private static void validateEventTimes(Message message, String selected) {
        for (FieldDescriptor field : message.getDescriptorForType().getFields()) {
            if (field.getJavaType() == FieldDescriptor.JavaType.DOUBLE
                    && message.getDescriptorForType().findFieldByName(field.getName() + "_value") != null
                    && TimeWire.hasTime(message, field.getName())) TimeWire.read(message, field.getName(), selected);
        }
        for (java.util.Map.Entry<FieldDescriptor, Object> field : message.getAllFields().entrySet()) {
            if (field.getKey().getJavaType() != FieldDescriptor.JavaType.MESSAGE) continue;
            if (field.getKey().isRepeated()) {
                for (Object value : (List<?>) field.getValue()) validateEventTimes((Message) value, selected);
            } else validateEventTimes((Message) field.getValue(), selected);
        }
    }

    private static byte[] digest(byte[] value) {
        try { return MessageDigest.getInstance("SHA-256").digest(value); }
        catch (NoSuchAlgorithmException impossible) { throw new IllegalStateException("Java requires SHA-256", impossible); }
    }
}
