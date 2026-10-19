package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotEncode;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.time.HLAinteger64Interval;
import hla.rti1516e.time.HLAinteger64Time;
import java.util.Objects;

final class Integer64Time implements HLAinteger64Time {
    private static final long serialVersionUID = 1L;
    private final long value;

    Integer64Time(long value) { this.value = Integer64Arithmetic.validValue(value); }

    public boolean isInitial() { return value == 0L; }
    public boolean isFinal() { return value == Long.MAX_VALUE; }
    public long getValue() { return value; }

    public HLAinteger64Time add(HLAinteger64Interval interval) throws IllegalTimeArithmetic {
        return new Integer64Time(Integer64Arithmetic.add(value, operand(interval)));
    }

    public HLAinteger64Time subtract(HLAinteger64Interval interval) throws IllegalTimeArithmetic {
        return new Integer64Time(Integer64Arithmetic.subtract(value, operand(interval)));
    }

    private static long operand(HLAinteger64Interval interval) throws IllegalTimeArithmetic {
        try {
            if (interval == null) throw new IllegalArgumentException("Integer64 interval is null");
            return Integer64Arithmetic.validValue(interval.getValue());
        } catch (IllegalArgumentException invalid) {
            // The standardized subtype narrows away InvalidLogicalTimeInterval.
            throw new IllegalTimeArithmetic(invalid.getMessage(), invalid);
        }
    }

    public HLAinteger64Interval distance(HLAinteger64Time other) {
        long otherValue = Integer64Arithmetic.validValue(Objects.requireNonNull(other, "other").getValue());
        return new Integer64Interval(value >= otherValue ? value - otherValue : otherValue - value);
    }

    public int compareTo(HLAinteger64Time other) {
        return Long.compare(value,
                Integer64Arithmetic.validValue(Objects.requireNonNull(other, "other").getValue()));
    }

    public int encodedLength() { return Long.BYTES; }

    public void encode(byte[] buffer, int offset) throws CouldNotEncode {
        Integer64Encoding.encode(value, buffer, offset);
    }

    @Override
    public boolean equals(Object other) {
        return other instanceof Integer64Time && value == ((Integer64Time) other).value;
    }

    @Override
    public int hashCode() { return Long.hashCode(value); }

    @Override
    public String toString() { return "HLAinteger64Time(" + value + ")"; }
}
