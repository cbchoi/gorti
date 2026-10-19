package org.gorti.rti;

import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.TimeQueryReturn;
import hla.rti1516e.exceptions.AsynchronousDeliveryAlreadyDisabled;
import hla.rti1516e.exceptions.AsynchronousDeliveryAlreadyEnabled;
import hla.rti1516e.exceptions.FederateNameAlreadyInUse;
import hla.rti1516e.exceptions.FederationExecutionAlreadyExists;
import hla.rti1516e.exceptions.TimeConstrainedAlreadyEnabled;
import hla.rti1516e.exceptions.TimeRegulationAlreadyEnabled;
import hla.rti1516e.time.HLAfloat64Interval;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URI;
import java.net.URL;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

import static org.junit.jupiter.api.Assertions.*;

/** Two independent RTI sessions in one JVM; the server is started externally. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Live test requires an external RTI and -Dgorti.endpoint/-Dgorti.fom")
class LiveFederationTest {
    private static final Duration CALLBACK_TIMEOUT = Duration.ofSeconds(10);
    private static final Duration BLOCKED_OBSERVATION = Duration.ofMillis(250);
    private static final double EVOKE_MAX_SECONDS = 0.05;

    @Test
    @Timeout(120)
    void twoSessionsObserveActualEnableCallbacksAndPeerDependentGrants() throws Throwable {
        String endpoint = System.getProperty("gorti.endpoint");
        URI address = URI.create(endpoint);
        assertEquals("grpc", address.getScheme(), "gorti.endpoint must be grpc://host:port");
        assertNotNull(address.getHost(), "gorti.endpoint must include a host");
        assertTrue(address.getPort() > 0, "gorti.endpoint must include a port");
        String fomProperty = System.getProperty("gorti.fom");
        assertNotNull(fomProperty, "Set gorti.fom to an existing local FOM file");
        Path fomPath = Path.of(fomProperty).toAbsolutePath().normalize();
        assertTrue(Files.isRegularFile(fomPath) && Files.isReadable(fomPath),
                "FOM must be a readable file: " + fomPath);
        URL[] modules = {fomPath.toUri().toURL()};
        String federation = "gorti-java-live-" + UUID.randomUUID();
        Participant a = new Participant("A");
        Participant b = new Participant("B");
        Participant duplicateName = new Participant("A");
        boolean created = false;
        Throwable originalFailure = null;
        try {
            a.connect(endpoint);
            b.connect(endpoint);
            a.rti.createFederationExecution(federation, modules);
            created = true;
            assertThrows(FederationExecutionAlreadyExists.class,
                    () -> b.rti.createFederationExecution(federation, modules));
            FederateHandle aHandle = a.join(federation);
            FederateHandle bHandle = b.join(federation);
            assertNotEquals(aHandle, bHandle, "The ambassadors must own different federate sessions");
            assertHandleRoundTrip(a, aHandle);
            assertHandleRoundTrip(b, bHandle);
            duplicateName.connect(endpoint);
            assertThrows(FederateNameAlreadyInUse.class, () -> duplicateName.join(federation));
            assertEquals(HLAfloat64TimeFactory.NAME, a.time.getName());
            assertEquals(HLAfloat64TimeFactory.NAME, b.time.getName());
            assertLogicalTime(a, 0.0);
            assertLogicalTime(b, 0.0);

            a.rti.enableTimeRegulation(a.time.makeInterval(0.5));
            b.rti.enableTimeRegulation(b.time.makeInterval(0.5));
            assertTrue(a.callbacks.regulation.isEmpty(), "A enable RPC must not invoke a callback");
            assertTrue(b.callbacks.regulation.isEmpty(), "B enable RPC must not invoke a callback");
            await("regulation enabled", () -> !a.callbacks.regulation.isEmpty()
                    && !b.callbacks.regulation.isEmpty(), a, b);
            assertEquals(List.of(0.0), a.callbacks.regulation);
            assertEquals(List.of(0.0), b.callbacks.regulation);

            a.rti.enableTimeConstrained();
            b.rti.enableTimeConstrained();
            assertTrue(a.callbacks.constrained.isEmpty(), "A must wait for evocation");
            assertTrue(b.callbacks.constrained.isEmpty(), "B must wait for evocation");
            await("constrained enabled", () -> !a.callbacks.constrained.isEmpty()
                    && !b.callbacks.constrained.isEmpty(), a, b);
            assertEquals(List.of(0.0), a.callbacks.constrained);
            assertEquals(List.of(0.0), b.callbacks.constrained);
            for (Participant participant : List.of(a, b)) {
                assertThrows(TimeRegulationAlreadyEnabled.class,
                        () -> participant.rti.enableTimeRegulation(participant.time.makeInterval(0.5)));
                assertThrows(TimeConstrainedAlreadyEnabled.class, participant.rti::enableTimeConstrained);
                participant.rti.enableAsynchronousDelivery();
                assertThrows(AsynchronousDeliveryAlreadyEnabled.class,
                        participant.rti::enableAsynchronousDelivery);
                participant.rti.disableAsynchronousDelivery();
                assertThrows(AsynchronousDeliveryAlreadyDisabled.class,
                        participant.rti::disableAsynchronousDelivery);
                assertQueries(participant, 0.0, 0.5, 0.5);
            }

            a.rti.timeAdvanceRequest(a.time.makeTime(2.0));
            assertTrue(a.callbacks.grants.isEmpty(), "TAR return is not an evoked TAG");
            observeBlocked(a, b);
            assertLogicalTime(a, 0.0);
            assertLogicalTime(b, 0.0);
            b.rti.timeAdvanceRequest(b.time.makeTime(2.0));
            assertTrue(a.callbacks.grants.isEmpty());
            assertTrue(b.callbacks.grants.isEmpty());
            awaitGrants(2.0, 1, a, b);
            assertQueries(a, 2.0, 0.5, 2.5);
            assertQueries(b, 2.0, 0.5, 2.5);

            a.rti.modifyLookahead(a.time.makeInterval(1.0));
            b.rti.modifyLookahead(b.time.makeInterval(1.0));
            assertQueries(a, 2.0, 1.0, 3.0);
            assertQueries(b, 2.0, 1.0, 3.0);

            // One-unit steps keep FQR at the requested horizon without relying on a peer wait.
            advanceBoth("TARA", 3.0, 2, a, b,
                    RTIambassador::timeAdvanceRequestAvailable);
            advanceBoth("NMR", 4.0, 3, a, b, RTIambassador::nextMessageRequest);
            advanceBoth("NMRA", 5.0, 4, a, b, RTIambassador::nextMessageRequestAvailable);
            advanceBoth("FQR", 6.0, 5, a, b, RTIambassador::flushQueueRequest);
            for (Participant participant : List.of(a, b)) {
                assertQueries(participant, 6.0, 1.0, 7.0);
                assertEquals(List.of(2.0, 3.0, 4.0, 5.0, 6.0), participant.callbacks.grants);
                assertEquals(List.of(0.0), participant.callbacks.regulation,
                        "Duplicate requests must not produce another enabled callback");
                assertEquals(List.of(0.0), participant.callbacks.constrained);
                participant.assertHealthy();
            }

            a.rti.disableCallbacks();
            a.rti.timeAdvanceRequest(a.time.makeTime(7.0));
            b.rti.timeAdvanceRequest(b.time.makeTime(7.0));
            await("B TAG(7) while A callbacks are disabled",
                    () -> b.callbacks.grants.size() >= 6, a, b);
            assertEquals(6, b.callbacks.grants.size());
            assertEquals(7.0, b.callbacks.grants.get(5), 0.0);
            long disabledDeadline = System.nanoTime() + BLOCKED_OBSERVATION.toNanos();
            do {
                evoke(a);
                assertEquals(5, a.callbacks.grants.size(), "Disabled callbacks must retain A's TAG");
            } while (System.nanoTime() < disabledDeadline);
            a.rti.enableCallbacks();
            assertEquals(5, a.callbacks.grants.size(), "Enabling callbacks must still await evoke");
            awaitGrants(7.0, 6, a, b);
            for (Participant participant : List.of(a, b)) {
                assertQueries(participant, 7.0, 1.0, 8.0);
                assertEquals(List.of(2.0, 3.0, 4.0, 5.0, 6.0, 7.0), participant.callbacks.grants);
            }
        } catch (Throwable failure) {
            originalFailure = failure;
            throw failure;
        } finally {
            List<Throwable> cleanupFailures = new ArrayList<>();
            cleanup(duplicateName::resign, cleanupFailures);
            cleanup(b::resign, cleanupFailures);
            cleanup(a::resign, cleanupFailures);
            if (created) {
                cleanup(() -> a.rti.destroyFederationExecution(federation), cleanupFailures);
            }
            cleanup(duplicateName::disconnect, cleanupFailures);
            cleanup(b::disconnect, cleanupFailures);
            cleanup(a::disconnect, cleanupFailures);
            if (!cleanupFailures.isEmpty()) {
                AssertionError cleanupFailure = new AssertionError("Live federation cleanup failed");
                cleanupFailures.forEach(cleanupFailure::addSuppressed);
                if (originalFailure != null) {
                    originalFailure.addSuppressed(cleanupFailure);
                } else {
                    throw cleanupFailure;
                }
            }
        }
    }

    private static void advanceBoth(String mode, double target, int expectedGrants,
            Participant a, Participant b, Advance request) throws Exception {
        request.invoke(a.rti, a.time.makeTime(target));
        request.invoke(b.rti, b.time.makeTime(target));
        assertEquals(expectedGrants - 1, a.callbacks.grants.size(), mode + " A must await evoke");
        assertEquals(expectedGrants - 1, b.callbacks.grants.size(), mode + " B must await evoke");
        awaitGrants(target, expectedGrants, a, b);
    }

    private static void awaitGrants(double target, int expectedGrants,
            Participant a, Participant b) throws Exception {
        await("TAG(" + target + ")", () -> a.callbacks.grants.size() >= expectedGrants
                && b.callbacks.grants.size() >= expectedGrants, a, b);
        for (Participant participant : List.of(a, b)) {
            assertEquals(expectedGrants, participant.callbacks.grants.size(), participant.name);
            assertEquals(target, participant.callbacks.grants.get(expectedGrants - 1), 0.0);
            assertLogicalTime(participant, target);
        }
    }

    private static void observeBlocked(Participant a, Participant b) throws Exception {
        long deadline = System.nanoTime() + BLOCKED_OBSERVATION.toNanos();
        do {
            evoke(a);
            evoke(b);
            assertTrue(a.callbacks.grants.isEmpty(), "A must wait while regulating B remains at zero");
            assertTrue(b.callbacks.grants.isEmpty(), "B has not requested an advance");
        } while (System.nanoTime() < deadline);
    }

    private static void await(String description, BooleanSupplier complete,
            Participant... participants) throws Exception {
        long deadline = System.nanoTime() + CALLBACK_TIMEOUT.toNanos();
        while (!complete.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant participant : participants) {
                evoke(participant);
            }
        }
        for (Participant participant : participants) {
            participant.assertHealthy();
        }
        assertTrue(complete.getAsBoolean(), "Timed out awaiting " + description);
    }

    private static void evoke(Participant participant) throws Exception {
        participant.rti.evokeMultipleCallbacks(0.0, EVOKE_MAX_SECONDS);
        participant.assertHealthy();
    }

    private static void assertQueries(Participant participant, double logicalTime,
            double lookahead, double bound) throws Exception {
        assertLogicalTime(participant, logicalTime);
        assertEquals(lookahead,
                assertInstanceOf(HLAfloat64Interval.class, participant.rti.queryLookahead()).getValue(), 0.0);
        assertBound(participant.rti.queryGALT(), bound, participant.name + " GALT");
        assertBound(participant.rti.queryLITS(), bound, participant.name + " LITS with no queued TSO");
    }

    private static void assertHandleRoundTrip(Participant participant, FederateHandle handle)
            throws Exception {
        byte[] encoded = new byte[handle.encodedLength() + 2];
        encoded[0] = 0x5a;
        encoded[encoded.length - 1] = 0x5a;
        handle.encode(encoded, 1);
        assertEquals(0x5a, encoded[0]);
        assertEquals(0x5a, encoded[encoded.length - 1]);
        assertEquals(handle, participant.rti.getFederateHandleFactory().decode(encoded, 1));
    }

    private static void assertLogicalTime(Participant participant, double expected) throws Exception {
        assertEquals(expected, value(participant.rti.queryLogicalTime()), 0.0, participant.name);
    }

    private static void assertBound(TimeQueryReturn result, double expected, String description) {
        assertTrue(result.timeIsValid, description + " must be defined with a regulating peer");
        assertEquals(expected, value(result.time), 0.0, description);
    }

    private static double value(LogicalTime time) {
        return assertInstanceOf(HLAfloat64Time.class, time).getValue();
    }

    private static void cleanup(Cleanup action, List<Throwable> failures) {
        try {
            action.run();
        } catch (Throwable failure) {
            failures.add(failure);
        }
    }

    @FunctionalInterface
    private interface Advance {
        void invoke(RTIambassador rti, LogicalTime time) throws Exception;
    }

    @FunctionalInterface
    private interface Cleanup {
        void run() throws Exception;
    }

    private static final class Participant {
        final String name;
        final RTIambassador rti;
        final Callbacks callbacks = new Callbacks();
        final Thread invokingThread = Thread.currentThread();
        HLAfloat64TimeFactory time;
        boolean connected;
        boolean joined;

        Participant(String name) throws Exception {
            this.name = name;
            rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        }

        void connect(String endpoint) throws Exception {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint);
            connected = true;
        }

        FederateHandle join(String federation) throws Exception {
            FederateHandle handle = rti.joinFederationExecution(name, "java-live", federation);
            joined = true;
            time = assertInstanceOf(HLAfloat64TimeFactory.class, rti.getTimeFactory());
            return handle;
        }

        void assertHealthy() {
            assertNull(callbacks.connectionFailure.get(), name + " connection lost");
            for (Thread thread : callbacks.threads) {
                assertSame(invokingThread, thread, name + " HLA_EVOKED callback thread");
            }
        }

        void resign() throws Exception {
            if (joined) {
                rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
                joined = false;
            }
        }

        void disconnect() throws Exception {
            if (connected) {
                rti.disconnect();
                connected = false;
            }
        }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final List<Double> regulation = new CopyOnWriteArrayList<>();
        final List<Double> constrained = new CopyOnWriteArrayList<>();
        final List<Double> grants = new CopyOnWriteArrayList<>();
        final List<Thread> threads = new CopyOnWriteArrayList<>();
        final AtomicReference<String> connectionFailure = new AtomicReference<>();

        @Override
        public void timeRegulationEnabled(LogicalTime time) {
            threads.add(Thread.currentThread());
            regulation.add(value(time));
        }

        @Override
        public void timeConstrainedEnabled(LogicalTime time) {
            threads.add(Thread.currentThread());
            constrained.add(value(time));
        }

        @Override
        public void timeAdvanceGrant(LogicalTime time) {
            threads.add(Thread.currentThread());
            grants.add(value(time));
        }

        @Override
        public void connectionLost(String description) {
            threads.add(Thread.currentThread());
            connectionFailure.compareAndSet(null, description);
        }
    }
}
