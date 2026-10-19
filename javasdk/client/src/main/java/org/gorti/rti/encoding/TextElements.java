package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.nio.charset.StandardCharsets;
import java.util.Iterator;
import java.util.NoSuchElementException;
import java.util.Objects;

final class TextElements {
    private TextElements() { }

    private abstract static class Counted extends AbstractElement {
        @Override public final int getOctetBoundary() { return 4; }
        @Override final int minimumLength() { return 4; }
    }

    static final class ASCIIstring extends Counted implements HLAASCIIstring {
        private String value;
        ASCIIstring(String value) { setValue(value); }
        @Override public String getValue() { return value; }
        @Override public void setValue(String value) {
            Objects.requireNonNull(value, "value");
            for (int i = 0; i < value.length(); i++) {
                if (value.charAt(i) > 0x7f) throw new IllegalArgumentException("Not a standard ASCII string");
            }
            this.value = value;
        }
        @Override public int getEncodedLength() { return Math.addExact(4, value.length()); }
        @Override void encodeValue(ByteWrapper wrapper) {
            wrapper.putInt(value.length());
            for (int i = 0; i < value.length(); i++) wrapper.put(value.charAt(i));
        }
        @Override void decodeValue(ByteWrapper wrapper) {
            int count = EncodingSupport.readCount(wrapper, 1);
            byte[] decoded = new byte[count];
            wrapper.get(decoded);
            for (byte b : decoded) {
                if (b < 0) throw new IllegalArgumentException("Not a standard ASCII string");
            }
            value = new String(decoded, StandardCharsets.US_ASCII);
        }
    }

    static final class UnicodeString extends Counted implements HLAunicodeString {
        private String value;
        UnicodeString(String value) { setValue(value); }
        @Override public String getValue() { return value; }
        @Override public void setValue(String value) { this.value = Objects.requireNonNull(value, "value"); }
        @Override public int getEncodedLength() { return Math.addExact(4, Math.multiplyExact(2, value.length())); }
        @Override void encodeValue(ByteWrapper wrapper) {
            wrapper.putInt(value.length());
            // HLA counts UTF-16 code units, including both halves of a surrogate pair.
            for (int i = 0; i < value.length(); i++) {
                char codeUnit = value.charAt(i);
                wrapper.put(codeUnit >>> 8); wrapper.put(codeUnit & 0xff);
            }
        }
        @Override void decodeValue(ByteWrapper wrapper) {
            int count = EncodingSupport.readCount(wrapper, 2);
            char[] decoded = new char[count];
            for (int i = 0; i < count; i++) decoded[i] = (char) ((wrapper.get() << 8) | wrapper.get());
            value = new String(decoded);
        }
    }

    static final class OpaqueData extends Counted implements HLAopaqueData {
        private byte[] value;
        OpaqueData(byte[] value) { setValue(value); }
        @Override public int size() { return value.length; }
        @Override public byte get(int index) { return value[index]; }
        @Override public byte[] getValue() { return value.clone(); }
        @Override public void setValue(byte[] value) { this.value = Objects.requireNonNull(value, "value").clone(); }
        @Override public int getEncodedLength() { return Math.addExact(4, value.length); }
        @Override void encodeValue(ByteWrapper wrapper) { wrapper.putInt(value.length); wrapper.put(value); }
        @Override void decodeValue(ByteWrapper wrapper) {
            int count = EncodingSupport.readCount(wrapper, 1);
            byte[] decoded = new byte[count];
            wrapper.get(decoded);
            value = decoded;
        }
        @Override public Iterator<Byte> iterator() {
            byte[] snapshot = value;
            return new Iterator<Byte>() {
                private int index;
                @Override public boolean hasNext() { return index < snapshot.length; }
                @Override public Byte next() {
                    if (!hasNext()) throw new NoSuchElementException();
                    return snapshot[index++];
                }
            };
        }
    }
}
