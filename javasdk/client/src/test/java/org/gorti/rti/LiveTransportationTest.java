package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URL;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

/** Public Java API cases against an external GoRTI, including backend conformance regressions. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external GoRTI and -Dgorti.endpoint=grpc://host:port")
@Timeout(90)
class LiveTransportationTest {
    @Test void changesQueriesPendingErrorsAndReceivedMetadataUseEvokedCallbacks() throws Exception {
        try (Session s = new Session()) {
            s.start();
            assertThrows(AttributeNotOwned.class, () -> s.peer.rti.requestAttributeTransportationTypeChange(s.object, s.attributes, s.reliable));
            assertThrows(AttributeNotOwned.class, () -> s.peer.rti.changeAttributeOrderType(s.object, s.attributes, OrderType.RECEIVE));
            s.peer.rti.unpublishInteractionClass(s.interaction);
            assertThrows(InteractionClassNotPublished.class, () -> s.peer.rti.requestInteractionTransportationTypeChange(s.interaction, s.reliable));
            assertThrows(InteractionClassNotPublished.class, () -> s.peer.rti.changeInteractionOrderType(s.interaction, OrderType.RECEIVE));
            s.peer.rti.publishInteractionClass(s.interaction);

            s.owner.rti.disableCallbacks();
            s.requestReliable();
            assertTrue(s.owner.callbacks.events.isEmpty());
            s.owner.evoke();
            assertTrue(s.owner.callbacks.events.isEmpty(), "Disabled callbacks must not deliver confirmations");
            assertThrows(AttributeAlreadyBeingChanged.class, () -> s.owner.rti.requestAttributeTransportationTypeChange(s.object, s.attributes, s.bestEffort));
            assertThrows(InteractionClassAlreadyBeingChanged.class, () -> s.owner.rti.requestInteractionTransportationTypeChange(s.interaction, s.bestEffort));
            s.owner.rti.enableCallbacks();
            s.awaitConfirmed();
            Event attributes = s.owner.callbacks.last("confirm-attributes");
            assertEquals(s.object, attributes.object);
            assertEquals(Set.copyOf(s.attributes), attributes.attributes);
            assertEquals(s.reliable, attributes.transport);
            Event interaction = s.owner.callbacks.last("confirm-interaction");
            assertEquals(s.interaction, interaction.interaction);
            assertEquals(s.reliable, interaction.transport);

            s.peer.rti.queryAttributeTransportationType(s.object, s.attribute);
            s.peer.rti.queryInteractionTransportationType(s.owner.handle, s.interaction);
            s.peer.rti.queryInteractionTransportationType(s.peer.handle, s.interaction);
            assertEquals(0, s.peer.callbacks.events.size());
            await(() -> s.peer.callbacks.events.size() == 3, s.peer);
            Event attributeReport = s.peer.callbacks.last("report-attribute");
            assertEquals(s.object, attributeReport.object);
            assertEquals(Set.of(s.attribute), attributeReport.attributes);
            assertEquals(s.reliable, attributeReport.transport);
            List<Event> reports = s.peer.callbacks.ofKind("report-interaction");
            assertEquals(s.owner.handle, reports.get(0).producer);
            assertEquals(s.reliable, reports.get(0).transport);
            assertEquals(s.peer.handle, reports.get(1).producer);
            assertEquals(s.bestEffort, reports.get(1).transport);
            assertTrue(reports.stream().allMatch(event -> s.interaction.equals(event.interaction)));

            s.owner.rti.changeAttributeOrderType(s.object, s.attributes, OrderType.RECEIVE);
            s.owner.rti.changeInteractionOrderType(s.interaction, OrderType.RECEIVE);
            s.sendOwnerMessages();
            await(() -> s.peer.callbacks.ofKind("reflect").size() == 1 && s.peer.callbacks.ofKind("receive").size() == 1, s.peer);
            for (String kind : List.of("reflect", "receive")) {
                Event event = s.peer.callbacks.last(kind);
                assertEquals(s.reliable, event.transport);
                assertEquals(s.owner.handle, event.producer);
                assertEquals(OrderType.RECEIVE, event.order);
            }
            s.owner.rti.changeAttributeOrderType(s.object, s.attributes, OrderType.TIMESTAMP);
            s.owner.rti.changeInteractionOrderType(s.interaction, OrderType.TIMESTAMP);
            s.owner.rti.requestAttributeTransportationTypeChange(s.object, s.attributes, s.bestEffort);
            s.owner.rti.requestInteractionTransportationTypeChange(s.interaction, s.bestEffort);
            await(() -> s.owner.callbacks.ofKind("confirm-attributes").size() == 2
                    && s.owner.callbacks.ofKind("confirm-interaction").size() == 2, s.owner);
        }
    }

    @Test void transportationChangeMustNotAffectMessagesUntilConfirmationInvocation() throws Exception {
        try (Session s = new Session()) {
            s.start();
            s.requestReliable();
            s.sendOwnerMessages();
            // Only the receiving federate evokes. The producer has not received either confirmation.
            await(() -> s.peer.callbacks.ofKind("reflect").size() == 1 && s.peer.callbacks.ofKind("receive").size() == 1, s.peer);
            assertTrue(s.owner.callbacks.events.isEmpty());
            assertAll(
                    () -> assertEquals(s.bestEffort, s.peer.callbacks.last("reflect").transport, "Attribute transport activated before confirmation"),
                    () -> assertEquals(s.bestEffort, s.peer.callbacks.last("receive").transport, "Interaction transport activated before confirmation"));
            s.awaitConfirmed();
            s.sendOwnerMessages();
            await(() -> s.peer.callbacks.ofKind("reflect").size() == 2 && s.peer.callbacks.ofKind("receive").size() == 2, s.peer);
            assertEquals(s.reliable, s.peer.callbacks.last("reflect").transport);
            assertEquals(s.reliable, s.peer.callbacks.last("receive").transport);
        }
    }

    @Test void orderChangesControlTimestampedSendOrderWithoutDiscardingTimestamp() throws Exception {
        try (Session s = new Session()) {
            s.start();
            HLAfloat64TimeFactory time = (HLAfloat64TimeFactory) s.owner.rti.getTimeFactory();
            s.owner.rti.enableTimeRegulation(time.makeInterval(1));
            await(() -> s.owner.callbacks.regulating, s.owner);
            for (OrderType order : List.of(OrderType.RECEIVE, OrderType.TIMESTAMP)) {
                s.owner.rti.changeAttributeOrderType(s.object, s.attributes, order);
                s.owner.rti.changeInteractionOrderType(s.interaction, order);
                int expected = s.peer.callbacks.ofKind("reflect").size() + 1;
                AttributeHandleValueMap values = s.owner.rti.getAttributeHandleValueMapFactory().create(1);
                values.put(s.attribute, new byte[]{0, 0, 0, 42});
                s.owner.rti.updateAttributeValues(s.object, values, new byte[]{1}, time.makeTime(5));
                s.owner.rti.sendInteraction(s.interaction, s.owner.rti.getParameterHandleValueMapFactory().create(0), new byte[]{2}, time.makeTime(5));
                await(() -> s.peer.callbacks.ofKind("reflect").size() == expected
                        && s.peer.callbacks.ofKind("receive").size() == expected, s.peer);
                for (String kind : List.of("reflect", "receive")) {
                    Event event = s.peer.callbacks.last(kind);
                    assertEquals(order, event.order);
                    assertEquals(time.makeTime(5), event.time);
                    assertEquals(OrderType.RECEIVE, event.receivedOrder, "Unconstrained peer receives RO");
                    assertEquals(s.owner.handle, event.producer);
                }
            }
        }
    }

    @Test void unownedAttributeQueryReturnsFddTransportation() throws Exception {
        try (Session s = new Session()) {
            s.start(); s.requestReliable(); s.awaitConfirmed();
            s.owner.rti.unconditionalAttributeOwnershipDivestiture(s.object, s.attributes);
            s.peer.rti.queryAttributeTransportationType(s.object, s.attribute);
            await(() -> !s.peer.callbacks.ofKind("report-attribute").isEmpty(), s.peer);
            assertEquals(s.bestEffort, s.peer.callbacks.last("report-attribute").transport);
        }
    }

    @Test void reacquiredAttributeUsesFddTransportationInsteadOfFormerOverride() throws Exception {
        try (Session s = new Session()) {
            s.start(); s.requestReliable(); s.awaitConfirmed();
            s.owner.rti.unconditionalAttributeOwnershipDivestiture(s.object, s.attributes);
            s.owner.rti.attributeOwnershipAcquisitionIfAvailable(s.object, s.attributes);
            await(() -> !s.owner.callbacks.ofKind("acquired").isEmpty(), s.owner);
            s.owner.rti.queryAttributeTransportationType(s.object, s.attribute);
            await(() -> !s.owner.callbacks.ofKind("report-attribute").isEmpty(), s.owner);
            assertEquals(s.bestEffort, s.owner.callbacks.last("report-attribute").transport);
        }
    }

    @Test void unpublishedProducerQueryReturnsFddTransportation() throws Exception {
        try (Session s = new Session()) {
            s.start(); s.requestReliable(); s.awaitConfirmed();
            s.owner.rti.unpublishInteractionClass(s.interaction);
            s.peer.rti.queryInteractionTransportationType(s.owner.handle, s.interaction);
            await(() -> !s.peer.callbacks.ofKind("report-interaction").isEmpty(), s.peer);
            assertEquals(s.owner.handle, s.peer.callbacks.last("report-interaction").producer);
            assertEquals(s.bestEffort, s.peer.callbacks.last("report-interaction").transport);
        }
    }

    @Test void pendingChangeExcludesAttributeLostAndReacquiredBeforeConfirmation() throws Exception {
        try (Session s = new Session()) {
            s.start();
            s.owner.rti.requestAttributeTransportationTypeChange(s.object, s.attributes, s.reliable);
            s.owner.rti.unconditionalAttributeOwnershipDivestiture(s.object, s.attributes);
            s.owner.rti.attributeOwnershipAcquisitionIfAvailable(s.object, s.attributes);
            await(() -> !s.owner.callbacks.ofKind("confirm-attributes").isEmpty()
                    && !s.owner.callbacks.ofKind("acquired").isEmpty(), s.owner);
            assertTrue(s.owner.callbacks.last("confirm-attributes").attributes.isEmpty(), "Lost attributes must not be reinserted into an old transportation request");
            s.owner.rti.queryAttributeTransportationType(s.object, s.attribute);
            await(() -> !s.owner.callbacks.ofKind("report-attribute").isEmpty(), s.owner);
            assertEquals(s.bestEffort, s.owner.callbacks.last("report-attribute").transport);
            s.owner.rti.requestAttributeTransportationTypeChange(s.object, s.attributes, s.reliable);
            await(() -> s.owner.callbacks.ofKind("confirm-attributes").size() == 2, s.owner);
            assertEquals(Set.copyOf(s.attributes), s.owner.callbacks.last("confirm-attributes").attributes);
        }
    }

    private static void await(BooleanSupplier done, Participant... participants) throws Exception {
        long deadline = System.nanoTime() + 10_000_000_000L;
        while (!done.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant participant : participants) participant.evoke();
        }
        assertTrue(done.getAsBoolean(), "Timed out awaiting transportation callback");
    }

    private static final class Session implements AutoCloseable {
        final Participant owner = new Participant();
        final Participant peer = new Participant();
        final String federation = "java-transportation-" + UUID.randomUUID();
        boolean created;
        ObjectInstanceHandle object;
        AttributeHandle attribute;
        AttributeHandleSet attributes;
        InteractionClassHandle interaction;
        TransportationTypeHandle reliable;
        TransportationTypeHandle bestEffort;
        Session() throws Exception { }
        void start() throws Exception {
            String endpoint = System.getProperty("gorti.endpoint");
            owner.connect(endpoint); peer.connect(endpoint);
            URL fom = LiveTransportationTest.class.getResource("/transportation-fom.xml");
            assertNotNull(fom);
            owner.rti.createFederationExecution(federation, new URL[]{fom}); created = true;
            owner.join("owner", federation); peer.join("peer", federation);
            ObjectClassHandle type = owner.rti.getObjectClassHandle("HLAobjectRoot.TransportItem");
            attribute = owner.rti.getAttributeHandle(type, "value");
            attributes = owner.rti.getAttributeHandleSetFactory().create(); attributes.add(attribute);
            interaction = owner.rti.getInteractionClassHandle("HLAinteractionRoot.TransportPulse");
            reliable = owner.rti.getTransportationTypeHandle("HLAreliable");
            bestEffort = owner.rti.getTransportationTypeHandle("HLAbestEffort");
            owner.rti.publishObjectClassAttributes(type, attributes);
            peer.rti.publishObjectClassAttributes(type, attributes);
            peer.rti.subscribeObjectClassAttributes(type, attributes);
            owner.rti.publishInteractionClass(interaction);
            peer.rti.publishInteractionClass(interaction);
            peer.rti.subscribeInteractionClass(interaction);
            object = owner.rti.registerObjectInstance(type);
            await(() -> object.equals(peer.callbacks.discovered), owner, peer);
            owner.callbacks.events.clear(); peer.callbacks.events.clear();
        }
        void requestReliable() throws Exception {
            owner.rti.requestAttributeTransportationTypeChange(object, attributes, reliable);
            owner.rti.requestInteractionTransportationTypeChange(interaction, reliable);
        }
        void awaitConfirmed() throws Exception {
            await(() -> owner.callbacks.ofKind("confirm-attributes").size() == 1
                    && owner.callbacks.ofKind("confirm-interaction").size() == 1, owner);
        }
        void sendOwnerMessages() throws Exception {
            AttributeHandleValueMap values = owner.rti.getAttributeHandleValueMapFactory().create(1);
            values.put(attribute, new byte[]{0, 0, 0, 42});
            owner.rti.updateAttributeValues(object, values, new byte[]{1, 2});
            owner.rti.sendInteraction(interaction, owner.rti.getParameterHandleValueMapFactory().create(0), new byte[]{3, 4});
        }
        public void close() throws Exception {
            List<Throwable> failures = new ArrayList<>();
            cleanup(peer::resign, failures); cleanup(owner::resign, failures);
            if (created) cleanup(() -> owner.rti.destroyFederationExecution(federation), failures);
            cleanup(peer::disconnect, failures); cleanup(owner::disconnect, failures);
            if (!failures.isEmpty()) {
                Exception failure = new Exception("Transportation-session cleanup failed");
                for (Throwable cause : failures) failure.addSuppressed(cause);
                throw failure;
            }
        }
    }

    @FunctionalInterface private interface Cleanup { void run() throws Exception; }
    private static void cleanup(Cleanup action, List<Throwable> failures) { try { action.run(); } catch (Throwable failure) { failures.add(failure); } }

    private static final class Participant {
        final RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        final Callbacks callbacks = new Callbacks();
        FederateHandle handle;
        boolean connected;
        boolean joined;
        Participant() throws Exception { }
        void connect(String endpoint) throws Exception { rti.connect(callbacks, CallbackModel.HLA_EVOKED, endpoint); connected = true; }
        void join(String name, String federation) throws Exception { handle = rti.joinFederationExecution(name, "java-transportation", federation); joined = true; }
        void resign() throws Exception { if (joined) { rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); joined = false; } }
        void disconnect() throws Exception { if (connected) { rti.disconnect(); connected = false; } }
        void evoke() throws Exception {
            callbacks.evoking = true;
            try { rti.evokeMultipleCallbacks(0.01, 0.05); }
            finally { callbacks.evoking = false; }
            assertNull(callbacks.failure);
        }
    }

    private static final class Event {
        final String kind;
        final ObjectInstanceHandle object;
        final Set<AttributeHandle> attributes;
        final InteractionClassHandle interaction;
        final FederateHandle producer;
        final TransportationTypeHandle transport;
        final OrderType order;
        final LogicalTime time;
        final OrderType receivedOrder;
        Event(String kind, ObjectInstanceHandle object, Set<AttributeHandle> attributes, InteractionClassHandle interaction,
                FederateHandle producer, TransportationTypeHandle transport, OrderType order) {
            this(kind, object, attributes, interaction, producer, transport, order, null, null);
        }
        Event(String kind, ObjectInstanceHandle object, Set<AttributeHandle> attributes, InteractionClassHandle interaction,
                FederateHandle producer, TransportationTypeHandle transport, OrderType order, LogicalTime time, OrderType receivedOrder) {
            this.kind = kind; this.object = object; this.attributes = Set.copyOf(attributes); this.interaction = interaction;
            this.producer = producer; this.transport = transport; this.order = order;
            this.time = time; this.receivedOrder = receivedOrder;
        }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final List<Event> events = new ArrayList<>();
        final Thread caller = Thread.currentThread();
        boolean evoking;
        volatile String failure;
        ObjectInstanceHandle discovered;
        boolean regulating;
        void guard() { if (!evoking || Thread.currentThread() != caller) failure = "Callback delivered outside caller's evoke"; }
        void add(Event event) { guard(); events.add(event); }
        List<Event> ofKind(String kind) { List<Event> result = new ArrayList<>(); for (Event event : events) if (kind.equals(event.kind)) result.add(event); return result; }
        Event last(String kind) { List<Event> result = ofKind(kind); assertFalse(result.isEmpty(), kind); return result.get(result.size() - 1); }
        @Override public void connectionLost(String description) { failure = "Connection lost: " + description; }
        @Override public void timeRegulationEnabled(LogicalTime time) { guard(); regulating = true; }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { guard(); discovered = object; }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name, FederateHandle producer) { guard(); discovered = object; }
        @Override public void confirmAttributeTransportationTypeChange(ObjectInstanceHandle object, AttributeHandleSet attributes, TransportationTypeHandle transport) {
            add(new Event("confirm-attributes", object, attributes, null, null, transport, null));
        }
        @Override public void confirmInteractionTransportationTypeChange(InteractionClassHandle interaction, TransportationTypeHandle transport) {
            add(new Event("confirm-interaction", null, Set.of(), interaction, null, transport, null));
        }
        @Override public void reportAttributeTransportationType(ObjectInstanceHandle object, AttributeHandle attribute, TransportationTypeHandle transport) {
            add(new Event("report-attribute", object, Set.of(attribute), null, null, transport, null));
        }
        @Override public void reportInteractionTransportationType(FederateHandle federate, InteractionClassHandle interaction, TransportationTypeHandle transport) {
            add(new Event("report-interaction", null, Set.of(), interaction, federate, transport, null));
        }
        @Override public void attributeOwnershipAcquisitionNotification(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) {
            add(new Event("acquired", object, attributes, null, null, null, null));
        }
        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag, OrderType order, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            add(new Event("reflect", object, values.keySet(), null, info.hasProducingFederate() ? info.getProducingFederate() : null, transport, order));
        }
        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters, byte[] tag, OrderType order, TransportationTypeHandle transport, SupplementalReceiveInfo info) {
            add(new Event("receive", null, Set.of(), interaction, info.hasProducingFederate() ? info.getProducingFederate() : null, transport, order));
        }
        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag, OrderType order, TransportationTypeHandle transport,
                LogicalTime time, OrderType receivedOrder, SupplementalReflectInfo info) {
            add(new Event("reflect", object, values.keySet(), null, info.hasProducingFederate() ? info.getProducingFederate() : null, transport, order, time, receivedOrder));
        }
        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap parameters, byte[] tag, OrderType order, TransportationTypeHandle transport,
                LogicalTime time, OrderType receivedOrder, SupplementalReceiveInfo info) {
            add(new Event("receive", null, Set.of(), interaction, info.hasProducingFederate() ? info.getProducingFederate() : null, transport, order, time, receivedOrder));
        }
    }
}
