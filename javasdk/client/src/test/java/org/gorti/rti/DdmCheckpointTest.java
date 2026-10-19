package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import org.junit.jupiter.api.Test;

class DdmCheckpointTest {
    @Test void pendingRangesRoundTripUnsignedBoundsWithoutConsumingNextState() throws Exception {
        byte[] encoded = bytes(out -> {
            out.writeInt(2);
            out.writeLong(11); out.writeInt(2);
            out.writeLong(1); out.writeLong(0); out.writeLong(10);
            out.writeLong(2); out.writeLong(Long.MIN_VALUE); out.writeLong(-1L);
            out.writeLong(12); out.writeInt(1);
            out.writeLong(1); out.writeLong(20); out.writeLong(30);
        });
        DataInputStream in = new DataInputStream(new ByteArrayInputStream(encoded));
        DdmServices.State state = DdmServices.State.readFrom(in);
        assertEquals(0, in.available());
        assertArrayEquals(encoded, bytes(state::writeTo));
        byte[] framed = bytes(out -> { state.writeTo(out); out.writeInt(42); });
        in = new DataInputStream(new ByteArrayInputStream(framed));
        DdmServices.State.readFrom(in);
        assertEquals(42, in.readInt());
    }

    @Test void malformedCountsHandlesBoundsAndTruncationAreRejected() throws Exception {
        for (int count : new int[]{-1, 100001, Integer.MAX_VALUE}) {
            reject(bytes(out -> out.writeInt(count)));
            reject(bytes(out -> { out.writeInt(1); out.writeLong(11); out.writeInt(count); }));
        }
        reject(bytes(out -> { out.writeInt(1); out.writeLong(0); out.writeInt(0); }));
        reject(bytes(out -> { out.writeInt(2); out.writeLong(11); out.writeInt(0); out.writeLong(11); out.writeInt(0); }));
        reject(bytes(out -> { out.writeInt(1); out.writeLong(11); out.writeInt(1); out.writeLong(0); out.writeLong(0); out.writeLong(1); }));
        reject(bytes(out -> {
            out.writeInt(1); out.writeLong(11); out.writeInt(2);
            for (int i = 0; i < 2; i++) { out.writeLong(1); out.writeLong(0); out.writeLong(10); }
        }));
        for (long[] bounds : new long[][]{{1, 1}, {2, 1}, {-1L, 0}}) {
            reject(bytes(out -> { out.writeInt(1); out.writeLong(11); out.writeInt(1); out.writeLong(1); out.writeLong(bounds[0]); out.writeLong(bounds[1]); }));
        }
        reject(bytes(out -> { out.writeInt(1); out.writeLong(11); out.writeInt(1); }));
    }

    private static void reject(byte[] encoded) {
        assertThrows(IOException.class, () -> DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(encoded))));
    }
    private static byte[] bytes(Writer writer) throws IOException {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        try (DataOutputStream out = new DataOutputStream(bytes)) { writer.write(out); }
        return bytes.toByteArray();
    }
    private interface Writer { void write(DataOutputStream out) throws IOException; }
}
