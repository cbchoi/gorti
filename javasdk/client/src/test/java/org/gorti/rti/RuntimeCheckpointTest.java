package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;
import java.io.*;
import java.util.Set;
import java.util.Arrays;
import java.security.MessageDigest;
import org.junit.jupiter.api.Test;

class RuntimeCheckpointTest {
    private static RuntimeCheckpoint sample() throws Exception {
        ExchangeKnowledge knowledge = new ExchangeKnowledge(); knowledge.discovered(1, 2, "saved");
        DdmServices.State regions = DdmServices.State.readFrom(new DataInputStream(new ByteArrayInputStream(new byte[4])));
        return new RuntimeCheckpoint(new ExchangeServices.State(knowledge, 2, 4), regions, Set.of("ready"), true, false, false, true, true, 7);
    }

    @Test void roundTripIncludesPolicyAndMembershipIdentity() throws Exception {
        RuntimeCheckpoint copy = RuntimeCheckpoint.decode(sample().encode("federation", 7, 8, "save"), "federation", 7, 8, "save");
        assertEquals(Set.of("ready"), copy.synchronizations);
        assertTrue(copy.regulating); assertFalse(copy.constrained); assertFalse(copy.regulationPending);
        assertTrue(copy.constrainedPending); assertTrue(copy.advancePending); assertEquals(7, copy.logicalTime);
    }

    @Test void readsLegacyGrj1FloatCheckpoint() throws Exception {
        RuntimeCheckpoint source=sample();
        ByteArrayOutputStream bytes=new ByteArrayOutputStream();
        DataOutputStream output=new DataOutputStream(bytes);
        output.writeInt(0x47524a31);
        CheckpointIO.string(output,"federation");output.writeLong(7);output.writeLong(8);CheckpointIO.string(output,"save");
        source.exchangeState.writeTo(output);source.ddmState.writeTo(output);
        output.writeInt(1);CheckpointIO.string(output,"ready");output.writeByte(25);output.writeDouble(7);output.flush();
        RuntimeCheckpoint restored=RuntimeCheckpoint.decode(signed(bytes.toByteArray()),"federation",7,8,"save");
        assertEquals(7,restored.logicalTime);
        assertInstanceOf(hla.rti1516e.time.HLAfloat64Time.class,restored.logicalTimeValue);
    }

    @Test void rejectsWrongFederationGenerationParticipantOrLabel() throws Exception {
        byte[] bytes = sample().encode("federation", 7, 8, "save");
        assertAll(
            () -> assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(bytes, "other", 7, 8, "save")),
            () -> assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(bytes, "federation", 9, 8, "save")),
            () -> assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(bytes, "federation", 7, 9, "save")),
            () -> assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(bytes, "federation", 7, 8, "other")));
    }

    @Test void rejectsTruncationAndEverySingleByteCorruption() throws Exception {
        byte[] bytes = sample().encode("federation", 7, 8, "save");
        for (int i = 0; i < bytes.length; i++) {
            byte[] truncated = Arrays.copyOf(bytes, i);
            assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(truncated, "federation", 7, 8, "save"));
            byte[] corrupt = bytes.clone(); corrupt[i] ^= 1;
            assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(corrupt, "federation", 7, 8, "save"));
        }
    }

    @Test void rejectsFutureVersionsTrailingDataAndInvalidPolicyEvenWithValidDigest() throws Exception {
        byte[] bytes = sample().encode("federation", 7, 8, "save");
        byte[] body = Arrays.copyOf(bytes, bytes.length - 32);
        byte[] future = body.clone(); future[3]++;
        assertInvalid(signed(future));
        assertInvalid(signed(Arrays.copyOf(body, body.length + 1)));
        byte[] flags = body.clone(); flags[flags.length - 10] = 32;
        assertInvalid(signed(flags));
    }

    private static byte[] signed(byte[] body) throws Exception {
        byte[] result = Arrays.copyOf(body, body.length + 32);
        System.arraycopy(MessageDigest.getInstance("SHA-256").digest(body), 0, result, body.length, 32);
        return result;
    }
    private static void assertInvalid(byte[] bytes) {
        assertThrows(IOException.class, () -> RuntimeCheckpoint.decode(bytes, "federation", 7, 8, "save"));
    }
}
