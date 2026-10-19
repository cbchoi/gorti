package org.gorti.rti.types;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleFactory;
import hla.rti1516e.DimensionHandle;
import hla.rti1516e.DimensionHandleFactory;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.FederateHandleFactory;
import hla.rti1516e.InteractionClassHandle;
import hla.rti1516e.InteractionClassHandleFactory;
import hla.rti1516e.MessageRetractionHandle;
import hla.rti1516e.ObjectClassHandle;
import hla.rti1516e.ObjectClassHandleFactory;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.ObjectInstanceHandleFactory;
import hla.rti1516e.ParameterHandle;
import hla.rti1516e.ParameterHandleFactory;
import hla.rti1516e.RegionHandle;
import hla.rti1516e.TransportationTypeHandle;
import hla.rti1516e.TransportationTypeHandleFactory;
import hla.rti1516e.exceptions.CouldNotDecode;
import java.io.Serializable;
import java.nio.ByteBuffer;
import java.util.Objects;
import java.util.UUID;

/** Immutable, type-isolated handles for gorti's unsigned wire identifiers. */
public final class Handles {
    private Handles() {}

    public static FederateHandle federate(long value) { return new Federate(value); }
    public static ObjectClassHandle objectClass(long value) { return new ObjectClass(value); }
    public static ObjectInstanceHandle objectInstance(long value) { return new ObjectInstance(value); }
    public static InteractionClassHandle interactionClass(long value) { return new InteractionClass(value); }
    public static AttributeHandle attribute(long value) { return new Attribute(value); }
    public static ParameterHandle parameter(long value) { return new Parameter(value); }
    public static DimensionHandle dimension(long value) { return new Dimension(value); }
    public static RegionHandle region(long value) { return new Region(value); }
    public static MessageRetractionHandle retraction(long value) { return new Retraction(value); }

    /** Creates a retraction identity scoped to a federation generation and producing federate. */
    public static MessageRetractionHandle retraction(long generation, long producer, long value) {
        return retraction("", generation, producer, value);
    }

    public static MessageRetractionHandle retraction(String federation, long generation, long producer, long value) {
        return new ScopedRetraction(Objects.requireNonNull(federation, "federation"), generation, producer, value);
    }

    public static String retractionFederation(MessageRetractionHandle handle) {
        if (handle instanceof ScopedRetraction) return ((ScopedRetraction) handle).federation;
        if (handle instanceof Retraction) return "";
        throw new IllegalArgumentException("Expected a gorti retraction handle");
    }

    /** Identity for a native-client delivery whose sender did not request retraction support. */
    public static MessageRetractionHandle nonRetractableMessage() {
        return new NonRetractableMessage();
    }

    /** Returns the generation bits; legacy unscoped handles report zero. */
    public static long retractionGeneration(MessageRetractionHandle handle) {
        if (handle instanceof ScopedRetraction) return ((ScopedRetraction) handle).generation;
        if (handle instanceof Retraction) return 0;
        throw new IllegalArgumentException("Expected a gorti retraction handle");
    }

    /** Returns the producer bits; legacy unscoped handles report zero. */
    public static long retractionProducer(MessageRetractionHandle handle) {
        if (handle instanceof ScopedRetraction) return ((ScopedRetraction) handle).producer;
        if (handle instanceof Retraction) return 0;
        throw new IllegalArgumentException("Expected a gorti retraction handle");
    }

    public static TransportationTypeHandle transportation(int value) {
        switch (value) {
            case 1: return Transportation.RELIABLE;
            case 2: return Transportation.BEST_EFFORT;
            default: throw new IllegalArgumentException("Unsupported transportation identifier: " + value);
        }
    }

    /** Returns the wire bits, not a lossy conversion from a foreign RTI handle. */
    public static long value(Object typedHandle) {
        if (typedHandle instanceof ScopedRetraction) return ((ScopedRetraction) typedHandle).value;
        if (!(typedHandle instanceof NumericHandle)) {
            throw new IllegalArgumentException("Expected a gorti handle");
        }
        return ((NumericHandle) typedHandle).value;
    }

    public static FederateHandleFactory federateFactory() { return FederateFactory.INSTANCE; }
    public static ObjectClassHandleFactory objectClassFactory() { return ObjectClassFactory.INSTANCE; }
    public static ObjectInstanceHandleFactory objectInstanceFactory() { return ObjectInstanceFactory.INSTANCE; }
    public static InteractionClassHandleFactory interactionClassFactory() { return InteractionClassFactory.INSTANCE; }
    public static AttributeHandleFactory attributeFactory() { return AttributeFactory.INSTANCE; }
    public static ParameterHandleFactory parameterFactory() { return ParameterFactory.INSTANCE; }
    public static DimensionHandleFactory dimensionFactory() { return DimensionFactory.INSTANCE; }
    public static TransportationTypeHandleFactory transportationFactory() { return TransportationFactory.INSTANCE; }

    private abstract static class NumericHandle implements Serializable {
        private static final long serialVersionUID = 1L;
        private final long value;

        NumericHandle(long value) { this.value = value; }

        @Override
        public final boolean equals(Object other) {
            return other != null && getClass() == other.getClass()
                    && value == ((NumericHandle) other).value;
        }

        @Override
        public final int hashCode() { return Long.hashCode(value); }

        @Override
        public final String toString() {
            return getClass().getSimpleName() + "(" + Long.toUnsignedString(value) + ")";
        }
    }

    private abstract static class EncodedHandle extends NumericHandle {
        private static final long serialVersionUID = 1L;

        EncodedHandle(long value) { super(value); }

        public int encodedLength() { return Long.BYTES; }

        public void encode(byte[] buffer, int offset) {
            Objects.requireNonNull(buffer, "buffer");
            Objects.checkFromIndexSize(offset, encodedLength(), buffer.length);
            ByteBuffer.wrap(buffer, offset, encodedLength()).putLong(Handles.value(this));
        }
    }

    private static final class Federate extends EncodedHandle implements FederateHandle {
        private static final long serialVersionUID = 1L;
        Federate(long value) { super(value); }
    }

    private static final class ObjectClass extends EncodedHandle implements ObjectClassHandle {
        private static final long serialVersionUID = 1L;
        ObjectClass(long value) { super(value); }
    }

    private static final class ObjectInstance extends EncodedHandle implements ObjectInstanceHandle {
        private static final long serialVersionUID = 1L;
        ObjectInstance(long value) { super(value); }
    }

    private static final class InteractionClass extends EncodedHandle implements InteractionClassHandle {
        private static final long serialVersionUID = 1L;
        InteractionClass(long value) { super(value); }
    }

    private static final class Attribute extends EncodedHandle implements AttributeHandle {
        private static final long serialVersionUID = 1L;
        Attribute(long value) { super(value); }
    }

    private static final class Parameter extends EncodedHandle implements ParameterHandle {
        private static final long serialVersionUID = 1L;
        Parameter(long value) { super(value); }
    }

    private static final class Dimension extends EncodedHandle implements DimensionHandle {
        private static final long serialVersionUID = 1L;
        Dimension(long value) { super(value); }
    }

    private static final class Region extends NumericHandle implements RegionHandle {
        private static final long serialVersionUID = 1L;
        Region(long value) { super(value); }
    }

    private static final class Retraction extends NumericHandle implements MessageRetractionHandle {
        private static final long serialVersionUID = 1L;
        Retraction(long value) { super(value); }
    }

    // A separate class keeps legacy single-number identities disjoint without changing their serialization.
    private static final class ScopedRetraction implements MessageRetractionHandle {
        private static final long serialVersionUID = 1L;
        private final String federation;
        private final long generation;
        private final long producer;
        private final long value;

        ScopedRetraction(String federation, long generation, long producer, long value) {
            this.federation = federation;
            this.generation = generation;
            this.producer = producer;
            this.value = value;
        }

        @Override
        public boolean equals(Object other) {
            if (!(other instanceof ScopedRetraction)) return false;
            ScopedRetraction handle = (ScopedRetraction) other;
            return Objects.equals(federation, handle.federation) && generation == handle.generation
                    && producer == handle.producer && value == handle.value;
        }

        @Override
        public int hashCode() {
            int hash = 31 * Objects.hashCode(federation) + Long.hashCode(generation);
            hash = 31 * hash + Long.hashCode(producer);
            return 31 * hash + Long.hashCode(value);
        }

        @Override
        public String toString() {
            return "ScopedRetraction(" + federation + "," + Long.toUnsignedString(generation) + ","
                    + Long.toUnsignedString(producer) + "," + Long.toUnsignedString(value) + ")";
        }
    }

    private static final class NonRetractableMessage implements MessageRetractionHandle {
        private static final long serialVersionUID = 1L;
        private final UUID identity = UUID.randomUUID();
        @Override public boolean equals(Object other) {
            return other instanceof NonRetractableMessage && identity.equals(((NonRetractableMessage) other).identity);
        }
        @Override public int hashCode() { return identity.hashCode(); }
        @Override public String toString() { return "NonRetractableMessage(" + identity + ")"; }
    }

    private static final class Transportation extends EncodedHandle implements TransportationTypeHandle {
        private static final long serialVersionUID = 1L;
        private static final Transportation RELIABLE = new Transportation(1);
        private static final Transportation BEST_EFFORT = new Transportation(2);

        private Transportation(int value) { super(value); }

        @Override
        public int encodedLength() { return Integer.BYTES; }

        @Override
        public void encode(byte[] buffer, int offset) {
            Objects.requireNonNull(buffer, "buffer");
            Objects.checkFromIndexSize(offset, Integer.BYTES, buffer.length);
            ByteBuffer.wrap(buffer, offset, Integer.BYTES).putInt((int) Handles.value(this));
        }
    }

    private static long decodeLong(byte[] buffer, int offset) throws CouldNotDecode {
        checkDecodeBounds(buffer, offset, Long.BYTES);
        return ByteBuffer.wrap(buffer, offset, Long.BYTES).getLong();
    }

    private static void checkDecodeBounds(byte[] buffer, int offset, int length) throws CouldNotDecode {
        if (buffer == null || offset < 0 || offset > buffer.length - length) {
            throw new CouldNotDecode("Handle requires " + length + " bytes at offset " + offset);
        }
    }

    private enum FederateFactory implements FederateHandleFactory {
        INSTANCE;
        public FederateHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return federate(decodeLong(buffer, offset));
        }
    }

    private enum ObjectClassFactory implements ObjectClassHandleFactory {
        INSTANCE;
        public ObjectClassHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return objectClass(decodeLong(buffer, offset));
        }
    }

    private enum ObjectInstanceFactory implements ObjectInstanceHandleFactory {
        INSTANCE;
        public ObjectInstanceHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return objectInstance(decodeLong(buffer, offset));
        }
    }

    private enum InteractionClassFactory implements InteractionClassHandleFactory {
        INSTANCE;
        public InteractionClassHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return interactionClass(decodeLong(buffer, offset));
        }
    }

    private enum AttributeFactory implements AttributeHandleFactory {
        INSTANCE;
        public AttributeHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return attribute(decodeLong(buffer, offset));
        }
    }

    private enum ParameterFactory implements ParameterHandleFactory {
        INSTANCE;
        public ParameterHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return parameter(decodeLong(buffer, offset));
        }
    }

    private enum DimensionFactory implements DimensionHandleFactory {
        INSTANCE;
        public DimensionHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            return dimension(decodeLong(buffer, offset));
        }
    }

    private enum TransportationFactory implements TransportationTypeHandleFactory {
        INSTANCE;

        public TransportationTypeHandle decode(byte[] buffer, int offset) throws CouldNotDecode {
            checkDecodeBounds(buffer, offset, Integer.BYTES);
            int value = ByteBuffer.wrap(buffer, offset, Integer.BYTES).getInt();
            try {
                return transportation(value);
            } catch (IllegalArgumentException invalid) {
                throw new CouldNotDecode(invalid.getMessage(), invalid);
            }
        }

        public TransportationTypeHandle getHLAdefaultReliable() { return Transportation.RELIABLE; }
        public TransportationTypeHandle getHLAdefaultBestEffort() { return Transportation.BEST_EFFORT; }
    }
}
