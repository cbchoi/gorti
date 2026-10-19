package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotDecode;
import hla.rti1516e.exceptions.CouldNotEncode;
import java.nio.ByteBuffer;

final class Integer64Encoding {
    private Integer64Encoding() {}

    static void encode(long value, byte[] buffer, int offset) throws CouldNotEncode {
        if (!fits(buffer, offset)) {
            throw new CouldNotEncode("Integer64 requires eight bytes at offset " + offset);
        }
        ByteBuffer.wrap(buffer, offset, Long.BYTES).putLong(value);
    }

    static long decode(byte[] buffer, int offset) throws CouldNotDecode {
        if (!fits(buffer, offset)) {
            throw new CouldNotDecode("Integer64 requires eight bytes at offset " + offset);
        }
        long value = ByteBuffer.wrap(buffer, offset, Long.BYTES).getLong();
        try {
            return Integer64Arithmetic.validValue(value);
        } catch (IllegalArgumentException invalid) {
            throw new CouldNotDecode(invalid.getMessage(), invalid);
        }
    }

    private static boolean fits(byte[] buffer, int offset) {
        return buffer != null && offset >= 0 && offset <= buffer.length - Long.BYTES;
    }
}
