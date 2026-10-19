package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Iterator;
import java.util.List;
import java.util.Objects;

final class FixedRecord extends AbstractElement implements HLAfixedRecord {
    private final List<DataElement> fields = new ArrayList<>();

    @Override public void add(DataElement element) { fields.add(Objects.requireNonNull(element, "element")); }
    @Override public int size() { return fields.size(); }
    @Override public DataElement get(int index) { return fields.get(index); }
    @Override public Iterator<DataElement> iterator() { return Collections.unmodifiableList(fields).iterator(); }

    @Override public int getOctetBoundary() {
        int boundary = 1;
        for (DataElement field : fields) boundary = Math.max(boundary, EncodingSupport.boundary(field.getOctetBoundary()));
        return boundary;
    }

    private int length(boolean minimum) {
        int length = 0;
        for (DataElement field : fields) {
            length = EncodingSupport.aligned(length, field.getOctetBoundary());
            length = Math.addExact(length, minimum ? EncodingSupport.minimumLength(field) : field.getEncodedLength());
        }
        return length;
    }

    @Override public int getEncodedLength() { return length(false); }
    @Override int minimumLength() { return length(true); }
    @Override void encodeValue(ByteWrapper wrapper) {
        for (DataElement field : fields) {
            EncodingSupport.align(wrapper, field.getOctetBoundary(), true);
            field.encode(wrapper);
        }
    }
    @Override void decodeValue(ByteWrapper wrapper) throws DecoderException {
        for (DataElement field : fields) {
            EncodingSupport.align(wrapper, field.getOctetBoundary(), false);
            field.decode(wrapper);
        }
    }
}
