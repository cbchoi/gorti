package org.gorti.rti.encoding;

import hla.rti1516e.encoding.ByteWrapper;
import hla.rti1516e.encoding.DataElement;
import hla.rti1516e.encoding.DecoderException;
import hla.rti1516e.encoding.EncoderException;

abstract class AbstractElement implements DataElement {
    abstract void encodeValue(ByteWrapper wrapper);
    abstract void decodeValue(ByteWrapper wrapper) throws DecoderException;
    abstract int minimumLength();

    @Override public final void encode(ByteWrapper wrapper) {
        try {
            EncodingSupport.align(wrapper, getOctetBoundary(), true);
            EncodingSupport.requireRemaining(wrapper, getEncodedLength());
            // Use an explicitly bounded slice: ByteWrapper.slice() can escape a parent's limit.
            ByteWrapper value = wrapper.slice(wrapper.remaining());
            encodeValue(value);
            wrapper.advance(value.getPos() - wrapper.getPos());
        } catch (EncoderException exception) {
            throw exception;
        } catch (RuntimeException exception) {
            throw new EncoderException("Cannot encode " + getClass().getSimpleName(), exception);
        }
    }

    @Override public final void decode(ByteWrapper wrapper) throws DecoderException {
        DecodeBudget budget = DecodeBudget.enter();
        try {
            EncodingSupport.align(wrapper, getOctetBoundary(), false);
            ByteWrapper value = wrapper.slice(wrapper.remaining());
            decodeValue(value);
            wrapper.advance(value.getPos() - wrapper.getPos());
        } catch (RuntimeException exception) {
            throw new DecoderException("Cannot decode " + getClass().getSimpleName(), exception);
        } finally {
            budget.close();
        }
    }

    @Override public final byte[] toByteArray() {
        try {
            ByteWrapper wrapper = new ByteWrapper(getEncodedLength());
            encode(wrapper);
            return wrapper.array();
        } catch (EncoderException exception) {
            throw exception;
        } catch (RuntimeException exception) {
            throw new EncoderException("Cannot allocate encoding for " + getClass().getSimpleName(), exception);
        }
    }

    @Override public final void decode(byte[] bytes) throws DecoderException {
        if (bytes == null) throw new DecoderException("Encoded bytes must not be null");
        decode(new ByteWrapper(bytes));
    }
}
