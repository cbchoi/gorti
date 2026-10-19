package org.gorti.rti;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.InteractionClassHandle;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.LogicalTimeFactory;
import hla.rti1516e.MessageRetractionReturn;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.ParameterHandleValueMap;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.TimeQueryReturn;
import hla.rti1516e.exceptions.IllegalTimeArithmetic;
import hla.rti1516e.exceptions.InvalidLogicalTime;
import hla.rti1516e.exceptions.MessageCanNoLongerBeRetracted;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import hla.rti1516e.time.HLAinteger64Interval;
import hla.rti1516e.time.HLAinteger64Time;
import hla.rti1516e.time.HLAinteger64TimeFactory;
import java.net.URL;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import static org.junit.jupiter.api.Assertions.*;

// S1516.1: 8.1.4, 8.9, 8.16-8.17, 8.20-8.21, 12.3-12.4.
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires the assigned external pinned server")
@Timeout(60)
class LiveTimeCompletionBoundaryTest {
    @Test
    void float64PredecessorAndFinalRetainFiniteSendQueryAndGrantBoundaries() throws Exception {
        try (Federation f = new Federation("HLAfloat64Time")) {
            f.start();
            HLAfloat64TimeFactory time = assertInstanceOf(HLAfloat64TimeFactory.class, f.owner.factory);
            assertEquals(0.0, floating(f.owner.rti.queryLogicalTime()), 0.0);
            assertEquals(Double.MAX_VALUE, time.makeFinal().getValue(), 0.0);
            double predecessor = Math.nextDown(Double.MAX_VALUE);
            assertEquals(Double.MAX_VALUE, time.makeTime(predecessor).add(time.makeEpsilon()).getValue(), 0.0);
            assertThrows(IllegalTimeArithmetic.class, () -> time.makeFinal().add(time.makeEpsilon()));

            f.owner.rti.enableTimeRegulation(time.makeEpsilon());
            f.await("regulation", () -> f.owner.callbacks.regulated != null);
            assertEquals(0.0, floating(f.owner.callbacks.regulated), 0.0);
            assertEquals(Double.MIN_VALUE,
                    assertInstanceOf(HLAfloat64Interval.class, f.owner.rti.queryLookahead()).getValue(), 0.0);
            f.advance(time.makeTime(predecessor));
            assertEquals(predecessor, floating(f.owner.rti.queryLogicalTime()), 0.0);
            TimeQueryReturn galt = f.peer.rti.queryGALT();
            assertTrue(galt.timeIsValid);
            assertEquals(Double.MAX_VALUE, floating(galt.time), 0.0);
            assertTrue(assertInstanceOf(HLAfloat64Time.class, galt.time).isFinal());

            assertThrows(InvalidLogicalTime.class, () -> f.send(time.makeTime(predecessor)));
            MessageRetractionReturn sent = f.send(time.makeFinal());
            assertTrue(sent.retractionHandleIsValid);
            assertNotNull(sent.handle);
            assertThrows(MessageCanNoLongerBeRetracted.class, () -> f.owner.rti.retract(sent.handle));
            assertEquals(predecessor, floating(f.owner.rti.queryLogicalTime()), 0.0);

            f.advance(time.makeFinal());
            assertEquals(Double.MAX_VALUE, floating(f.owner.rti.queryLogicalTime()), 0.0);
            assertEquals(2, f.owner.callbacks.grants.size());
            assertTrue(assertInstanceOf(HLAfloat64Time.class, f.owner.callbacks.grants.get(1)).isFinal());
        }
    }

    @ParameterizedTest
    @ValueSource(longs = {9007199254740993L, 9223372036854775806L})
    void integer64ExactSendQueryAndGrantBoundaries(long current) throws Exception {
        try (Federation f = new Federation("HLAinteger64Time")) {
            f.start();
            HLAinteger64TimeFactory time = assertInstanceOf(HLAinteger64TimeFactory.class, f.owner.factory);
            assertEquals(0L, integer(f.owner.rti.queryLogicalTime()));
            assertEquals(Long.MAX_VALUE, time.makeFinal().getValue());
            assertThrows(IllegalTimeArithmetic.class, () -> time.makeFinal().add(time.makeEpsilon()));
            f.owner.rti.enableTimeRegulation(time.makeEpsilon());
            f.await("regulation", () -> f.owner.callbacks.regulated != null);
            assertEquals(0L, integer(f.owner.callbacks.regulated));
            assertEquals(1L, assertInstanceOf(HLAinteger64Interval.class, f.owner.rti.queryLookahead()).getValue());

            f.advance(time.makeTime(current));
            assertEquals(current, integer(f.owner.rti.queryLogicalTime()));
            TimeQueryReturn galt = f.peer.rti.queryGALT();
            assertTrue(galt.timeIsValid);
            assertEquals(current + 1L, integer(galt.time));
            assertThrows(InvalidLogicalTime.class, () -> f.send(time.makeTime(current)));
            MessageRetractionReturn sent = f.send(time.makeTime(current + 1L));
            assertTrue(sent.retractionHandleIsValid);
            assertNotNull(sent.handle);
            assertThrows(MessageCanNoLongerBeRetracted.class, () -> f.owner.rti.retract(sent.handle));
            assertEquals(current, integer(f.owner.rti.queryLogicalTime()));

            f.advance(time.makeFinal());
            assertEquals(Long.MAX_VALUE, integer(f.owner.rti.queryLogicalTime()));
            assertEquals(2, f.owner.callbacks.grants.size());
            assertEquals(current, integer(f.owner.callbacks.grants.get(0)));
            assertTrue(assertInstanceOf(HLAinteger64Time.class, f.owner.callbacks.grants.get(1)).isFinal());
        }
    }

    private static double floating(LogicalTime time) {
        return assertInstanceOf(HLAfloat64Time.class, time).getValue();
    }

    private static long integer(LogicalTime time) {
        return assertInstanceOf(HLAinteger64Time.class, time).getValue();
    }

    private static final class Callbacks extends NullFederateAmbassador {
        LogicalTime regulated;
        final List<LogicalTime> grants = new ArrayList<>();
        @Override public void timeRegulationEnabled(LogicalTime time) { regulated = time; }
        @Override public void timeAdvanceGrant(LogicalTime time) { grants.add(time); }
    }

    private static final class Participant {
        final RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        final Callbacks callbacks = new Callbacks();
        LogicalTimeFactory factory;
        boolean connected;
        boolean joined;

        Participant() throws Exception { }

        void connect() throws Exception {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
            connected = true;
        }

        void join(String federation, String name) throws Exception {
            rti.joinFederationExecution(name, "time-completion-boundary", federation);
            joined = true;
            factory = rti.getTimeFactory();
        }

        void resign() throws Exception {
            if (joined) {
                rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                joined = false;
            }
        }

        void disconnect() throws Exception {
            if (connected) { rti.disconnect(); connected = false; }
        }
    }

    private static final class Federation implements AutoCloseable {
        final String name = "gorti-time-boundary-" + UUID.randomUUID();
        final String representation;
        final Participant owner = new Participant();
        final Participant peer = new Participant();
        InteractionClassHandle pulse;
        ParameterHandleValueMap parameters;
        boolean created;

        Federation(String representation) throws Exception { this.representation = representation; }

        void start() throws Exception {
            URL fom = LiveTimeCompletionBoundaryTest.class.getResource("/exchange-fom.xml");
            assertNotNull(fom);
            owner.connect();
            owner.rti.createFederationExecution(name, new URL[] {fom}, representation);
            created = true;
            owner.join(name, "owner");
            peer.connect();
            peer.join(name, "peer");
            assertEquals(representation, owner.factory.getName());
            assertEquals(representation, peer.factory.getName());
            pulse = owner.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            owner.rti.publishInteractionClass(pulse);
            peer.rti.subscribeInteractionClass(peer.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse"));
            parameters = owner.rti.getParameterHandleValueMapFactory().create(1);
            parameters.put(owner.rti.getParameterHandle(pulse, "payload"), new byte[] {0, 0, 0, 1});
        }

        MessageRetractionReturn send(LogicalTime time) throws Exception {
            return owner.rti.sendInteraction(pulse, parameters, new byte[] {1, 2}, time);
        }

        void advance(LogicalTime target) throws Exception {
            int count = owner.callbacks.grants.size();
            owner.rti.timeAdvanceRequestAvailable(target);
            await("available grant", () -> owner.callbacks.grants.size() > count);
            assertEquals(count + 1, owner.callbacks.grants.size());
            assertEquals(target, owner.callbacks.grants.get(count));
        }

        void await(String description, BooleanSupplier complete) throws Exception {
            long deadline = System.nanoTime() + 10_000_000_000L;
            while (!complete.getAsBoolean() && System.nanoTime() < deadline) {
                owner.rti.evokeMultipleCallbacks(0.001, 0.02);
                peer.rti.evokeMultipleCallbacks(0.001, 0.02);
            }
            assertTrue(complete.getAsBoolean(), "Timed out awaiting " + description);
        }

        @Override public void close() {
            List<Throwable> failures = new ArrayList<>();
            cleanup(peer::resign, failures);
            cleanup(owner::resign, failures);
            if (created) cleanup(() -> owner.rti.destroyFederationExecution(name), failures);
            cleanup(peer::disconnect, failures);
            cleanup(owner::disconnect, failures);
            if (!failures.isEmpty()) {
                AssertionError error = new AssertionError("Boundary fixture cleanup failed");
                failures.forEach(error::addSuppressed);
                throw error;
            }
        }
    }

    @FunctionalInterface private interface Cleanup { void run() throws Exception; }
    private static void cleanup(Cleanup action, List<Throwable> failures) {
        try { action.run(); } catch (Throwable failure) { failures.add(failure); }
    }
}
