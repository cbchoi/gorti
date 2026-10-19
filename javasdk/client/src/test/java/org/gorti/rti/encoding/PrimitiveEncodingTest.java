package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.util.Arrays;
import java.util.function.Function;
import java.util.stream.Stream;
import org.gorti.rti.GortiRtiFactory;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.Arguments;
import org.junit.jupiter.params.provider.MethodSource;
import static org.junit.jupiter.api.Assertions.*;

class PrimitiveEncodingTest {
    static EncoderFactory factory() throws Exception { return new GortiRtiFactory().getEncoderFactory(); }

    static byte[] hex(String value) {
        String compact = value.replace(" ", "");
        byte[] result = new byte[compact.length() / 2];
        for (int i = 0; i < result.length; i++) {
            result[i] = (byte) Integer.parseInt(compact.substring(i * 2, i * 2 + 2), 16);
        }
        return result;
    }

    static Arguments vector(String name, int boundary, String bytes, Function<EncoderFactory, DataElement> create) {
        return Arguments.of(name, boundary, bytes, create);
    }

    static Stream<Arguments> vectors() {
        return Stream.of(
                vector("ASCIIchar", 1, "41", f -> f.createHLAASCIIchar((byte) 'A')),
                vector("byte", 1, "80", f -> f.createHLAbyte((byte) 0x80)),
                vector("octet", 1, "ff", f -> f.createHLAoctet((byte) 0xff)),
                vector("boolean", 4, "00000001", f -> f.createHLAboolean(true)),
                vector("integer16BE", 2, "8123", f -> f.createHLAinteger16BE((short) 0x8123)),
                vector("integer16LE", 2, "2381", f -> f.createHLAinteger16LE((short) 0x8123)),
                vector("octetPairBE", 2, "abcd", f -> f.createHLAoctetPairBE((short) 0xabcd)),
                vector("octetPairLE", 2, "cdab", f -> f.createHLAoctetPairLE((short) 0xabcd)),
                vector("unicodeChar", 2, "ac00", f -> f.createHLAunicodeChar((short) 0xac00)),
                vector("integer32BE", 4, "81234567", f -> f.createHLAinteger32BE(0x81234567)),
                vector("integer32LE", 4, "67452381", f -> f.createHLAinteger32LE(0x81234567)),
                vector("integer64BE", 8, "8123456789abcdef", f -> f.createHLAinteger64BE(0x8123456789abcdefL)),
                vector("integer64LE", 8, "efcdab8967452381", f -> f.createHLAinteger64LE(0x8123456789abcdefL)),
                vector("float32BE", 4, "c0200000", f -> f.createHLAfloat32BE(-2.5f)),
                vector("float32LE", 4, "000020c0", f -> f.createHLAfloat32LE(-2.5f)),
                vector("float64BE", 8, "3ff8000000000000", f -> f.createHLAfloat64BE(1.5)),
                vector("float64LE", 8, "000000000000f83f", f -> f.createHLAfloat64LE(1.5)),
                vector("ASCIIstring", 4, "0000000341007f", f -> f.createHLAASCIIstring("A\0\u007f")),
                vector("unicodeString", 4, "000000040041ac00d83dde00", f -> f.createHLAunicodeString("A\uac00\ud83d\ude00")),
                vector("opaqueData", 4, "000000030080ff", f -> f.createHLAopaqueData(hex("0080ff"))));
    }

    @ParameterizedTest(name = "{0}: independent bytes") @MethodSource("vectors")
    void independentWireVectors(String name, int boundary, String expected, Function<EncoderFactory, DataElement> create)
            throws Exception {
        DataElement element = create.apply(factory());
        assertEquals(boundary, element.getOctetBoundary());
        assertEquals(hex(expected).length, element.getEncodedLength());
        assertArrayEquals(hex(expected), element.toByteArray());
    }

    @ParameterizedTest(name = "{0}: wrapper origin") @MethodSource("vectors")
    void wrapperOffsetsAndZeroPadding(String name, int boundary, String expected, Function<EncoderFactory, DataElement> create)
            throws Exception {
        DataElement element = create.apply(factory());
        byte[] vector = hex(expected);
        byte[] storage = new byte[3 + boundary + vector.length + 2];
        Arrays.fill(storage, (byte) 0x55);
        ByteWrapper wrapper = new ByteWrapper(storage, 3, boundary + vector.length);
        wrapper.advance(1);
        element.encode(wrapper);
        assertEquals(3 + boundary + vector.length, wrapper.getPos());
        assertArrayEquals(vector, Arrays.copyOfRange(storage, 3 + boundary, wrapper.getPos()));
        for (int i = 4; i < 3 + boundary; i++) assertEquals(0, storage[i]);
        assertEquals(0x55, storage[2]);
        assertEquals(0x55, storage[3]);
        assertEquals(0x55, storage[storage.length - 2]);
        ByteWrapper source = new ByteWrapper(storage, 3, boundary + vector.length);
        source.advance(1);
        element.decode(source);
        assertEquals(wrapper.getPos(), source.getPos());
        assertArrayEquals(vector, element.toByteArray());
    }

    @ParameterizedTest(name = "{0}: short segment") @MethodSource("vectors")
    void wrapperLimitsAreNotBackingArrayLimits(String name, int boundary, String expected,
                                               Function<EncoderFactory, DataElement> create) throws Exception {
        DataElement element = create.apply(factory());
        byte[] storage = new byte[64];
        byte[] vector = hex(expected);
        System.arraycopy(vector, 0, storage, 5, vector.length);
        assertThrows(EncoderException.class, () -> element.encode(new ByteWrapper(storage, 5, vector.length - 1)));
        assertThrows(DecoderException.class, () -> element.decode(new ByteWrapper(storage, 5, vector.length - 1)));
    }

    @Test void scalarDecodeValuesAndSetters() throws Exception {
        EncoderFactory f = factory();
        HLAASCIIchar ascii = f.createHLAASCIIchar(); ascii.decode(hex("41")); assertEquals('A', ascii.getValue()); ascii.setValue((byte) 'B'); assertArrayEquals(hex("42"), ascii.toByteArray());
        HLAbyte b = f.createHLAbyte(); b.decode(hex("80")); assertEquals((byte) 0x80, b.getValue()); b.setValue((byte) 7); assertArrayEquals(hex("07"), b.toByteArray());
        HLAoctet o = f.createHLAoctet(); o.decode(hex("ff")); assertEquals((byte) -1, o.getValue()); o.setValue((byte) 1); assertArrayEquals(hex("01"), o.toByteArray());
        HLAboolean bool = f.createHLAboolean(); bool.decode(hex("00000001")); assertTrue(bool.getValue()); bool.setValue(false); assertArrayEquals(hex("00000000"), bool.toByteArray());
        HLAinteger16BE sbe = f.createHLAinteger16BE(); sbe.decode(hex("8123")); assertEquals((short) 0x8123, sbe.getValue()); sbe.setValue((short) 1); assertArrayEquals(hex("0001"), sbe.toByteArray());
        HLAinteger16LE sle = f.createHLAinteger16LE(); sle.decode(hex("2381")); assertEquals((short) 0x8123, sle.getValue()); sle.setValue((short) 1); assertArrayEquals(hex("0100"), sle.toByteArray());
        HLAoctetPairBE pbe = f.createHLAoctetPairBE(); pbe.decode(hex("abcd")); assertEquals((short) 0xabcd, pbe.getValue()); pbe.setValue((short) 1); assertArrayEquals(hex("0001"), pbe.toByteArray());
        HLAoctetPairLE ple = f.createHLAoctetPairLE(); ple.decode(hex("cdab")); assertEquals((short) 0xabcd, ple.getValue()); ple.setValue((short) 1); assertArrayEquals(hex("0100"), ple.toByteArray());
        HLAunicodeChar uc = f.createHLAunicodeChar(); uc.decode(hex("ac00")); assertEquals((short) 0xac00, uc.getValue()); uc.setValue((short) 65); assertArrayEquals(hex("0041"), uc.toByteArray());
        HLAinteger32BE ibe = f.createHLAinteger32BE(); ibe.decode(hex("81234567")); assertEquals(0x81234567, ibe.getValue()); ibe.setValue(1); assertArrayEquals(hex("00000001"), ibe.toByteArray());
        HLAinteger32LE ile = f.createHLAinteger32LE(); ile.decode(hex("67452381")); assertEquals(0x81234567, ile.getValue()); ile.setValue(1); assertArrayEquals(hex("01000000"), ile.toByteArray());
        HLAinteger64BE lbe = f.createHLAinteger64BE(); lbe.decode(hex("8123456789abcdef")); assertEquals(0x8123456789abcdefL, lbe.getValue()); lbe.setValue(1); assertArrayEquals(hex("0000000000000001"), lbe.toByteArray());
        HLAinteger64LE lle = f.createHLAinteger64LE(); lle.decode(hex("efcdab8967452381")); assertEquals(0x8123456789abcdefL, lle.getValue()); lle.setValue(1); assertArrayEquals(hex("0100000000000000"), lle.toByteArray());
        HLAfloat32BE fbe = f.createHLAfloat32BE(); fbe.decode(hex("c0200000")); assertEquals(-2.5f, fbe.getValue()); fbe.setValue(-0.0f); assertArrayEquals(hex("80000000"), fbe.toByteArray());
        HLAfloat32LE fle = f.createHLAfloat32LE(); fle.decode(hex("000020c0")); assertEquals(-2.5f, fle.getValue()); fle.setValue(Float.POSITIVE_INFINITY); assertArrayEquals(hex("0000807f"), fle.toByteArray());
        HLAfloat64BE dbe = f.createHLAfloat64BE(); dbe.decode(hex("3ff8000000000000")); assertEquals(1.5, dbe.getValue()); dbe.setValue(Double.NEGATIVE_INFINITY); assertArrayEquals(hex("fff0000000000000"), dbe.toByteArray());
        HLAfloat64LE dle = f.createHLAfloat64LE(); dle.decode(hex("000000000000f83f")); assertEquals(1.5, dle.getValue()); dle.setValue(-0.0); assertArrayEquals(hex("0000000000000080"), dle.toByteArray());
    }

    @Test void stringDecodeValuesAndOpaqueIsolation() throws Exception {
        EncoderFactory f = factory();
        HLAASCIIstring ascii = f.createHLAASCIIstring(); ascii.decode(hex("0000000341007f")); assertEquals("A\0\u007f", ascii.getValue()); ascii.setValue("B"); assertArrayEquals(hex("0000000142"), ascii.toByteArray());
        HLAunicodeString unicode = f.createHLAunicodeString(); unicode.decode(hex("000000040041ac00d83dde00")); assertEquals("A\uac00\ud83d\ude00", unicode.getValue()); unicode.setValue("\ud800"); assertArrayEquals(hex("00000001d800"), unicode.toByteArray());
        HLAopaqueData opaque = f.createHLAopaqueData(); opaque.decode(hex("000000030080ff")); assertEquals(3, opaque.size()); assertEquals((byte) 0x80, opaque.get(1));
        byte[] bytes = hex("0102"); opaque.setValue(bytes); bytes[0] = 9; opaque.getValue()[0] = 8;
        assertArrayEquals(hex("000000020102"), opaque.toByteArray());
        assertEquals(Arrays.asList((byte) 1, (byte) 2), java.util.stream.StreamSupport.stream(opaque.spliterator(), false).collect(java.util.stream.Collectors.toList()));
    }

    @Test void floatingPointNanPayloadsArePreserved() throws Exception {
        EncoderFactory f = factory();
        HLAfloat32BE single = f.createHLAfloat32BE(); single.decode(hex("7fc01234")); assertTrue(Float.isNaN(single.getValue())); assertArrayEquals(hex("7fc01234"), single.toByteArray());
        HLAfloat64LE wide = f.createHLAfloat64LE(); wide.decode(hex("341200000000f87f")); assertTrue(Double.isNaN(wide.getValue())); assertArrayEquals(hex("341200000000f87f"), wide.toByteArray());
    }
}
