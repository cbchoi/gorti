package org.gorti.rti;

import com.google.protobuf.Descriptors.FieldDescriptor;
import com.google.protobuf.Message;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.LogicalTimeFactory;
import hla.rti1516e.LogicalTimeInterval;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.time.HLAinteger64Interval;
import org.gorti.rti.types.Integer64TimeFactory;
import org.gorti.rti.types.TimeValues;
import rti.v1.Common;

/** Lossless SDK/domain boundary; the selected federation factory is authoritative. */
final class TimeWire {
    static final String FLOAT = "HLAfloat64Time";
    static final String INTEGER = "HLAinteger64Time";
    private TimeWire() { }

    static LogicalTimeFactory factory(String name) {
        if (name == null || name.isEmpty() || FLOAT.equals(name)) return TimeValues.factory();
        if (INTEGER.equals(name)) return new Integer64TimeFactory();
        throw new IllegalArgumentException("Unsupported logical time factory: " + name);
    }

    static LogicalTime checked(LogicalTime value, String selected) {
        if (INTEGER.equals(selected)) {
            if (!(value instanceof HLAinteger64Time) || ((HLAinteger64Time)value).getValue() < 0)
                throw new IllegalArgumentException("Expected nonnegative HLAinteger64Time");
        } else {
            TimeValues.value(value);
        }
        return value;
    }

    static LogicalTimeInterval checked(LogicalTimeInterval value, String selected) {
        if (INTEGER.equals(selected)) {
            if (!(value instanceof HLAinteger64Interval) || ((HLAinteger64Interval)value).getValue() < 0)
                throw new IllegalArgumentException("Expected nonnegative HLAinteger64Interval");
        } else {
            TimeValues.value(value);
        }
        return value;
    }

    static <B extends Message.Builder> B write(B builder, String field, LogicalTime value, String selected) {
        checked(value, selected);
        Common.LogicalTimeValue exact = INTEGER.equals(selected)
                ? Common.LogicalTimeValue.newBuilder().setInteger64Value(((HLAinteger64Time)value).getValue()).build()
                : Common.LogicalTimeValue.newBuilder().setFloat64Value(TimeValues.value(value)).build();
        return writeValue(builder, field, exact);
    }

    static <B extends Message.Builder> B writeInterval(B builder, String field, LogicalTimeInterval value, String selected) {
        checked(value, selected);
        Common.LogicalTimeValue exact = INTEGER.equals(selected)
                ? Common.LogicalTimeValue.newBuilder().setInteger64Value(((HLAinteger64Interval)value).getValue()).build()
                : Common.LogicalTimeValue.newBuilder().setFloat64Value(TimeValues.value(value)).build();
        return writeValue(builder, field, exact);
    }

    private static <B extends Message.Builder> B writeValue(B builder, String field, Common.LogicalTimeValue exact) {
        FieldDescriptor legacy = builder.getDescriptorForType().findFieldByName(field);
        FieldDescriptor target = builder.getDescriptorForType().findFieldByName(exactName(field));
        if (target == null) throw new IllegalArgumentException("Exact time field is absent from wire schema");
        builder.clearField(legacy);
        if (exact.getValueCase() == Common.LogicalTimeValue.ValueCase.FLOAT64_VALUE)
            builder.setField(legacy, exact.getFloat64Value());
        builder.setField(target, exact);
        return builder;
    }

    static boolean hasTime(Message message, String field) {
        FieldDescriptor legacy = message.getDescriptorForType().findFieldByName(field);
        FieldDescriptor exact = message.getDescriptorForType().findFieldByName(exactName(field));
        return (exact != null && message.hasField(exact)) || message.hasField(legacy);
    }

    static LogicalTime read(Message message, String field, String selected) {
        Common.LogicalTimeValue value = readValue(message, field, selected);
        if (INTEGER.equals(selected)) return new Integer64TimeFactory().makeTime(value.getInteger64Value());
        return TimeValues.time(value.getFloat64Value());
    }

    static LogicalTimeInterval readInterval(Message message, String field, String selected) {
        Common.LogicalTimeValue value = readValue(message, field, selected);
        if (INTEGER.equals(selected)) return new Integer64TimeFactory().makeInterval(value.getInteger64Value());
        return TimeValues.interval(value.getFloat64Value());
    }

    private static Common.LogicalTimeValue readValue(Message message, String field, String selected) {
        FieldDescriptor legacy = message.getDescriptorForType().findFieldByName(field);
        FieldDescriptor exact = message.getDescriptorForType().findFieldByName(exactName(field));
        double oldValue = (Double)message.getField(legacy);
        boolean oldPresent = legacy.hasPresence() ? message.hasField(legacy) : Double.doubleToRawLongBits(oldValue) != 0;
        boolean integer = INTEGER.equals(selected);
        if (exact == null || !message.hasField(exact)) {
            if (integer) throw new IllegalArgumentException("Integer64 response is missing its exact value");
            TimeValues.time(oldValue);
            return Common.LogicalTimeValue.newBuilder().setFloat64Value(oldValue).build();
        }
        Common.LogicalTimeValue value = (Common.LogicalTimeValue)message.getField(exact);
        if (!value.getUnknownFields().asMap().isEmpty()
                || value.getValueCase() != (integer ? Common.LogicalTimeValue.ValueCase.INTEGER64_VALUE
                                                  : Common.LogicalTimeValue.ValueCase.FLOAT64_VALUE))
            throw new IllegalArgumentException("Wire time representation differs from selected factory");
        if (oldPresent && (integer || oldValue != value.getFloat64Value()))
            throw new IllegalArgumentException("Conflicting legacy and exact logical time");
        if (integer) { if (value.getInteger64Value() < 0) throw new IllegalArgumentException("Negative logical time"); }
        else TimeValues.time(value.getFloat64Value());
        return value;
    }

    private static String exactName(String field) { return "value".equals(field) ? "logical_time_value" : field + "_value"; }

    static int compare(LogicalTime left, LogicalTime right) {
        return left.compareTo(right);
    }

    static void writeCheckpoint(java.io.DataOutput output, LogicalTime value, boolean exact) throws java.io.IOException {
        if (!exact) { output.writeDouble(TimeValues.value(value)); return; }
        if (value instanceof HLAinteger64Time) {
            output.writeByte(2); output.writeLong(((HLAinteger64Time)value).getValue());
        } else {
            output.writeByte(1); output.writeDouble(TimeValues.value(value));
        }
    }

    static LogicalTime readCheckpoint(java.io.DataInput input, boolean exact) throws java.io.IOException {
        try {
            if (!exact) return TimeValues.time(CheckpointIO.time(input));
            switch (input.readUnsignedByte()) {
                case 1: return TimeValues.time(input.readDouble());
                case 2: return new Integer64TimeFactory().makeTime(input.readLong());
                default: throw new java.io.IOException("Unknown checkpoint time representation");
            }
        } catch (IllegalArgumentException invalid) {
            throw new java.io.IOException("Invalid checkpoint logical time", invalid);
        }
    }
}
