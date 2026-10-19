package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.AttributeAlreadyBeingDivested;
import hla.rti1516e.exceptions.AttributeAlreadyOwned;
import hla.rti1516e.exceptions.AttributeAcquisitionWasNotRequested;
import hla.rti1516e.exceptions.AttributeDivestitureWasNotRequested;
import hla.rti1516e.exceptions.AttributeNotDefined;
import hla.rti1516e.exceptions.AttributeNotOwned;
import hla.rti1516e.exceptions.FederateOwnsAttributes;
import hla.rti1516e.exceptions.NoAcquisitionPending;
import java.net.URL;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import org.gorti.rti.types.Handles;

/** Two independent Java sessions, through the standard public API and an external GoRTI. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+",
        disabledReason = "Requires an external GoRTI and -Dgorti.endpoint=grpc://host:port")
class LiveOwnershipTest {
    @Test @Timeout(90)
    void twoSessionsNegotiateCancelAndConfirmTransferWithEvokedCallbacks() throws Throwable {
        String endpoint = System.getProperty("gorti.endpoint");
        URL fom = LiveOwnershipTest.class.getResource("/ownership-fom.xml");
        assertNotNull(fom);
        String federation = "java-ownership-" + UUID.randomUUID();
        Participant owner = new Participant();
        Participant peer = new Participant();
        boolean created = false;
        Throwable original = null;
        try {
            owner.connect(endpoint); peer.connect(endpoint);
            owner.rti.createFederationExecution(federation, new URL[]{fom});
            created = true;
            owner.join("owner", federation); peer.join("peer", federation);
            assertNotEquals(owner.handle, peer.handle);
            ObjectClassHandle type = owner.rti.getObjectClassHandle("HLAobjectRoot.Asset");
            AttributeHandle attribute = owner.rti.getAttributeHandle(type, "value");
            AttributeHandleSet attributes = owner.rti.getAttributeHandleSetFactory().create();
            attributes.add(attribute);
            for (Participant participant : List.of(owner, peer)) {
                participant.rti.publishObjectClassAttributes(type, attributes);
                participant.rti.subscribeObjectClassAttributes(type, attributes);
            }
            ObjectInstanceHandle object = owner.rti.registerObjectInstance(type);
            await(() -> object.equals(peer.callbacks.discovered), owner, peer);

            // First ownership call intentionally fails against the pre-ownership Java baseline.
            assertTrue(owner.rti.isAttributeOwnedByFederate(object, attribute));
            assertFalse(peer.rti.isAttributeOwnedByFederate(object, attribute));
            assertThrows(AttributeNotOwned.class, () -> peer.rti.unconditionalAttributeOwnershipDivestiture(object, attributes));
            assertThrows(FederateOwnsAttributes.class, () -> owner.rti.attributeOwnershipAcquisition(object, attributes, new byte[0]));
            assertThrows(AttributeDivestitureWasNotRequested.class,
                    () -> owner.rti.cancelNegotiatedAttributeOwnershipDivestiture(object, attributes));
            assertThrows(AttributeAcquisitionWasNotRequested.class,
                    () -> peer.rti.cancelAttributeOwnershipAcquisition(object, attributes));
            assertThrows(AttributeAlreadyOwned.class,
                    () -> owner.rti.cancelAttributeOwnershipAcquisition(object, attributes));
            assertThrows(AttributeNotOwned.class,
                    () -> peer.rti.cancelNegotiatedAttributeOwnershipDivestiture(object, attributes));
            AttributeHandleSet invalidMixed = owner.rti.getAttributeHandleSetFactory().create();
            invalidMixed.add(attribute);
            invalidMixed.add(Handles.attribute(999999));

            peer.rti.attributeOwnershipAcquisitionIfAvailable(object, attributes);
            await(() -> peer.callbacks.count("unavailable") == 1, owner, peer);
            peer.callbacks.check("unavailable", object, attributes, null);
            byte[] acquireTag = {0, -1, 11};
            peer.rti.attributeOwnershipAcquisition(object, attributes, acquireTag);
            await(() -> owner.callbacks.count("release") == 1, owner, peer);
            owner.callbacks.check("release", object, attributes, acquireTag);
            assertThrows(AttributeNotDefined.class,
                    () -> owner.rti.attributeOwnershipDivestitureIfWanted(object, invalidMixed));
            assertTrue(owner.rti.isAttributeOwnedByFederate(object, attribute));
            owner.rti.attributeOwnershipReleaseDenied(object, attributes);
            await(() -> peer.callbacks.count("unavailable") == 2, owner, peer);
            assertTrue(owner.rti.attributeOwnershipDivestitureIfWanted(object, attributes).isEmpty());

            byte[] offerTag = {7, 0, -1};
            owner.rti.negotiatedAttributeOwnershipDivestiture(object, attributes, offerTag);
            await(() -> peer.callbacks.count("assumption") == 1, owner, peer);
            peer.callbacks.check("assumption", object, attributes, offerTag);
            assertThrows(AttributeNotDefined.class,
                    () -> owner.rti.cancelNegotiatedAttributeOwnershipDivestiture(object, invalidMixed));
            owner.rti.cancelNegotiatedAttributeOwnershipDivestiture(object, attributes);
            owner.rti.negotiatedAttributeOwnershipDivestiture(object, attributes, offerTag);
            await(() -> peer.callbacks.count("assumption") == 2, owner, peer);
            assertThrows(AttributeAlreadyBeingDivested.class,
                    () -> owner.rti.negotiatedAttributeOwnershipDivestiture(object, attributes, offerTag));

            peer.rti.attributeOwnershipAcquisition(object, attributes, acquireTag);
            await(() -> owner.callbacks.count("confirm") == 1, owner, peer);
            owner.callbacks.check("confirm", object, attributes, null);
            assertTrue(owner.rti.isAttributeOwnedByFederate(object, attribute), "Negotiation must not transfer before confirmation");
            assertFalse(peer.rti.isAttributeOwnedByFederate(object, attribute));
            assertEquals(0, peer.callbacks.count("acquired"));

            assertThrows(AttributeNotDefined.class,
                    () -> peer.rti.cancelAttributeOwnershipAcquisition(object, invalidMixed));
            peer.rti.cancelAttributeOwnershipAcquisition(object, attributes);
            assertEquals(0, peer.callbacks.count("cancelled"), "RPC-result callback must not run inline");
            assertThrows(AttributeAcquisitionWasNotRequested.class,
                    () -> peer.rti.cancelAttributeOwnershipAcquisition(object, attributes));
            await(() -> peer.callbacks.count("cancelled") == 1, owner, peer);
            peer.callbacks.check("cancelled", object, attributes, null);
            byte[] confirmTag = {-128, 0, 23};
            assertThrows(NoAcquisitionPending.class, () -> owner.rti.confirmDivestiture(object, attributes, confirmTag));
            assertTrue(owner.rti.isAttributeOwnedByFederate(object, attribute));

            peer.rti.attributeOwnershipAcquisition(object, attributes, acquireTag);
            await(() -> owner.callbacks.count("confirm") == 2, owner, peer);
            owner.rti.confirmDivestiture(object, attributes, confirmTag);
            assertEquals(0, peer.callbacks.count("acquired"));
            await(() -> peer.callbacks.count("acquired") == 1, owner, peer);
            peer.callbacks.check("acquired", object, attributes, confirmTag);
            assertFalse(owner.rti.isAttributeOwnedByFederate(object, attribute));
            assertTrue(peer.rti.isAttributeOwnedByFederate(object, attribute));

            owner.rti.queryAttributeOwnership(object, attribute);
            assertEquals(0, owner.callbacks.count("owned"));
            await(() -> owner.callbacks.count("owned") == 1, owner, peer);
            assertEquals(peer.handle, owner.callbacks.informedOwner);
            owner.callbacks.check("owned", object, attributes, null);
            AttributeHandleValueMap values = peer.rti.getAttributeHandleValueMapFactory().create(1);
            values.put(attribute, new byte[]{0, 0, 0, 42});
            assertThrows(AttributeNotOwned.class, () -> owner.rti.updateAttributeValues(object, values, new byte[0]));
            peer.rti.updateAttributeValues(object, values, confirmTag);
            await(() -> owner.callbacks.reflected != null, owner, peer);
            assertArrayEquals(new byte[]{0, 0, 0, 42}, owner.callbacks.reflected);
            assertEquals(peer.handle, owner.callbacks.producer);

            peer.rti.unconditionalAttributeOwnershipDivestiture(object, attributes);
            owner.rti.queryAttributeOwnership(object, attribute);
            assertEquals(0, owner.callbacks.count("unowned"));
            await(() -> owner.callbacks.count("unowned") == 1, owner, peer);
            owner.callbacks.check("unowned", object, attributes, null);
            owner.rti.attributeOwnershipAcquisitionIfAvailable(object, attributes);
            await(() -> owner.callbacks.count("acquired") == 1, owner, peer);
            owner.callbacks.check("acquired", object, attributes, new byte[0]);
            assertTrue(owner.rti.isAttributeOwnedByFederate(object, attribute));

            long confirmations = owner.callbacks.count("confirm");
            peer.rti.attributeOwnershipAcquisition(object, attributes, acquireTag);
            await(() -> owner.callbacks.count("release") == 2, owner, peer);
            assertEquals(attributes, owner.rti.attributeOwnershipDivestitureIfWanted(object, attributes));
            await(() -> peer.callbacks.count("acquired") == 2, owner, peer);
            drain(owner, peer);
            assertEquals(confirmations, owner.callbacks.count("confirm"), "Completed divest-if-wanted must not request confirmation");
            assertTrue(peer.rti.isAttributeOwnedByFederate(object, attribute));
            assertThrows(AttributeAlreadyOwned.class,
                    () -> peer.rti.cancelAttributeOwnershipAcquisition(object, attributes));
            owner.healthy(); peer.healthy();
        } catch (Throwable failure) {
            original = failure;
            throw failure;
        } finally {
            List<Throwable> failures = new ArrayList<>();
            cleanup(peer::resign, failures); cleanup(owner::resign, failures);
            if (created) cleanup(() -> owner.rti.destroyFederationExecution(federation), failures);
            cleanup(peer::disconnect, failures); cleanup(owner::disconnect, failures);
            if (original != null) for (Throwable failure : failures) original.addSuppressed(failure);
            else if (!failures.isEmpty()) {
                AssertionError failure = new AssertionError("Ownership-session cleanup failed");
                for (Throwable detail : failures) failure.addSuppressed(detail);
                throw failure;
            }
        }
    }

    @Test @Timeout(90)
    void predefinedMomAttributeIsReportedAsRtiOwnedAndCannotBeAcquired() throws Throwable {
        Participant observer = new Participant();
        String federation = "java-rti-ownership-" + UUID.randomUUID();
        boolean created = false;
        Throwable original = null;
        try {
            observer.connect(System.getProperty("gorti.endpoint"));
            observer.rti.createFederationExecution(federation,
                    new URL[]{LiveOwnershipTest.class.getResource("/ownership-fom.xml")});
            created = true;
            observer.join("observer", federation);
            ObjectClassHandle type = observer.rti.getObjectClassHandle("HLAobjectRoot.HLAmanager.HLAfederation");
            AttributeHandle attribute = observer.rti.getAttributeHandle(type, "HLAfederationName");
            AttributeHandleSet attributes = observer.rti.getAttributeHandleSetFactory().create();
            attributes.add(attribute);
            observer.rti.publishObjectClassAttributes(type, attributes);
            observer.rti.subscribeObjectClassAttributes(type, attributes);
            await(() -> observer.callbacks.discovered != null, observer);
            ObjectInstanceHandle object = observer.callbacks.discovered;
            observer.rti.queryAttributeOwnership(object, attribute);
            assertEquals(0, observer.callbacks.count("rti-owned"));
            await(() -> observer.callbacks.count("rti-owned") == 1, observer);
            assertEquals(0, observer.callbacks.count("owned"));
            assertFalse(observer.rti.isAttributeOwnedByFederate(object, attribute));
            observer.rti.attributeOwnershipAcquisition(object, attributes, new byte[]{1});
            await(() -> observer.callbacks.count("unavailable") == 1, observer);
            observer.rti.attributeOwnershipAcquisitionIfAvailable(object, attributes);
            await(() -> observer.callbacks.count("unavailable") == 2, observer);
            assertThrows(AttributeAcquisitionWasNotRequested.class,
                    () -> observer.rti.cancelAttributeOwnershipAcquisition(object, attributes));
            observer.rti.queryAttributeOwnership(object, attribute);
            await(() -> observer.callbacks.count("rti-owned") == 2, observer);
            assertEquals(0, observer.callbacks.count("acquired"));
            observer.healthy();
        } catch (Throwable failure) {
            original = failure;
            throw failure;
        } finally {
            List<Throwable> failures = new ArrayList<>();
            cleanup(observer::resign, failures);
            if (created) cleanup(() -> observer.rti.destroyFederationExecution(federation), failures);
            cleanup(observer::disconnect, failures);
            if (original != null) for (Throwable failure : failures) original.addSuppressed(failure);
            else if (!failures.isEmpty()) {
                AssertionError failure = new AssertionError("MOM ownership-session cleanup failed");
                for (Throwable detail : failures) failure.addSuppressed(detail);
                throw failure;
            }
        }
    }

    private static void drain(Participant... participants) throws Exception {
        long deadline = System.nanoTime() + 300_000_000L;
        while (System.nanoTime() < deadline) {
            for (Participant participant : participants) {
                participant.callbacks.evoking = true;
                try { participant.rti.evokeMultipleCallbacks(0.01, 0.05); }
                finally { participant.callbacks.evoking = false; }
                participant.healthy();
            }
        }
    }

    private static void await(BooleanSupplier done, Participant... participants) throws Exception {
        long deadline = System.nanoTime() + 10_000_000_000L;
        while (!done.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant participant : participants) {
                participant.callbacks.evoking = true;
                try { participant.rti.evokeMultipleCallbacks(0.01, 0.05); }
                finally { participant.callbacks.evoking = false; }
                participant.healthy();
            }
        }
        assertTrue(done.getAsBoolean(), "Timed out awaiting ownership callback");
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
        void join(String name, String federation) throws Exception { handle = rti.joinFederationExecution(name, "java-ownership", federation); joined = true; }
        void resign() throws Exception { if (joined) { rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); joined = false; } }
        void disconnect() throws Exception { if (connected) { rti.disconnect(); connected = false; } }
        void healthy() { assertNull(callbacks.failure); }
    }

    private static final class Event {
        final String kind;
        final ObjectInstanceHandle object;
        final Set<AttributeHandle> attributes;
        final byte[] tag;
        Event(String kind, ObjectInstanceHandle object, Set<AttributeHandle> attributes, byte[] tag) {
            this.kind = kind; this.object = object; this.attributes = new HashSet<>(attributes); this.tag = tag == null ? null : tag.clone();
        }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final Thread caller = Thread.currentThread();
        final List<Event> events = new CopyOnWriteArrayList<>();
        volatile boolean evoking;
        volatile String failure;
        ObjectInstanceHandle discovered;
        FederateHandle informedOwner;
        FederateHandle producer;
        byte[] reflected;
        void guard() { if (!evoking || Thread.currentThread() != caller) failure = "Callback delivered outside caller's evoke"; }
        void record(String kind, ObjectInstanceHandle object, Set<AttributeHandle> attributes, byte[] tag) { guard(); events.add(new Event(kind, object, attributes, tag)); }
        long count(String kind) { return events.stream().filter(event -> event.kind.equals(kind)).count(); }
        void check(String kind, ObjectInstanceHandle object, Set<AttributeHandle> attributes, byte[] tag) {
            Event event = events.stream().filter(item -> item.kind.equals(kind)).reduce((first, last) -> last).orElseThrow(AssertionError::new);
            assertEquals(object, event.object); assertEquals(attributes, event.attributes); assertArrayEquals(tag, event.tag);
        }
        @Override public void connectionLost(String description) { failure = "Connection lost: " + description; }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { guard(); discovered = object; }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name, FederateHandle producer) { guard(); discovered = object; }
        @Override public void requestAttributeOwnershipAssumption(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) { record("assumption", object, attributes, tag); }
        @Override public void requestAttributeOwnershipRelease(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) { record("release", object, attributes, tag); }
        @Override public void requestDivestitureConfirmation(ObjectInstanceHandle object, AttributeHandleSet attributes) { record("confirm", object, attributes, null); }
        @Override public void attributeOwnershipAcquisitionNotification(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) { record("acquired", object, attributes, tag); }
        @Override public void attributeOwnershipUnavailable(ObjectInstanceHandle object, AttributeHandleSet attributes) { record("unavailable", object, attributes, null); }
        @Override public void confirmAttributeOwnershipAcquisitionCancellation(ObjectInstanceHandle object, AttributeHandleSet attributes) { record("cancelled", object, attributes, null); }
        @Override public void informAttributeOwnership(ObjectInstanceHandle object, AttributeHandle attribute, FederateHandle owner) { informedOwner = owner; record("owned", object, Set.of(attribute), null); }
        @Override public void attributeIsNotOwned(ObjectInstanceHandle object, AttributeHandle attribute) { record("unowned", object, Set.of(attribute), null); }
        @Override public void attributeIsOwnedByRTI(ObjectInstanceHandle object, AttributeHandle attribute) { record("rti-owned", object, Set.of(attribute), null); }
        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag, OrderType order, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            guard(); reflected = values.values().iterator().next().clone(); producer = info.hasProducingFederate() ? info.getProducingFederate() : null;
        }
    }
}
