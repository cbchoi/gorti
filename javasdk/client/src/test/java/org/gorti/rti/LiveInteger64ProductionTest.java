package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.InvalidLogicalTime;
import hla.rti1516e.exceptions.InvalidLookahead;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.time.HLAinteger64TimeFactory;
import java.net.URL;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.EnumSource;

import static org.junit.jupiter.api.Assertions.*;

/** Independent standard-API acceptance against a running production server. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires a production server with Integer64 enabled")
@Timeout(60)
class LiveInteger64ProductionTest {
    private static final long FIRST = (1L << 53) + 1;
    private static final long SECOND = FIRST + 1;

    enum Advance {
        TAR, TARA, NMR, NMRA, FQR;

        void request(RTIambassador rti, LogicalTime target) throws Exception {
            switch (this) {
                case TAR: rti.timeAdvanceRequest(target); break;
                case TARA: rti.timeAdvanceRequestAvailable(target); break;
                case NMR: rti.nextMessageRequest(target); break;
                case NMRA: rti.nextMessageRequestAvailable(target); break;
                case FQR: rti.flushQueueRequest(target); break;
                default: throw new AssertionError(this);
            }
        }
    }

    @ParameterizedTest
    @EnumSource(Advance.class)
    void everyAdvancePreservesAdjacentLargeIntegersAndFinalTime(Advance advance) throws Exception {
        try (Federation f = new Federation()) {
            Participant p = f.owner;
            assertEquals("HLAinteger64Time", p.time.getName());
            assertEquals(0L, exact(p.rti.queryLogicalTime()));
            for (long target : new long[]{FIRST, SECOND, Long.MAX_VALUE}) {
                int previous = p.callbacks.grants.size();
                advance.request(p.rti, p.time.makeTime(target));
                f.await(() -> p.callbacks.grants.size() == previous + 1);
                assertEquals(target, p.callbacks.grants.get(previous).longValue());
                assertEquals(target, exact(p.rti.queryLogicalTime()));
            }
            assertEquals(Arrays.asList(FIRST, SECOND, Long.MAX_VALUE), p.callbacks.grants);
            assertTrue(p.time.makeFinal().isFinal());
            assertEquals(Long.MAX_VALUE, exact(p.time.makeFinal()));
        }
    }

    @Test
    void mixedTimeAndIntervalRepresentationsAreRejectedWithoutAdvancing() throws Exception {
        try (Federation f = new Federation()) {
            Participant p = f.owner;
            HLAfloat64TimeFactory floats = LogicalTimeFactoryFactory.getLogicalTimeFactory(HLAfloat64TimeFactory.class);
            assertNotNull(floats);
            for (Advance advance : Advance.values()) {
                assertThrows(InvalidLogicalTime.class,
                        () -> advance.request(p.rti, floats.makeTime(4.0)), advance.name());
                assertEquals(0L, exact(p.rti.queryLogicalTime()));
            }
            assertThrows(InvalidLookahead.class, () -> p.rti.enableTimeRegulation(floats.makeInterval(1.0)));
            p.rti.evokeMultipleCallbacks(0, 0);
            assertTrue(p.callbacks.grants.isEmpty());
            assertFalse(p.callbacks.regulating);
        }
    }

    @Test
    void adjacentTimestampMessagesAreDeliveredExactlyInOrderBeforeGrant() throws Exception {
        try (Federation f = new Federation()) {
            Participant sender = f.owner;
            Participant receiver = f.add("receiver");
            InteractionClassHandle sendClass = sender.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            InteractionClassHandle receiveClass = receiver.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            ParameterHandle payload = sender.rti.getParameterHandle(sendClass, "payload");
            receiver.callbacks.payload = receiver.rti.getParameterHandle(receiveClass, "payload");
            sender.rti.publishInteractionClass(sendClass);
            receiver.rti.subscribeInteractionClass(receiveClass);
            sender.rti.enableTimeRegulation(sender.time.makeInterval(1));
            receiver.rti.enableTimeConstrained();
            f.await(() -> sender.callbacks.regulating && receiver.callbacks.constrained);

            send(sender, sendClass, payload, SECOND, 2);
            send(sender, sendClass, payload, FIRST, 1);
            sender.rti.timeAdvanceRequest(sender.time.makeTime(SECOND));
            f.await(() -> sender.callbacks.grants.contains(SECOND));
            receiver.rti.timeAdvanceRequest(receiver.time.makeTime(SECOND));
            f.await(() -> receiver.callbacks.grants.contains(SECOND));

            assertEquals(Arrays.asList(FIRST, SECOND), receiver.callbacks.received);
            assertEquals(Arrays.asList(1, 2), receiver.callbacks.values);
            assertEquals(Arrays.asList("tick:" + FIRST, "tick:" + SECOND), receiver.callbacks.tags);
            assertEquals(Arrays.asList("receive:" + FIRST, "receive:" + SECOND, "grant:" + SECOND),
                    receiver.callbacks.trace);
            assertEquals(SECOND, exact(receiver.rti.queryLogicalTime()));
        }
    }

    private static void send(Participant p, InteractionClassHandle cls, ParameterHandle payload,
                             long timestamp, int value) throws Exception {
        ParameterHandleValueMap parameters = p.rti.getParameterHandleValueMapFactory().create(1);
        parameters.put(payload, ByteBuffer.allocate(4).putInt(value).array());
        MessageRetractionReturn result = p.rti.sendInteraction(cls, parameters,
                ("tick:" + timestamp).getBytes(StandardCharsets.US_ASCII), p.time.makeTime(timestamp));
        assertNotNull(result);
    }

    private static long exact(LogicalTime value) {
        return assertInstanceOf(HLAinteger64Time.class, value).getValue();
    }

    private static final class Observations extends NullFederateAmbassador {
        final List<Long> grants = new ArrayList<>();
        final List<Long> received = new ArrayList<>();
        final List<Integer> values = new ArrayList<>();
        final List<String> tags = new ArrayList<>();
        final List<String> trace = new ArrayList<>();
        ParameterHandle payload;
        boolean regulating;
        boolean constrained;

        @Override public void timeAdvanceGrant(LogicalTime time) {
            long value = exact(time);
            grants.add(value);
            trace.add("grant:" + value);
        }

        @Override public void timeRegulationEnabled(LogicalTime time) {
            assertEquals(0L, exact(time));
            regulating = true;
        }

        @Override public void timeConstrainedEnabled(LogicalTime time) {
            assertEquals(0L, exact(time));
            constrained = true;
        }

        @Override public void receiveInteraction(InteractionClassHandle cls, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
            fail("Timestamped interaction was delivered without logical time");
        }

        @Override public void receiveInteraction(InteractionClassHandle cls, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType receivedOrder, SupplementalReceiveInfo info) {
            record(parameters, tag, sent, time, receivedOrder);
        }

        @Override public void receiveInteraction(InteractionClassHandle cls, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType receivedOrder, MessageRetractionHandle handle, SupplementalReceiveInfo info) {
            record(parameters, tag, sent, time, receivedOrder);
        }

        private void record(ParameterHandleValueMap parameters, byte[] tag, OrderType sent,
                            LogicalTime time, OrderType receivedOrder) {
            assertEquals(OrderType.TIMESTAMP, sent);
            assertEquals(OrderType.TIMESTAMP, receivedOrder);
            long value = exact(time);
            received.add(value);
            byte[] encoded = parameters.get(payload);
            assertNotNull(encoded);
            assertEquals(4, encoded.length);
            values.add(ByteBuffer.wrap(encoded).getInt());
            tags.add(new String(tag, StandardCharsets.US_ASCII));
            trace.add("receive:" + value);
        }
    }

    private static final class Participant {
        final RTIambassador rti;
        final Observations callbacks = new Observations();
        HLAinteger64TimeFactory time;
        boolean connected;
        boolean joined;

        Participant() throws Exception {
            rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        }
    }

    private static final class Federation implements AutoCloseable {
        final String name = "java-integer-live-" + UUID.randomUUID();
        final List<Participant> participants = new ArrayList<>();
        final Participant owner;
        boolean created;

        Federation() throws Exception {
            owner = new Participant();
            participants.add(owner);
            try {
                connect(owner);
                URL fom = LiveInteger64ProductionTest.class.getResource("/exchange-fom.xml");
                assertNotNull(fom);
                owner.rti.createFederationExecution(name, new URL[]{fom}, "HLAinteger64Time");
                created = true;
                join(owner, "sender");
            } catch (Exception | AssertionError failure) {
                try { close(); } catch (Exception cleanup) { failure.addSuppressed(cleanup); }
                throw failure;
            }
        }

        Participant add(String federateName) throws Exception {
            Participant p = new Participant();
            participants.add(p);
            connect(p);
            join(p, federateName);
            return p;
        }

        private void connect(Participant p) throws Exception {
            p.rti.connect(p.callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
            p.connected = true;
        }

        private void join(Participant p, String federateName) throws Exception {
            p.rti.joinFederationExecution(federateName, "java-integer-acceptance", name);
            p.joined = true;
            p.time = assertInstanceOf(HLAinteger64TimeFactory.class, p.rti.getTimeFactory());
        }

        void await(BooleanSupplier condition) throws Exception {
            long deadline = System.nanoTime() + 15_000_000_000L;
            while (!condition.getAsBoolean() && System.nanoTime() < deadline) {
                for (Participant p : participants) p.rti.evokeMultipleCallbacks(0.001, 0.02);
            }
            assertTrue(condition.getAsBoolean(), "Timed out awaiting actual server callbacks");
        }

        @Override public void close() throws Exception {
            Exception failure = null;
            for (int i = participants.size() - 1; i >= 0; i--) {
                Participant p = participants.get(i);
                if (p.joined) {
                    try { p.rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); }
                    catch (Exception e) { if (failure == null) failure = e; else failure.addSuppressed(e); }
                }
            }
            if (created) {
                try { owner.rti.destroyFederationExecution(name); }
                catch (Exception e) { if (failure == null) failure = e; else failure.addSuppressed(e); }
            }
            for (Participant p : participants) {
                if (p.connected) {
                    try { p.rti.disconnect(); }
                    catch (Exception e) { if (failure == null) failure = e; else failure.addSuppressed(e); }
                }
            }
            if (failure != null) throw failure;
        }
    }
}
