package org.gorti.rti;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.AttributeHandleValueMap;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.InteractionClassHandle;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.MessageRetractionHandle;
import hla.rti1516e.MessageRetractionReturn;
import hla.rti1516e.NullFederateAmbassador;
import hla.rti1516e.ObjectClassHandle;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.OrderType;
import hla.rti1516e.ParameterHandle;
import hla.rti1516e.ParameterHandleValueMap;
import hla.rti1516e.RTIambassador;
import hla.rti1516e.ResignAction;
import hla.rti1516e.RtiFactoryFactory;
import hla.rti1516e.TransportationTypeHandle;
import hla.rti1516e.exceptions.ObjectInstanceNotKnown;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URI;
import java.net.URL;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Real-server, same-JVM two-session deletion regressions, not a conformance suite.
 * Mirrors the delivered-retraction and provisional-removal cases in
 * pysdk/tests/spec/m21/test_ieee2010_flush_enable_retract_live.py.
 */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external GoRTI and -Dgorti.endpoint=grpc://host:port")
class LiveRetractionTest {
    private static final Duration CALLBACK_TIMEOUT = Duration.ofSeconds(10);
    private static final byte[] DELETE_TAG = {0x51, 0, (byte) 0xff};
    private static final byte[] RESTORED_VALUE = {0, (byte) 0xff, (byte) 0x80, 0x01};
    private static final byte[] RESTORED_TAG = {0x52, (byte) 0x80, 0};
    private static final byte[] CONTROL_VALUE = {0, 0, 0, 0x53};
    private static final byte[] CONTROL_TAG = {0x53, 0, (byte) 0xfe};

    @ParameterizedTest(name = "retract delivered deletion, constrained={0}")
    @ValueSource(booleans = {true, false})
    @Timeout(90)
    void deliveredTimestampedDeletionRestoresSenderAndReceiver(boolean constrained) throws Exception {
        try (Federation federation = new Federation()) {
            federation.start(constrained);
            federation.deleteAtFive(constrained);
            if (constrained) {
                federation.reader.assertKnown();
            } else {
                federation.reader.assertUnknown();
            }

            federation.sender.rti.retract(federation.retraction);
            federation.sender.assertKnown();
            federation.await("requestRetraction and restored receiver knowledge",
                    () -> !federation.reader.callbacks.retractions.isEmpty());
            assertEquals(List.of(federation.retraction), federation.reader.callbacks.retractions);
            federation.reader.assertKnown();
            assertTrue(federation.reader.callbacks.events.indexOf("remove")
                    < federation.reader.callbacks.events.indexOf("retract"));

            // A successful real update proves the restored object is usable, not just cached.
            federation.reader.rti.enableAsynchronousDelivery();
            AttributeHandleValueMap values = federation.sender.rti.getAttributeHandleValueMapFactory().create(1);
            values.put(federation.sender.value, RESTORED_VALUE.clone());
            federation.sender.rti.updateAttributeValues(federation.object, values, RESTORED_TAG.clone());
            federation.await("reflection on restored object", () -> federation.reader.callbacks.reflections.size() == 1);
            federation.controlFence();
            assertEquals(1, federation.reader.callbacks.removals.size());
            assertEquals(1, federation.reader.callbacks.retractions.size());
            assertEquals(1, federation.reader.callbacks.discoveries.size(), "Restoration must not require rediscovery");
            federation.sender.assertKnown();
            federation.reader.assertKnown();
        }
    }

    @ParameterizedTest(name = "expire provisional deletion, disableConstraint={0}")
    @ValueSource(booleans = {false, true})
    @Timeout(90)
    void provisionalTsoRemovalExpiresAtGrantOrConstraintDisable(boolean disableConstraint) throws Exception {
        try (Federation federation = new Federation()) {
            federation.start(true);
            federation.deleteAtFive(true);
            federation.reader.assertKnown();
            if (disableConstraint) {
                federation.reader.rti.disableTimeConstrained();
            } else {
                // Match the Python oracle: raise the sender's bound without advancing the sender.
                federation.sender.rti.modifyLookahead(federation.sender.time.makeInterval(20.0));
                federation.reader.expectUnknownAtGrant = true;
                federation.reader.rti.timeAdvanceRequestAvailable(federation.reader.time.makeTime(5.0));
                federation.await("TAG(5) and expired object knowledge",
                        () -> federation.reader.callbacks.grants.size() >= 2);
                assertEquals(List.of(1.0, 5.0), federation.reader.callbacks.grants);
                assertEquals(5.0, timeValue(federation.reader.rti.queryLogicalTime()), 0.0);
            }
            federation.reader.assertUnknown();
            federation.reader.rti.enableAsynchronousDelivery();
            federation.controlFence();
            assertEquals(1, federation.reader.callbacks.removals.size(), "Expiry must not repeat removal");
            assertTrue(federation.reader.callbacks.retractions.isEmpty());
            federation.reader.assertUnknown();
        }
    }

    private static double timeValue(LogicalTime time) {
        return assertInstanceOf(HLAfloat64Time.class, time).getValue();
    }

    private static final class Federation implements AutoCloseable {
        final String name = "gorti-java-retraction-" + UUID.randomUUID();
        final String objectName = "retractable-item-" + UUID.randomUUID();
        final Participant sender;
        final Participant reader;
        ObjectInstanceHandle object;
        MessageRetractionHandle retraction;
        boolean created;

        Federation() throws Exception {
            sender = new Participant(this, "sender");
            reader = new Participant(this, "reader");
        }

        void start(boolean constrained) throws Exception {
            String endpoint = System.getProperty("gorti.endpoint");
            URI address = URI.create(endpoint);
            assertEquals("grpc", address.getScheme());
            assertNotNull(address.getHost());
            assertTrue(address.getPort() > 0, "gorti.endpoint must be grpc://host:port");
            URL fom = LiveRetractionTest.class.getResource("/exchange-fom.xml");
            assertNotNull(fom, "The exchange FOM must be on the test classpath");
            sender.connect(endpoint);
            reader.connect(endpoint);
            sender.rti.createFederationExecution(name, new URL[] {fom});
            created = true;
            sender.join();
            reader.join();
            assertNotEquals(sender.handle, reader.handle);
            sender.rti.publishObjectClassAttributes(sender.item, sender.attributes());
            reader.rti.subscribeObjectClassAttributes(reader.item, reader.attributes());
            sender.rti.publishInteractionClass(sender.pulse);
            reader.rti.subscribeInteractionClass(reader.pulse);
            sender.rti.reserveObjectInstanceName(objectName);
            await("object name reservation", () -> sender.callbacks.reservations.contains(objectName));
            object = sender.rti.registerObjectInstance(sender.item, objectName);
            await("object discovery", () -> reader.callbacks.discoveries.contains(object));
            sender.assertKnown();
            reader.assertKnown();
            sender.rti.enableTimeRegulation(sender.time.makeInterval(1.0));
            await("sender regulation", () -> !sender.callbacks.regulation.isEmpty());
            assertEquals(List.of(0.0), sender.callbacks.regulation);
            if (constrained) {
                reader.rti.enableTimeConstrained();
                await("receiver constrained", () -> !reader.callbacks.constrained.isEmpty());
                assertEquals(List.of(0.0), reader.callbacks.constrained);
            }
        }

        void deleteAtFive(boolean constrained) throws Exception {
            MessageRetractionReturn result = sender.rti.deleteObjectInstance(object, DELETE_TAG.clone(),
                    sender.time.makeTime(5.0));
            assertNotNull(result);
            assertTrue(result.retractionHandleIsValid);
            retraction = assertNotNullHandle(result.handle);
            if (constrained) {
                reader.rti.flushQueueRequest(reader.time.makeTime(10.0));
            }
            await("timestamped deletion delivery", () -> !reader.callbacks.removals.isEmpty()
                    && (!constrained || !reader.callbacks.grants.isEmpty()));
            assertEquals(1, reader.callbacks.removals.size());
            Removal removal = reader.callbacks.removals.get(0);
            assertEquals(constrained, removal.retractionOverload, "Use full TSO or middle timestamped-RO overload");
            assertEquals(constrained ? OrderType.TIMESTAMP : OrderType.RECEIVE, removal.received);
            assertEquals(constrained ? retraction : null, removal.retraction);
            if (constrained) {
                assertEquals(retraction.hashCode(), removal.retraction.hashCode());
                assertEquals(List.of(1.0), reader.callbacks.grants);
                assertEquals(1.0, timeValue(reader.rti.queryLogicalTime()), 0.0);
                assertTrue(reader.callbacks.events.indexOf("remove") < reader.callbacks.events.indexOf("grant"),
                        "FQR must deliver the deletion before granting below its timestamp");
            } else {
                assertTrue(reader.callbacks.grants.isEmpty(), "Unconstrained delivery requires no advance");
            }
        }

        void controlFence() throws Exception {
            ParameterHandleValueMap parameters = sender.rti.getParameterHandleValueMapFactory().create(1);
            parameters.put(sender.payload, CONTROL_VALUE.clone());
            sender.rti.sendInteraction(sender.pulse, parameters, CONTROL_TAG.clone());
            await("RO control after deletion processing", () -> reader.callbacks.controls.size() >= 1);
            assertEquals(1, reader.callbacks.controls.size());
        }

        void await(String description, BooleanSupplier complete) throws Exception {
            long deadline = System.nanoTime() + CALLBACK_TIMEOUT.toNanos();
            while (!complete.getAsBoolean() && System.nanoTime() < deadline) {
                sender.evoke();
                reader.evoke();
            }
            sender.assertHealthy();
            reader.assertHealthy();
            assertTrue(complete.getAsBoolean(), "Timed out awaiting " + description);
        }

        @Override
        public void close() {
            List<Throwable> failures = new ArrayList<>();
            cleanup(reader::resign, failures);
            cleanup(sender::resign, failures);
            if (created) cleanup(() -> sender.rti.destroyFederationExecution(name), failures);
            cleanup(reader::disconnect, failures);
            cleanup(sender::disconnect, failures);
            if (!failures.isEmpty()) {
                AssertionError failure = new AssertionError("Live retraction cleanup failed");
                failures.forEach(failure::addSuppressed);
                throw failure;
            }
        }
    }

    private static MessageRetractionHandle assertNotNullHandle(MessageRetractionHandle handle) {
        assertNotNull(handle);
        return handle;
    }

    private static void cleanup(Cleanup action, List<Throwable> failures) {
        try { action.run(); } catch (Throwable failure) { failures.add(failure); }
    }

    @FunctionalInterface
    private interface Cleanup { void run() throws Exception; }

    private static final class Participant {
        final Federation federation;
        final String name;
        final RTIambassador rti;
        final Callbacks callbacks;
        final Thread invokingThread = Thread.currentThread();
        FederateHandle handle;
        HLAfloat64TimeFactory time;
        ObjectClassHandle item;
        AttributeHandle value;
        InteractionClassHandle pulse;
        ParameterHandle payload;
        TransportationTypeHandle reliable;
        boolean connected;
        boolean joined;
        boolean expectUnknownAtGrant;
        volatile boolean evoking;

        Participant(Federation federation, String name) throws Exception {
            this.federation = federation;
            this.name = name;
            rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
            callbacks = new Callbacks(this);
        }

        void connect(String endpoint) throws Exception {
            rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint);
            connected = true;
        }

        void join() throws Exception {
            handle = rti.joinFederationExecution(name, "java-retraction", federation.name);
            joined = true;
            time = assertInstanceOf(HLAfloat64TimeFactory.class, rti.getTimeFactory());
            item = rti.getObjectClassHandle("HLAobjectRoot.Item");
            value = rti.getAttributeHandle(item, "value");
            pulse = rti.getInteractionClassHandle("HLAinteractionRoot.Pulse");
            payload = rti.getParameterHandle(pulse, "payload");
            reliable = rti.getTransportationTypeHandle("HLAreliable");
        }

        AttributeHandleSet attributes() throws Exception {
            AttributeHandleSet result = rti.getAttributeHandleSetFactory().create();
            result.add(value);
            return result;
        }

        void assertKnown() throws Exception {
            assertEquals(federation.object, rti.getObjectInstanceHandle(federation.objectName), name);
            assertEquals(federation.objectName, rti.getObjectInstanceName(federation.object), name);
            assertEquals(item, rti.getKnownObjectClassHandle(federation.object), name);
        }

        void assertUnknown() {
            assertThrows(ObjectInstanceNotKnown.class, () -> rti.getObjectInstanceHandle(federation.objectName), name);
            assertThrows(ObjectInstanceNotKnown.class, () -> rti.getObjectInstanceName(federation.object), name);
            assertThrows(ObjectInstanceNotKnown.class, () -> rti.getKnownObjectClassHandle(federation.object), name);
        }

        void evoke() throws Exception {
            evoking = true;
            try { rti.evokeMultipleCallbacks(0.01, 0.05); } finally { evoking = false; }
            assertHealthy();
        }

        void assertHealthy() { assertNull(callbacks.failure.get(), name + " callbacks: " + callbacks.events); }

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

    private static final class Removal {
        final OrderType received;
        final MessageRetractionHandle retraction;
        final boolean retractionOverload;
        Removal(OrderType received, MessageRetractionHandle retraction, boolean retractionOverload) {
            this.received = received;
            this.retraction = retraction;
            this.retractionOverload = retractionOverload;
        }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final Participant participant;
        final AtomicReference<String> failure = new AtomicReference<>();
        final List<String> events = new CopyOnWriteArrayList<>();
        final List<String> reservations = new CopyOnWriteArrayList<>();
        final List<ObjectInstanceHandle> discoveries = new CopyOnWriteArrayList<>();
        final List<Removal> removals = new CopyOnWriteArrayList<>();
        final List<MessageRetractionHandle> retractions = new CopyOnWriteArrayList<>();
        final List<ObjectInstanceHandle> reflections = new CopyOnWriteArrayList<>();
        final List<InteractionClassHandle> controls = new CopyOnWriteArrayList<>();
        final List<Double> regulation = new CopyOnWriteArrayList<>();
        final List<Double> constrained = new CopyOnWriteArrayList<>();
        final List<Double> grants = new CopyOnWriteArrayList<>();

        Callbacks(Participant participant) { this.participant = participant; }

        private void record(String event) {
            events.add(event);
            if (!participant.evoking || Thread.currentThread() != participant.invokingThread) {
                failure.compareAndSet(null, event + " must run on the invoking thread inside evoke");
            }
        }

        private void unexpected(String event) { record(event); failure.compareAndSet(null, event); }

        private void knownInsideCallback() {
            try { participant.assertKnown(); }
            catch (Exception error) { failure.compareAndSet(null, "Object lookup inside callback failed: " + error); }
        }

        @Override public void connectionLost(String description) { unexpected("connection lost: " + description); }
        @Override public void objectInstanceNameReservationFailed(String name) { unexpected("reservation failed: " + name); }
        @Override public void objectInstanceNameReservationSucceeded(String name) {
            record("reserved"); reservations.add(name);
        }

        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle objectClass, String name) {
            unexpected("discovery overload without producer");
        }

        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle objectClass,
                String name, FederateHandle producer) {
            record("discover");
            assertEquals(participant.federation.object, object);
            assertEquals(participant.item, objectClass);
            assertEquals(participant.federation.objectName, name);
            assertEquals(participant.federation.sender.handle, producer);
            knownInsideCallback();
            discoveries.add(object);
        }

        @Override public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent,
                SupplementalRemoveInfo info) {
            unexpected("untimed remove overload for timestamped deletion");
        }

        @Override public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent,
                LogicalTime time, OrderType received, SupplementalRemoveInfo info) {
            removed(object, tag, sent, time, received, null, false, info);
        }

        @Override public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent,
                LogicalTime time, OrderType received, MessageRetractionHandle retraction, SupplementalRemoveInfo info) {
            removed(object, tag, sent, time, received, retraction, true, info);
        }

        private void removed(ObjectInstanceHandle object, byte[] tag, OrderType sent, LogicalTime time,
                OrderType received, MessageRetractionHandle retraction, boolean retractionOverload,
                SupplementalRemoveInfo info) {
            record("remove");
            assertEquals(participant.federation.object, object);
            assertArrayEquals(DELETE_TAG, tag);
            assertEquals(OrderType.TIMESTAMP, sent);
            assertEquals(5.0, timeValue(time), 0.0);
            assertTrue(info.hasProducingFederate());
            assertEquals(participant.federation.sender.handle, info.getProducingFederate());
            knownInsideCallback();
            removals.add(new Removal(received, retraction, retractionOverload));
        }

        @Override public void requestRetraction(MessageRetractionHandle handle) {
            record("retract");
            assertEquals(participant.federation.retraction, handle);
            assertEquals(participant.federation.retraction.hashCode(), handle.hashCode());
            knownInsideCallback();
            retractions.add(handle);
        }

        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            record("reflect restored");
            assertEquals(participant.federation.object, object);
            assertEquals(Set.of(participant.value), values.keySet());
            assertArrayEquals(RESTORED_VALUE, values.get(participant.value));
            assertArrayEquals(RESTORED_TAG, tag);
            assertEquals(OrderType.RECEIVE, sent);
            assertEquals(participant.reliable, transport);
            assertTrue(info.hasProducingFederate());
            assertEquals(participant.federation.sender.handle, info.getProducingFederate());
            knownInsideCallback();
            reflections.add(object);
        }

        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, SupplementalReflectInfo info) {
            unexpected("timestamped reflect overload for untimed restored update");
        }

        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, MessageRetractionHandle retraction, SupplementalReflectInfo info) {
            unexpected("TSO reflect overload for untimed restored update");
        }

        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
            record("control");
            assertEquals(participant.pulse, interaction);
            assertEquals(Set.of(participant.payload), values.keySet());
            assertArrayEquals(CONTROL_VALUE, values.get(participant.payload));
            assertArrayEquals(CONTROL_TAG, tag);
            assertEquals(OrderType.RECEIVE, sent);
            assertEquals(participant.reliable, transport);
            controls.add(interaction);
        }

        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, SupplementalReceiveInfo info) {
            unexpected("timestamped interaction overload for RO control");
        }

        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, MessageRetractionHandle retraction, SupplementalReceiveInfo info) {
            unexpected("TSO interaction overload for RO control");
        }

        @Override public void timeRegulationEnabled(LogicalTime time) {
            record("regulating"); regulation.add(timeValue(time));
        }

        @Override public void timeConstrainedEnabled(LogicalTime time) {
            record("constrained"); constrained.add(timeValue(time));
        }

        @Override public void timeAdvanceGrant(LogicalTime time) {
            record("grant");
            if (participant.expectUnknownAtGrant) participant.assertUnknown();
            grants.add(timeValue(time));
        }
    }
}
