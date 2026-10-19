package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.util.ArrayList;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.Iterator;
import java.util.List;
import java.util.Objects;
import java.util.Set;

final class ArrayElements {
    private ArrayElements() { }

    private static <T extends DataElement> void append(List<T> target, Set<DataElement> identities, T element) {
        Objects.requireNonNull(element, "Array element factory returned null");
        EncodingSupport.boundary(element.getOctetBoundary());
        if (!identities.add(element)) throw new IllegalArgumentException("Array elements must be distinct mutable instances");
        target.add(element);
    }

    private static <T extends DataElement> List<T> copy(T[] source) {
        Objects.requireNonNull(source, "elements");
        EncodingSupport.arraySize(source.length);
        List<T> result = new ArrayList<>(source.length);
        Set<DataElement> identities = Collections.newSetFromMap(new IdentityHashMap<>());
        for (T element : source) append(result, identities, element);
        return result;
    }

    private static <T extends DataElement> List<T> resized(List<T> source, DataElementFactory<T> factory, int size) {
        EncodingSupport.arraySize(size);
        List<T> result = new ArrayList<>(size);
        Set<DataElement> identities = Collections.newSetFromMap(new IdentityHashMap<>());
        for (int i = 0; i < Math.min(size, source.size()); i++) append(result, identities, source.get(i));
        for (int i = source.size(); i < size; i++) append(result, identities, factory.createElement(i));
        return result;
    }

    private abstract static class Sequence<T extends DataElement> extends AbstractElement implements Iterable<T> {
        List<T> elements;
        T template;

        Sequence(List<T> elements, T template) {
            this.elements = elements;
            this.template = template;
        }

        public final int size() { return elements.size(); }
        public final T get(int index) { return elements.get(index); }
        @Override public final Iterator<T> iterator() { return Collections.unmodifiableList(elements).iterator(); }

        final int elementBoundary() {
            int boundary = template == null ? 1 : EncodingSupport.boundary(template.getOctetBoundary());
            for (T element : elements) boundary = Math.max(boundary, EncodingSupport.boundary(element.getOctetBoundary()));
            return boundary;
        }

        final int lengthFrom(int start, boolean minimum) {
            int length = start;
            int boundary = elementBoundary();
            for (T element : elements) {
                length = EncodingSupport.aligned(length, boundary);
                length = Math.addExact(length, minimum ? EncodingSupport.minimumLength(element) : element.getEncodedLength());
            }
            return length;
        }

        final void encodeElements(ByteWrapper wrapper) {
            int boundary = elementBoundary();
            for (T element : elements) {
                EncodingSupport.align(wrapper, boundary, true);
                element.encode(wrapper);
            }
        }

        final void decodeElements(ByteWrapper wrapper, List<T> decoded) throws DecoderException {
            int boundary = elementBoundary();
            for (T element : decoded) {
                if (element.getOctetBoundary() > boundary) {
                    throw new DecoderException("Array factory changed the element type's octet boundary");
                }
                EncodingSupport.align(wrapper, boundary, false);
                element.decode(wrapper);
            }
        }
    }

    static final class Fixed<T extends DataElement> extends Sequence<T> implements HLAfixedArray<T> {
        Fixed(T[] elements) { this(copy(elements)); }

        private Fixed(List<T> elements) { super(elements, elements.isEmpty() ? null : elements.get(0)); }

        Fixed(DataElementFactory<T> factory, int size) {
            this(resized(Collections.emptyList(), Objects.requireNonNull(factory, "factory"), size));
            if (size == 0) template = Objects.requireNonNull(factory.createElement(0), "Array element factory returned null");
        }

        @Override public int getOctetBoundary() { return elementBoundary(); }
        @Override public int getEncodedLength() { return lengthFrom(0, false); }
        @Override int minimumLength() { return lengthFrom(0, true); }
        @Override void encodeValue(ByteWrapper wrapper) { encodeElements(wrapper); }
        @Override void decodeValue(ByteWrapper wrapper) throws DecoderException {
            DecodeBudget.reserve(elements.size());
            decodeElements(wrapper, elements);
        }
    }

    static final class Variable<T extends DataElement> extends Sequence<T> implements HLAvariableArray<T> {
        private final DataElementFactory<T> factory;

        Variable(DataElementFactory<T> factory, T[] elements) {
            super(copy(elements), null);
            this.factory = Objects.requireNonNull(factory, "factory");
            template = this.elements.isEmpty()
                    ? Objects.requireNonNull(factory.createElement(0), "Array element factory returned null") : this.elements.get(0);
            EncodingSupport.boundary(template.getOctetBoundary());
        }

        @Override public int getOctetBoundary() { return Math.max(4, elementBoundary()); }
        private int headerLength() { return EncodingSupport.aligned(4, getOctetBoundary()); }
        @Override public int getEncodedLength() { return lengthFrom(headerLength(), false); }
        @Override int minimumLength() { return headerLength(); }

        @Override public void addElement(T element) {
            Objects.requireNonNull(element, "element");
            EncodingSupport.arraySize(Math.addExact(elements.size(), 1));
            EncodingSupport.boundary(element.getOctetBoundary());
            for (T existing : elements) {
                if (existing == element) throw new IllegalArgumentException("Array elements must be distinct mutable instances");
            }
            elements.add(element);
        }

        @Override public void resize(int size) {
            // Build additions first so a failing factory cannot partially resize the array.
            elements = resized(elements, factory, size);
        }

        @Override void encodeValue(ByteWrapper wrapper) {
            wrapper.putInt(elements.size());
            EncodingSupport.align(wrapper, getOctetBoundary(), true);
            encodeElements(wrapper);
        }

        @Override void decodeValue(ByteWrapper wrapper) throws DecoderException {
            EncodingSupport.requireRemaining(wrapper, 4);
            int size = wrapper.getInt();
            EncodingSupport.arraySize(size);
            EncodingSupport.align(wrapper, getOctetBoundary(), false);
            int minimum = EncodingSupport.minimumLength(template);
            for (T element : elements) minimum = Math.min(minimum, EncodingSupport.minimumLength(element));
            if (minimum > 0 && size > wrapper.remaining() / minimum) {
                throw new DecoderException("Array element count exceeds the available encoded data");
            }
            DecodeBudget.reserve(size);
            List<T> decoded = resized(elements, factory, size);
            decodeElements(wrapper, decoded);
            elements = decoded;
        }
    }
}
