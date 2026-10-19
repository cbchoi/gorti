package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotDecode;
import hla.rti1516e.time.HLAinteger64Interval;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.time.HLAinteger64TimeFactory;

/**
 * Exact local Integer64 values, discoverable as a standard logical-time provider.
 * Factory availability does not enable Integer64 federation services or establish
 * end-to-end RTI time conformance; the server binding remains independently gated.
 */
public final class Integer64TimeFactory implements HLAinteger64TimeFactory {
    private static final long serialVersionUID = 1L;

    public Integer64TimeFactory() {}

    public HLAinteger64Time decodeTime(byte[] buffer, int offset) throws CouldNotDecode {
        return new Integer64Time(Integer64Encoding.decode(buffer, offset));
    }

    public HLAinteger64Interval decodeInterval(byte[] buffer, int offset) throws CouldNotDecode {
        return new Integer64Interval(Integer64Encoding.decode(buffer, offset));
    }

    public HLAinteger64Time makeInitial() { return new Integer64Time(0L); }
    public HLAinteger64Time makeFinal() { return new Integer64Time(Long.MAX_VALUE); }
    public HLAinteger64Time makeTime(long value) { return new Integer64Time(value); }
    public HLAinteger64Interval makeZero() { return new Integer64Interval(0L); }
    public HLAinteger64Interval makeEpsilon() { return new Integer64Interval(1L); }
    public HLAinteger64Interval makeInterval(long value) { return new Integer64Interval(value); }
    public String getName() { return NAME; }
}
