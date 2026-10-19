package org.gorti.rti.types;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.CouldNotDecode;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.ObjectInputStream;
import java.io.ObjectOutputStream;
import java.lang.reflect.Proxy;
import java.util.Arrays;
import java.util.HashSet;
import java.util.Set;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class HandlesTest {
    private static Object[] handles(long value) {
        return new Object[] {Handles.federate(value), Handles.objectClass(value),
                Handles.objectInstance(value), Handles.interactionClass(value),
                Handles.attribute(value), Handles.parameter(value), Handles.dimension(value),
                Handles.region(value), Handles.retraction(value)};
    }

    @Test
    void eachHandleImplementsOnlyItsOfficialHandleType() {
        Class<?>[] types = {FederateHandle.class, ObjectClassHandle.class, ObjectInstanceHandle.class,
                InteractionClassHandle.class, AttributeHandle.class, ParameterHandle.class,
                DimensionHandle.class, RegionHandle.class, MessageRetractionHandle.class,
                TransportationTypeHandle.class};
        Object[] values = Arrays.copyOf(handles(1), 10);
        values[9] = Handles.transportation(1);
        for (int i = 0; i < values.length; i++) {
            for (int j = 0; j < types.length; j++) {
                assertEquals(i == j, types[j].isInstance(values[i]), types[j].getSimpleName());
            }
        }
        assertEquals(10, new HashSet<>(Arrays.asList(values)).size());
    }

    @Test
    void equalityHashingAndUnsignedBitsArePreserved() {
        for (long value : new long[] {0, 1, Long.MAX_VALUE, Long.MIN_VALUE, -1}) {
            Object[] first = handles(value);
            Object[] second = handles(value);
            for (int i = 0; i < first.length; i++) {
                assertEquals(value, Handles.value(first[i]));
                assertEquals(first[i], second[i]);
                assertEquals(first[i].hashCode(), second[i].hashCode());
                assertNotEquals(first[i], handles(value + 1)[i]);
                assertNotEquals(first[i], null);
                assertTrue(first[i].toString().contains(Long.toUnsignedString(value)));
            }
        }
    }

    @Test
    void everyEncodableHandleHasBigEndianRoundTrip() throws Exception {
        long value = 0x0102030405060708L;
        for (int kind = 0; kind < 7; kind++) {
            byte[] bytes = new byte[12];
            Arrays.fill(bytes, (byte) 0x55);
            Object handle = handles(value)[kind];
            encode(handle, bytes, 2);
            assertArrayEquals(new byte[] {0x55, 0x55, 1, 2, 3, 4, 5, 6, 7, 8, 0x55, 0x55}, bytes);
            assertEquals(handle, decode(kind, bytes, 2));
            encode(handles(-1)[kind], bytes, 2);
            assertEquals(-1L, Handles.value(decode(kind, bytes, 2)));
        }
    }

    @Test
    void handleEncodingRejectsBadBoundsWithoutPartialWrite() {
        for (int kind = 0; kind < 7; kind++) {
            Object handle = handles(5)[kind];
            assertThrows(NullPointerException.class, () -> encode(handle, null, 0));
            for (int offset : new int[] {-1, 1, Integer.MIN_VALUE, Integer.MAX_VALUE}) {
                byte[] bytes = new byte[8];
                Arrays.fill(bytes, (byte) 0x55);
                byte[] before = bytes.clone();
                assertThrows(IndexOutOfBoundsException.class, () -> encode(handle, bytes, offset));
                assertArrayEquals(before, bytes);
            }
        }
    }

    @Test
    void everyHandleFactoryTranslatesMalformedBoundsToCouldNotDecode() {
        for (int kind = 0; kind < 7; kind++) {
            final int index = kind;
            assertThrows(CouldNotDecode.class, () -> decode(index, null, 0));
            assertThrows(CouldNotDecode.class, () -> decode(index, new byte[7], 0));
            for (int offset : new int[] {-1, 1, Integer.MIN_VALUE, Integer.MAX_VALUE}) {
                assertThrows(CouldNotDecode.class, () -> decode(index, new byte[8], offset));
            }
        }
    }

    @Test
    void transportationFactoryUsesSupportedBackendIdentifiers() throws Exception {
        TransportationTypeHandleFactory factory = Handles.transportationFactory();
        assertEquals(Handles.transportation(1), factory.getHLAdefaultReliable());
        assertEquals(Handles.transportation(2), factory.getHLAdefaultBestEffort());
        assertNotEquals(factory.getHLAdefaultReliable(), factory.getHLAdefaultBestEffort());
        for (int id : new int[] {1, 2}) {
            TransportationTypeHandle handle = Handles.transportation(id);
            assertEquals(4, handle.encodedLength());
            byte[] bytes = new byte[6];
            handle.encode(bytes, 1);
            assertArrayEquals(new byte[] {0, 0, 0, 0, (byte) id, 0}, bytes);
            assertEquals(handle, factory.decode(bytes, 1));
            assertEquals(id, Handles.value(handle));
        }
        for (int id : new int[] {0, 3, -1, Integer.MAX_VALUE}) {
            assertThrows(IllegalArgumentException.class, () -> Handles.transportation(id));
        }
        assertThrows(CouldNotDecode.class, () -> factory.decode(new byte[4], 0));
        assertThrows(CouldNotDecode.class, () -> factory.decode(new byte[] {0, 0, 0, 3}, 0));
        assertThrows(CouldNotDecode.class, () -> factory.decode(null, 0));
        assertThrows(CouldNotDecode.class, () -> factory.decode(new byte[3], 0));
        assertThrows(CouldNotDecode.class, () -> factory.decode(new byte[4], Integer.MAX_VALUE));
        assertThrows(CouldNotDecode.class, () -> factory.decode(new byte[4], -1));
        assertThrows(NullPointerException.class, () -> Handles.transportation(1).encode(null, 0));
        byte[] untouched = {9, 9, 9, 9};
        assertThrows(IndexOutOfBoundsException.class, () -> Handles.transportation(1).encode(untouched, 1));
        assertArrayEquals(new byte[] {9, 9, 9, 9}, untouched);
    }

    @Test
    void extractionRejectsForeignHandlesNullAndPlainNumbers() {
        FederateHandle foreign = (FederateHandle) Proxy.newProxyInstance(
                getClass().getClassLoader(), new Class<?>[] {FederateHandle.class},
                (proxy, method, arguments) -> { throw new AssertionError("Foreign handle must not be inspected"); });
        assertThrows(IllegalArgumentException.class, () -> Handles.value(foreign));
        assertThrows(IllegalArgumentException.class, () -> Handles.value(null));
        assertThrows(IllegalArgumentException.class, () -> Handles.value(1L));
    }

    @Test
    void scopedRetractionsIncludeGenerationProducerAndValueInIdentity() {
        MessageRetractionHandle first = Handles.retraction(11, 22, 33);
        MessageRetractionHandle same = Handles.retraction(11, 22, 33);
        assertEquals(first, same);
        assertEquals(first.hashCode(), same.hashCode());
        assertEquals(11, Handles.retractionGeneration(first));
        assertEquals(22, Handles.retractionProducer(first));
        assertEquals(33, Handles.value(first));
        Set<MessageRetractionHandle> distinct = new HashSet<>(Arrays.asList(first,
                Handles.retraction(12, 22, 33), Handles.retraction(11, 23, 33),
                Handles.retraction(11, 22, 34), Handles.retraction(33)));
        assertEquals(5, distinct.size());
        assertNotEquals(first.hashCode(), Handles.retraction(12, 22, 33).hashCode());
        assertNotEquals(first.hashCode(), Handles.retraction(11, 23, 33).hashCode());
        assertNotEquals(first.hashCode(), Handles.retraction(11, 22, 34).hashCode());
    }

    @Test
    void legacyRetractionsRemainDistinctEvenFromZeroScopedRetractions() {
        MessageRetractionHandle legacy = Handles.retraction(7);
        MessageRetractionHandle scoped = Handles.retraction(0, 0, 7);
        assertEquals(legacy, Handles.retraction(7));
        assertNotEquals(legacy, scoped);
        assertNotEquals(scoped, legacy);
        assertEquals(0, Handles.retractionGeneration(legacy));
        assertEquals(0, Handles.retractionProducer(legacy));
        assertEquals(7, Handles.value(legacy));
    }

    @Test
    void scopedRetractionsDoNotCollideAcrossFederations() throws Exception {
        MessageRetractionHandle first = Handles.retraction("a", 1, 1, 1);
        MessageRetractionHandle other = Handles.retraction("b", 1, 1, 1);
        assertNotEquals(first, other);
        assertEquals("a", Handles.retractionFederation(first));
        assertEquals(first, serializedCopy(first));
        assertNotEquals(first, Handles.retraction(1, 1, 1));
    }

    @Test
    void nonRetractableNativeDeliveriesHaveUniqueSerializableNonWireIdentities() throws Exception {
        MessageRetractionHandle first = Handles.nonRetractableMessage();
        assertNotEquals(first, Handles.nonRetractableMessage());
        assertEquals(first, serializedCopy(first));
        assertEquals(first.hashCode(), serializedCopy(first).hashCode());
        assertThrows(IllegalArgumentException.class, () -> Handles.value(first));
        assertThrows(IllegalArgumentException.class, () -> Handles.retractionFederation(first));
    }

    @Test
    void scopedRetractionsPreserveUnsignedBitsAndSerialization() throws Exception {
        MessageRetractionHandle handle = Handles.retraction(-1, Long.MIN_VALUE, Long.MAX_VALUE);
        MessageRetractionHandle copy = (MessageRetractionHandle) serializedCopy(handle);
        assertEquals(handle, copy);
        assertEquals(handle.hashCode(), copy.hashCode());
        assertEquals(-1, Handles.retractionGeneration(copy));
        assertEquals(Long.MIN_VALUE, Handles.retractionProducer(copy));
        assertEquals(Long.MAX_VALUE, Handles.value(copy));
        assertTrue(copy.toString().contains(Long.toUnsignedString(-1)));
        assertTrue(copy.toString().contains(Long.toUnsignedString(Long.MIN_VALUE)));
        assertTrue(copy.toString().contains(Long.toUnsignedString(Long.MAX_VALUE)));
    }

    @Test
    void retractionScopeExtractionRejectsForeignHandlesAndNull() {
        MessageRetractionHandle foreign = (MessageRetractionHandle) Proxy.newProxyInstance(
                getClass().getClassLoader(), new Class<?>[] {MessageRetractionHandle.class},
                (proxy, method, arguments) -> { throw new AssertionError("Foreign handle must not be inspected"); });
        assertThrows(IllegalArgumentException.class, () -> Handles.retractionGeneration(foreign));
        assertThrows(IllegalArgumentException.class, () -> Handles.retractionProducer(foreign));
        assertThrows(IllegalArgumentException.class, () -> Handles.value(foreign));
        assertThrows(IllegalArgumentException.class, () -> Handles.retractionGeneration(null));
        assertThrows(IllegalArgumentException.class, () -> Handles.retractionProducer(null));
    }

    @Test
    void handlesAndFactoriesSurviveJavaSerialization() throws Exception {
        Set<Object> values = new HashSet<>(Arrays.asList(handles(-1)));
        values.add(Handles.transportation(1));
        for (Object value : values) {
            Object restored = serializedCopy(value);
            assertEquals(value, restored);
            assertEquals(Handles.value(value), Handles.value(restored));
        }
        Object[] factories = {Handles.federateFactory(), Handles.objectClassFactory(),
                Handles.objectInstanceFactory(), Handles.interactionClassFactory(),
                Handles.attributeFactory(), Handles.parameterFactory(), Handles.dimensionFactory(),
                Handles.transportationFactory()};
        for (Object factory : factories) {
            assertSame(factory, serializedCopy(factory));
        }
    }

    private static Object serializedCopy(Object value) throws Exception {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        try (ObjectOutputStream output = new ObjectOutputStream(bytes)) { output.writeObject(value); }
        try (ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(bytes.toByteArray()))) {
            return input.readObject();
        }
    }

    private static void encode(Object handle, byte[] bytes, int offset) {
        if (handle instanceof FederateHandle) {
            assertEquals(8, ((FederateHandle) handle).encodedLength());
            ((FederateHandle) handle).encode(bytes, offset);
        } else if (handle instanceof ObjectClassHandle) {
            assertEquals(8, ((ObjectClassHandle) handle).encodedLength());
            ((ObjectClassHandle) handle).encode(bytes, offset);
        } else if (handle instanceof ObjectInstanceHandle) {
            assertEquals(8, ((ObjectInstanceHandle) handle).encodedLength());
            ((ObjectInstanceHandle) handle).encode(bytes, offset);
        } else if (handle instanceof InteractionClassHandle) {
            assertEquals(8, ((InteractionClassHandle) handle).encodedLength());
            ((InteractionClassHandle) handle).encode(bytes, offset);
        } else if (handle instanceof AttributeHandle) {
            assertEquals(8, ((AttributeHandle) handle).encodedLength());
            ((AttributeHandle) handle).encode(bytes, offset);
        } else if (handle instanceof ParameterHandle) {
            assertEquals(8, ((ParameterHandle) handle).encodedLength());
            ((ParameterHandle) handle).encode(bytes, offset);
        } else if (handle instanceof DimensionHandle) {
            assertEquals(8, ((DimensionHandle) handle).encodedLength());
            ((DimensionHandle) handle).encode(bytes, offset);
        } else {
            fail("Unexpected handle type");
        }
    }

    private static Object decode(int kind, byte[] bytes, int offset) throws Exception {
        switch (kind) {
            case 0: return Handles.federateFactory().decode(bytes, offset);
            case 1: return Handles.objectClassFactory().decode(bytes, offset);
            case 2: return Handles.objectInstanceFactory().decode(bytes, offset);
            case 3: return Handles.interactionClassFactory().decode(bytes, offset);
            case 4: return Handles.attributeFactory().decode(bytes, offset);
            case 5: return Handles.parameterFactory().decode(bytes, offset);
            case 6: return Handles.dimensionFactory().decode(bytes, offset);
            default: throw new AssertionError("Unknown handle kind");
        }
    }
}
