package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotEncode;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAfloat64Time;
import java.util.Objects;

final strictfp class Float64Time implements HLAfloat64Time {
    private static final long serialVersionUID = 1L;
    private final double value;

    Float64Time(double value) { this.value = TimeValues.validValue(value); }

    public boolean isInitial() { return value == 0.0; }
    public boolean isFinal() { return value == Double.MAX_VALUE; }
    public double getValue() { return value; }

    public HLAfloat64Time add(HLAfloat64Interval interval) throws IllegalTimeArithmetic {
        return new Float64Time(Float64Arithmetic.add(value, operand(interval)));
    }

    public HLAfloat64Time subtract(HLAfloat64Interval interval) throws IllegalTimeArithmetic {
        return new Float64Time(Float64Arithmetic.subtract(value, operand(interval)));
    }

    private static double operand(HLAfloat64Interval interval) throws IllegalTimeArithmetic {
        try {
            return TimeValues.value(interval);
        } catch (IllegalArgumentException invalid) {
            // The standardized subtype narrows away InvalidLogicalTimeInterval.
            throw new IllegalTimeArithmetic(invalid.getMessage(), invalid);
        }
    }

    public HLAfloat64Interval distance(HLAfloat64Time other) {
        return new Float64Interval(Math.abs(value - TimeValues.value(Objects.requireNonNull(other, "other"))));
    }

    public int compareTo(HLAfloat64Time other) {
        return Double.compare(value, TimeValues.value(Objects.requireNonNull(other, "other")));
    }

    public int encodedLength() { return Double.BYTES; }
    public void encode(byte[] buffer, int offset) throws CouldNotEncode {
        Float64Encoding.encode(value, buffer, offset);
    }

    @Override
    public boolean equals(Object other) {
        return other instanceof Float64Time && value == ((Float64Time) other).value;
    }

    @Override
    public int hashCode() { return Double.hashCode(value); }

    @Override
    public String toString() { return "HLAfloat64Time(" + value + ")"; }
}
