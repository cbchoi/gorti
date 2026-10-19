package org.gorti.rti;

import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.AttributeHandleValueMap;
import hla.rti1516e.CallbackModel;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.FederateHandleSet;
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
import hla.rti1516e.SynchronizationPointFailureReason;
import hla.rti1516e.TransportationTypeHandle;
import hla.rti1516e.exceptions.InteractionClassNotPublished;
import hla.rti1516e.exceptions.ObjectInstanceNotKnown;
import hla.rti1516e.time.HLAfloat64Time;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URI;
import java.net.URL;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Two separate RTI sessions in the same JVM against an externally started GoRTI.
 * This is a public-API exchange regression, not a cross-process benchmark or full conformance test.
 */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external GoRTI and -Dgorti.endpoint=grpc://host:port")
class LiveExchangeTest {
    private static final Duration CALLBACK_TIMEOUT = Duration.ofSeconds(10);
    private static final String ITEM = "HLAobjectRoot.Item";
    private static final String PULSE = "HLAinteractionRoot.Pulse";

    @Test
    @Timeout(120)
    void twoSessionsExchangeObjectsInteractionsAndSynchronizationThroughPublicApi() throws Throwable {
        String endpoint = System.getProperty("gorti.endpoint");
        URI address = URI.create(endpoint);
        assertEquals("grpc", address.getScheme(), "gorti.endpoint must be grpc://host:port");
        assertNotNull(address.getHost(), "gorti.endpoint must include a host");
        assertTrue(address.getPort() > 0, "gorti.endpoint must include a port");
        URL fom = LiveExchangeTest.class.getResource("/exchange-fom.xml");
        assertNotNull(fom, "The exchange FOM must be on the test classpath");
        String federation = "gorti-java-exchange-" + UUID.randomUUID();
        String objectName = "item-" + UUID.randomUUID();
        Participant owner = new Participant("owner");
        Participant peer = new Participant("peer");
        boolean created = false;
        Throwable originalFailure = null;
        try {
            owner.connect(endpoint);
            peer.connect(endpoint);
            owner.rti.createFederationExecution(federation, new URL[] {fom});
            created = true;
            owner.join(federation);
            peer.join(federation);
            assertNotEquals(owner.handle, peer.handle, "Each ambassador must have its own session");

            Model published = new Model(owner.rti);
            Model subscribed = new Model(peer.rti);
            assertEquals(published.item, subscribed.item);
            assertEquals(published.value, subscribed.value);
            assertEquals(published.pulse, subscribed.pulse);
            assertEquals(published.payload, subscribed.payload);
            for (Participant participant : List.of(owner, peer)) {
                assertEquals(owner.handle, participant.rti.getFederateHandle(owner.name));
                assertEquals(peer.handle, participant.rti.getFederateHandle(peer.name));
                assertEquals(owner.name, participant.rti.getFederateName(owner.handle));
                assertEquals(peer.name, participant.rti.getFederateName(peer.handle));
            }

            AttributeHandleSet ownerAttributes = attributes(owner.rti, published.value);
            AttributeHandleSet peerAttributes = attributes(peer.rti, subscribed.value);
            byte[] roValue = integer32(0x0080ff7f);
            byte[] roPayload = integer32(0xff007f80);
            byte[] roUpdateTag = {0, (byte) 0xff, 0x11};
            byte[] roInteractionTag = {(byte) 0x80, 0, 0x22};
            AttributeHandleValueMap roAttributes = values(owner.rti, published.value, roValue);
            ParameterHandleValueMap roParameters = parameters(owner.rti, published.payload, roPayload);
            assertThrows(InteractionClassNotPublished.class,
                    () -> owner.rti.sendInteraction(published.pulse, roParameters, roInteractionTag));

            // Subscription precedes publication; optional declaration advisories remain no-ops.
            peer.rti.subscribeObjectClassAttributes(subscribed.item, peerAttributes);
            peer.rti.subscribeInteractionClass(subscribed.pulse);
            owner.rti.publishObjectClassAttributes(published.item, ownerAttributes);
            owner.rti.publishInteractionClass(published.pulse);
            owner.rti.reserveObjectInstanceName(objectName);
            await("named-object reservation", () -> owner.callbacks.reservations.contains(objectName),
                    owner, peer);
            assertEquals(List.of(objectName), owner.callbacks.reservations);
            ObjectInstanceHandle object = owner.rti.registerObjectInstance(published.item, objectName);
            await("peer discovery", () -> !peer.callbacks.discoveries.isEmpty(), owner, peer);
            assertEquals(1, peer.callbacks.discoveries.size());
            Discovery discovery = peer.callbacks.discoveries.get(0);
            assertEquals(object, discovery.object);
            assertEquals(subscribed.item, discovery.objectClass);
            assertEquals(objectName, discovery.name);
            assertEquals(owner.handle, discovery.producer);
            for (Participant participant : List.of(owner, peer)) {
                assertEquals(object, participant.rti.getObjectInstanceHandle(objectName));
                assertEquals(objectName, participant.rti.getObjectInstanceName(object));
                assertEquals(published.item, participant.rti.getKnownObjectClassHandle(object));
            }

            // Untimed service overloads send RO even though the FOM permits timestamped delivery.
            owner.rti.updateAttributeValues(object, roAttributes, roUpdateTag);
            owner.rti.sendInteraction(published.pulse, roParameters, roInteractionTag);
            await("RO reflection and interaction", () -> peer.callbacks.reflections.size() >= 1
                    && peer.callbacks.interactions.size() >= 1, owner, peer);
            assertDelivery(peer.callbacks.reflections.get(0), object, subscribed.value, roValue,
                    roUpdateTag, subscribed.reliable, owner.handle, null, null);
            assertDelivery(peer.callbacks.interactions.get(0), subscribed.pulse, subscribed.payload,
                    roPayload, roInteractionTag, subscribed.reliable, owner.handle, null, null);

            byte[] requestTag = {0x33, 0, (byte) 0xfe};
            peer.rti.requestAttributeValueUpdate(object, peerAttributes, requestTag);
            await("owner provideAttributeValueUpdate", () -> !owner.callbacks.provideUpdates.isEmpty(),
                    owner, peer);
            assertEquals(1, owner.callbacks.provideUpdates.size());
            UpdateRequest request = owner.callbacks.provideUpdates.get(0);
            assertEquals(object, request.object);
            assertEquals(Set.of(published.value), request.attributes);
            assertArrayEquals(requestTag, request.tag);

            synchronizeWithFailedPeer(owner, peer);
            exchangeTimestamped(owner, peer, published, subscribed, object);

            // Permit RO removal while the constrained peer is no longer advancing.
            peer.rti.enableAsynchronousDelivery();
            byte[] deleteTag = {(byte) 0xff, 0x44, 0};
            owner.rti.deleteObjectInstance(object, deleteTag);
            await("RO removal", () -> !peer.callbacks.removals.isEmpty(), owner, peer);
            assertEquals(1, peer.callbacks.removals.size());
            Removal removal = peer.callbacks.removals.get(0);
            assertEquals(object, removal.object);
            assertArrayEquals(deleteTag, removal.tag);
            assertEquals(OrderType.RECEIVE, removal.order);
            assertEquals(owner.handle, removal.producer);
            assertThrows(ObjectInstanceNotKnown.class, () -> peer.rti.getObjectInstanceName(object));
            assertThrows(ObjectInstanceNotKnown.class, () -> peer.rti.getObjectInstanceHandle(objectName));
            assertThrows(ObjectInstanceNotKnown.class, () -> peer.rti.getKnownObjectClassHandle(object));

            peer.rti.unsubscribeObjectClassAttributes(subscribed.item, peerAttributes);
            peer.rti.unsubscribeInteractionClass(subscribed.pulse);
            owner.rti.unpublishObjectClass(published.item);
            owner.rti.unpublishInteractionClass(published.pulse);
            assertThrows(InteractionClassNotPublished.class,
                    () -> owner.rti.sendInteraction(published.pulse, roParameters, roInteractionTag));
            for (Participant participant : List.of(owner, peer)) {
                evoke(participant);
                participant.assertHealthy();
            }
            assertEquals(1, peer.callbacks.discoveries.size());
            assertEquals(2, peer.callbacks.reflections.size());
            assertEquals(2, peer.callbacks.interactions.size());
            assertTrue(owner.callbacks.reflections.isEmpty(), "The owner did not subscribe");
            assertTrue(owner.callbacks.interactions.isEmpty(), "The owner did not subscribe");
        } catch (Throwable failure) {
            originalFailure = failure;
            throw failure;
        } finally {
            List<Throwable> failures = new ArrayList<>();
            cleanup(peer::resign, failures);
            cleanup(owner::resign, failures);
            if (created) {
                cleanup(() -> owner.rti.destroyFederationExecution(federation), failures);
            }
            cleanup(peer::disconnect, failures);
            cleanup(owner::disconnect, failures);
            if (!failures.isEmpty()) {
                AssertionError failure = new AssertionError("Live exchange cleanup failed");
                failures.forEach(failure::addSuppressed);
                if (originalFailure != null) {
                    originalFailure.addSuppressed(failure);
                } else {
                    throw failure;
                }
            }
        }
    }

    @Test
    @Timeout(45)
    void explicitEmptyParticipantsSynchronizeBothEvokedMembersWithImplicitControl() throws Throwable {
        URL fom = LiveExchangeTest.class.getResource("/exchange-fom.xml");
        assertNotNull(fom);
        String federation = "gorti-java-empty-sync-" + UUID.randomUUID();
        Participant owner = new Participant("owner");
        Participant peer = new Participant("peer");
        boolean created = false;
        Throwable originalFailure = null;
        try {
            owner.connect(System.getProperty("gorti.endpoint"));
            peer.connect(System.getProperty("gorti.endpoint"));
            owner.rti.createFederationExecution(federation, new URL[]{fom});
            created = true;
            owner.join(federation);
            peer.join(federation);
            assertNotEquals(owner.handle, peer.handle);
            for (String label : List.of("explicit-empty", "implicit-control")) {
                byte[] tag = {0x55, 0, (byte) 0xff};
                if (label.equals("explicit-empty")) {
                    FederateHandleSet empty = owner.rti.getFederateHandleSetFactory().create();
                    assertTrue(empty.isEmpty());
                    owner.rti.registerFederationSynchronizationPoint(label, tag, empty);
                } else {
                    owner.rti.registerFederationSynchronizationPoint(label, tag);
                }
                assertFalse(owner.callbacks.syncRegistrations.contains(label));
                assertFalse(owner.callbacks.announcements.containsKey(label));
                assertFalse(peer.callbacks.announcements.containsKey(label));
                await("both all-member announcements " + label,
                        () -> owner.callbacks.syncRegistrations.contains(label)
                                && owner.callbacks.announcements.containsKey(label)
                                && peer.callbacks.announcements.containsKey(label), owner, peer);
                assertArrayEquals(tag, owner.callbacks.announcements.get(label));
                assertArrayEquals(tag, peer.callbacks.announcements.get(label));
                assertTrue(peer.callbacks.syncRegistrations.isEmpty());
                owner.rti.synchronizationPointAchieved(label);
                for (int i = 0; i < 3; i++) {
                    evoke(owner);
                    evoke(peer);
                    assertFalse(owner.callbacks.synchronizedSets.containsKey(label));
                    assertFalse(peer.callbacks.synchronizedSets.containsKey(label));
                }
                peer.rti.synchronizationPointAchieved(label);
                assertFalse(owner.callbacks.synchronizedSets.containsKey(label));
                assertFalse(peer.callbacks.synchronizedSets.containsKey(label));
                await("both synchronized callbacks " + label,
                        () -> owner.callbacks.synchronizedSets.containsKey(label)
                                && peer.callbacks.synchronizedSets.containsKey(label), owner, peer);
                assertEquals(Set.of(), owner.callbacks.synchronizedSets.get(label));
                assertEquals(Set.of(), peer.callbacks.synchronizedSets.get(label));
            }
            assertEquals(List.of("explicit-empty", "implicit-control"), owner.callbacks.syncRegistrations);
            for (Participant participant : List.of(owner, peer)) {
                evoke(participant);
                assertEquals(2, participant.callbacks.announcements.size());
                assertEquals(2, participant.callbacks.synchronizedSets.size());
                participant.assertHealthy();
            }
        } catch (Throwable failure) {
            originalFailure = failure;
            throw failure;
        } finally {
            List<Throwable> failures = new ArrayList<>();
            cleanup(peer::resign, failures);
            cleanup(owner::resign, failures);
            if (created) cleanup(() -> owner.rti.destroyFederationExecution(federation), failures);
            cleanup(peer::disconnect, failures);
            cleanup(owner::disconnect, failures);
            if (!failures.isEmpty()) {
                AssertionError failure = new AssertionError("Live empty synchronization cleanup failed");
                failures.forEach(failure::addSuppressed);
                if (originalFailure != null) originalFailure.addSuppressed(failure);
                else throw failure;
            }
        }
    }

    private static void synchronizeWithFailedPeer(Participant owner, Participant peer) throws Exception {
        String label = "exchange-sync";
        byte[] tag = {0x55, 0, (byte) 0x80};
        FederateHandleSet members = owner.rti.getFederateHandleSetFactory().create();
        assertTrue(members.isEmpty());
        members.add(owner.handle);
        members.add(peer.handle);
        assertEquals(Set.of(owner.handle, peer.handle), members);
        assertTrue(owner.rti.getFederateHandleSetFactory().create().isEmpty());
        owner.rti.registerFederationSynchronizationPoint(label, tag, members);
        await("sync registration and announcements", () -> owner.callbacks.syncRegistrations.contains(label)
                && owner.callbacks.announcements.containsKey(label)
                && peer.callbacks.announcements.containsKey(label), owner, peer);
        assertEquals(List.of(label), owner.callbacks.syncRegistrations);
        assertTrue(peer.callbacks.syncRegistrations.isEmpty());
        assertArrayEquals(tag, owner.callbacks.announcements.get(label));
        assertArrayEquals(tag, peer.callbacks.announcements.get(label));
        owner.rti.synchronizationPointAchieved(label);
        peer.rti.synchronizationPointAchieved(label, false);
        await("federation synchronized with failed peer", () -> owner.callbacks.synchronizedSets.containsKey(label)
                && peer.callbacks.synchronizedSets.containsKey(label), owner, peer);
        assertEquals(Set.of(peer.handle), owner.callbacks.synchronizedSets.get(label));
        assertEquals(Set.of(peer.handle), peer.callbacks.synchronizedSets.get(label));
    }

    private static void exchangeTimestamped(Participant owner, Participant peer,
            Model published, Model subscribed, ObjectInstanceHandle object) throws Exception {
        owner.rti.enableTimeRegulation(owner.time.makeInterval(0.5));
        peer.rti.enableTimeRegulation(peer.time.makeInterval(0.5));
        peer.rti.enableTimeConstrained();
        await("both regulating and receiver constrained", () -> owner.callbacks.regulation.size() >= 1
                && peer.callbacks.regulation.size() >= 1 && peer.callbacks.constrained.size() >= 1,
                owner, peer);
        assertEquals(List.of(0.0), owner.callbacks.regulation);
        assertEquals(List.of(0.0), peer.callbacks.regulation);
        assertEquals(List.of(0.0), peer.callbacks.constrained);
        assertTrue(owner.callbacks.constrained.isEmpty());

        byte[] value = integer32(0x80ff0001);
        byte[] payload = integer32(0x7f00fe02);
        byte[] updateTag = {0x66, (byte) 0xff, 0};
        byte[] interactionTag = {0x77, 0, (byte) 0x80};
        // Enqueue both messages at time 1 before either session requests time 2.
        MessageRetractionReturn update = owner.rti.updateAttributeValues(object,
                values(owner.rti, published.value, value), updateTag, owner.time.makeTime(1.0));
        MessageRetractionReturn interaction = owner.rti.sendInteraction(published.pulse,
                parameters(owner.rti, published.payload, payload), interactionTag, owner.time.makeTime(1.0));
        assertRetraction(update);
        assertRetraction(interaction);
        assertNotEquals(update.handle, interaction.handle, "Each timestamped message needs a distinct handle");
        owner.rti.timeAdvanceRequest(owner.time.makeTime(2.0));
        peer.rti.timeAdvanceRequest(peer.time.makeTime(2.0));
        await("TSO reflection, interaction and both TAGs", () -> peer.callbacks.reflections.size() >= 2
                && peer.callbacks.interactions.size() >= 2 && owner.callbacks.grants.size() >= 1
                && peer.callbacks.grants.size() >= 1, owner, peer);
        assertDelivery(peer.callbacks.reflections.get(1), object, subscribed.value, value, updateTag,
                subscribed.reliable, owner.handle, 1.0, update.handle);
        assertDelivery(peer.callbacks.interactions.get(1), subscribed.pulse, subscribed.payload, payload,
                interactionTag, subscribed.reliable, owner.handle, 1.0, interaction.handle);
        assertEquals(List.of(2.0), owner.callbacks.grants);
        assertEquals(List.of(2.0), peer.callbacks.grants);
        assertEquals(2.0, timeValue(owner.rti.queryLogicalTime()), 0.0);
        assertEquals(2.0, timeValue(peer.rti.queryLogicalTime()), 0.0);
        List<String> events = peer.callbacks.events;
        int grant = events.indexOf("TAG");
        assertTrue(events.indexOf("TSO reflect") >= 0 && events.indexOf("TSO reflect") < grant,
                "TSO reflection must precede the receiver's TAG: " + events);
        assertTrue(events.indexOf("TSO interaction") >= 0 && events.indexOf("TSO interaction") < grant,
                "TSO interaction must precede the receiver's TAG: " + events);
    }

    private static void assertRetraction(MessageRetractionReturn result) {
        assertNotNull(result);
        assertTrue(result.retractionHandleIsValid, "A regulating timestamped send must return a valid handle");
        assertNotNull(result.handle);
    }

    private static void assertDelivery(Delivery delivery, Object target, Object key, byte[] value,
            byte[] tag, TransportationTypeHandle transport, FederateHandle producer,
            Double time, MessageRetractionHandle retraction) {
        assertEquals(target, delivery.target);
        assertEquals(Set.of(key), delivery.values.keySet());
        assertArrayEquals(value, delivery.values.get(key));
        assertArrayEquals(tag, delivery.tag);
        assertEquals(transport, delivery.transport);
        assertEquals(producer, delivery.producer);
        assertFalse(delivery.hasRegions, "This exchange does not use DDM regions");
        assertEquals(time, delivery.time);
        assertEquals(time == null ? OrderType.RECEIVE : OrderType.TIMESTAMP, delivery.sentOrder);
        assertEquals(time == null ? null : OrderType.TIMESTAMP, delivery.receivedOrder);
        assertEquals(retraction, delivery.retraction, "Compare opaque handles, not implementation-specific IDs");
        if (retraction != null) {
            assertEquals(retraction.hashCode(), delivery.retraction.hashCode());
        }
    }

    private static AttributeHandleSet attributes(RTIambassador rti, AttributeHandle handle) throws Exception {
        AttributeHandleSet result = rti.getAttributeHandleSetFactory().create();
        assertTrue(result.isEmpty());
        assertTrue(result.add(handle));
        assertFalse(result.add(handle));
        assertEquals(Set.of(handle), result);
        assertTrue(rti.getAttributeHandleSetFactory().create().isEmpty());
        return result;
    }

    private static AttributeHandleValueMap values(RTIambassador rti, AttributeHandle handle,
            byte[] bytes) throws Exception {
        AttributeHandleValueMap result = rti.getAttributeHandleValueMapFactory().create(1);
        assertTrue(result.isEmpty());
        result.put(handle, bytes.clone());
        assertArrayEquals(bytes, result.get(handle));
        assertTrue(rti.getAttributeHandleValueMapFactory().create(0).isEmpty());
        return result;
    }

    private static ParameterHandleValueMap parameters(RTIambassador rti, ParameterHandle handle,
            byte[] bytes) throws Exception {
        ParameterHandleValueMap result = rti.getParameterHandleValueMapFactory().create(1);
        assertTrue(result.isEmpty());
        result.put(handle, bytes.clone());
        assertArrayEquals(bytes, result.get(handle));
        assertTrue(rti.getParameterHandleValueMapFactory().create(0).isEmpty());
        return result;
    }

    private static byte[] integer32(int value) {
        return new byte[] {(byte) (value >>> 24), (byte) (value >>> 16), (byte) (value >>> 8), (byte) value};
    }

    private static double timeValue(LogicalTime time) {
        return assertInstanceOf(HLAfloat64Time.class, time).getValue();
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
        participant.callbacks.evoking = true;
        try {
            participant.rti.evokeMultipleCallbacks(0.01, 0.05);
        } finally {
            participant.callbacks.evoking = false;
        }
        participant.assertHealthy();
    }

    private static void cleanup(Cleanup action, List<Throwable> failures) {
        try {
            action.run();
        } catch (Throwable failure) {
            failures.add(failure);
        }
    }

    @FunctionalInterface
    private interface Cleanup {
        void run() throws Exception;
    }

    private static final class Model {
        final ObjectClassHandle item;
        final AttributeHandle value;
        final InteractionClassHandle pulse;
        final ParameterHandle payload;
        final TransportationTypeHandle reliable;

        Model(RTIambassador rti) throws Exception {
            item = rti.getObjectClassHandle(ITEM);
            value = rti.getAttributeHandle(item, "value");
            pulse = rti.getInteractionClassHandle(PULSE);
            payload = rti.getParameterHandle(pulse, "payload");
            reliable = rti.getTransportationTypeHandle("HLAreliable");
            assertEquals(ITEM, rti.getObjectClassName(item));
            assertEquals("value", rti.getAttributeName(item, value));
            assertEquals(PULSE, rti.getInteractionClassName(pulse));
            assertEquals("payload", rti.getParameterName(pulse, payload));
            assertEquals("HLAreliable", rti.getTransportationTypeName(reliable));
        }
    }

    private static final class Participant {
        final String name;
        final RTIambassador rti;
        final Callbacks callbacks = new Callbacks();
        FederateHandle handle;
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

        void join(String federation) throws Exception {
            handle = rti.joinFederationExecution(name, "java-exchange", federation);
            joined = true;
            time = assertInstanceOf(HLAfloat64TimeFactory.class, rti.getTimeFactory());
        }

        void assertHealthy() {
            assertNull(callbacks.failure.get(), name + " callback contract: " + callbacks.events);
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

    private static final class Discovery {
        final ObjectInstanceHandle object;
        final ObjectClassHandle objectClass;
        final String name;
        final FederateHandle producer;

        Discovery(ObjectInstanceHandle object, ObjectClassHandle objectClass, String name,
                FederateHandle producer) {
            this.object = object;
            this.objectClass = objectClass;
            this.name = name;
            this.producer = producer;
        }
    }

    private static final class Delivery {
        final Object target;
        final Map<Object, byte[]> values = new HashMap<>();
        final byte[] tag;
        final OrderType sentOrder;
        final TransportationTypeHandle transport;
        final Double time;
        final OrderType receivedOrder;
        final MessageRetractionHandle retraction;
        final FederateHandle producer;
        final boolean hasRegions;

        Delivery(Object target, Map<?, byte[]> values, byte[] tag, OrderType sentOrder,
                TransportationTypeHandle transport, LogicalTime time, OrderType receivedOrder,
                MessageRetractionHandle retraction, FederateHandle producer, boolean hasRegions) {
            this.target = target;
            values.forEach((key, value) -> this.values.put(key, value.clone()));
            this.tag = tag.clone();
            this.sentOrder = sentOrder;
            this.transport = transport;
            this.time = time == null ? null : timeValue(time);
            this.receivedOrder = receivedOrder;
            this.retraction = retraction;
            this.producer = producer;
            this.hasRegions = hasRegions;
        }
    }

    private static final class UpdateRequest {
        final ObjectInstanceHandle object;
        final Set<AttributeHandle> attributes;
        final byte[] tag;

        UpdateRequest(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) {
            this.object = object;
            this.attributes = new HashSet<>(attributes);
            this.tag = tag.clone();
        }
    }

    private static final class Removal {
        final ObjectInstanceHandle object;
        final byte[] tag;
        final OrderType order;
        final FederateHandle producer;

        Removal(ObjectInstanceHandle object, byte[] tag, OrderType order, FederateHandle producer) {
            this.object = object;
            this.tag = tag.clone();
            this.order = order;
            this.producer = producer;
        }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final Thread invokingThread = Thread.currentThread();
        final AtomicReference<String> failure = new AtomicReference<>();
        final List<String> events = new CopyOnWriteArrayList<>();
        final List<String> reservations = new CopyOnWriteArrayList<>();
        final List<Discovery> discoveries = new CopyOnWriteArrayList<>();
        final List<Delivery> reflections = new CopyOnWriteArrayList<>();
        final List<Delivery> interactions = new CopyOnWriteArrayList<>();
        final List<UpdateRequest> provideUpdates = new CopyOnWriteArrayList<>();
        final List<Removal> removals = new CopyOnWriteArrayList<>();
        final List<String> syncRegistrations = new CopyOnWriteArrayList<>();
        final Map<String, byte[]> announcements = new ConcurrentHashMap<>();
        final Map<String, Set<FederateHandle>> synchronizedSets = new ConcurrentHashMap<>();
        final List<Double> regulation = new CopyOnWriteArrayList<>();
        final List<Double> constrained = new CopyOnWriteArrayList<>();
        final List<Double> grants = new CopyOnWriteArrayList<>();
        volatile boolean evoking;

        private void record(String event) {
            events.add(event);
            if (Thread.currentThread() != invokingThread || !evoking) {
                failure.compareAndSet(null, event + " must run on the caller thread inside evoke");
            }
        }

        private void unexpected(String description) {
            record(description);
            failure.compareAndSet(null, description);
        }

        @Override
        public void connectionLost(String description) {
            unexpected("connectionLost: " + description);
        }

        @Override
        public void objectInstanceNameReservationSucceeded(String name) {
            record("reservation succeeded");
            reservations.add(name);
        }

        @Override
        public void objectInstanceNameReservationFailed(String name) {
            unexpected("reservation failed: " + name);
        }

        @Override
        public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle objectClass,
                String name) {
            unexpected("discover overload without producing federate");
        }

        @Override
        public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle objectClass,
                String name, FederateHandle producer) {
            record("discover");
            discoveries.add(new Discovery(object, objectClass, name, producer));
        }

        @Override
        public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap attributes,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            record("RO reflect");
            reflections.add(new Delivery(object, attributes, tag, sent, transport, null, null, null,
                    info.hasProducingFederate() ? info.getProducingFederate() : null, info.hasSentRegions()));
        }

        @Override
        public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap attributes,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, SupplementalReflectInfo info) {
            unexpected("reflect timestamp overload without retraction handle");
        }

        @Override
        public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap attributes,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, MessageRetractionHandle retraction, SupplementalReflectInfo info) {
            record("TSO reflect");
            reflections.add(new Delivery(object, attributes, tag, sent, transport, time, received, retraction,
                    info.hasProducingFederate() ? info.getProducingFederate() : null, info.hasSentRegions()));
        }

        @Override
        public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
            record("RO interaction");
            interactions.add(new Delivery(interaction, parameters, tag, sent, transport, null, null, null,
                    info.hasProducingFederate() ? info.getProducingFederate() : null, info.hasSentRegions()));
        }

        @Override
        public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, SupplementalReceiveInfo info) {
            unexpected("receive timestamp overload without retraction handle");
        }

        @Override
        public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
                byte[] tag, OrderType sent, TransportationTypeHandle transport, LogicalTime time,
                OrderType received, MessageRetractionHandle retraction, SupplementalReceiveInfo info) {
            record("TSO interaction");
            interactions.add(new Delivery(interaction, parameters, tag, sent, transport, time, received, retraction,
                    info.hasProducingFederate() ? info.getProducingFederate() : null, info.hasSentRegions()));
        }

        @Override
        public void provideAttributeValueUpdate(ObjectInstanceHandle object, AttributeHandleSet attributes,
                byte[] tag) {
            record("provide update");
            provideUpdates.add(new UpdateRequest(object, attributes, tag));
        }

        @Override
        public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent,
                SupplementalRemoveInfo info) {
            record("RO remove");
            removals.add(new Removal(object, tag, sent,
                    info.hasProducingFederate() ? info.getProducingFederate() : null));
        }

        @Override
        public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent,
                LogicalTime time, OrderType received, SupplementalRemoveInfo info) {
            unexpected("remove timestamp overload for untimed deletion");
        }

        @Override
        public void removeObjectInstance(ObjectInstanceHandle object, byte[] tag, OrderType sent,
                LogicalTime time, OrderType received, MessageRetractionHandle retraction,
                SupplementalRemoveInfo info) {
            unexpected("remove timestamp/retraction overload for untimed deletion");
        }

        @Override
        public void synchronizationPointRegistrationSucceeded(String label) {
            record("sync registered");
            syncRegistrations.add(label);
        }

        @Override
        public void synchronizationPointRegistrationFailed(String label, SynchronizationPointFailureReason reason) {
            unexpected("sync registration failed: " + label + " " + reason);
        }

        @Override
        public void announceSynchronizationPoint(String label, byte[] tag) {
            record("sync announced");
            if (announcements.putIfAbsent(label, tag.clone()) != null) {
                unexpected("duplicate sync announcement: " + label);
            }
        }

        @Override
        public void federationSynchronized(String label, FederateHandleSet failed) {
            record("synchronized");
            if (synchronizedSets.putIfAbsent(label, new HashSet<>(failed)) != null) {
                unexpected("duplicate federationSynchronized: " + label);
            }
        }

        @Override
        public void timeRegulationEnabled(LogicalTime time) {
            record("regulation enabled");
            regulation.add(timeValue(time));
        }

        @Override
        public void timeConstrainedEnabled(LogicalTime time) {
            record("constrained enabled");
            constrained.add(timeValue(time));
        }

        @Override
        public void timeAdvanceGrant(LogicalTime time) {
            record("TAG");
            grants.add(timeValue(time));
        }
    }
}
