package org.gorti.rti;

import java.io.*;
import java.nio.ByteBuffer;
import java.nio.charset.CharacterCodingException;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;

/** Bounded, data-only checkpoint encoding. No Java object deserialization. */
final class CheckpointIO {
    static final int MAX_BYTES = 1 << 20;
    private CheckpointIO() { }

    static int count(DataInput input) throws IOException {
        return count(input.readInt());
    }

    static int count(int value) throws IOException {
        if (value < 0 || value > 100_000) throw new IOException("Invalid checkpoint collection size");
        return value;
    }

    static void string(DataOutput output, String value) throws IOException {
        byte[] encoded;
        try {
            ByteBuffer buffer = StandardCharsets.UTF_8.newEncoder().onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT).encode(java.nio.CharBuffer.wrap(value));
            encoded = new byte[buffer.remaining()]; buffer.get(encoded);
        } catch (CharacterCodingException invalid) { throw new IOException("Invalid checkpoint string", invalid); }
        if (encoded.length > MAX_BYTES) throw new IOException("Checkpoint string exceeds the size limit");
        output.writeInt(encoded.length);
        output.write(encoded);
    }

    static String string(DataInput input) throws IOException {
        int size = input.readInt();
        if (size < 0 || size > MAX_BYTES) throw new IOException("Invalid checkpoint string size");
        byte[] encoded = new byte[size];
        input.readFully(encoded);
        try {
            return StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                    .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(encoded)).toString();
        } catch (CharacterCodingException failure) { throw new IOException("Invalid checkpoint string", failure); }
    }

    static double time(DataInput input) throws IOException {
        double value = input.readDouble();
        if (!Double.isFinite(value) || value < 0) throw new IOException("Invalid checkpoint logical time");
        return value;
    }

    static long handle(DataInput input) throws IOException {
        long value = input.readLong();
        if (value == 0) throw new IOException("Invalid checkpoint handle");
        return value;
    }

    static DataOutputStream output(ByteArrayOutputStream bytes) {
        return new DataOutputStream(new FilterOutputStream(bytes) {
            private void reserve(int length) throws IOException {
                if (length < 0 || length > MAX_BYTES - bytes.size()) throw new IOException("SDK runtime checkpoint exceeds 1 MiB");
            }
            @Override public void write(int value) throws IOException { reserve(1); out.write(value); }
            @Override public void write(byte[] value, int start, int length) throws IOException { reserve(length); out.write(value, start, length); }
        });
    }
}
