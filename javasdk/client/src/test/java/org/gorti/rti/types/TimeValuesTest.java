package org.gorti.rti.types;

import hla.rti1516e.LogicalTime;
import hla.rti1516e.LogicalTimeInterval;
import hla.rti1516e.exceptions.CouldNotDecode;
import hla.rti1516e.exceptions.CouldNotEncode;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.exceptions.InvalidLogicalTimeInterval;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import hla.rti1516e.time.HLAinteger64Interval;
import hla.rti1516e.time.HLAinteger64Time;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.ObjectInputStream;
import java.io.ObjectOutputStream;
import java.lang.reflect.Proxy;
import java.nio.ByteBuffer;
import java.util.Arrays;
import java.util.Random;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class TimeValuesTest {
    private final HLAfloat64TimeFactory factory = TimeValues.factory();

    @Test
    void factoryUsesStandardFiniteFinalAndSmallestPositiveEpsilon() {
        assertEquals("HLAfloat64Time", factory.getName());
        assertEquals(0.0, factory.makeInitial().getValue());
        assertTrue(factory.makeInitial().isInitial());
        assertEquals(Double.MAX_VALUE, factory.makeFinal().getValue());
        assertTrue(factory.makeFinal().isFinal());
        assertFalse(factory.makeTime(1.0).isInitial());
        assertFalse(factory.makeTime(1.0).isFinal());
        assertEquals(0.0, factory.makeZero().getValue());
        assertTrue(factory.makeZero().isZero());
        assertEquals(Double.MIN_VALUE, factory.makeEpsilon().getValue());
        assertTrue(factory.makeEpsilon().isEpsilon());
        assertFalse(factory.makeInterval(1.0).isEpsilon());
        assertFalse(factory.makeEpsilon().isZero());
        assertEquals(factory.makeInitial(), new Float64TimeFactory().makeInitial());
    }

    @Test
    void timeAndIntervalArithmeticIsImmutable() throws Exception {
        HLAfloat64Time time = factory.makeTime(12.0);
        HLAfloat64Interval interval = factory.makeInterval(3.0);
        assertEquals(factory.makeTime(15.0), time.add(interval));
        assertEquals(factory.makeTime(9.0), time.subtract(interval));
        assertEquals(factory.makeInterval(6.0), interval.add(interval));
        assertEquals(factory.makeZero(), interval.subtract(interval));
        assertEquals(12.0, time.getValue());
        assertEquals(3.0, interval.getValue());
        assertEquals(time, time.add(factory.makeZero()));
        assertEquals(time, time.subtract(factory.makeZero()));
        assertEquals(interval, interval.add(factory.makeZero()));
        assertEquals(interval, interval.subtract(factory.makeZero()));
    }

    @Test
    void addingOrSubtractingNonzeroIntervalsNeverRoundsBack() throws Exception {
        HLAfloat64Interval epsilon = factory.makeEpsilon();
        for (double value : new double[] {Double.MIN_VALUE, Double.MIN_NORMAL, 1.0, 1e100, 1e300}) {
            HLAfloat64Time time = factory.makeTime(value);
            HLAfloat64Interval interval = factory.makeInterval(value);
            assertEquals(Math.nextUp(value), time.add(epsilon).getValue());
            assertEquals(Math.nextDown(value), time.subtract(epsilon).getValue());
            assertEquals(Math.nextUp(value), interval.add(epsilon).getValue());
            assertEquals(Math.nextDown(value), interval.subtract(epsilon).getValue());
        }
        HLAfloat64Interval small = factory.makeInterval(Math.ulp(1e100) / 4.0);
        assertEquals(Math.nextUp(1e100), factory.makeTime(1e100).add(small).getValue());
        assertEquals(Math.nextDown(1e100), factory.makeTime(1e100).subtract(small).getValue());
    }

    @Test
    void arithmeticRejectsOverflowAndNegativeResults() {
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeFinal().add(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeFinal().add(factory.makeInterval(Double.MAX_VALUE)));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeInitial().subtract(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeTime(1).subtract(factory.makeInterval(2)));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeInterval(Double.MAX_VALUE).add(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeZero().subtract(factory.makeEpsilon()));
    }

    @Test
    void distanceIsNonnegativeAndComparisonIsOrdered() {
        HLAfloat64Time low = factory.makeTime(2.0);
        HLAfloat64Time high = factory.makeTime(5.5);
        assertEquals(factory.makeInterval(3.5), low.distance(high));
        assertEquals(low.distance(high), high.distance(low));
        assertEquals(factory.makeZero(), high.distance(high));
        assertEquals(factory.makeInterval(Double.MAX_VALUE), factory.makeFinal().distance(factory.makeInitial()));
        assertTrue(low.compareTo(high) < 0);
        assertTrue(high.compareTo(low) > 0);
        assertEquals(0, low.compareTo(factory.makeTime(2.0)));
        assertTrue(factory.makeZero().compareTo(factory.makeEpsilon()) < 0);
        assertThrows(NullPointerException.class, () -> low.compareTo(null));
        assertThrows(NullPointerException.class, () -> low.distance(null));
        assertThrows(NullPointerException.class, () -> factory.makeZero().compareTo(null));
    }

    @Test
    void equalitySeparatesTimeFromIntervalAndCanonicalizesNegativeZero() {
        assertEquals(factory.makeTime(2), factory.makeTime(2));
        assertEquals(factory.makeTime(2).hashCode(), factory.makeTime(2).hashCode());
        assertNotEquals(factory.makeTime(2), factory.makeTime(3));
        assertNotEquals(factory.makeTime(2), factory.makeInterval(2));
        assertNotEquals(factory.makeInterval(2), factory.makeTime(2));
        assertNotEquals(factory.makeInterval(2), factory.makeInterval(3));
        assertNotEquals(factory.makeTime(2), null);
        assertNotEquals(factory.makeInterval(2), null);
        assertEquals(factory.makeInitial(), factory.makeTime(-0.0));
        assertEquals(factory.makeZero(), factory.makeInterval(-0.0));
        assertEquals(factory.makeInitial().hashCode(), factory.makeTime(-0.0).hashCode());
        assertEquals(factory.makeZero().hashCode(), factory.makeInterval(-0.0).hashCode());
        assertEquals(0L, Double.doubleToRawLongBits(factory.makeTime(-0.0).getValue()));
        assertTrue(factory.makeTime(2).toString().contains("2.0"));
        assertTrue(factory.makeInterval(2).toString().contains("2.0"));
    }

    @Test
    void invalidNumericValuesAreRejectedAtConstructionAndDecode() {
        for (double value : new double[] {-1.0, -Double.MIN_VALUE, Double.NaN,
                Double.POSITIVE_INFINITY, Double.NEGATIVE_INFINITY}) {
            assertThrows(IllegalArgumentException.class, () -> factory.makeTime(value));
            assertThrows(IllegalArgumentException.class, () -> factory.makeInterval(value));
            byte[] bytes = ByteBuffer.allocate(8).putDouble(value).array();
            assertThrows(CouldNotDecode.class, () -> factory.decodeTime(bytes, 0));
            assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(bytes, 0));
        }
    }

    @Test
    void encodingIsExactlyEightByteBigEndianWithOffset() throws Exception {
        byte[] bytes = new byte[12];
        Arrays.fill(bytes, (byte) 0x55);
        assertEquals(8, factory.makeTime(3.25).encodedLength());
        assertEquals(8, factory.makeInterval(3.25).encodedLength());
        factory.makeTime(3.25).encode(bytes, 2);
        assertArrayEquals(new byte[] {0x55, 0x55, 0x40, 0x0a, 0, 0, 0, 0, 0, 0, 0x55, 0x55}, bytes);
        assertEquals(factory.makeTime(3.25), factory.decodeTime(bytes, 2));
        assertEquals(factory.makeInterval(3.25), factory.decodeInterval(bytes, 2));
        factory.makeEpsilon().encode(bytes, 2);
        assertArrayEquals(new byte[] {0x55, 0x55, 0, 0, 0, 0, 0, 0, 0, 1, 0x55, 0x55}, bytes);
        byte[] finalBytes = new byte[8];
        factory.makeFinal().encode(finalBytes, 0);
        assertArrayEquals(new byte[] {0x7f, (byte) 0xef, -1, -1, -1, -1, -1, -1}, finalBytes);
    }

    @Test
    void invalidEncodingBoundsUseDeclaredExceptionsAndDoNotWrite() {
        HLAfloat64Time time = factory.makeTime(1);
        HLAfloat64Interval interval = factory.makeInterval(1);
        assertThrows(CouldNotEncode.class, () -> time.encode(null, 0));
        assertThrows(CouldNotEncode.class, () -> interval.encode(null, 0));
        assertThrows(CouldNotDecode.class, () -> factory.decodeTime(null, 0));
        assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(null, 0));
        for (int size : new int[] {0, 7, 8}) {
            for (int offset : new int[] {-1, 1, Integer.MIN_VALUE, Integer.MAX_VALUE}) {
                byte[] bytes = new byte[size];
                Arrays.fill(bytes, (byte) 0x55);
                byte[] before = bytes.clone();
                assertThrows(CouldNotEncode.class, () -> time.encode(bytes, offset));
                assertThrows(CouldNotEncode.class, () -> interval.encode(bytes, offset));
                assertArrayEquals(before, bytes);
                assertThrows(CouldNotDecode.class, () -> factory.decodeTime(bytes, offset));
                assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(bytes, offset));
            }
        }
        assertThrows(CouldNotDecode.class, () -> factory.decodeTime(new byte[7], 0));
        assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(new byte[7], 0));
    }

    @Test
    void bitExactRoundTripsIncludeExtremeAndSeededFiniteValues() throws Exception {
        Random random = new Random(15162010L);
        for (int i = 0; i < 1024; i++) {
            long bits = random.nextLong() & Long.MAX_VALUE;
            double value = Double.longBitsToDouble(bits);
            if (!Double.isFinite(value)) { continue; }
            byte[] bytes = new byte[8];
            factory.makeTime(value).encode(bytes, 0);
            assertEquals(bits, Double.doubleToRawLongBits(factory.decodeTime(bytes, 0).getValue()));
            factory.makeInterval(value).encode(bytes, 0);
            assertEquals(bits, Double.doubleToRawLongBits(factory.decodeInterval(bytes, 0).getValue()));
        }
    }

    @Test
    void noInteger64OrNullConversionIsPretended() {
        HLAinteger64Time integerTime = proxy(HLAinteger64Time.class, null);
        HLAinteger64Interval integerInterval = proxy(HLAinteger64Interval.class, null);
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value(integerTime));
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value(integerInterval));
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value((LogicalTime<?, ?>) null));
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value((LogicalTimeInterval<?>) null));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeInitial().add(null));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeInitial().subtract(null));
        assertThrows(InvalidLogicalTimeInterval.class, () -> factory.makeZero().add(null));
        assertThrows(InvalidLogicalTimeInterval.class, () -> factory.makeZero().subtract(null));
    }

    @Test
    void standardizedForeignFloat64OperandsAreValidated() throws Exception {
        HLAfloat64Time time = proxy(HLAfloat64Time.class, 3.5);
        HLAfloat64Interval interval = proxy(HLAfloat64Interval.class, 1.5);
        assertEquals(3.5, TimeValues.value(time));
        assertEquals(1.5, TimeValues.value(interval));
        assertEquals(factory.makeTime(5), factory.makeTime(3.5).add(interval));
        assertEquals(0, factory.makeTime(3.5).compareTo(time));
        HLAfloat64Interval invalid = proxy(HLAfloat64Interval.class, Double.NaN);
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value(invalid));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeTime(1).add(invalid));
        assertThrows(InvalidLogicalTimeInterval.class, () -> factory.makeInterval(1).add(invalid));
    }

    @Test
    void valuesAndFactoryAreSerializable() throws Exception {
        for (Object value : new Object[] {factory.makeTime(3.5), factory.makeInterval(0.5)}) {
            Object copy = serializedCopy(value);
            assertEquals(value, copy);
            assertEquals(value.hashCode(), copy.hashCode());
        }
        HLAfloat64TimeFactory copy = (HLAfloat64TimeFactory) serializedCopy(factory);
        assertEquals(factory.getName(), copy.getName());
        assertEquals(factory.makeFinal(), copy.makeFinal());
        assertEquals(factory.makeEpsilon(), copy.makeEpsilon());
    }

    private static Object serializedCopy(Object value) throws Exception {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        try (ObjectOutputStream output = new ObjectOutputStream(bytes)) { output.writeObject(value); }
        try (ObjectInputStream input = new ObjectInputStream(new ByteArrayInputStream(bytes.toByteArray()))) {
            return input.readObject();
        }
    }

    private static <T> T proxy(Class<T> type, Double value) {
        return type.cast(Proxy.newProxyInstance(type.getClassLoader(), new Class<?>[] {type},
                (instance, method, arguments) -> {
                    if (method.getName().equals("getValue") && value != null) { return value; }
                    throw new AssertionError("Unexpected invocation: " + method.getName());
                }));
    }
}
