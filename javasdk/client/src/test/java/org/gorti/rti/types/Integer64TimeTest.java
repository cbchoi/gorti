package org.gorti.rti.types;

import hla.rti1516e.LogicalTime;
import hla.rti1516e.LogicalTimeInterval;
import hla.rti1516e.exceptions.CouldNotDecode;
import hla.rti1516e.exceptions.CouldNotEncode;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.exceptions.InvalidLogicalTimeInterval;
import hla.rti1516e.time.HLAinteger64Interval;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.time.HLAinteger64TimeFactory;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.ObjectInputStream;
import java.io.ObjectOutputStream;
import java.lang.reflect.Proxy;
import java.math.BigInteger;
import java.util.Arrays;
import java.util.Random;
import org.junit.jupiter.api.Test;
import static org.gorti.rti.types.Integer64TimeFactoryTest.factory;
import static org.junit.jupiter.api.Assertions.*;

class Integer64TimeTest {
    // Integer algebra: IEEE 1516.1-2010 12.3, supplied PDF pp.325-326.
    private static final long ABOVE_DOUBLE_PRECISION = (1L << 53) + 1L;

    @Test
    void adjacentLargeIntegersStayDistinct() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        for (long value : new long[] {(1L << 53) - 1, 1L << 53, ABOVE_DOUBLE_PRECISION,
                Long.MAX_VALUE - 1}) {
            HLAinteger64Time time = factory.makeTime(value);
            HLAinteger64Interval interval = factory.makeInterval(value);
            assertEquals(value, time.getValue());
            assertEquals(value + 1, time.add(factory.makeEpsilon()).getValue());
            assertEquals(value - 1, time.subtract(factory.makeEpsilon()).getValue());
            assertEquals(value + 1, interval.add(factory.makeEpsilon()).getValue());
            assertEquals(value - 1, interval.subtract(factory.makeEpsilon()).getValue());
            assertTrue(time.compareTo(factory.makeTime(value + 1)) < 0);
            assertNotEquals(time, factory.makeTime(value + 1));
            assertEquals(1L, time.distance(factory.makeTime(value + 1)).getValue());
        }
    }

    @Test
    void arithmeticIsExactAndImmutable() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        HLAinteger64Time time = factory.makeTime(ABOVE_DOUBLE_PRECISION);
        HLAinteger64Interval interval = factory.makeInterval(3);
        assertEquals(ABOVE_DOUBLE_PRECISION + 3, time.add(interval).getValue());
        assertEquals(ABOVE_DOUBLE_PRECISION - 3, time.subtract(interval).getValue());
        assertEquals(6L, interval.add(interval).getValue());
        assertEquals(2L, interval.subtract(factory.makeEpsilon()).getValue());
        assertEquals(ABOVE_DOUBLE_PRECISION, time.getValue());
        assertEquals(3L, interval.getValue());
        assertNotSame(time, time.add(interval));
        assertNotSame(interval, interval.add(interval));
    }

    @Test
    void zeroIsAnIdentityAndEqualValuesHaveZeroDifference() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        for (long value : new long[] {0, 1, ABOVE_DOUBLE_PRECISION, Long.MAX_VALUE}) {
            HLAinteger64Time time = factory.makeTime(value);
            HLAinteger64Interval interval = factory.makeInterval(value);
            assertEquals(time, time.add(factory.makeZero()));
            assertEquals(time, time.subtract(factory.makeZero()));
            assertEquals(interval, interval.add(factory.makeZero()));
            assertEquals(interval, interval.subtract(factory.makeZero()));
            assertEquals(factory.makeZero(), time.distance(time));
            assertEquals(factory.makeZero(), interval.subtract(interval));
        }
    }

    @Test
    void arithmeticRejectsOverflowAndNegativeResultsWithoutMutation() {
        HLAinteger64TimeFactory factory = factory();
        HLAinteger64Time finalTime = factory.makeFinal();
        HLAinteger64Interval largestInterval = factory.makeInterval(Long.MAX_VALUE);
        assertThrows(IllegalTimeArithmetic.class, () -> finalTime.add(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> finalTime.add(largestInterval));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeInitial().subtract(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeTime(1).subtract(largestInterval));
        assertThrows(IllegalTimeArithmetic.class, () -> largestInterval.add(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> largestInterval.add(largestInterval));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeZero().subtract(factory.makeEpsilon()));
        assertThrows(IllegalTimeArithmetic.class, () -> factory.makeEpsilon().subtract(largestInterval));
        assertEquals(Long.MAX_VALUE, finalTime.getValue());
        assertEquals(Long.MAX_VALUE, largestInterval.getValue());
    }

    @Test
    void distanceIsSymmetricNonnegativeAndExactAtExtremes() {
        HLAinteger64TimeFactory factory = factory();
        HLAinteger64Time low = factory.makeTime(2);
        HLAinteger64Time high = factory.makeTime(ABOVE_DOUBLE_PRECISION);
        assertEquals(ABOVE_DOUBLE_PRECISION - 2, low.distance(high).getValue());
        assertEquals(low.distance(high), high.distance(low));
        assertEquals(Long.MAX_VALUE, factory.makeInitial().distance(factory.makeFinal()).getValue());
        assertEquals(Long.MAX_VALUE, factory.makeFinal().distance(factory.makeInitial()).getValue());
        assertTrue(low.compareTo(high) < 0);
        assertTrue(high.compareTo(low) > 0);
        assertEquals(0, low.compareTo(factory.makeTime(2)));
        assertTrue(factory.makeZero().compareTo(factory.makeInterval(Long.MAX_VALUE)) < 0);
        assertTrue(factory.makeInterval(Long.MAX_VALUE).compareTo(factory.makeZero()) > 0);
    }

    @Test
    void valueEqualityHashingAndPrintingPreserveKindAndExactValue() {
        HLAinteger64TimeFactory factory = factory();
        HLAinteger64Time time = factory.makeTime(ABOVE_DOUBLE_PRECISION);
        HLAinteger64Interval interval = factory.makeInterval(ABOVE_DOUBLE_PRECISION);
        assertEquals(time, factory.makeTime(ABOVE_DOUBLE_PRECISION));
        assertEquals(time.hashCode(), factory.makeTime(ABOVE_DOUBLE_PRECISION).hashCode());
        assertEquals(interval, factory.makeInterval(ABOVE_DOUBLE_PRECISION));
        assertEquals(interval.hashCode(), factory.makeInterval(ABOVE_DOUBLE_PRECISION).hashCode());
        assertNotEquals(time, factory.makeTime(ABOVE_DOUBLE_PRECISION - 1));
        assertNotEquals(interval, factory.makeInterval(ABOVE_DOUBLE_PRECISION - 1));
        assertNotEquals(time, interval);
        assertNotEquals(interval, time);
        assertNotEquals(time, null);
        assertNotEquals(interval, null);
        assertNotEquals(factory.makeInitial(), TimeValues.time(0));
        assertNotEquals(factory.makeZero(), TimeValues.interval(0));
        assertEquals("HLAinteger64Time(9007199254740993)", time.toString());
        assertEquals("HLAinteger64Interval(9007199254740993)", interval.toString());
    }

    @Test
    void encodingMatchesIndependentBigEndianGoldenVectors() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        long[] values = {0, 1, 0x0102030405060708L, ABOVE_DOUBLE_PRECISION, Long.MAX_VALUE};
        byte[][] vectors = {
            {0, 0, 0, 0, 0, 0, 0, 0},
            {0, 0, 0, 0, 0, 0, 0, 1},
            {1, 2, 3, 4, 5, 6, 7, 8},
            {0, 0x20, 0, 0, 0, 0, 0, 1},
            {0x7f, -1, -1, -1, -1, -1, -1, -1}
        };
        for (int i = 0; i < values.length; i++) {
            byte[] target = new byte[12];
            Arrays.fill(target, (byte) 0x55);
            byte[] expected = target.clone();
            System.arraycopy(vectors[i], 0, expected, 2, 8);
            HLAinteger64Time time = factory.makeTime(values[i]);
            HLAinteger64Interval interval = factory.makeInterval(values[i]);
            assertEquals(8, time.encodedLength());
            assertEquals(8, interval.encodedLength());
            time.encode(target, 2);
            assertArrayEquals(expected, target);
            Arrays.fill(target, (byte) 0x55);
            interval.encode(target, 2);
            assertArrayEquals(expected, target);
            assertEquals(values[i], factory.decodeTime(vectors[i], 0).getValue());
            assertEquals(values[i], factory.decodeInterval(vectors[i], 0).getValue());
            assertEquals(time, factory.decodeTime(target, 2));
            assertEquals(interval, factory.decodeInterval(target, 2));
            assertArrayEquals(expected, target);
        }
    }

    @Test
    void negativeWireValuesUseTheDeclaredDecodeException() {
        HLAinteger64TimeFactory factory = factory();
        for (byte[] bytes : new byte[][] {
                {-1, -1, -1, -1, -1, -1, -1, -1},
                {(byte) 0x80, 0, 0, 0, 0, 0, 0, 0}}) {
            byte[] before = bytes.clone();
            assertThrows(CouldNotDecode.class, () -> factory.decodeTime(bytes, 0));
            assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(bytes, 0));
            assertArrayEquals(before, bytes);
        }
    }

    @Test
    void invalidEncodingBoundsDoNotWriteAndUseDeclaredExceptions() {
        HLAinteger64TimeFactory factory = factory();
        HLAinteger64Time time = factory.makeInitial();
        HLAinteger64Interval interval = factory.makeZero();
        assertThrows(CouldNotEncode.class, () -> time.encode(null, 0));
        assertThrows(CouldNotEncode.class, () -> interval.encode(null, 0));
        assertThrows(CouldNotDecode.class, () -> factory.decodeTime(null, 0));
        assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(null, 0));
        for (int size : new int[] {0, 1, 7, 8, 12}) {
            for (int offset : new int[] {-1, Integer.MIN_VALUE, Integer.MAX_VALUE, size - 7}) {
                byte[] bytes = new byte[size];
                Arrays.fill(bytes, (byte) 0x55);
                byte[] before = bytes.clone();
                assertThrows(CouldNotEncode.class, () -> time.encode(bytes, offset));
                assertThrows(CouldNotEncode.class, () -> interval.encode(bytes, offset));
                assertArrayEquals(before, bytes);
                assertThrows(CouldNotDecode.class, () -> factory.decodeTime(bytes, offset));
                assertThrows(CouldNotDecode.class, () -> factory.decodeInterval(bytes, offset));
                assertArrayEquals(before, bytes);
            }
        }
    }

    @Test
    void seededRoundTripsRetainEveryIntegerBit() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        Random random = new Random(15162010L);
        for (int i = 0; i < 1024; i++) {
            long value = random.nextLong() & Long.MAX_VALUE;
            byte[] bytes = new byte[8];
            factory.makeTime(value).encode(bytes, 0);
            assertEquals(value, factory.decodeTime(bytes, 0).getValue());
            factory.makeInterval(value).encode(bytes, 0);
            assertEquals(value, factory.decodeInterval(bytes, 0).getValue());
        }
    }

    @Test
    void seededAlgebraMatchesIndependentBigIntegerOracle() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        Random random = new Random(20101516L);
        BigInteger maximum = BigInteger.valueOf(Long.MAX_VALUE);
        for (int i = 0; i < 512; i++) {
            long left = random.nextLong() & Long.MAX_VALUE;
            long right = random.nextLong() & Long.MAX_VALUE;
            HLAinteger64Time time = factory.makeTime(left);
            HLAinteger64Interval interval = factory.makeInterval(left);
            HLAinteger64Interval operand = factory.makeInterval(right);
            BigInteger sum = BigInteger.valueOf(left).add(BigInteger.valueOf(right));
            if (sum.compareTo(maximum) > 0) {
                assertThrows(IllegalTimeArithmetic.class, () -> time.add(operand));
                assertThrows(IllegalTimeArithmetic.class, () -> interval.add(operand));
            } else {
                assertEquals(sum.longValueExact(), time.add(operand).getValue());
                assertEquals(sum.longValueExact(), interval.add(operand).getValue());
            }
            BigInteger difference = BigInteger.valueOf(left).subtract(BigInteger.valueOf(right));
            if (difference.signum() < 0) {
                assertThrows(IllegalTimeArithmetic.class, () -> time.subtract(operand));
                assertThrows(IllegalTimeArithmetic.class, () -> interval.subtract(operand));
            } else {
                assertEquals(difference.longValueExact(), time.subtract(operand).getValue());
                assertEquals(difference.longValueExact(), interval.subtract(operand).getValue());
            }
            assertEquals(difference.abs().longValueExact(), time.distance(factory.makeTime(right)).getValue());
            assertEquals(difference.signum(), Integer.signum(time.compareTo(factory.makeTime(right))));
            assertEquals(difference.signum(), Integer.signum(interval.compareTo(operand)));
        }
    }

    @Test
    void standardForeignIntegerOperandsAreAcceptedWithoutConcreteCasts() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        HLAinteger64Time time = proxy(HLAinteger64Time.class, ABOVE_DOUBLE_PRECISION);
        HLAinteger64Interval interval = proxy(HLAinteger64Interval.class, 3L);
        assertEquals(ABOVE_DOUBLE_PRECISION + 3, factory.makeTime(ABOVE_DOUBLE_PRECISION).add(interval).getValue());
        assertEquals(ABOVE_DOUBLE_PRECISION - 3, factory.makeTime(ABOVE_DOUBLE_PRECISION).subtract(interval).getValue());
        assertEquals(6L, factory.makeInterval(3).add(interval).getValue());
        assertEquals(0L, factory.makeInterval(3).subtract(interval).getValue());
        assertEquals(0, factory.makeTime(ABOVE_DOUBLE_PRECISION).compareTo(time));
        assertEquals(0, factory.makeInterval(3).compareTo(interval));
        assertEquals(ABOVE_DOUBLE_PRECISION, factory.makeInitial().distance(time).getValue());
    }

    @Test
    void invalidForeignOperandsAndNullUseTheNarrowedStandardSignatures() {
        HLAinteger64TimeFactory factory = factory();
        for (HLAinteger64Interval interval : new HLAinteger64Interval[] {
                null, proxy(HLAinteger64Interval.class, -1L), proxy(HLAinteger64Interval.class, Long.MIN_VALUE)}) {
            assertThrows(IllegalTimeArithmetic.class, () -> factory.makeInitial().add(interval));
            assertThrows(IllegalTimeArithmetic.class, () -> factory.makeFinal().subtract(interval));
            assertThrows(InvalidLogicalTimeInterval.class, () -> factory.makeZero().add(interval));
            assertThrows(InvalidLogicalTimeInterval.class, () -> factory.makeZero().subtract(interval));
        }
        HLAinteger64Time invalidTime = proxy(HLAinteger64Time.class, Long.MIN_VALUE);
        HLAinteger64Interval invalidInterval = proxy(HLAinteger64Interval.class, -1L);
        assertThrows(IllegalArgumentException.class, () -> factory.makeInitial().distance(invalidTime));
        assertThrows(IllegalArgumentException.class, () -> factory.makeInitial().compareTo(invalidTime));
        assertThrows(IllegalArgumentException.class, () -> factory.makeZero().compareTo(invalidInterval));
        assertThrows(NullPointerException.class, () -> factory.makeInitial().distance(null));
        assertThrows(NullPointerException.class, () -> factory.makeInitial().compareTo(null));
        assertThrows(NullPointerException.class, () -> factory.makeZero().compareTo(null));
    }

    @Test
    void sharedFloatOnlyConversionStillRejectsActualIntegerValues() {
        HLAinteger64TimeFactory factory = factory();
        LogicalTime<?, ?> time = factory.makeTime(ABOVE_DOUBLE_PRECISION);
        LogicalTimeInterval<?> interval = factory.makeInterval(ABOVE_DOUBLE_PRECISION);
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value(time));
        assertThrows(IllegalArgumentException.class, () -> TimeValues.value(interval));
    }

    @Test
    void valuesAndFactorySurviveSerializationExactly() throws Exception {
        HLAinteger64TimeFactory factory = factory();
        for (long number : new long[] {0, 1, ABOVE_DOUBLE_PRECISION, Long.MAX_VALUE}) {
            for (Object value : new Object[] {factory.makeTime(number), factory.makeInterval(number)}) {
                Object copy = serializedCopy(value);
                assertEquals(value, copy);
                assertEquals(value.hashCode(), copy.hashCode());
            }
        }
        HLAinteger64TimeFactory copy = (HLAinteger64TimeFactory) serializedCopy(factory);
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

    private static <T> T proxy(Class<T> type, long value) {
        return type.cast(Proxy.newProxyInstance(type.getClassLoader(), new Class<?>[] {type},
                (instance, method, arguments) -> {
                    if (method.getName().equals("getValue")) return value;
                    throw new AssertionError("Unexpected invocation: " + method.getName());
                }));
    }
}
