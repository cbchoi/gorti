package org.gorti.rti.encoding;

import hla.rti1516e.encoding.ByteWrapper;
import hla.rti1516e.encoding.DataElement;
import java.util.Arrays;

final class EncodingSupport {
    static final int MAX_ARRAY_ELEMENTS = 1_048_576;

    private EncodingSupport() { }

    static int boundary(int value) {
        if (value <= 0 || (value & (value - 1)) != 0) {
            throw new IllegalArgumentException("Octet boundary must be a positive power of two");
        }
        return value;
    }

    static int aligned(int length, int alignment) {
        boundary(alignment);
        if (length < 0) throw new IllegalArgumentException("Negative encoded length");
        return Math.addExact(length, (alignment - length % alignment) % alignment);
    }

    static void align(ByteWrapper wrapper, int alignment, boolean writing) {
        validateWrapper(wrapper);
        boundary(alignment);
        int start = wrapper.getPos();
        // ByteWrapper owns the segment origin; absolute array indices do not define alignment.
        wrapper.align(alignment);
        if (writing) Arrays.fill(wrapper.array(), start, wrapper.getPos(), (byte) 0);
    }

    static void requireRemaining(ByteWrapper wrapper, int length) {
        validateWrapper(wrapper);
        if (length < 0 || length > wrapper.remaining()) {
            throw new IllegalArgumentException("Encoded data exceeds the ByteWrapper segment");
        }
    }

    private static void validateWrapper(ByteWrapper wrapper) {
        int position = wrapper.getPos();
        int remaining = wrapper.remaining();
        if (position < 0 || position > wrapper.array().length || remaining < 0
                || remaining > wrapper.array().length - position) {
            throw new IllegalArgumentException("Invalid ByteWrapper segment bounds");
        }
    }

    static int readCount(ByteWrapper wrapper, int octetsPerElement) {
        requireRemaining(wrapper, 4);
        int count = wrapper.getInt();
        if (count < 0 || count > wrapper.remaining() / octetsPerElement) {
            throw new IllegalArgumentException("Invalid or truncated element count: " + count);
        }
        return count;
    }

    static void arraySize(int count) {
        if (count < 0 || count > MAX_ARRAY_ELEMENTS) {
            throw new IllegalArgumentException("Array element count must be between 0 and " + MAX_ARRAY_ELEMENTS);
        }
    }

    static int minimumLength(DataElement element) {
        return element instanceof AbstractElement ? ((AbstractElement) element).minimumLength() : 0;
    }
}
