package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

final class VariantRecord<T extends DataElement> extends AbstractElement implements HLAvariantRecord<T> {
    private T discriminant;
    private final Map<EncodedKey, DataElement> alternatives = new LinkedHashMap<>();

    VariantRecord(T discriminant) { setDiscriminant(discriminant); }

    @Override public void setVariant(T discriminant, DataElement element) {
        alternatives.put(new EncodedKey(Objects.requireNonNull(discriminant, "discriminant")),
                Objects.requireNonNull(element, "element"));
    }
    @Override public void setDiscriminant(T discriminant) { this.discriminant = Objects.requireNonNull(discriminant, "discriminant"); }
    @Override public T getDiscriminant() { return discriminant; }
    @Override public DataElement getValue() { return alternatives.get(new EncodedKey(discriminant)); }

    private int alternativeBoundary() {
        int boundary = 1;
        for (DataElement element : alternatives.values()) {
            boundary = Math.max(boundary, EncodingSupport.boundary(element.getOctetBoundary()));
        }
        return boundary;
    }

    @Override public int getOctetBoundary() {
        return Math.max(EncodingSupport.boundary(discriminant.getOctetBoundary()), alternativeBoundary());
    }

    @Override public int getEncodedLength() {
        DataElement value = getValue();
        int paddingBoundary = value == null ? discriminant.getOctetBoundary() : alternativeBoundary();
        int prefix = EncodingSupport.aligned(discriminant.getEncodedLength(), paddingBoundary);
        return value == null ? prefix : Math.addExact(prefix, value.getEncodedLength());
    }

    @Override int minimumLength() { return EncodingSupport.minimumLength(discriminant); }

    @Override void encodeValue(ByteWrapper wrapper) {
        discriminant.encode(wrapper);
        DataElement value = getValue();
        // Even a narrow active alternative uses the maximum boundary of ALL alternatives.
        EncodingSupport.align(wrapper, value == null ? discriminant.getOctetBoundary() : alternativeBoundary(), true);
        if (value != null) value.encode(wrapper);
    }

    @Override void decodeValue(ByteWrapper wrapper) throws DecoderException {
        discriminant.decode(wrapper);
        DataElement value = getValue();
        EncodingSupport.align(wrapper, value == null ? discriminant.getOctetBoundary() : alternativeBoundary(), false);
        if (value != null) value.decode(wrapper);
    }

    private static final class EncodedKey {
        private final byte[] bytes;
        EncodedKey(DataElement element) { bytes = element.toByteArray().clone(); }
        @Override public int hashCode() { return Arrays.hashCode(bytes); }
        @Override public boolean equals(Object other) {
            return other instanceof EncodedKey && Arrays.equals(bytes, ((EncodedKey) other).bytes);
        }
    }
}
