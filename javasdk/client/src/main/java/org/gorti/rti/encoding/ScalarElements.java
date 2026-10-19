package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;

final class ScalarElements {
    private ScalarElements() { }

    private abstract static class Bits extends AbstractElement {
        final int width;
        final boolean littleEndian;
        long bits;

        Bits(int width, boolean littleEndian, long bits) {
            this.width = width;
            this.littleEndian = littleEndian;
            this.bits = bits;
        }

        @Override public final int getOctetBoundary() { return width; }
        @Override public final int getEncodedLength() { return width; }
        @Override final int minimumLength() { return width; }

        @Override final void encodeValue(ByteWrapper wrapper) {
            for (int i = 0; i < width; i++) {
                int shift = (littleEndian ? i : width - i - 1) * 8;
                wrapper.put((int) (bits >>> shift) & 0xff);
            }
        }

        @Override final void decodeValue(ByteWrapper wrapper) {
            EncodingSupport.requireRemaining(wrapper, width);
            long decoded = 0;
            for (int i = 0; i < width; i++) {
                int shift = (littleEndian ? i : width - i - 1) * 8;
                decoded |= (long) wrapper.get() << shift;
            }
            validate(decoded);
            bits = decoded;
        }

        void validate(long decoded) { }
    }

    private abstract static class ByteValue extends Bits {
        ByteValue(byte value) { super(1, false, value); }
        public byte getValue() { return (byte) bits; }
        public void setValue(byte value) { validate(value & 0xff); bits = value; }
    }

    static final class ASCIIchar extends ByteValue implements HLAASCIIchar {
        ASCIIchar(byte value) { super(value); validate(value & 0xff); }
        @Override void validate(long value) {
            if (value > 0x7f) throw new IllegalArgumentException("Not a standard ASCII character");
        }
    }

    static final class ByteElement extends ByteValue implements HLAbyte {
        ByteElement(byte value) { super(value); }
    }

    static final class Octet extends ByteValue implements HLAoctet {
        Octet(byte value) { super(value); }
    }

    static final class BooleanElement extends Bits implements HLAboolean {
        BooleanElement(boolean value) { super(4, false, value ? 1 : 0); }
        @Override public boolean getValue() { return bits == 1; }
        @Override public void setValue(boolean value) { bits = value ? 1 : 0; }
        @Override void validate(long value) {
            if (value != 0 && value != 1) throw new IllegalArgumentException("HLAboolean must be 0 or 1");
        }
    }

    private abstract static class ShortValue extends Bits {
        ShortValue(boolean littleEndian, short value) { super(2, littleEndian, value); }
        public short getValue() { return (short) bits; }
        public void setValue(short value) { bits = value; }
    }

    static final class Integer16BE extends ShortValue implements HLAinteger16BE {
        Integer16BE(short value) { super(false, value); }
    }
    static final class Integer16LE extends ShortValue implements HLAinteger16LE {
        Integer16LE(short value) { super(true, value); }
    }
    static final class OctetPairBE extends ShortValue implements HLAoctetPairBE {
        OctetPairBE(short value) { super(false, value); }
    }
    static final class OctetPairLE extends ShortValue implements HLAoctetPairLE {
        OctetPairLE(short value) { super(true, value); }
    }
    static final class UnicodeChar extends ShortValue implements HLAunicodeChar {
        UnicodeChar(short value) { super(false, value); }
    }

    private abstract static class IntValue extends Bits {
        IntValue(boolean littleEndian, int value) { super(4, littleEndian, value); }
        public int getValue() { return (int) bits; }
        public void setValue(int value) { bits = value; }
    }
    static final class Integer32BE extends IntValue implements HLAinteger32BE {
        Integer32BE(int value) { super(false, value); }
    }
    static final class Integer32LE extends IntValue implements HLAinteger32LE {
        Integer32LE(int value) { super(true, value); }
    }

    private abstract static class LongValue extends Bits {
        LongValue(boolean littleEndian, long value) { super(8, littleEndian, value); }
        public long getValue() { return bits; }
        public void setValue(long value) { bits = value; }
    }
    static final class Integer64BE extends LongValue implements HLAinteger64BE {
        Integer64BE(long value) { super(false, value); }
    }
    static final class Integer64LE extends LongValue implements HLAinteger64LE {
        Integer64LE(long value) { super(true, value); }
    }

    private abstract static class FloatValue extends Bits {
        FloatValue(boolean littleEndian, float value) { super(4, littleEndian, Float.floatToRawIntBits(value)); }
        public float getValue() { return Float.intBitsToFloat((int) bits); }
        public void setValue(float value) { bits = Float.floatToRawIntBits(value); }
    }
    static final class Float32BE extends FloatValue implements HLAfloat32BE {
        Float32BE(float value) { super(false, value); }
    }
    static final class Float32LE extends FloatValue implements HLAfloat32LE {
        Float32LE(float value) { super(true, value); }
    }

    private abstract static class DoubleValue extends Bits {
        DoubleValue(boolean littleEndian, double value) { super(8, littleEndian, Double.doubleToRawLongBits(value)); }
        public double getValue() { return Double.longBitsToDouble(bits); }
        public void setValue(double value) { bits = Double.doubleToRawLongBits(value); }
    }
    static final class Float64BE extends DoubleValue implements HLAfloat64BE {
        Float64BE(double value) { super(false, value); }
    }
    static final class Float64LE extends DoubleValue implements HLAfloat64LE {
        Float64LE(double value) { super(true, value); }
    }
}
