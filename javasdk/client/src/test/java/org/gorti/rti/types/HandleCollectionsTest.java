package org.gorti.rti.types;

import hla.rti1516e.*;
import hla.rti1516e.encoding.ByteWrapper;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.ObjectInputStream;
import java.io.ObjectOutputStream;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.*;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.EnumSource;
import static org.junit.jupiter.api.Assertions.*;

@SuppressWarnings({"rawtypes", "unchecked"})
class HandleCollectionsTest {
    enum SetKind {
        ATTRIBUTE, FEDERATE, DIMENSION, REGION;

        Set<Object> create() {
            switch (this) {
                case ATTRIBUTE: return (Set) HandleCollections.attributeSetFactory().create();
                case FEDERATE: return (Set) HandleCollections.federateSetFactory().create();
                case DIMENSION: return (Set) HandleCollections.dimensionSetFactory().create();
                case REGION: return (Set) HandleCollections.regionSetFactory().create();
                default: throw new AssertionError(this);
            }
        }

        Object handle(long value) {
            switch (this) {
                case ATTRIBUTE: return Handles.attribute(value);
                case FEDERATE: return Handles.federate(value);
                case DIMENSION: return Handles.dimension(value);
                case REGION: return Handles.region(value);
                default: throw new AssertionError(this);
            }
        }
    }

    enum MapKind {
        ATTRIBUTE, PARAMETER;

        Map<Object, Object> create(int capacity) {
            return (Map) (this == ATTRIBUTE
                    ? HandleCollections.attributeValueMapFactory().create(capacity)
                    : HandleCollections.parameterValueMapFactory().create(capacity));
        }

        Object handle(long value) {
            return this == ATTRIBUTE ? Handles.attribute(value) : Handles.parameter(value);
        }

        ByteWrapper reference(Map map, Object key) {
            return this == ATTRIBUTE
                    ? ((AttributeHandleValueMap) map).getValueReference((AttributeHandle) key)
                    : ((ParameterHandleValueMap) map).getValueReference((ParameterHandle) key);
        }

        ByteWrapper reference(Map map, Object key, ByteWrapper wrapper) {
            return this == ATTRIBUTE
                    ? ((AttributeHandleValueMap) map).getValueReference((AttributeHandle) key, wrapper)
                    : ((ParameterHandleValueMap) map).getValueReference((ParameterHandle) key, wrapper);
        }
    }

    @ParameterizedTest
    @EnumSource(SetKind.class)
    void setsHaveNormalEqualityArraysAndIndependentShallowClones(SetKind kind) throws Exception {
        Set<Object> set = kind.create();
        Object first = kind.handle(1);
        assertTrue(set.add(first));
        assertFalse(set.add(kind.handle(1)));
        assertTrue(set.add(kind.handle(2)));
        Set<Object> standard = new HashSet<>(Arrays.asList(first, kind.handle(2)));
        assertEquals(standard, set);
        assertEquals(set, standard);
        assertEquals(standard.hashCode(), set.hashCode());
        assertEquals(standard, new HashSet<>(Arrays.asList(set.toArray())));
        Object[] target = new Object[3];
        target[2] = "sentinel";
        assertSame(target, set.toArray(target));
        assertNull(target[2]);
        Set<Object> copy = (Set<Object>) cloneOf(set);
        assertNotSame(set, copy);
        assertEquals(set, copy);
        assertSame(first, copy.stream().filter(first::equals).findFirst().get());
        copy.remove(first);
        assertTrue(set.contains(first));
        assertNotEquals(set, copy);
        assertEquals(set, serializedCopy(set));
        assertTrue(kind.create().isEmpty());
    }

    @ParameterizedTest
    @EnumSource(SetKind.class)
    void setsSupportAllBulkAndIteratorMutations(SetKind kind) {
        Set<Object> set = kind.create();
        Set<Object> incoming = kind.create();
        incoming.add(kind.handle(1));
        incoming.add(kind.handle(2));
        assertTrue(set.addAll(incoming));
        assertFalse(set.addAll(set));
        Iterator<Object> iterator = set.iterator();
        Object removed = iterator.next();
        iterator.remove();
        assertFalse(set.contains(removed));
        assertThrows(IllegalStateException.class, iterator::remove);
        assertTrue(set.addAll(incoming));
        Set<Object> keep = kind.create();
        keep.add(kind.handle(1));
        assertTrue(set.retainAll(keep));
        assertFalse(set.retainAll(set));
        assertTrue(set.removeAll(keep));
        assertTrue(set.isEmpty());
        set.addAll(incoming);
        assertTrue(set.removeAll(set));
        set.addAll(incoming);
        assertTrue(set.removeIf(value -> value.equals(kind.handle(1))));
        set.clear();
        assertTrue(set.isEmpty());
        assertThrows(NoSuchElementException.class, () -> set.iterator().next());
    }

    @ParameterizedTest
    @EnumSource(SetKind.class)
    void setsRejectNullWrongHandlesAndNonOfficialBulkArguments(SetKind kind) {
        Set<Object> set = kind.create();
        set.add(kind.handle(1));
        for (Object invalid : new Object[] {null, "wrong", Handles.parameter(1)}) {
            assertThrows(IllegalArgumentException.class, () -> set.add(invalid));
            assertThrows(IllegalArgumentException.class, () -> set.remove(invalid));
        }
        List<Collection<?>> invalid = new ArrayList<>();
        invalid.add(null);
        invalid.add(Collections.emptySet());
        invalid.add(Collections.singleton(kind.handle(1)));
        for (SetKind other : SetKind.values()) {
            if (other != kind) invalid.add(other.create());
        }
        for (Collection collection : invalid) {
            assertThrows(IllegalArgumentException.class, () -> set.addAll(collection));
            assertThrows(IllegalArgumentException.class, () -> set.removeAll(collection));
            assertThrows(IllegalArgumentException.class, () -> set.retainAll(collection));
        }
        assertEquals(Collections.singleton(kind.handle(1)), set);
        assertFalse(set.contains(null));
        assertFalse(set.contains("wrong"));
    }

    @ParameterizedTest
    @EnumSource(SetKind.class)
    void setsAcceptOfficialForeignImplementationsAndValidateBulkElements(SetKind kind) {
        Set<Object> set = kind.create();
        Class<?> handleType = kind.handle(1).getClass().getInterfaces()[0];
        Object foreign = foreignHandle(handleType);
        assertTrue(set.add(foreign));
        assertTrue(set.contains(foreign));
        Set<Object> source = new LinkedHashSet<>();
        source.add(kind.handle(1));
        source.add(foreign);
        Class<?> setType = set.getClass().getInterfaces()[0];
        Collection external = (Collection) Proxy.newProxyInstance(
                getClass().getClassLoader(), new Class<?>[] {setType},
                (proxy, method, args) -> method.invoke(source, args));
        assertTrue(set.addAll(external));
        assertEquals(source, set);
        source.add(kind.handle(2));
        source.add("wrong");
        assertThrows(IllegalArgumentException.class, () -> set.addAll(external));
        assertThrows(IllegalArgumentException.class, () -> set.removeAll(external));
        assertThrows(IllegalArgumentException.class, () -> set.retainAll(external));
        assertEquals(2, set.size());
        assertFalse(set.contains(kind.handle(2)));
    }

    @Test
    void attributeSetExposesOfficialCloneMethod() {
        AttributeHandleSet set = HandleCollections.attributeSetFactory().create();
        set.add(Handles.attribute(1));
        AttributeHandleSet copy = set.clone();
        assertEquals(set, copy);
        copy.clear();
        assertEquals(1, set.size());
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapsKeepByteArrayIdentityAndNormalMapEquality(MapKind kind) {
        Map<Object, Object> map = kind.create(0);
        Object key = kind.handle(1);
        byte[] bytes = {1, 2};
        assertNull(map.put(key, bytes));
        assertSame(bytes, map.get(key));
        bytes[0] = 9;
        assertEquals(9, ((byte[]) map.get(key))[0]);
        Map<Object, Object> standard = new HashMap<>();
        standard.put(key, bytes);
        assertEquals(standard, map);
        assertEquals(map, standard);
        assertEquals(standard.hashCode(), map.hashCode());
        standard.put(key, bytes.clone());
        assertNotEquals(standard, map);
        byte[] replacement = {3};
        assertSame(bytes, map.put(key, replacement));
        assertSame(replacement, map.remove(key));
        assertNull(map.remove(key));
        assertTrue(map.isEmpty());
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void valueReferencesWriteThroughAndReassignTheProvidedWrapper(MapKind kind) {
        Map<Object, Object> map = kind.create(1);
        Object key = kind.handle(1);
        byte[] bytes = {1, 2, 3};
        map.put(key, bytes);
        ByteWrapper reference = kind.reference(map, key);
        assertSame(bytes, reference.array());
        reference.put(7);
        assertEquals(7, bytes[0]);
        ByteWrapper reuse = new ByteWrapper(new byte[8], 2, 4);
        reuse.advance(2);
        assertSame(reuse, kind.reference(map, key, reuse));
        assertSame(bytes, reuse.array());
        assertEquals(0, reuse.getPos());
        assertEquals(bytes.length, reuse.remaining());
        reuse.put(8);
        assertEquals(8, ((byte[]) map.get(key))[0]);
        assertNull(kind.reference(map, kind.handle(2)));
        assertNull(kind.reference(map, kind.handle(2), reuse));
        assertSame(bytes, reuse.array());
        assertEquals(1, reuse.getPos());
        byte[] replacement = {4};
        map.put(key, replacement);
        assertSame(bytes, reference.array());
        assertSame(replacement, kind.reference(map, key).array());
        map.put(key, new byte[0]);
        assertEquals(0, kind.reference(map, key, reuse).remaining());
        assertThrows(IndexOutOfBoundsException.class, reuse::get);
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapsAcceptForeignOfficialKeysAndPreserveReferencesOnBulkInsertion(MapKind kind) {
        Map<Object, Object> map = kind.create(0);
        Object foreign = foreignHandle(kind.handle(1).getClass().getInterfaces()[0]);
        byte[] bytes = {1, 2};
        Map<Object, Object> source = Collections.singletonMap(foreign, bytes);
        map.putAll(source);
        assertSame(bytes, map.get(foreign));
        assertSame(bytes, map.values().iterator().next());
        assertSame(bytes, map.entrySet().iterator().next().getValue());
        assertSame(bytes, kind.reference(map, foreign).array());
        assertThrows(NullPointerException.class, () -> kind.reference(map, foreign, null));
        assertNull(kind.reference(map, null));
        assertNull(kind.reference(map, kind.handle(99), null));
        assertSame(bytes, map.remove(foreign));
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapClonesCopyStructureButShareValuesAndViewsStayIndependent(MapKind kind) throws Exception {
        Map<Object, Object> map = kind.create(0);
        Object key = kind.handle(1);
        byte[] bytes = {1};
        map.put(key, bytes);
        Set<Object> keys = map.keySet();
        Collection<Object> values = map.values();
        Set<Map.Entry<Object, Object>> entries = map.entrySet();
        Map<Object, Object> copy = (Map<Object, Object>) cloneOf(map);
        assertEquals(map, copy);
        assertSame(bytes, copy.get(key));
        ((byte[]) copy.get(key))[0] = 9;
        assertEquals(9, bytes[0]);
        copy.keySet().clear();
        assertEquals(1, keys.size());
        assertEquals(1, values.size());
        assertEquals(1, entries.size());
        copy.put(key, bytes);
        copy.values().clear();
        assertEquals(1, map.size());
        copy.put(key, bytes);
        copy.entrySet().clear();
        assertEquals(1, map.size());
        Map<Object, Object> restored = (Map<Object, Object>) serializedCopy(map);
        assertEquals(map.keySet(), restored.keySet());
        assertArrayEquals(bytes, (byte[]) restored.get(key));
        assertNotSame(bytes, restored.get(key));
        assertThrows(IllegalArgumentException.class, () -> restored.put(key, null));
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapsRejectInvalidRawPutRemoveAndBulkMappings(MapKind kind) {
        Map<Object, Object> map = kind.create(1);
        Object key = kind.handle(1);
        byte[] bytes = {1};
        map.put(key, bytes);
        for (Object invalid : new Object[] {null, "wrong", Handles.region(1)}) {
            assertThrows(IllegalArgumentException.class, () -> map.put(invalid, bytes));
            assertThrows(IllegalArgumentException.class, () -> map.remove(invalid));
        }
        for (Object invalid : new Object[] {null, "wrong", new Byte[] {1}}) {
            assertThrows(IllegalArgumentException.class, () -> map.put(key, invalid));
            Map<Object, Object> incoming = new LinkedHashMap<>();
            incoming.put(kind.handle(2), bytes);
            incoming.put(key, invalid);
            assertThrows(IllegalArgumentException.class, () -> map.putAll(incoming));
            assertEquals(1, map.size());
        }
        assertThrows(IllegalArgumentException.class, () -> map.putAll(null));
        assertThrows(IllegalArgumentException.class,
                () -> map.putAll(Collections.singletonMap("wrong", bytes)));
        map.putAll(Collections.singletonMap(kind.handle(2), bytes));
        map.putAll(map);
        assertEquals(2, map.size());
        assertNull(map.get(null));
        assertNull(map.get("wrong"));
        assertFalse(map.containsKey("wrong"));
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void entriesValidateSetValueThroughEveryExposedEntryPath(MapKind kind) {
        Map<Object, Object> map = kind.create(1);
        Object key = kind.handle(1);
        byte[] original = {1};
        map.put(key, original);
        List<Map.Entry> entries = Arrays.asList(map.entrySet().iterator().next(),
                (Map.Entry) map.entrySet().toArray()[0],
                map.entrySet().toArray(new Map.Entry[0])[0],
                map.entrySet().stream().findFirst().get());
        for (Map.Entry entry : entries) {
            for (Object invalid : new Object[] {null, "wrong", new Byte[] {1}}) {
                assertThrows(IllegalArgumentException.class, () -> entry.setValue(invalid));
                assertSame(original, map.get(key));
            }
        }
        Map.Entry entry = entries.get(0);
        byte[] replacement = {2};
        assertSame(original, entry.setValue(replacement));
        assertSame(replacement, map.get(key));
        assertEquals(new AbstractMap.SimpleEntry<>(key, replacement), entry);
        assertEquals(new AbstractMap.SimpleEntry<>(key, replacement).hashCode(), entry.hashCode());
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapViewsAreLiveAndSupportRemovalOperations(MapKind kind) {
        Map<Object, Object> map = kind.create(0);
        Object one = kind.handle(1);
        Object two = kind.handle(2);
        byte[] first = {1};
        byte[] second = {2};
        Set<Object> keys = map.keySet();
        Collection<Object> values = map.values();
        Set<Map.Entry<Object, Object>> entries = map.entrySet();
        map.put(one, first);
        map.put(two, second);
        assertTrue(keys.contains(one));
        assertTrue(values.contains(first));
        assertTrue(keys.remove(one));
        assertFalse(map.containsKey(one));
        map.put(one, first);
        assertTrue(values.remove(first));
        map.put(one, first);
        assertTrue(entries.remove(new AbstractMap.SimpleEntry<>(one, first)));
        map.put(one, first);
        Iterator<Map.Entry<Object, Object>> iterator = entries.iterator();
        Object removed = iterator.next().getKey();
        iterator.remove();
        assertFalse(map.containsKey(removed));
        map.put(one, first);
        map.put(two, second);
        assertTrue(keys.retainAll(Collections.singleton(one)));
        map.put(two, second);
        assertTrue(values.removeAll(Collections.singleton(first)));
        assertTrue(entries.removeIf(entry -> entry.getKey().equals(two)));
        assertTrue(map.isEmpty());
        map.put(one, first);
        values.clear();
        assertTrue(map.isEmpty());
        map.put(one, first);
        keys.clear();
        assertTrue(map.isEmpty());
        map.put(one, first);
        entries.clear();
        assertTrue(map.isEmpty());
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapDefaultMutationsWorkAndCannotInstallInvalidValues(MapKind kind) {
        Map<Object, Object> map = kind.create(0);
        Object key = kind.handle(1);
        byte[] one = {1};
        byte[] two = {2};
        assertNull(map.putIfAbsent(key, one));
        assertSame(one, map.putIfAbsent(key, two));
        assertTrue(map.replace(key, one, two));
        assertSame(two, map.replace(key, one));
        assertSame(two, map.compute(key, (k, v) -> two));
        assertSame(one, map.computeIfPresent(key, (k, v) -> one));
        assertSame(two, map.merge(key, two, (a, b) -> b));
        map.replaceAll((k, v) -> one);
        assertSame(one, map.get(key));
        assertThrows(IllegalArgumentException.class, () -> map.replaceAll((k, v) -> null));
        assertThrows(IllegalArgumentException.class, () -> map.replaceAll((k, v) -> "wrong"));
        assertThrows(IllegalArgumentException.class, () -> map.compute(key, (k, v) -> "wrong"));
        assertThrows(IllegalArgumentException.class, () -> map.computeIfPresent(key, (k, v) -> "wrong"));
        assertThrows(IllegalArgumentException.class, () -> map.merge(key, two, (a, b) -> "wrong"));
        assertThrows(IllegalArgumentException.class,
                () -> map.computeIfAbsent(kind.handle(2), k -> "wrong"));
        assertSame(one, map.get(key));
        assertFalse(map.remove(key, two));
        assertTrue(map.remove(key, one));
        assertSame(one, map.computeIfAbsent(key, k -> one));
        assertNull(map.computeIfPresent(key, (k, v) -> null));
        assertFalse(map.containsKey(key));
        map.put(key, one);
        assertNull(map.merge(key, two, (a, b) -> null));
        assertFalse(map.containsKey(key));
        assertNull(map.computeIfAbsent(key, k -> null));
        assertFalse(map.containsKey(key));
    }

    @ParameterizedTest
    @EnumSource(MapKind.class)
    void mapDefaultMutationsValidateEvenWhenTheyWouldNotChangeAnEntry(MapKind kind) {
        Map<Object, Object> map = kind.create(0);
        Object present = kind.handle(1);
        Object absent = kind.handle(2);
        byte[] bytes = {1};
        map.put(present, bytes);
        for (Object invalid : new Object[] {null, "wrong", Handles.region(1)}) {
            assertThrows(IllegalArgumentException.class, () -> map.putIfAbsent(invalid, bytes));
            assertThrows(IllegalArgumentException.class, () -> map.replace(invalid, bytes));
            assertThrows(IllegalArgumentException.class, () -> map.replace(invalid, bytes, bytes));
            assertThrows(IllegalArgumentException.class, () -> map.remove(invalid, bytes));
            assertThrows(IllegalArgumentException.class, () -> map.compute(invalid, (k, v) -> null));
            assertThrows(IllegalArgumentException.class, () -> map.computeIfAbsent(invalid, k -> null));
            assertThrows(IllegalArgumentException.class, () -> map.computeIfPresent(invalid, (k, v) -> null));
            assertThrows(IllegalArgumentException.class, () -> map.merge(invalid, bytes, (a, b) -> null));
        }
        for (Object invalid : new Object[] {null, "wrong", new Byte[] {1}}) {
            assertThrows(IllegalArgumentException.class, () -> map.putIfAbsent(present, invalid));
            assertThrows(IllegalArgumentException.class, () -> map.replace(absent, invalid));
            assertThrows(IllegalArgumentException.class, () -> map.replace(absent, bytes, invalid));
            assertThrows(IllegalArgumentException.class, () -> map.replace(absent, invalid, bytes));
            assertThrows(IllegalArgumentException.class, () -> map.remove(absent, invalid));
            assertThrows(IllegalArgumentException.class, () -> map.merge(present, invalid, (a, b) -> bytes));
        }
        assertEquals(Collections.singletonMap(present, bytes), map);
    }

    @Test
    void capacityBoundsAndFactorySerialization() throws Exception {
        for (MapKind kind : MapKind.values()) {
            assertThrows(IllegalArgumentException.class, () -> kind.create(-1));
            assertThrows(IllegalArgumentException.class, () -> kind.create(Integer.MIN_VALUE));
            assertTrue(kind.create(0).isEmpty());
        }
        assertThrows(IllegalArgumentException.class,
                () -> HandleCollections.attributeRegionListFactory().create(-1));
        assertTrue(HandleCollections.attributeRegionListFactory().create(0).isEmpty());
        Object[] factories = {HandleCollections.attributeSetFactory(), HandleCollections.federateSetFactory(),
                HandleCollections.dimensionSetFactory(), HandleCollections.regionSetFactory(),
                HandleCollections.attributeValueMapFactory(), HandleCollections.parameterValueMapFactory(),
                HandleCollections.attributeRegionListFactory()};
        for (Object factory : factories) assertSame(factory, serializedCopy(factory));
    }

    @Test
    void associationListSupportsIndexedBulkIteratorAndSubListMutations() {
        AttributeSetRegionSetPairList list = HandleCollections.attributeRegionListFactory().create(0);
        AttributeRegionAssociation one = association(1);
        AttributeRegionAssociation two = association(2);
        AttributeRegionAssociation three = association(3);
        assertTrue(list.add(one));
        list.add(0, two);
        assertSame(one, list.set(1, three));
        assertTrue(list.addAll(Arrays.asList(one, two)));
        assertTrue(list.addAll(1, Collections.singletonList(one)));
        assertEquals(Arrays.asList(two, one, three, one, two), list);
        assertEquals(1, list.indexOf(one));
        assertEquals(3, list.lastIndexOf(one));
        ListIterator<AttributeRegionAssociation> iterator = list.listIterator();
        assertSame(two, iterator.next());
        iterator.set(three);
        iterator.add(two);
        assertSame(two, iterator.previous());
        iterator.remove();
        list.subList(1, 3).clear();
        assertEquals(Arrays.asList(three, one, two), list);
        list.subList(1, 2).set(0, two);
        list.subList(1, 2).add(one);
        assertEquals(Arrays.asList(three, two, one, two), list);
        assertTrue(list.removeAll(Collections.singleton(two)));
        assertTrue(list.retainAll(Collections.singleton(one)));
        assertTrue(list.removeIf(item -> item == one));
        assertTrue(list.isEmpty());
    }

    @Test
    void associationListRejectsInvalidRawMutationsIncludingViewsAndIterators() {
        List list = HandleCollections.attributeRegionListFactory().create(0);
        AttributeRegionAssociation association = association(1);
        list.add(association);
        for (Object invalid : new Object[] {null, "wrong", Handles.attribute(1)}) {
            assertThrows(IllegalArgumentException.class, () -> list.add(invalid));
            assertThrows(IllegalArgumentException.class, () -> list.add(0, invalid));
            assertThrows(IllegalArgumentException.class, () -> list.set(0, invalid));
            assertThrows(IllegalArgumentException.class,
                    () -> list.addAll(Arrays.asList(association, invalid)));
            assertThrows(IllegalArgumentException.class,
                    () -> list.addAll(0, Arrays.asList(association, invalid)));
            assertThrows(IllegalArgumentException.class, () -> list.subList(0, 1).add(invalid));
            assertThrows(IllegalArgumentException.class, () -> list.subList(0, 1).set(0, invalid));
            List nested = list.subList(0, 1).subList(0, 1);
            assertThrows(IllegalArgumentException.class, () -> nested.addAll(Arrays.asList(association, invalid)));
            assertThrows(IllegalArgumentException.class, () -> nested.addAll(0, Arrays.asList(association, invalid)));
            ListIterator iterator = list.listIterator();
            iterator.next();
            assertThrows(IllegalArgumentException.class, () -> iterator.set(invalid));
            assertThrows(IllegalArgumentException.class, () -> iterator.add(invalid));
            assertThrows(IllegalArgumentException.class, () -> list.replaceAll(value -> invalid));
            assertEquals(Collections.singletonList(association), list);
        }
    }

    @Test
    void associationListBoundsEqualityCloneAndSerialization() throws Exception {
        AttributeSetRegionSetPairList list = HandleCollections.attributeRegionListFactory().create(1);
        AttributeRegionAssociation one = association(1);
        AttributeRegionAssociation two = association(2);
        list.add(one);
        for (int index : new int[] {-1, 1, Integer.MIN_VALUE, Integer.MAX_VALUE}) {
            assertThrows(IndexOutOfBoundsException.class, () -> list.get(index));
            assertThrows(IndexOutOfBoundsException.class, () -> list.set(index, one));
            assertThrows(IndexOutOfBoundsException.class, () -> list.remove(index));
        }
        for (int index : new int[] {-1, 2, Integer.MIN_VALUE, Integer.MAX_VALUE}) {
            assertThrows(IndexOutOfBoundsException.class, () -> list.add(index, one));
            assertThrows(IndexOutOfBoundsException.class, () -> list.addAll(index, Collections.emptyList()));
        }
        assertEquals(Collections.singletonList(one), list);
        assertEquals(Collections.singletonList(one).hashCode(), list.hashCode());
        assertArrayEquals(new AttributeRegionAssociation[] {one}, list.toArray(new AttributeRegionAssociation[0]));
        AttributeSetRegionSetPairList copy = (AttributeSetRegionSetPairList) cloneOf(list);
        assertEquals(list, copy);
        assertSame(one, copy.get(0));
        copy.add(two);
        assertEquals(1, list.size());
        one.ahset.add(Handles.attribute(9));
        assertTrue(copy.get(0).ahset.contains(Handles.attribute(9)));
        list.addAll(list);
        assertEquals(Arrays.asList(one, one), list);
        list.replaceAll(value -> two);
        list.sort(Comparator.comparingInt(value -> value.ahset.size()));
        assertEquals(Arrays.asList(two, two), list);
        AttributeSetRegionSetPairList restored = (AttributeSetRegionSetPairList) serializedCopy(copy);
        assertEquals(copy.size(), restored.size());
        assertEquals(copy.get(0).ahset, restored.get(0).ahset);
        assertEquals(copy.get(0).rhset, restored.get(0).rhset);
        restored.clear();
        assertEquals(2, copy.size());
    }

    private static AttributeRegionAssociation association(long id) {
        AttributeHandleSet attributes = HandleCollections.attributeSetFactory().create();
        attributes.add(Handles.attribute(id));
        RegionHandleSet regions = HandleCollections.regionSetFactory().create();
        regions.add(Handles.region(id));
        return new AttributeRegionAssociation(attributes, regions);
    }

    private static Object foreignHandle(Class<?> type) {
        return Proxy.newProxyInstance(HandleCollectionsTest.class.getClassLoader(), new Class<?>[] {type},
                (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "equals": return proxy == args[0];
                        case "hashCode": return System.identityHashCode(proxy);
                        case "toString": return "Foreign" + type.getSimpleName();
                        default: throw new AssertionError("Collection must not decode foreign handles");
                    }
                });
    }

    private static Object cloneOf(Object value) throws Exception {
        Method clone = value.getClass().getMethod("clone");
        clone.setAccessible(true);
        return clone.invoke(value);
    }

    private static Object serializedCopy(Object value) throws Exception {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        try (ObjectOutputStream output = new ObjectOutputStream(bytes)) { output.writeObject(value); }
        try (ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(bytes.toByteArray()))) {
            return input.readObject();
        }
    }
}
