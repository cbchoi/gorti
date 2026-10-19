package org.gorti.rti.types;

import hla.rti1516e.exceptions.CouldNotDecode;
import hla.rti1516e.exceptions.CouldNotEncode;
import java.nio.ByteBuffer;

final class Float64Encoding {
    private Float64Encoding() {}

    static void encode(double value, byte[] buffer, int offset) throws CouldNotEncode {
        if (!fits(buffer, offset)) {
            throw new CouldNotEncode("Float64 requires eight bytes at offset " + offset);
        }
        ByteBuffer.wrap(buffer, offset, Double.BYTES).putDouble(value);
    }

    static double decode(byte[] buffer, int offset) throws CouldNotDecode {
        if (!fits(buffer, offset)) {
            throw new CouldNotDecode("Float64 requires eight bytes at offset " + offset);
        }
        double value = ByteBuffer.wrap(buffer, offset, Double.BYTES).getDouble();
        try {
            return TimeValues.validValue(value);
        } catch (IllegalArgumentException invalid) {
            throw new CouldNotDecode(invalid.getMessage(), invalid);
        }
    }

    private static boolean fits(byte[] buffer, int offset) {
        return buffer != null && offset >= 0 && offset <= buffer.length - Double.BYTES;
    }
}
