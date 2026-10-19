package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;

/**
 * IEEE 1516.2 encoders. Element instances are mutable and are not thread-safe.
 * Object arrays are limited to 1,048,576 entries to bound factory-driven allocations,
 * including arrays of zero-width elements. Nested decoding shares this entry budget
 * across all object arrays in the value. Strings and opaque bytes are bounded by
 * the supplied buffer instead. Empty factory-created arrays inspect index zero
 * once to determine their element type's alignment.
 */
public final class GortiEncoderFactory implements EncoderFactory {
    @Override public HLAASCIIchar createHLAASCIIchar() { return createHLAASCIIchar((byte) 0); }
    @Override public HLAASCIIchar createHLAASCIIchar(byte value) { return new ScalarElements.ASCIIchar(value); }
    @Override public HLAASCIIstring createHLAASCIIstring() { return createHLAASCIIstring(""); }
    @Override public HLAASCIIstring createHLAASCIIstring(String value) { return new TextElements.ASCIIstring(value); }
    @Override public HLAboolean createHLAboolean() { return createHLAboolean(false); }
    @Override public HLAboolean createHLAboolean(boolean value) { return new ScalarElements.BooleanElement(value); }
    @Override public HLAbyte createHLAbyte() { return createHLAbyte((byte) 0); }
    @Override public HLAbyte createHLAbyte(byte value) { return new ScalarElements.ByteElement(value); }
    @Override public HLAfloat32BE createHLAfloat32BE() { return createHLAfloat32BE(0); }
    @Override public HLAfloat32BE createHLAfloat32BE(float value) { return new ScalarElements.Float32BE(value); }
    @Override public HLAfloat32LE createHLAfloat32LE() { return createHLAfloat32LE(0); }
    @Override public HLAfloat32LE createHLAfloat32LE(float value) { return new ScalarElements.Float32LE(value); }
    @Override public HLAfloat64BE createHLAfloat64BE() { return createHLAfloat64BE(0); }
    @Override public HLAfloat64BE createHLAfloat64BE(double value) { return new ScalarElements.Float64BE(value); }
    @Override public HLAfloat64LE createHLAfloat64LE() { return createHLAfloat64LE(0); }
    @Override public HLAfloat64LE createHLAfloat64LE(double value) { return new ScalarElements.Float64LE(value); }
    @Override public HLAinteger16BE createHLAinteger16BE() { return createHLAinteger16BE((short) 0); }
    @Override public HLAinteger16BE createHLAinteger16BE(short value) { return new ScalarElements.Integer16BE(value); }
    @Override public HLAinteger16LE createHLAinteger16LE() { return createHLAinteger16LE((short) 0); }
    @Override public HLAinteger16LE createHLAinteger16LE(short value) { return new ScalarElements.Integer16LE(value); }
    @Override public HLAinteger32BE createHLAinteger32BE() { return createHLAinteger32BE(0); }
    @Override public HLAinteger32BE createHLAinteger32BE(int value) { return new ScalarElements.Integer32BE(value); }
    @Override public HLAinteger32LE createHLAinteger32LE() { return createHLAinteger32LE(0); }
    @Override public HLAinteger32LE createHLAinteger32LE(int value) { return new ScalarElements.Integer32LE(value); }
    @Override public HLAinteger64BE createHLAinteger64BE() { return createHLAinteger64BE(0); }
    @Override public HLAinteger64BE createHLAinteger64BE(long value) { return new ScalarElements.Integer64BE(value); }
    @Override public HLAinteger64LE createHLAinteger64LE() { return createHLAinteger64LE(0); }
    @Override public HLAinteger64LE createHLAinteger64LE(long value) { return new ScalarElements.Integer64LE(value); }
    @Override public HLAoctet createHLAoctet() { return createHLAoctet((byte) 0); }
    @Override public HLAoctet createHLAoctet(byte value) { return new ScalarElements.Octet(value); }
    @Override public HLAoctetPairBE createHLAoctetPairBE() { return createHLAoctetPairBE((short) 0); }
    @Override public HLAoctetPairBE createHLAoctetPairBE(short value) { return new ScalarElements.OctetPairBE(value); }
    @Override public HLAoctetPairLE createHLAoctetPairLE() { return createHLAoctetPairLE((short) 0); }
    @Override public HLAoctetPairLE createHLAoctetPairLE(short value) { return new ScalarElements.OctetPairLE(value); }
    @Override public HLAopaqueData createHLAopaqueData() { return createHLAopaqueData(new byte[0]); }
    @Override public HLAopaqueData createHLAopaqueData(byte[] value) { return new TextElements.OpaqueData(value); }
    @Override public HLAunicodeChar createHLAunicodeChar() { return createHLAunicodeChar((short) 0); }
    @Override public HLAunicodeChar createHLAunicodeChar(short value) { return new ScalarElements.UnicodeChar(value); }
    @Override public HLAunicodeString createHLAunicodeString() { return createHLAunicodeString(""); }
    @Override public HLAunicodeString createHLAunicodeString(String value) { return new TextElements.UnicodeString(value); }
    @Override public HLAfixedRecord createHLAfixedRecord() { return new FixedRecord(); }
    @Override public <T extends DataElement> HLAvariantRecord<T> createHLAvariantRecord(T discriminant) { return new VariantRecord<>(discriminant); }
    @Override public <T extends DataElement> HLAfixedArray<T> createHLAfixedArray(DataElementFactory<T> factory, int size) { return new ArrayElements.Fixed<>(factory, size); }
    @SafeVarargs @SuppressWarnings("varargs")
    @Override public final <T extends DataElement> HLAfixedArray<T> createHLAfixedArray(T... elements) { return new ArrayElements.Fixed<>(elements); }
    @SafeVarargs @SuppressWarnings("varargs")
    @Override public final <T extends DataElement> HLAvariableArray<T> createHLAvariableArray(DataElementFactory<T> factory, T... elements) { return new ArrayElements.Variable<>(factory, elements); }
}
