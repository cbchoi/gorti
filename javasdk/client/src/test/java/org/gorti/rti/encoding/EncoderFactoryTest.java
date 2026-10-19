package org.gorti.rti.encoding;

import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.encoding.*;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;
import static org.gorti.rti.encoding.PrimitiveEncodingTest.*;

class EncoderFactoryTest {
    @Test void everyPrimitiveDefaultIsAnIndependentZeroOrEmptyValue() throws Exception {
        EncoderFactory f = factory();
        DataElement[] defaults = {f.createHLAASCIIchar(), f.createHLAASCIIstring(), f.createHLAboolean(),
                f.createHLAbyte(), f.createHLAfloat32BE(), f.createHLAfloat32LE(), f.createHLAfloat64BE(),
                f.createHLAfloat64LE(), f.createHLAinteger16BE(), f.createHLAinteger16LE(), f.createHLAinteger32BE(),
                f.createHLAinteger32LE(), f.createHLAinteger64BE(), f.createHLAinteger64LE(), f.createHLAoctet(),
                f.createHLAoctetPairBE(), f.createHLAoctetPairLE(), f.createHLAopaqueData(), f.createHLAunicodeChar(),
                f.createHLAunicodeString()};
        for (DataElement value : defaults) assertArrayEquals(new byte[value.getEncodedLength()], value.toByteArray());
        HLAinteger32BE first = f.createHLAinteger32BE(); HLAinteger32BE second = f.createHLAinteger32BE();
        first.setValue(42); assertEquals(0, second.getValue());
    }

    @Test void discoveredProviderAndDirectFactoryAreBothUsable() throws Exception {
        EncoderFactory discovered = RtiFactoryFactory.getRtiFactory("gorti").getEncoderFactory();
        EncoderFactory direct = new GortiEncoderFactory();
        assertArrayEquals(hex("00000002"), discovered.createHLAinteger32BE(2).toByteArray());
        assertArrayEquals(hex("02000000"), direct.createHLAinteger32LE(2).toByteArray());
    }
}
