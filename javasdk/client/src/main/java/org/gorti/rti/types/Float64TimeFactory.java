package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotDecode;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;

/** Public no-argument constructor supports registration as a logical-time provider. */
public final class Float64TimeFactory implements HLAfloat64TimeFactory {
    private static final long serialVersionUID = 1L;

    public Float64TimeFactory() {}

    public HLAfloat64Time decodeTime(byte[] buffer, int offset) throws CouldNotDecode {
        return TimeValues.time(Float64Encoding.decode(buffer, offset));
    }

    public HLAfloat64Interval decodeInterval(byte[] buffer, int offset) throws CouldNotDecode {
        return TimeValues.interval(Float64Encoding.decode(buffer, offset));
    }

    public HLAfloat64Time makeInitial() { return TimeValues.time(0.0); }
    public HLAfloat64Time makeFinal() { return TimeValues.time(Double.MAX_VALUE); }
    public HLAfloat64Time makeTime(double value) { return TimeValues.time(value); }
    public HLAfloat64Interval makeZero() { return TimeValues.interval(0.0); }
    public HLAfloat64Interval makeEpsilon() { return TimeValues.interval(Double.MIN_VALUE); }
    public HLAfloat64Interval makeInterval(double value) { return TimeValues.interval(value); }
    public String getName() { return NAME; }
}
