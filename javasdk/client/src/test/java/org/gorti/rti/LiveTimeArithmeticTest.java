package org.gorti.rti;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.InteractionClassHandle;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.MessageRetractionReturn;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.ParameterHandleValueMap;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.TimeQueryReturn;
import hla.rti1516e.exceptions.InvalidLogicalTime;
import hla.rti1516e.exceptions.MessageCanNoLongerBeRetracted;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URL;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import static org.junit.jupiter.api.Assertions.*;

/** Real-server Float64 bound regressions with two independent RTI sessions. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external GoRTI and -Dgorti.endpoint=grpc://host:port")
class LiveTimeArithmeticTest {
    @ParameterizedTest(name = "positive lookahead remains visible at logical time {0}")
    @ValueSource(doubles = {1.0, 9007199254740992.0})
    @Timeout(60)
    void sendGaltAndRetractionUseTheSameRepresentableBound(double current) throws Exception {
        try (Federation federation = new Federation()) {
            federation.start();
            Participant owner = federation.owner;
            double lookahead = current == 1.0 ? Double.MIN_VALUE : 1.0;
            double bound = Math.nextUp(current);
            owner.rti.enableTimeRegulation(owner.time.makeInterval(lookahead));
            federation.await("time regulation", () -> owner.callbacks.regulated);
            owner.rti.timeAdvanceRequest(owner.time.makeTime(current));
            federation.await("time advance grant", () -> owner.callbacks.grants.contains(current));
            assertEquals(current, value(owner.rti.queryLogicalTime()), 0.0);

            InteractionClassHandle pulse = owner.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            ParameterHandleValueMap parameters = owner.rti.getParameterHandleValueMapFactory().create(1);
            parameters.put(owner.rti.getParameterHandle(pulse, "payload"), new byte[] {0, 0, 0, 1});
            TimeQueryReturn galt = federation.peer.rti.queryGALT();
            assertTrue(galt.timeIsValid);
            assertEquals(bound, value(galt.time), 0.0, "GALT must preserve nonzero lookahead");
            assertThrows(InvalidLogicalTime.class, () -> owner.rti.sendInteraction(pulse, parameters,
                    new byte[0], owner.time.makeTime(current)));

            MessageRetractionReturn sent = owner.rti.sendInteraction(pulse, parameters,
                    new byte[0], owner.time.makeTime(bound));
            assertTrue(sent.retractionHandleIsValid);
            assertNotNull(sent.handle);
            assertThrows(MessageCanNoLongerBeRetracted.class, () -> owner.rti.retract(sent.handle));
        }
    }

    private static double value(LogicalTime time) {
        return assertInstanceOf(HLAfloat64Time.class, time).getValue();
    }

    private static final class Callbacks extends NullFederateAmbassador {
        boolean regulated;
        final List<Double> grants = new ArrayList<>();

        @Override public void timeRegulationEnabled(LogicalTime time) { regulated = true; }
        @Override public void timeAdvanceGrant(LogicalTime time) { grants.add(value(time)); }
    }

    private static final class Participant {
        final RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        final Callbacks callbacks = new Callbacks();
        HLAfloat64TimeFactory time;
        boolean connected;
        boolean joined;

        Participant() throws Exception { }

        void start(String federation, String name) throws Exception {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
            connected = true;
            rti.joinFederationExecution(name, "time-arithmetic", federation);
            joined = true;
            time = assertInstanceOf(HLAfloat64TimeFactory.class, rti.getTimeFactory());
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
        final String name = "gorti-time-arithmetic-" + UUID.randomUUID();
        final Participant owner = new Participant();
        final Participant peer = new Participant();
        boolean created;

        Federation() throws Exception { }

        void start() throws Exception {
            URL fom = LiveTimeArithmeticTest.class.getResource("/exchange-fom.xml");
            assertNotNull(fom);
            owner.rti.connect(owner.callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
            owner.connected = true;
            owner.rti.createFederationExecution(name, new URL[] {fom});
            created = true;
            owner.rti.joinFederationExecution("owner", "time-arithmetic", name);
            owner.joined = true;
            owner.time = assertInstanceOf(HLAfloat64TimeFactory.class, owner.rti.getTimeFactory());
            peer.start(name, "peer");
            owner.rti.publishInteractionClass(owner.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse"));
            peer.rti.subscribeInteractionClass(peer.rti.getInteractionClassHandle("HLAinteractionRoot.Pulse"));
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
                AssertionError error = new AssertionError("Time arithmetic test cleanup failed");
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
