package org.gorti.rti.encoding;

import hla.rti1516e.encoding.*;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import static org.gorti.rti.encoding.PrimitiveEncodingTest.*;

class EncodingSafetyTest {
    @Test void negativeHugeAndTruncatedLengthsFailAsDecoderExceptions() throws Exception {
        EncoderFactory f = factory();
        DataElement[] values = {f.createHLAASCIIstring(), f.createHLAunicodeString(), f.createHLAopaqueData(),
                f.createHLAvariableArray(i -> f.createHLAinteger32BE())};
        for (DataElement value : values) {
            assertThrows(DecoderException.class, () -> value.decode(hex("ffffffff")));
            assertThrows(DecoderException.class, () -> value.decode(hex("7fffffff")));
            assertThrows(DecoderException.class, () -> value.decode(hex("0000000201")));
            assertThrows(DecoderException.class, () -> value.decode(hex("000000")));
        }
    }

    @Test void booleansAndAsciiRejectOutOfDomainValues() throws Exception {
        EncoderFactory f = factory();
        HLAboolean bool = f.createHLAboolean(true);
        assertThrows(DecoderException.class, () -> bool.decode(hex("00000002"))); assertTrue(bool.getValue());
        assertThrows(DecoderException.class, () -> bool.decode(hex("ffffffff")));
        assertThrows(IllegalArgumentException.class, () -> f.createHLAASCIIchar((byte) 0x80));
        assertThrows(IllegalArgumentException.class, () -> f.createHLAASCIIstring("\u00e9"));
        assertThrows(DecoderException.class, () -> f.createHLAASCIIchar().decode(hex("ff")));
        HLAASCIIstring ascii = f.createHLAASCIIstring("old");
        assertThrows(DecoderException.class, () -> ascii.decode(hex("0000000180"))); assertEquals("old", ascii.getValue());
    }

    @Test void invalidSizesAreRejectedBeforeCallingFactories() throws Exception {
        EncoderFactory f = factory(); AtomicInteger calls = new AtomicInteger();
        DataElementFactory<HLAbyte> elements = i -> { calls.incrementAndGet(); return f.createHLAbyte(); };
        assertThrows(IllegalArgumentException.class, () -> f.createHLAfixedArray(elements, -1));
        assertThrows(IllegalArgumentException.class, () -> f.createHLAfixedArray(elements, Integer.MAX_VALUE));
        assertEquals(0, calls.get());
        HLAvariableArray<HLAbyte> value = f.createHLAvariableArray(elements);
        int before = calls.get();
        assertThrows(IllegalArgumentException.class, () -> value.resize(-1));
        assertThrows(IllegalArgumentException.class, () -> value.resize(Integer.MAX_VALUE));
        assertThrows(DecoderException.class, () -> value.decode(hex("7fffffff")));
        assertEquals(before, calls.get());
    }

    @Test void nullAndAliasedFactoryResultsFailClearly() throws Exception {
        EncoderFactory f = factory();
        assertThrows(NullPointerException.class, () -> f.createHLAvariableArray(null));
        assertThrows(NullPointerException.class, () -> f.createHLAfixedArray(i -> null, 1));
        assertThrows(NullPointerException.class, () -> f.createHLAfixedRecord().add(null));
        assertThrows(NullPointerException.class, () -> f.createHLAopaqueData(null));
        HLAinteger32BE shared = f.createHLAinteger32BE();
        assertThrows(IllegalArgumentException.class, () -> f.createHLAfixedArray(i -> shared, 2));
        HLAvariableArray<HLAinteger32BE> array = f.createHLAvariableArray(i -> shared);
        assertThrows(DecoderException.class, () -> array.decode(hex("000000020000000100000002")));
        assertEquals(0, array.size());
    }

    @Test void failedGrowthDoesNotPartiallyResize() throws Exception {
        EncoderFactory f = factory();
        HLAvariableArray<HLAinteger32BE> array = f.createHLAvariableArray(i -> {
            if (i == 2) throw new IllegalStateException("factory unavailable");
            return f.createHLAinteger32BE(i);
        }, f.createHLAinteger32BE(9));
        assertThrows(IllegalStateException.class, () -> array.resize(3));
        assertEquals(1, array.size()); assertEquals(9, array.get(0).getValue());
        assertThrows(DecoderException.class, () -> array.decode(hex("00000003000000010000000200000003")));
        assertEquals(1, array.size()); assertEquals(9, array.get(0).getValue());
    }

    @Test void zeroWidthArrayCannotAllocateUnboundedWireCounts() throws Exception {
        EncoderFactory f = factory(); AtomicInteger calls = new AtomicInteger();
        HLAvariableArray<HLAfixedRecord> array = f.createHLAvariableArray(i -> { calls.incrementAndGet(); return f.createHLAfixedRecord(); });
        int before = calls.get();
        assertThrows(DecoderException.class, () -> array.decode(hex("7fffffff")));
        assertEquals(before, calls.get());
    }

    @Test void overflowingWrapperRangeCannotBypassBounds() throws Exception {
        EncoderFactory f = factory();
        byte[] storage = hex("55000003e8010203");
        assertThrows(EncoderException.class,
                () -> f.createHLAbyte((byte) 9).encode(new ByteWrapper(storage, 1, Integer.MAX_VALUE)));
        AtomicInteger calls = new AtomicInteger();
        HLAvariableArray<HLAbyte> array = f.createHLAvariableArray(i -> { calls.incrementAndGet(); return f.createHLAbyte(); });
        int before = calls.get();
        assertThrows(DecoderException.class, () -> array.decode(new ByteWrapper(storage, 1, Integer.MAX_VALUE)));
        assertEquals(before, calls.get());
        assertArrayEquals(hex("55000003e8010203"), storage);
    }

    @Test void ordinaryTruncatedArrayRejectsCountBeforeCreatingElements() throws Exception {
        EncoderFactory f = factory(); AtomicInteger calls = new AtomicInteger();
        HLAvariableArray<HLAbyte> array = f.createHLAvariableArray(i -> { calls.incrementAndGet(); return f.createHLAbyte(); });
        int before = calls.get();
        assertThrows(DecoderException.class, () -> array.decode(hex("000003e8010203")));
        assertEquals(before, calls.get());
    }

    @SuppressWarnings("unchecked")
    @Test void nestedZeroWidthArraysShareAnAllocationBudget() throws Exception {
        EncoderFactory f = factory(); AtomicInteger calls = new AtomicInteger();
        DataElementFactory<HLAfixedRecord> tokens = i -> {
            if (calls.incrementAndGet() > 10) throw new IllegalStateException("Unexpected bulk allocation");
            return f.createHLAfixedRecord();
        };
        HLAvariableArray<HLAvariableArray<HLAfixedRecord>> nested =
                f.createHLAvariableArray(i -> f.createHLAvariableArray(tokens));
        assertEquals(1, calls.get());
        assertThrows(DecoderException.class, () -> nested.decode(hex("000000020010000000000000")));
        assertEquals(3, calls.get(), "Only the template probes may run before rejecting the nested allocation");
        assertEquals(0, nested.size());
        nested.decode(hex("0000000100000001"));
        assertEquals(1, nested.size()); assertEquals(1, nested.get(0).size());
    }

    @Test void failedDecodeReleasesAllocationBudget() throws Exception {
        EncoderFactory f = factory(); AtomicInteger calls = new AtomicInteger();
        HLAvariableArray<HLAfixedRecord> array = f.createHLAvariableArray(i -> {
            if (calls.incrementAndGet() > 1) throw new IllegalStateException("Factory failure after reservation");
            return f.createHLAfixedRecord();
        });
        assertThrows(DecoderException.class, () -> array.decode(hex("00100000")));
        assertThrows(DecoderException.class, () -> array.decode(hex("00100000")));
        assertEquals(3, calls.get(), "Both independent decodes must reach the factory with a fresh budget");
    }
}
