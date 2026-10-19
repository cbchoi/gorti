package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotEncode;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.exceptions.InvalidLogicalTimeInterval;
import hla.rti1516e.time.HLAfloat64Interval;
import java.util.Objects;

final class Float64Interval implements HLAfloat64Interval {
    private static final long serialVersionUID = 1L;
    private final double value;

    Float64Interval(double value) { this.value = TimeValues.validValue(value); }

    public boolean isZero() { return value == 0.0; }
    public boolean isEpsilon() { return value == Double.MIN_VALUE; }
    public double getValue() { return value; }

    public HLAfloat64Interval add(HLAfloat64Interval interval)
            throws IllegalTimeArithmetic, InvalidLogicalTimeInterval {
        return new Float64Interval(Float64Arithmetic.add(value, operand(interval)));
    }

    public HLAfloat64Interval subtract(HLAfloat64Interval interval)
            throws IllegalTimeArithmetic, InvalidLogicalTimeInterval {
        return new Float64Interval(Float64Arithmetic.subtract(value, operand(interval)));
    }

    private static double operand(HLAfloat64Interval interval) throws InvalidLogicalTimeInterval {
        try {
            return TimeValues.value(interval);
        } catch (IllegalArgumentException invalid) {
            throw new InvalidLogicalTimeInterval(invalid.getMessage(), invalid);
        }
    }

    public int compareTo(HLAfloat64Interval other) {
        return Double.compare(value, TimeValues.value(Objects.requireNonNull(other, "other")));
    }

    public int encodedLength() { return Double.BYTES; }
    public void encode(byte[] buffer, int offset) throws CouldNotEncode {
        Float64Encoding.encode(value, buffer, offset);
    }

    @Override
    public boolean equals(Object other) {
        return other instanceof Float64Interval && value == ((Float64Interval) other).value;
    }

    @Override
    public int hashCode() { return Double.hashCode(value); }

    @Override
    public String toString() { return "HLAfloat64Interval(" + value + ")"; }
}
