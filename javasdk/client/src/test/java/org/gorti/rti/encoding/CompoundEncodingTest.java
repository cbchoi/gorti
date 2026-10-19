package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.util.Arrays;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import static org.gorti.rti.encoding.PrimitiveEncodingTest.*;

class CompoundEncodingTest {
    private HLAfixedRecord record(EncoderFactory f, int number, byte tail) {
        HLAfixedRecord value = f.createHLAfixedRecord();
        value.add(f.createHLAinteger32BE(number)); value.add(f.createHLAoctet(tail));
        return value;
    }

    @Test void normativeFixedRecordExampleAndReverse() throws Exception {
        EncoderFactory f = factory();
        HLAfixedRecord value = f.createHLAfixedRecord();
        value.add(f.createHLAoctet((byte) 0x7f)); value.add(f.createHLAboolean(true)); value.add(f.createHLAfloat64BE(1.5));
        assertEquals(8, value.getOctetBoundary());
        assertEquals(16, value.getEncodedLength());
        assertArrayEquals(hex("7f000000000000013ff8000000000000"), value.toByteArray());
        HLAfixedRecord reverse = f.createHLAfixedRecord();
        reverse.add(value.get(2)); reverse.add(value.get(1)); reverse.add(value.get(0));
        assertEquals(13, reverse.getEncodedLength());
        assertArrayEquals(hex("3ff8000000000000000000017f"), reverse.toByteArray());
        reverse.decode(hex("40000000000000000000000042"));
        assertEquals(2.0, ((HLAfloat64BE) reverse.get(0)).getValue());
        assertFalse(((HLAboolean) reverse.get(1)).getValue());
        assertEquals(0x42, ((HLAoctet) reverse.get(2)).getValue());
    }

    @Test void fixedArrayHasInterElementPaddingButNoTrailingPadding() throws Exception {
        EncoderFactory f = factory();
        HLAfixedArray<HLAfixedRecord> array = f.createHLAfixedArray(record(f, 1, (byte) 2), record(f, 3, (byte) 4));
        assertEquals(4, array.getOctetBoundary()); assertEquals(13, array.getEncodedLength());
        assertArrayEquals(hex("00000001020000000000000304"), array.toByteArray());
        HLAfixedArray<HLAfixedRecord> decoded = f.createHLAfixedArray(i -> record(f, 0, (byte) 0), 2);
        decoded.decode(hex("00000001020000000000000304"));
        assertEquals(3, ((HLAinteger32BE) decoded.get(1).get(0)).getValue());
        assertEquals(4, ((HLAoctet) decoded.get(1).get(1)).getValue());
        assertThrows(UnsupportedOperationException.class, () -> { java.util.Iterator<?> it = decoded.iterator(); it.next(); it.remove(); });
    }

    @Test void variableArrayIncludesAlignedBigEndianCount() throws Exception {
        EncoderFactory f = factory();
        HLAvariableArray<HLAfloat64LE> value = f.createHLAvariableArray(i -> f.createHLAfloat64LE(), f.createHLAfloat64LE(1.5));
        assertEquals(8, value.getOctetBoundary());
        assertArrayEquals(hex("0000000100000000000000000000f83f"), value.toByteArray());
        HLAvariableArray<HLAfloat64LE> decoded = f.createHLAvariableArray(i -> f.createHLAfloat64LE());
        assertEquals(8, decoded.getOctetBoundary());
        decoded.decode(hex("0000000200000000000000000000f83f0000000000000040"));
        assertEquals(2, decoded.size()); assertEquals(1.5, decoded.get(0).getValue()); assertEquals(2, decoded.get(1).getValue());
        decoded.resize(1); decoded.addElement(f.createHLAfloat64LE(3)); decoded.resize(3);
        assertEquals(3, decoded.get(1).getValue()); assertEquals(0, decoded.get(2).getValue());
        decoded.resize(0); assertArrayEquals(hex("0000000000000000"), decoded.toByteArray());
    }

    @Test void nestedArraysAndRecordsRespectSegmentOrigin() throws Exception {
        EncoderFactory f = factory();
        HLAvariableArray<HLAASCIIstring> strings = f.createHLAvariableArray(i -> f.createHLAASCIIstring(), f.createHLAASCIIstring("A"), f.createHLAASCIIstring("BC"));
        HLAfixedRecord outer = f.createHLAfixedRecord(); outer.add(f.createHLAoctet((byte) 9)); outer.add(strings); outer.add(f.createHLAinteger16LE((short) 0x1234));
        byte[] expected = hex("090000000000000200000001410000000000000242433412");
        assertArrayEquals(expected, outer.toByteArray());
        byte[] storage = new byte[expected.length + 9]; Arrays.fill(storage, (byte) 0x55);
        ByteWrapper wrapper = new ByteWrapper(storage, 3, expected.length + 4); wrapper.advance(1); outer.encode(wrapper);
        assertArrayEquals(expected, Arrays.copyOfRange(storage, 7, wrapper.getPos()));
        ByteWrapper input = new ByteWrapper(storage, 3, expected.length + 4); input.advance(1); outer.decode(input);
        assertEquals(wrapper.getPos(), input.getPos()); assertEquals("BC", strings.get(1).getValue());
    }

    @Test void variantAlignsToWidestAlternativeAndMatchesEncodedDiscriminants() throws Exception {
        EncoderFactory f = factory();
        HLAvariantRecord<HLAoctet> value = f.createHLAvariantRecord(f.createHLAoctet((byte) 2));
        value.setVariant(f.createHLAoctet((byte) 1), f.createHLAinteger32BE(0x01020304));
        HLAfixedRecord narrow = f.createHLAfixedRecord(); narrow.add(f.createHLAoctet((byte) 5)); narrow.add(f.createHLAoctet((byte) 6));
        value.setVariant(f.createHLAoctet((byte) 2), narrow);
        assertSame(narrow, value.getValue()); assertEquals(4, value.getOctetBoundary());
        assertArrayEquals(hex("020000000506"), value.toByteArray());
        value.setDiscriminant(f.createHLAoctet((byte) 1));
        assertArrayEquals(hex("0100000001020304"), value.toByteArray());
        value.decode(hex("020000000708"));
        assertEquals(2, value.getDiscriminant().getValue()); assertSame(narrow, value.getValue());
        assertEquals(7, ((HLAoctet) narrow.get(0)).getValue());
        assertEquals(8, ((HLAoctet) narrow.get(1)).getValue());
    }

    @Test void variantRegistrationSnapshotsKeysAndReplacesEqualEncoding() throws Exception {
        EncoderFactory f = factory();
        HLAoctet key = f.createHLAoctet((byte) 1);
        HLAvariantRecord<HLAoctet> value = f.createHLAvariantRecord(key);
        value.setVariant(key, f.createHLAinteger32BE(9)); key.setValue((byte) 2);
        value.setVariant(f.createHLAoctet((byte) 1), f.createHLAoctet((byte) 7));
        value.setDiscriminant(f.createHLAoctet((byte) 1));
        assertArrayEquals(hex("0107"), value.toByteArray());
        value.setDiscriminant(f.createHLAoctet((byte) 3));
        assertNull(value.getValue()); assertArrayEquals(hex("03"), value.toByteArray());
        value.decode(hex("04")); assertEquals(4, value.getDiscriminant().getValue()); assertNull(value.getValue());
    }

    @Test void emptyAndZeroWidthCompoundsAreUsable() throws Exception {
        EncoderFactory f = factory();
        HLAfixedRecord empty = f.createHLAfixedRecord();
        assertEquals(1, empty.getOctetBoundary()); assertArrayEquals(new byte[0], empty.toByteArray());
        assertArrayEquals(new byte[0], f.createHLAfixedArray().toByteArray());
        assertArrayEquals(new byte[0], f.createHLAfixedArray(i -> f.createHLAinteger64BE(), 0).toByteArray());
        HLAvariableArray<HLAfixedRecord> tokens = f.createHLAvariableArray(i -> f.createHLAfixedRecord());
        tokens.decode(hex("00000003")); assertEquals(3, tokens.size()); assertArrayEquals(hex("00000003"), tokens.toByteArray());
    }

    @Test void recordsAlignCallerSuppliedElements() throws Exception {
        EncoderFactory f = factory();
        HLAfixedRecord value = f.createHLAfixedRecord();
        Raw24 field = new Raw24(hex("020304"));
        value.add(f.createHLAoctet((byte) 1)); value.add(field);
        assertArrayEquals(hex("01000000020304"), value.toByteArray());
        value.decode(hex("09000000060708"));
        assertArrayEquals(hex("060708"), field.toByteArray());
        assertEquals(9, ((HLAoctet) value.get(0)).getValue());
    }

    @Test void arraysSupportNonPowerOfTwoElementLengths() throws Exception {
        EncoderFactory f = factory();
        HLAvariableArray<Raw24> value = f.createHLAvariableArray(i -> new Raw24(new byte[3]),
                new Raw24(hex("010203")), new Raw24(hex("040506")));
        assertArrayEquals(hex("0000000201020300040506"), value.toByteArray());
        value.decode(hex("00000002070809000a0b0c"));
        assertArrayEquals(hex("0a0b0c"), value.get(1).toByteArray());
    }

    @Test void fixedArrayCopiesVarargsContainerAndEmptyFactoryRetainsTypeBoundary() throws Exception {
        EncoderFactory f = factory();
        HLAinteger32BE[] source = {f.createHLAinteger32BE(1), f.createHLAinteger32BE(2)};
        HLAfixedArray<HLAinteger32BE> array = f.createHLAfixedArray(source);
        source[0] = f.createHLAinteger32BE(9);
        assertArrayEquals(hex("0000000100000002"), array.toByteArray());
        assertEquals(8, f.createHLAfixedArray(i -> f.createHLAinteger64BE(), 0).getOctetBoundary());
    }

    @Test void unmatchedDiscriminantIsPaddedToItsOwnBoundary() throws Exception {
        EncoderFactory f = factory();
        HLAvariantRecord<Raw24> variant = f.createHLAvariantRecord(new Raw24(hex("010203")));
        variant.setVariant(new Raw24(hex("040506")), f.createHLAinteger64BE(1));
        assertEquals(8, variant.getOctetBoundary());
        assertArrayEquals(hex("01020300"), variant.toByteArray());
        variant.decode(hex("07080900"));
        assertArrayEquals(hex("070809"), variant.getDiscriminant().toByteArray());
        assertNull(variant.getValue());
    }

    private static final class Raw24 implements DataElement {
        private byte[] value;
        Raw24(byte[] value) { this.value = value.clone(); }
        @Override public int getOctetBoundary() { return 4; }
        @Override public int getEncodedLength() { return 3; }
        @Override public byte[] toByteArray() { return value.clone(); }
        @Override public void encode(ByteWrapper wrapper) { wrapper.put(value); }
        @Override public void decode(ByteWrapper wrapper) { wrapper.get(value); }
        @Override public void decode(byte[] bytes) { value = Arrays.copyOf(bytes, 3); }
    }
}
