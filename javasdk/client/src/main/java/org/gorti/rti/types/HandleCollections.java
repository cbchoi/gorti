package org.gorti.rti.types;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.AttributeHandleSetFactory;
import hla.rti1516e.AttributeHandleValueMap;
import hla.rti1516e.AttributeHandleValueMapFactory;
import hla.rti1516e.AttributeRegionAssociation;
import hla.rti1516e.AttributeSetRegionSetPairList;
import hla.rti1516e.AttributeSetRegionSetPairListFactory;
import hla.rti1516e.DimensionHandle;
import hla.rti1516e.DimensionHandleSet;
import hla.rti1516e.DimensionHandleSetFactory;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.FederateHandleSet;
import hla.rti1516e.FederateHandleSetFactory;
import hla.rti1516e.ParameterHandle;
import hla.rti1516e.ParameterHandleValueMap;
import hla.rti1516e.ParameterHandleValueMapFactory;
import hla.rti1516e.RegionHandle;
import hla.rti1516e.RegionHandleSet;
import hla.rti1516e.RegionHandleSetFactory;
import hla.rti1516e.encoding.ByteWrapper;
import java.io.Serializable;
import java.util.AbstractList;
import java.util.AbstractMap;
import java.util.AbstractSet;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.Map;
import java.util.Objects;
import java.util.RandomAccess;
import java.util.Set;
import java.util.function.BiFunction;
import java.util.function.Function;

/** Mutable IEEE collections with shallow clones and ordinary Java reference semantics. */
public final class HandleCollections {
    private HandleCollections() {}

    public static AttributeHandleSetFactory attributeSetFactory() { return AttributeSetFactory.INSTANCE; }
    public static FederateHandleSetFactory federateSetFactory() { return FederateSetFactory.INSTANCE; }
    public static DimensionHandleSetFactory dimensionSetFactory() { return DimensionSetFactory.INSTANCE; }
    public static RegionHandleSetFactory regionSetFactory() { return RegionSetFactory.INSTANCE; }
    public static AttributeHandleValueMapFactory attributeValueMapFactory() {
        return AttributeValueMapFactory.INSTANCE;
    }
    public static ParameterHandleValueMapFactory parameterValueMapFactory() {
        return ParameterValueMapFactory.INSTANCE;
    }
    public static AttributeSetRegionSetPairListFactory attributeRegionListFactory() {
        return AttributeRegionListFactory.INSTANCE;
    }

    private static <T> T requireType(Class<T> type, Object value) {
        if (!type.isInstance(value)) {
            throw new IllegalArgumentException("Expected a non-null " + type.getSimpleName());
        }
        return type.cast(value);
    }

    // Unbounded type parameters let raw mutations reach validation instead of compiler-generated casts.
    private abstract static class HandleSet<E> extends AbstractSet<E> implements Cloneable, Serializable {
        private static final long serialVersionUID = 1L;
        private final Class<E> elementType;
        private final Class<?> setType;
        private LinkedHashSet<E> elements = new LinkedHashSet<>();

        HandleSet(Class<E> elementType, Class<?> setType) {
            this.elementType = elementType;
            this.setType = setType;
        }

        @Override
        public int size() { return elements.size(); }

        @Override
        public Iterator<E> iterator() { return elements.iterator(); }

        @Override
        public boolean contains(Object value) { return elements.contains(value); }

        @Override
        public boolean add(E value) { return elements.add(requireType(elementType, value)); }

        @Override
        public boolean remove(Object value) { return elements.remove(requireType(elementType, value)); }

        private void checkSet(Collection<?> values) {
            requireType(setType, values);
            for (Object value : values) requireType(elementType, value);
        }

        @Override
        public boolean addAll(Collection<? extends E> values) {
            checkSet(values);
            return elements.addAll(values);
        }

        @Override
        public boolean removeAll(Collection<?> values) {
            checkSet(values);
            return elements.removeAll(values);
        }

        @Override
        public boolean retainAll(Collection<?> values) {
            checkSet(values);
            return elements.retainAll(values);
        }

        @Override
        public void clear() { elements.clear(); }

        @Override
        @SuppressWarnings("unchecked")
        public HandleSet<E> clone() {
            try {
                HandleSet<E> copy = (HandleSet<E>) super.clone();
                copy.elements = new LinkedHashSet<>(elements);
                return copy;
            } catch (CloneNotSupportedException impossible) {
                throw new AssertionError(impossible);
            }
        }
    }

    private static final class AttributeSet extends HandleSet<AttributeHandle> implements AttributeHandleSet {
        private static final long serialVersionUID = 1L;
        AttributeSet() { super(AttributeHandle.class, AttributeHandleSet.class); }

        @Override
        public AttributeSet clone() { return (AttributeSet) super.clone(); }
    }

    private static final class FederateSet extends HandleSet<FederateHandle> implements FederateHandleSet {
        private static final long serialVersionUID = 1L;
        FederateSet() { super(FederateHandle.class, FederateHandleSet.class); }
    }

    private static final class DimensionSet extends HandleSet<DimensionHandle> implements DimensionHandleSet {
        private static final long serialVersionUID = 1L;
        DimensionSet() { super(DimensionHandle.class, DimensionHandleSet.class); }
    }

    private static final class RegionSet extends HandleSet<RegionHandle> implements RegionHandleSet {
        private static final long serialVersionUID = 1L;
        RegionSet() { super(RegionHandle.class, RegionHandleSet.class); }
    }

    private abstract static class ValueMap<K, V> extends AbstractMap<K, V> implements Cloneable, Serializable {
        private static final long serialVersionUID = 1L;
        private final Class<K> keyType;
        private final Class<V> valueType;
        private LinkedHashMap<K, V> mappings;

        ValueMap(Class<K> keyType, Class<V> valueType, int capacity) {
            this.keyType = keyType;
            this.valueType = valueType;
            mappings = new LinkedHashMap<>(capacity);
        }

        @Override
        public int size() { return mappings.size(); }

        @Override
        public boolean containsKey(Object key) { return mappings.containsKey(key); }

        @Override
        public boolean containsValue(Object value) { return mappings.containsValue(value); }

        @Override
        public V get(Object key) { return mappings.get(key); }

        @Override
        public V put(K key, V value) {
            return mappings.put(requireType(keyType, key), requireType(valueType, value));
        }

        @Override
        public void putAll(Map<? extends K, ? extends V> values) {
            requireType(Map.class, values);
            for (Entry<?, ?> entry : values.entrySet()) {
                requireType(keyType, entry.getKey());
                requireType(valueType, entry.getValue());
            }
            for (Entry<? extends K, ? extends V> entry : values.entrySet()) {
                put(entry.getKey(), entry.getValue());
            }
        }

        @Override
        public V remove(Object key) { return mappings.remove(requireType(keyType, key)); }

        @Override
        public boolean remove(Object key, Object value) {
            return mappings.remove(requireType(keyType, key), requireType(valueType, value));
        }

        @Override
        public V putIfAbsent(K key, V value) {
            return mappings.putIfAbsent(requireType(keyType, key), requireType(valueType, value));
        }

        @Override
        public V replace(K key, V value) {
            return mappings.replace(requireType(keyType, key), requireType(valueType, value));
        }

        @Override
        public boolean replace(K key, V oldValue, V newValue) {
            return mappings.replace(requireType(keyType, key), requireType(valueType, oldValue),
                    requireType(valueType, newValue));
        }

        @Override
        public V computeIfAbsent(K key, Function<? super K, ? extends V> function) {
            return super.computeIfAbsent(requireType(keyType, key), function);
        }

        @Override
        public V computeIfPresent(K key, BiFunction<? super K, ? super V, ? extends V> function) {
            return super.computeIfPresent(requireType(keyType, key), function);
        }

        @Override
        public V compute(K key, BiFunction<? super K, ? super V, ? extends V> function) {
            return super.compute(requireType(keyType, key), function);
        }

        @Override
        public V merge(K key, V value, BiFunction<? super V, ? super V, ? extends V> function) {
            return super.merge(requireType(keyType, key), requireType(valueType, value), function);
        }

        @Override
        public void clear() { mappings.clear(); }

        final ByteWrapper reference(Object key) {
            byte[] bytes = (byte[]) get(key);
            return bytes == null ? null : new ByteWrapper(bytes);
        }

        final ByteWrapper reference(Object key, ByteWrapper wrapper) {
            byte[] bytes = (byte[]) get(key);
            if (bytes == null) return null;
            Objects.requireNonNull(wrapper, "byteWrapper").reassign(bytes, 0, bytes.length);
            return wrapper;
        }

        @Override
        public Set<Entry<K, V>> entrySet() {
            return new AbstractSet<Entry<K, V>>() {
                @Override
                public int size() { return mappings.size(); }

                @Override
                public void clear() { mappings.clear(); }

                @Override
                public Iterator<Entry<K, V>> iterator() {
                    Iterator<Entry<K, V>> iterator = mappings.entrySet().iterator();
                    return new Iterator<Entry<K, V>>() {
                        @Override
                        public boolean hasNext() { return iterator.hasNext(); }

                        @Override
                        public Entry<K, V> next() {
                            Entry<K, V> entry = iterator.next();
                            return new Entry<K, V>() {
                                public K getKey() { return entry.getKey(); }
                                public V getValue() { return entry.getValue(); }
                                public V setValue(V value) {
                                    return entry.setValue(requireType(valueType, value));
                                }
                                public boolean equals(Object other) { return entry.equals(other); }
                                public int hashCode() { return entry.hashCode(); }
                                public String toString() { return entry.toString(); }
                            };
                        }

                        @Override
                        public void remove() { iterator.remove(); }
                    };
                }
            };
        }

        @Override
        @SuppressWarnings("unchecked")
        public ValueMap<K, V> clone() {
            try {
                ValueMap<K, V> copy = (ValueMap<K, V>) super.clone();
                copy.mappings = new LinkedHashMap<>(mappings);
                return copy;
            } catch (CloneNotSupportedException impossible) {
                throw new AssertionError(impossible);
            }
        }
    }

    private static final class AttributeValueMap extends ValueMap<AttributeHandle, byte[]>
            implements AttributeHandleValueMap {
        private static final long serialVersionUID = 1L;
        AttributeValueMap(int capacity) { super(AttributeHandle.class, byte[].class, capacity); }

        @Override
        public ByteWrapper getValueReference(AttributeHandle key) { return reference(key); }

        @Override
        public ByteWrapper getValueReference(AttributeHandle key, ByteWrapper wrapper) {
            return reference(key, wrapper);
        }
    }

    private static final class ParameterValueMap extends ValueMap<ParameterHandle, byte[]>
            implements ParameterHandleValueMap {
        private static final long serialVersionUID = 1L;
        ParameterValueMap(int capacity) { super(ParameterHandle.class, byte[].class, capacity); }

        @Override
        public ByteWrapper getValueReference(ParameterHandle key) { return reference(key); }

        @Override
        public ByteWrapper getValueReference(ParameterHandle key, ByteWrapper wrapper) {
            return reference(key, wrapper);
        }
    }

    private abstract static class CheckedList<E> extends AbstractList<E>
            implements RandomAccess, Cloneable, Serializable {
        private static final long serialVersionUID = 1L;
        private final Class<E> elementType;
        private ArrayList<E> elements;

        CheckedList(Class<E> elementType, int capacity) {
            this.elementType = elementType;
            elements = new ArrayList<>(capacity);
        }

        @Override
        public int size() { return elements.size(); }

        @Override
        public E get(int index) { return elements.get(index); }

        @Override
        public E set(int index, E value) { return elements.set(index, requireType(elementType, value)); }

        @Override
        public void add(int index, E value) {
            elements.add(index, requireType(elementType, value));
            modCount++;
        }

        @Override
        public boolean addAll(Collection<? extends E> values) { return addAll(size(), values); }

        @Override
        public boolean addAll(int index, Collection<? extends E> values) {
            if (index < 0 || index > size()) throw new IndexOutOfBoundsException("Index: " + index);
            Objects.requireNonNull(values, "values");
            ArrayList<E> checked = new ArrayList<>(values.size());
            for (Object value : values) checked.add(requireType(elementType, value));
            boolean changed = elements.addAll(index, checked);
            if (changed) modCount++;
            return changed;
        }

        @Override
        public E remove(int index) {
            E removed = elements.remove(index);
            modCount++;
            return removed;
        }

        @Override
        protected void removeRange(int fromIndex, int toIndex) {
            elements.subList(fromIndex, toIndex).clear();
            modCount++;
        }

        @Override
        @SuppressWarnings("unchecked")
        public CheckedList<E> clone() {
            try {
                CheckedList<E> copy = (CheckedList<E>) super.clone();
                copy.elements = new ArrayList<>(elements);
                copy.modCount = 0;
                return copy;
            } catch (CloneNotSupportedException impossible) {
                throw new AssertionError(impossible);
            }
        }
    }

    private static final class AttributeRegionList extends CheckedList<AttributeRegionAssociation>
            implements AttributeSetRegionSetPairList {
        private static final long serialVersionUID = 1L;
        AttributeRegionList(int capacity) { super(AttributeRegionAssociation.class, capacity); }
    }

    private enum AttributeSetFactory implements AttributeHandleSetFactory {
        INSTANCE;
        public AttributeHandleSet create() { return new AttributeSet(); }
    }

    private enum FederateSetFactory implements FederateHandleSetFactory {
        INSTANCE;
        public FederateHandleSet create() { return new FederateSet(); }
    }

    private enum DimensionSetFactory implements DimensionHandleSetFactory {
        INSTANCE;
        public DimensionHandleSet create() { return new DimensionSet(); }
    }

    private enum RegionSetFactory implements RegionHandleSetFactory {
        INSTANCE;
        public RegionHandleSet create() { return new RegionSet(); }
    }

    private enum AttributeValueMapFactory implements AttributeHandleValueMapFactory {
        INSTANCE;
        public AttributeHandleValueMap create(int capacity) { return new AttributeValueMap(capacity); }
    }

    private enum ParameterValueMapFactory implements ParameterHandleValueMapFactory {
        INSTANCE;
        public ParameterHandleValueMap create(int capacity) { return new ParameterValueMap(capacity); }
    }

    private enum AttributeRegionListFactory implements AttributeSetRegionSetPairListFactory {
        INSTANCE;
        public AttributeSetRegionSetPairList create(int capacity) { return new AttributeRegionList(capacity); }
    }
}
