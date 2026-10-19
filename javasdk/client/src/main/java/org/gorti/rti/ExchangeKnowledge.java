package org.gorti.rti;

import hla.rti1516e.exceptions.ObjectInstanceNotKnown;
import java.util.HashMap;
import java.util.Map;
import java.util.Objects;
import java.io.DataInput;
import java.io.DataOutput;
import java.io.IOException;
import java.util.TreeMap;
import hla.rti1516e.LogicalTime;
import org.gorti.rti.types.TimeValues;

/** Only actual callback delivery and successful local operations change this view. */
final class ExchangeKnowledge {
    private final Map<Long, KnownObject> objects = new HashMap<>();
    private final Map<String, Long> names = new HashMap<>();
    private final Map<Retraction, Removal> removals = new HashMap<>();
    private final Map<Long, LogicalTime> pendingRemovals = new HashMap<>();

    void discovered(long object, long objectClass, String name) {
        forget(object);
        KnownObject known = new KnownObject(object, objectClass, name);
        objects.put(object, known);
        names.put(name, object);
    }

    KnownObject object(long object) throws ObjectInstanceNotKnown {
        KnownObject known = objects.get(object);
        if (known == null) throw new ObjectInstanceNotKnown("Object is not locally known: " + Long.toUnsignedString(object));
        return known;
    }

    long object(String name) throws ObjectInstanceNotKnown {
        Long object = names.get(name);
        if (object == null) throw new ObjectInstanceNotKnown("Object is not locally known: " + name);
        return object;
    }

    void removed(long object, boolean receivedTimestampOrder, long producer, long value, double timestamp, double logicalTime) {
        removed(object,receivedTimestampOrder,producer,value,timestamp < 0 ? null : TimeValues.time(timestamp),TimeValues.time(logicalTime));
    }

    void removed(long object, boolean receivedTimestampOrder, long producer, long value, LogicalTime timestamp, LogicalTime logicalTime) {
        boolean timed = timestamp != null;
        boolean provisional = timed && receivedTimestampOrder && TimeWire.compare(timestamp, logicalTime) > 0;
        boolean retractable = timed && (!receivedTimestampOrder || provisional);
        remove(object, provisional, retractable, producer, value, timestamp, receivedTimestampOrder);
    }

    void deleted(long object, long producer, long value, double timestamp) {
        deleted(object,producer,value,TimeValues.time(timestamp));
    }

    void deleted(long object, long producer, long value, LogicalTime timestamp) {
        remove(object, false, true, producer, value, timestamp, true);
    }

    private void remove(long object, boolean provisional, boolean retractable, long producer, long value,
            LogicalTime timestamp, boolean expiresAtGrant) {
        KnownObject known = objects.get(object);
        forget(object);
        if (known == null) return;
        if (provisional) {
            objects.put(object, known);
            names.put(known.name, object);
            pendingRemovals.put(object, timestamp);
        }
        if (retractable && producer != 0 && value != 0) {
            removals.put(new Retraction(producer, value), new Removal(known, timestamp, expiresAtGrant));
        }
    }

    void retracted(long producer, long value) {
        Removal saved = removals.remove(new Retraction(producer, value));
        if (saved == null) return;
        pendingRemovals.remove(saved.object.object);
        if (!objects.containsKey(saved.object.object) && !names.containsKey(saved.object.name)) {
            objects.put(saved.object.object, saved.object);
            names.put(saved.object.name, saved.object.object);
        }
    }

    void forget(long object) {
        forgetVisible(object);
        pendingRemovals.remove(object);
        removals.values().removeIf(saved -> saved.object.object == object);
    }

    private void forgetVisible(long object) {
        KnownObject known = objects.remove(object);
        if (known != null) names.remove(known.name, object);
    }

    void advance(double time) {
        advance(TimeValues.time(time));
    }

    void advance(LogicalTime time) {
        pendingRemovals.entrySet().removeIf(entry -> {
            if (TimeWire.compare(entry.getValue(), time) > 0) return false;
            forgetVisible(entry.getKey());
            return true;
        });
        removals.values().removeIf(saved -> saved.expiresAtGrant && saved.timestamp != null && TimeWire.compare(saved.timestamp, time) <= 0);
    }

    void constrainDisabled() {
        for (long object : pendingRemovals.keySet()) forgetVisible(object);
        // Constraint release makes provisional removals locally unknown, but their sender can still retract.
        for (Removal saved : removals.values()) {
            if (pendingRemovals.containsKey(saved.object.object)) saved.expiresAtGrant = false;
        }
        pendingRemovals.clear();
    }

    void clear() {
        objects.clear();
        names.clear();
        removals.clear();
        pendingRemovals.clear();
    }

    ExchangeKnowledge checkpoint() {
        ExchangeKnowledge copy = new ExchangeKnowledge();
        copy.restore(this);
        return copy;
    }

    void validateTime(String selected) {
        for (LogicalTime time : pendingRemovals.values()) TimeWire.checked(time, selected);
        for (Removal removal : removals.values()) TimeWire.checked(removal.timestamp, selected);
    }

    void restore(ExchangeKnowledge source) {
        clear();
        objects.putAll(source.objects);
        names.putAll(source.names);
        pendingRemovals.putAll(source.pendingRemovals);
        source.removals.forEach((key, saved) -> removals.put(key,
                new Removal(saved.object, saved.timestamp, saved.expiresAtGrant)));
    }

    void writeTo(DataOutput output) throws IOException {
        writeTo(output,false);
    }

    void writeTo(DataOutput output, boolean exact) throws IOException {
        output.writeInt(CheckpointIO.count(objects.size()));
        for (KnownObject object : new TreeMap<>(objects).values()) writeObject(output, object);
        output.writeInt(CheckpointIO.count(pendingRemovals.size()));
        for (Map.Entry<Long, LogicalTime> entry : new TreeMap<>(pendingRemovals).entrySet()) {
            output.writeLong(entry.getKey()); TimeWire.writeCheckpoint(output,entry.getValue(),exact);
        }
        output.writeInt(CheckpointIO.count(removals.size()));
        for (Map.Entry<Retraction, Removal> entry : removals.entrySet()) {
            output.writeLong(entry.getKey().producer); output.writeLong(entry.getKey().value);
            writeObject(output, entry.getValue().object);
            TimeWire.writeCheckpoint(output,entry.getValue().timestamp,exact); output.writeBoolean(entry.getValue().expiresAtGrant);
        }
    }

    static ExchangeKnowledge readFrom(DataInput input) throws IOException {
        return readFrom(input,false);
    }

    static ExchangeKnowledge readFrom(DataInput input, boolean exact) throws IOException {
        ExchangeKnowledge state = new ExchangeKnowledge();
        int count = CheckpointIO.count(input);
        for (int i = 0; i < count; i++) {
            KnownObject object = readObject(input);
            if (state.objects.putIfAbsent(object.object, object) != null
                    || state.names.putIfAbsent(object.name, object.object) != null) throw new IOException("Duplicate checkpoint object");
        }
        count = CheckpointIO.count(input);
        for (int i = 0; i < count; i++) {
            long object = CheckpointIO.handle(input);
            LogicalTime time = TimeWire.readCheckpoint(input,exact);
            if (!state.objects.containsKey(object) || state.pendingRemovals.putIfAbsent(object, time) != null)
                throw new IOException("Invalid pending object removal");
        }
        count = CheckpointIO.count(input);
        for (int i = 0; i < count; i++) {
            Retraction key = new Retraction(CheckpointIO.handle(input), CheckpointIO.handle(input));
            Removal removal = new Removal(readObject(input), TimeWire.readCheckpoint(input,exact), input.readBoolean());
            if (state.removals.putIfAbsent(key, removal) != null) throw new IOException("Duplicate checkpoint retraction");
        }
        return state;
    }

    private static void writeObject(DataOutput output, KnownObject object) throws IOException {
        output.writeLong(object.object); output.writeLong(object.objectClass); CheckpointIO.string(output, object.name);
    }

    private static KnownObject readObject(DataInput input) throws IOException {
        return new KnownObject(CheckpointIO.handle(input), CheckpointIO.handle(input), CheckpointIO.string(input));
    }

    static final class KnownObject {
        final long object;
        final long objectClass;
        final String name;
        KnownObject(long object, long objectClass, String name) {
            this.object = object;
            this.objectClass = objectClass;
            this.name = name;
        }
    }

    private static final class Removal {
        final KnownObject object;
        final LogicalTime timestamp;
        boolean expiresAtGrant;
        Removal(KnownObject object, LogicalTime timestamp, boolean expiresAtGrant) {
            this.object = object;
            this.timestamp = timestamp;
            this.expiresAtGrant = expiresAtGrant;
        }
    }

    private static final class Retraction {
        final long producer;
        final long value;
        Retraction(long producer, long value) { this.producer = producer; this.value = value; }
        @Override public boolean equals(Object other) {
            if (!(other instanceof Retraction)) return false;
            Retraction that = (Retraction) other;
            return producer == that.producer && value == that.value;
        }
        @Override public int hashCode() { return Objects.hash(producer, value); }
    }
}
