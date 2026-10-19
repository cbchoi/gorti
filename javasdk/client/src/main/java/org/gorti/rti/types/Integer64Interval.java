package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotEncode;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.exceptions.InvalidLogicalTimeInterval;
import hla.rti1516e.time.HLAinteger64Interval;
import java.util.Objects;

final class Integer64Interval implements HLAinteger64Interval {
    private static final long serialVersionUID = 1L;
    private final long value;

    Integer64Interval(long value) { this.value = Integer64Arithmetic.validValue(value); }

    public boolean isZero() { return value == 0L; }
    public boolean isEpsilon() { return value == 1L; }
    public long getValue() { return value; }

    public HLAinteger64Interval add(HLAinteger64Interval interval)
            throws IllegalTimeArithmetic, InvalidLogicalTimeInterval {
        return new Integer64Interval(Integer64Arithmetic.add(value, operand(interval)));
    }

    public HLAinteger64Interval subtract(HLAinteger64Interval interval)
            throws IllegalTimeArithmetic, InvalidLogicalTimeInterval {
        return new Integer64Interval(Integer64Arithmetic.subtract(value, operand(interval)));
    }

    private static long operand(HLAinteger64Interval interval) throws InvalidLogicalTimeInterval {
        try {
            if (interval == null) throw new IllegalArgumentException("Integer64 interval is null");
            return Integer64Arithmetic.validValue(interval.getValue());
        } catch (IllegalArgumentException invalid) {
            throw new InvalidLogicalTimeInterval(invalid.getMessage(), invalid);
        }
    }

    public int compareTo(HLAinteger64Interval other) {
        return Long.compare(value,
                Integer64Arithmetic.validValue(Objects.requireNonNull(other, "other").getValue()));
    }

    public int encodedLength() { return Long.BYTES; }

    public void encode(byte[] buffer, int offset) throws CouldNotEncode {
        Integer64Encoding.encode(value, buffer, offset);
    }

    @Override
    public boolean equals(Object other) {
        return other instanceof Integer64Interval && value == ((Integer64Interval) other).value;
    }

    @Override
    public int hashCode() { return Long.hashCode(value); }

    @Override
    public String toString() { return "HLAinteger64Interval(" + value + ")"; }
}
