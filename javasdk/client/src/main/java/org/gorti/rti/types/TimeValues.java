package org.gorti.rti.types;

import hla.rti1516e.LogicalTime;
import hla.rti1516e.LogicalTimeInterval;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;

/** Standard Float64 values; other logical-time representations are not converted. */
public final class TimeValues {
    private static final HLAfloat64TimeFactory FACTORY = new Float64TimeFactory();

    private TimeValues() {}

    public static HLAfloat64Time time(double value) { return new Float64Time(value); }
    public static HLAfloat64Interval interval(double value) { return new Float64Interval(value); }
    public static HLAfloat64TimeFactory factory() { return FACTORY; }

    public static double value(LogicalTime<?, ?> time) {
        if (!(time instanceof HLAfloat64Time)) {
            throw new IllegalArgumentException("Expected HLAfloat64Time");
        }
        return validValue(((HLAfloat64Time) time).getValue());
    }

    public static double value(LogicalTimeInterval<?> interval) {
        if (!(interval instanceof HLAfloat64Interval)) {
            throw new IllegalArgumentException("Expected HLAfloat64Interval");
        }
        return validValue(((HLAfloat64Interval) interval).getValue());
    }

    static double validValue(double value) {
        if (!Double.isFinite(value) || value < 0.0) {
            throw new IllegalArgumentException("Float64 time values must be finite and nonnegative");
        }
        return value == 0.0 ? 0.0 : value;
    }
}
