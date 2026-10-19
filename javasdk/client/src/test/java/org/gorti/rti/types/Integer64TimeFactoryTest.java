package org.gorti.rti.types;

import hla.rti1516e.LogicalTimeFactoryFactory;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import hla.rti1516e.time.HLAinteger64TimeFactory;
import java.util.HashSet;
import java.util.Set;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class Integer64TimeFactoryTest {
    // IEEE 1516.1-2010 12.4/Table 18 and 12.11.5.24.1-.3, supplied PDF pp.339,359-360.
    static HLAinteger64TimeFactory factory() {
        return assertInstanceOf(HLAinteger64TimeFactory.class,
                LogicalTimeFactoryFactory.getLogicalTimeFactory("HLAinteger64Time"));
    }

    @Test
    void standardFactoryIsDiscoverableByNameAndInterface() {
        HLAinteger64TimeFactory byName = factory();
        HLAinteger64TimeFactory byType =
                LogicalTimeFactoryFactory.getLogicalTimeFactory(HLAinteger64TimeFactory.class);
        assertNotNull(byType);
        assertEquals("HLAinteger64Time", byName.getName());
        assertEquals(byName.getClass(), byType.getClass());
        assertEquals("org.gorti.rti.types.Integer64TimeFactory", byName.getClass().getName());
    }

    @Test
    void enumerationContainsBothStandardFactories() {
        Set<String> names = new HashSet<>();
        LogicalTimeFactoryFactory.getAvailableLogicalTimeFactories()
                .forEach(factory -> names.add(factory.getName()));
        assertTrue(names.contains("HLAinteger64Time"));
        assertTrue(names.contains("HLAfloat64Time"));
    }

    @Test
    void defaultFactoryRemainsFloat64AndUnknownNameRemainsAbsent() {
        assertInstanceOf(HLAfloat64TimeFactory.class,
                LogicalTimeFactoryFactory.getLogicalTimeFactory(""));
        assertNull(LogicalTimeFactoryFactory.getLogicalTimeFactory("unknown-time-implementation"));
    }

    @Test
    void endpointsAndEpsilonUseExactStandardValues() {
        HLAinteger64TimeFactory factory = factory();
        assertEquals(0L, factory.makeInitial().getValue());
        assertTrue(factory.makeInitial().isInitial());
        assertFalse(factory.makeInitial().isFinal());
        assertEquals(Long.MAX_VALUE, factory.makeFinal().getValue());
        assertTrue(factory.makeFinal().isFinal());
        assertFalse(factory.makeFinal().isInitial());
        assertFalse(factory.makeTime(1).isInitial());
        assertFalse(factory.makeTime(Long.MAX_VALUE - 1).isFinal());
        assertEquals(0L, factory.makeZero().getValue());
        assertTrue(factory.makeZero().isZero());
        assertFalse(factory.makeZero().isEpsilon());
        assertEquals(1L, factory.makeEpsilon().getValue());
        assertTrue(factory.makeEpsilon().isEpsilon());
        assertFalse(factory.makeEpsilon().isZero());
        assertFalse(factory.makeInterval(2).isEpsilon());
    }

    @Test
    void negativeTimesAndIntervalsAreRejected() {
        HLAinteger64TimeFactory factory = factory();
        for (long value : new long[] {-1L, -Long.MAX_VALUE, Long.MIN_VALUE}) {
            assertThrows(IllegalArgumentException.class, () -> factory.makeTime(value));
            assertThrows(IllegalArgumentException.class, () -> factory.makeInterval(value));
        }
    }
}
