package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import java.io.*;
import hla.rti1516e.exceptions.ObjectInstanceNotKnown;
import org.junit.jupiter.api.Test;

class CheckpointEncodingTest {
    @Test void roundTripPreservesProvisionalRemovalAndRetraction() throws Exception {
        ExchangeKnowledge before = new ExchangeKnowledge();
        before.discovered(10, 20, "item");
        before.removed(10, true, 7, 8, 5, 1);
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        before.writeTo(CheckpointIO.output(bytes));
        ExchangeKnowledge after = ExchangeKnowledge.readFrom(new DataInputStream(new ByteArrayInputStream(bytes.toByteArray())));
        assertEquals(20, after.object(10).objectClass);
        after.retracted(7, 8); after.advance(6);
        assertEquals(10, after.object("item"));
        before.advance(6);
        assertThrows(ObjectInstanceNotKnown.class, () -> before.object(10));
    }

    @Test void rejectsMalformedCountsAndTruncatedData() throws Exception {
        for (int value : new int[]{-1, Integer.MAX_VALUE, 100_001}) {
            ByteArrayOutputStream bytes = new ByteArrayOutputStream();
            new DataOutputStream(bytes).writeInt(value);
            assertThrows(IOException.class, () -> ExchangeKnowledge.readFrom(new DataInputStream(new ByteArrayInputStream(bytes.toByteArray()))));
        }
        ExchangeKnowledge state = new ExchangeKnowledge(); state.discovered(1, 2, "sample");
        ByteArrayOutputStream bytes = new ByteArrayOutputStream(); state.writeTo(CheckpointIO.output(bytes));
        byte[] valid = bytes.toByteArray();
        for (int i = 0; i < valid.length; i++) {
            byte[] truncated = java.util.Arrays.copyOf(valid, i);
            assertThrows(IOException.class, () -> ExchangeKnowledge.readFrom(new DataInputStream(new ByteArrayInputStream(truncated))));
        }
    }

    @Test void outputIsBoundedBeforeAllocationGrowsPastLimit() throws Exception {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutputStream output = CheckpointIO.output(bytes);
        output.write(new byte[CheckpointIO.MAX_BYTES]);
        assertThrows(IOException.class, () -> output.write(1));
        assertEquals(CheckpointIO.MAX_BYTES, bytes.size());
    }
}
