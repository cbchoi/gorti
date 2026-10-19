package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import java.io.ByteArrayInputStream;
import java.io.DataInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.util.HashSet;
import java.util.Set;
import org.junit.jupiter.api.Test;

class RuntimeCheckpointReviewTest {
    @Test
    void synchronizationCountAtLimitRoundTrips() throws Exception {
        Set<String> labels = labels(100000);
        byte[] encoded = checkpoint(labels).encode("fed", 1, 1, "saved");
        assertTrue(encoded.length < CheckpointIO.MAX_BYTES);
        assertEquals(labels, RuntimeCheckpoint.decode(encoded, "fed", 1, 1, "saved").synchronizations);
    }

    @Test
    void synchronizationCountAboveReaderLimitIsRejectedBeforeSave() throws Exception {
        RuntimeCheckpoint checkpoint = checkpoint(labels(100001));
        IOException failure = assertThrows(IOException.class, () -> checkpoint.encode("fed", 1, 1, "saved"));
        assertEquals("Invalid checkpoint collection size", failure.getMessage());
    }

    @Test
    void objectCountAboveReaderLimitIsRejectedByNestedEncoder() throws Exception {
        ExchangeKnowledge knowledge = new ExchangeKnowledge();
        for (int i = 1; i <= 100001; i++) knowledge.discovered(i, 1, Integer.toString(i));
        IOException failure = assertThrows(IOException.class,
                () -> knowledge.writeTo(new DataOutputStream(new ByteArrayOutputStream())));
        assertEquals("Invalid checkpoint collection size", failure.getMessage());
    }

    @Test
    void totalByteLimitIncludesChecksumAndRoundTripsAtBoundary() throws Exception {
        int overhead = checkpoint(Set.of("")).encode("fed", 1, 1, "saved").length;
        String label = "x".repeat(CheckpointIO.MAX_BYTES - overhead);
        byte[] encoded = checkpoint(Set.of(label)).encode("fed", 1, 1, "saved");
        assertEquals(CheckpointIO.MAX_BYTES, encoded.length);
        assertEquals(Set.of(label), RuntimeCheckpoint.decode(encoded, "fed", 1, 1, "saved").synchronizations);
        assertThrows(IOException.class, () -> checkpoint(Set.of(label + "x")).encode("fed", 1, 1, "saved"));
    }

    private static Set<String> labels(int count) {
        Set<String> labels = new HashSet<>();
        for (int i = 0; i < count; i++) labels.add(Integer.toString(i, 36));
        return labels;
    }

    private static RuntimeCheckpoint checkpoint(Set<String> labels) throws IOException {
        DdmServices.State regions = DdmServices.State.readFrom(
                new DataInputStream(new ByteArrayInputStream(new byte[4])));
        return new RuntimeCheckpoint(
                new ExchangeServices.State(new ExchangeKnowledge(), 0, 0), regions, labels,
                false, false, false, false, false, 0);
    }
}
