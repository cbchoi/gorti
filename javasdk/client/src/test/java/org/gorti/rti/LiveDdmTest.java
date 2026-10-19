package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import hla.rti1516e.time.HLAfloat64TimeFactory;
import java.net.URL;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.function.BooleanSupplier;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;

/** Public API routing against the existing external Go server, with no private service calls. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
class LiveDdmTest {
    @Test @Timeout(120)
    void regionsRouteByAttributePairsAndCommittedBoundsAcrossThreeSessions() throws Throwable {
        String endpoint = System.getProperty("gorti.endpoint");
        String federation = "java-ddm-" + UUID.randomUUID();
        URL fom = LiveDdmTest.class.getResource("/ddm-fom.xml");
        assertNotNull(fom);
        Participant owner = new Participant();
        Participant hit = new Participant();
        Participant miss = new Participant();
        Participant[] participants = {owner, hit, miss};
        boolean created = false;
        Throwable original = null;
        try {
            for (Participant participant : participants) participant.connect(endpoint);
            owner.rti.createFederationExecution(federation, new URL[]{fom});
            created = true;
            for (Participant participant : participants) participant.join(federation);
            RTIambassador rti = owner.rti;
            DimensionHandle x = rti.getDimensionHandle("X");
            DimensionHandle y = rti.getDimensionHandle("Y");
            assertEquals("X", rti.getDimensionName(x));
            assertEquals(-1L, rti.getDimensionUpperBound(x));
            assertThrows(NameNotFound.class, () -> rti.getDimensionHandle("not-a-dimension"));
            ObjectClassHandle item = rti.getObjectClassHandle("HLAobjectRoot.RegionalItem");
            AttributeHandle first = rti.getAttributeHandle(item, "first");
            AttributeHandle second = rti.getAttributeHandle(item, "second");
            InteractionClassHandle pulse = rti.getInteractionClassHandle("HLAinteractionRoot.RegionalPulse");
            ParameterHandle payload = rti.getParameterHandle(pulse, "payload");
            assertEquals(dimensions(rti, x), rti.getAvailableDimensionsForClassAttribute(item, first));
            assertEquals(dimensions(rti, x), rti.getAvailableDimensionsForInteractionClass(pulse));
            rti.publishObjectClassAttributes(item, attributes(rti, first, second));
            rti.publishInteractionClass(pulse);

            RegionHandle near = region(rti, x, 0, 20);
            RegionHandle far = region(rti, x, 80, 100);
            RegionHandle hitNear = region(hit.rti, x, 0, 20);
            RegionHandle hitFar = region(hit.rti, x, 80, 100);
            RegionHandle missNear = region(miss.rti, x, 0, 20);
            RegionHandle missFar = region(miss.rti, x, 80, 100);
            RegionHandle wrongDimension = region(rti, y, 0, 20);
            assertEquals(dimensions(rti, x), rti.getDimensionHandleSet(near));
            assertThrows(RegionNotCreatedByThisFederate.class, () -> hit.rti.setRangeBounds(near, x, new RangeBounds(1, 2)));
            assertThrows(RegionDoesNotContainSpecifiedDimension.class, () -> rti.getRangeBounds(near, y));
            assertThrows(InvalidRangeBound.class, () -> rti.setRangeBounds(near, x, new RangeBounds(20, 20)));
            assertThrows(InvalidRegionContext.class, () -> rti.subscribeInteractionClassWithRegions(pulse, regions(rti, wrongDimension)));

            hit.rti.subscribeObjectClassAttributesWithRegions(item, pairs(hit.rti, first, hitNear, second, hitFar));
            miss.rti.subscribeObjectClassAttributesPassivelyWithRegions(item, pairs(miss.rti, first, missFar, second, missNear));
            assertThrows(RegionNotCreatedByThisFederate.class, () -> hit.rti.subscribeObjectClassAttributesWithRegions(
                    item, pairs(hit.rti, first, hitFar, second, far)));
            assertThrows(RegionNotCreatedByThisFederate.class, () -> hit.rti.unsubscribeObjectClassAttributesWithRegions(
                    item, pairs(hit.rti, first, hitNear, second, far)));
            hit.rti.subscribeInteractionClassWithRegions(pulse, regions(hit.rti, hitNear));
            miss.rti.subscribeInteractionClassPassivelyWithRegions(pulse, regions(miss.rti, missFar));
            ObjectInstanceHandle object = rti.registerObjectInstanceWithRegions(item, pairs(rti, first, near, second, far));
            assertEquals(item, rti.getKnownObjectClassHandle(object));
            assertEquals(object, rti.getObjectInstanceHandle(rti.getObjectInstanceName(object)));
            await(() -> hit.discovered.contains(object), participants);
            assertFalse(miss.discovered.contains(object));
            assertThrows(RegionInUseForUpdateOrSubscription.class, () -> rti.deleteRegion(near));

            AttributeHandleValueMap values = rti.getAttributeHandleValueMapFactory().create(2);
            values.put(first, new byte[]{0, 0, 0, 1}); values.put(second, new byte[]{0, 0, 0, 2});
            rti.updateAttributeValues(object, values, tag(1));
            barrier(owner, hit, miss, pulse, payload, near, far, 101);
            assertEquals(Set.of(first, second), hit.reflections.get(1));
            assertFalse(miss.reflections.containsKey(1));

            assertThrows(RegionNotCreatedByThisFederate.class, () -> rti.associateRegionsForUpdates(
                    object, pairs(rti, first, far, second, hitFar)));
            rti.updateAttributeValues(object, values, tag(10));
            barrier(owner, hit, miss, pulse, payload, near, far, 110);
            assertEquals(Set.of(first, second), hit.reflections.get(10));
            assertFalse(miss.reflections.containsKey(10), "Invalid later association must not add an earlier pair");
            rti.associateRegionsForUpdates(object, pair(rti, first, far));
            rti.associateRegionsForUpdates(object, pair(rti, first, far));
            rti.updateAttributeValues(object, values, tag(11));
            barrier(owner, hit, miss, pulse, payload, near, far, 111);
            assertEquals(Set.of(first, second), hit.reflections.get(11), "An added region must retain the first region");
            assertEquals(Set.of(first), miss.reflections.get(11));
            rti.unassociateRegionsForUpdates(object, rti.getAttributeSetRegionSetPairListFactory().create(0));
            rti.unassociateRegionsForUpdates(object, pair(rti, first, far));

            ParameterHandleValueMap params = rti.getParameterHandleValueMapFactory().create(1);
            params.put(payload, new byte[]{0, 0, 0, 7});
            rti.sendInteractionWithRegions(pulse, params, regions(rti, near), tag(2));
            barrier(owner, hit, miss, pulse, payload, near, far, 102);
            assertTrue(hit.received.contains(2)); assertFalse(miss.received.contains(2));
            rti.sendInteractionWithRegions(pulse, params, regions(rti, far), tag(3));
            barrier(owner, hit, miss, pulse, payload, near, far, 103);
            assertFalse(hit.received.contains(3)); assertTrue(miss.received.contains(3));

            assertThrows(RegionNotCreatedByThisFederate.class, () -> hit.rti.requestAttributeValueUpdateWithRegions(
                    item, pairs(hit.rti, first, hitNear, second, far), tag(14)));
            hit.rti.requestAttributeValueUpdateWithRegions(item, pairs(hit.rti, first, hitNear, second, hitFar), tag(4));
            await(() -> owner.provided.containsKey(4), participants);
            assertEquals(Set.of(first, second), owner.provided.get(4));
            assertEquals(1, owner.provideCalls.get(4), "Overlapping groups must coalesce per object/owner");
            assertFalse(owner.provided.containsKey(14), "Invalid later query must not emit earlier callbacks");
            AttributeSetRegionSetPairList emptyFirst = hit.rti.getAttributeSetRegionSetPairListFactory().create(2);
            emptyFirst.add(new AttributeRegionAssociation(attributes(hit.rti, first), regions(hit.rti)));
            emptyFirst.add(new AttributeRegionAssociation(attributes(hit.rti, second), regions(hit.rti, hitFar)));
            hit.rti.requestAttributeValueUpdateWithRegions(item, emptyFirst, tag(15));
            await(() -> owner.provided.containsKey(15), participants);
            assertEquals(Set.of(second), owner.provided.get(15));

            rti.setRangeBounds(near, x, new RangeBounds(80, 100));
            assertEquals(new RangeBounds(80, 100), rti.getRangeBounds(near, x));
            rti.sendInteractionWithRegions(pulse, params, regions(rti, near), tag(5));
            barrier(owner, hit, miss, pulse, payload, near, far, 105);
            assertTrue(hit.received.contains(5)); assertFalse(miss.received.contains(5));
            rti.commitRegionModifications(regions(rti, near));
            rti.sendInteractionWithRegions(pulse, params, regions(rti, near), tag(6));
            // A separate near region keeps the barrier visible to both subscribers after moving near.
            RegionHandle nearBarrier = region(rti, x, 0, 20);
            barrier(owner, hit, miss, pulse, payload, nearBarrier, far, 106);
            assertFalse(hit.received.contains(6)); assertTrue(miss.received.contains(6));
            rti.updateAttributeValues(object, values, tag(7));
            barrier(owner, hit, miss, pulse, payload, nearBarrier, far, 107);
            assertEquals(Set.of(second), hit.reflections.get(7));
            assertEquals(Set.of(first), miss.reflections.get(7));

            HLAfloat64TimeFactory time = (HLAfloat64TimeFactory) rti.getTimeFactory();
            rti.enableTimeRegulation(time.makeInterval(1));
            await(() -> owner.regulating, participants);
            MessageRetractionReturn ordinary = rti.sendInteraction(pulse, params, tag(8), time.makeTime(10));
            MessageRetractionReturn regional = rti.sendInteractionWithRegions(pulse, params, regions(rti, far), tag(9), time.makeTime(11));
            assertTrue(ordinary.retractionHandleIsValid); assertTrue(regional.retractionHandleIsValid);
            assertNotEquals(ordinary.handle, regional.handle);

            rti.unassociateRegionsForUpdates(object, pairs(rti, first, near, second, far));
            rti.associateRegionsForUpdates(object, pairs(rti, first, nearBarrier, second, far));
            hit.rti.unsubscribeObjectClassAttributesWithRegions(item, pairs(hit.rti, first, hitNear, second, hitFar));
            miss.rti.unsubscribeInteractionClassWithRegions(pulse, regions(miss.rti, missFar));
            rti.deleteRegion(near);
            rti.deleteRegion(wrongDimension);
            assertThrows(InvalidRegion.class, () -> rti.getDimensionHandleSet(near));
        } catch (Throwable failure) { original = failure; throw failure; }
        finally {
            List<Throwable> failures = new ArrayList<>();
            for (Participant participant : participants) cleanup(participant::resign, failures);
            if (created) cleanup(() -> owner.rti.destroyFederationExecution(federation), failures);
            for (Participant participant : participants) cleanup(participant::disconnect, failures);
            if (original != null) for (Throwable failure : failures) original.addSuppressed(failure);
            else if (!failures.isEmpty()) { AssertionError failure = new AssertionError("DDM cleanup failed"); failures.forEach(failure::addSuppressed); throw failure; }
        }
    }

    @Test @Timeout(90)
    void registrationEmptyProductsUseDefaultRegionsAndLateFailuresKeepReservation() throws Throwable {
        try (TwoFederates fixture = new TwoFederates()) {
            Participant owner = fixture.sender;
            Participant receiver = fixture.receiver;
            RTIambassador rti = owner.rti;
            DimensionHandle x = rti.getDimensionHandle("X");
            DimensionHandle y = rti.getDimensionHandle("Y");
            ObjectClassHandle item = rti.getObjectClassHandle("HLAobjectRoot.RegionalItem");
            AttributeHandle first = rti.getAttributeHandle(item, "first");
            AttributeHandle second = rti.getAttributeHandle(item, "second");
            rti.publishObjectClassAttributes(item, attributes(rti, first));
            RegionHandle near = region(rti, x, 0, 20);
            RegionHandle orphan = region(rti, y, 0, 20);
            RegionHandle far = region(receiver.rti, x, 80, 100);
            RegionHandle deleted = region(rti, x, 0, 20);
            rti.deleteRegion(deleted);
            receiver.rti.subscribeObjectClassAttributesWithRegions(item, pairs(receiver.rti, first, far, second, far));

            ObjectInstanceHandle empty = rti.registerObjectInstanceWithRegions(item, rti.getAttributeSetRegionSetPairListFactory().create(0));
            AttributeSetRegionSetPairList emptyProducts = rti.getAttributeSetRegionSetPairListFactory().create(3);
            emptyProducts.add(new AttributeRegionAssociation(attributes(rti, first), regions(rti)));
            emptyProducts.add(new AttributeRegionAssociation(attributes(rti), regions(rti, orphan)));
            emptyProducts.add(new AttributeRegionAssociation(attributes(rti), regions(rti)));
            ObjectInstanceHandle defaulted = rti.registerObjectInstanceWithRegions(item, emptyProducts);
            await(() -> receiver.discovered.contains(empty) && receiver.discovered.contains(defaulted), owner, receiver);
            rti.deleteRegion(orphan);
            AttributeHandleValueMap values = rti.getAttributeHandleValueMapFactory().create(2);
            values.put(first, new byte[]{0, 0, 0, 1});
            rti.updateAttributeValues(empty, values, tag(21));
            rti.updateAttributeValues(defaulted, values, tag(22));
            await(() -> receiver.reflections.containsKey(21) && receiver.reflections.containsKey(22), owner, receiver);
            assertEquals(Set.of(first), receiver.reflections.get(21));
            assertEquals(Set.of(first), receiver.reflections.get(22));

            AttributeSetRegionSetPairList mixed = pair(rti, first, near);
            mixed.add(new AttributeRegionAssociation(attributes(rti, second), regions(rti)));
            assertThrows(AttributeNotPublished.class, () -> rti.registerObjectInstanceWithRegions(item, mixed));
            rti.publishObjectClassAttributes(item, attributes(rti, second));
            String name = "ddm-empty-" + UUID.randomUUID();
            rti.reserveObjectInstanceName(name);
            await(() -> owner.reserved.contains(name), owner, receiver);
            int beforeDiscoveries = receiver.discovered.size();
            AttributeSetRegionSetPairList invalid = pair(rti, first, near);
            invalid.add(new AttributeRegionAssociation(attributes(rti), regions(rti, deleted)));
            assertThrows(InvalidRegion.class, () -> rti.registerObjectInstanceWithRegions(item, invalid, name));
            assertThrows(ObjectInstanceNotKnown.class, () -> rti.getObjectInstanceHandle(name));
            AttributeSetRegionSetPairList foreign = pair(rti, first, near);
            foreign.add(new AttributeRegionAssociation(attributes(rti), regions(rti, far)));
            assertThrows(RegionNotCreatedByThisFederate.class, () -> rti.registerObjectInstanceWithRegions(item, foreign, name));
            ObjectInstanceHandle registered = rti.registerObjectInstanceWithRegions(item, mixed, name);
            await(() -> receiver.discovered.contains(registered), owner, receiver);
            assertEquals(beforeDiscoveries + 1, receiver.discovered.size(), "Failed calls must not discover partial objects");
            assertEquals(name, rti.getObjectInstanceName(registered));
            values.put(second, new byte[]{0, 0, 0, 2});
            rti.updateAttributeValues(registered, values, tag(23));
            await(() -> receiver.reflections.containsKey(23), owner, receiver);
            assertEquals(Set.of(second), receiver.reflections.get(23), "Only the empty region set should retain default routing");
        }
    }

    @Test @Timeout(90)
    void twoFederatesSeeOverlapChangesOnlyAfterSelectedRegionCommit() throws Throwable {
        try (TwoFederates fixture = new TwoFederates()) {
            Participant sender = fixture.sender;
            Participant receiver = fixture.receiver;
            RTIambassador rti = sender.rti;
            DimensionHandle x = rti.getDimensionHandle("X");
            InteractionClassHandle pulse = rti.getInteractionClassHandle("HLAinteractionRoot.RegionalPulse");
            ParameterHandle payload = rti.getParameterHandle(pulse, "payload");
            rti.publishInteractionClass(pulse);
            RegionHandle sent = region(rti, x, 0, 20);
            RegionHandle barrier = region(rti, x, 0, 20);
            RegionHandle subscribed = region(receiver.rti, x, 0, 20);
            receiver.rti.subscribeInteractionClassWithRegions(pulse, regions(receiver.rti, subscribed));
            ParameterHandleValueMap values = rti.getParameterHandleValueMapFactory().create(1);
            values.put(payload, new byte[]{0, 0, 0, 1});

            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(1));
            await(() -> receiver.received.contains(1), sender, receiver);
            rti.setRangeBounds(sent, x, new RangeBounds(80, 100));
            assertEquals(new RangeBounds(80, 100), rti.getRangeBounds(sent, x));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(2));
            await(() -> receiver.received.contains(2), sender, receiver);

            assertThrows(RegionNotCreatedByThisFederate.class,
                    () -> rti.commitRegionModifications(regions(rti, sent, subscribed)));
            assertEquals(new RangeBounds(80, 100), rti.getRangeBounds(sent, x));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(3));
            await(() -> receiver.received.contains(3), sender, receiver);

            rti.commitRegionModifications(regions(rti, barrier));
            assertEquals(new RangeBounds(80, 100), rti.getRangeBounds(sent, x));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(4));
            await(() -> receiver.received.contains(4), sender, receiver);
            rti.commitRegionModifications(regions(rti, sent));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(5));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, barrier), tag(105));
            await(() -> receiver.received.contains(105), sender, receiver);
            assertFalse(receiver.received.contains(5), "Committed non-overlap must suppress delivery");

            receiver.rti.setRangeBounds(subscribed, x, new RangeBounds(80, 100));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(6));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, barrier), tag(106));
            await(() -> receiver.received.contains(106), sender, receiver);
            assertFalse(receiver.received.contains(6), "Staged subscription bounds must not change routing");
            receiver.rti.commitRegionModifications(regions(receiver.rti, subscribed));
            rti.sendInteractionWithRegions(pulse, values, regions(rti, sent), tag(7));
            await(() -> receiver.received.contains(7), sender, receiver);

            receiver.rti.unsubscribeInteractionClassWithRegions(pulse, regions(receiver.rti, subscribed));
            receiver.rti.deleteRegion(subscribed);
            rti.deleteRegion(sent);
            rti.deleteRegion(barrier);
        }
    }

    private static void barrier(Participant owner, Participant hit, Participant miss, InteractionClassHandle pulse,
            ParameterHandle payload, RegionHandle near, RegionHandle far, int tag) throws Exception {
        ParameterHandleValueMap parameters = owner.rti.getParameterHandleValueMapFactory().create(1);
        parameters.put(payload, new byte[]{0, 0, 0, 0});
        owner.rti.sendInteractionWithRegions(pulse, parameters, regions(owner.rti, near, far), tag(tag));
        await(() -> hit.received.contains(tag) && miss.received.contains(tag), owner, hit, miss);
    }

    private static RegionHandle region(RTIambassador rti, DimensionHandle dimension, long lower, long upper) throws Exception {
        RegionHandle region = rti.createRegion(dimensions(rti, dimension));
        rti.setRangeBounds(region, dimension, new RangeBounds(lower, upper));
        rti.commitRegionModifications(regions(rti, region));
        return region;
    }
    private static AttributeSetRegionSetPairList pairs(RTIambassador rti, AttributeHandle first, RegionHandle firstRegion,
            AttributeHandle second, RegionHandle secondRegion) throws Exception {
        AttributeSetRegionSetPairList result = rti.getAttributeSetRegionSetPairListFactory().create(2);
        result.add(new AttributeRegionAssociation(attributes(rti, first), regions(rti, firstRegion)));
        result.add(new AttributeRegionAssociation(attributes(rti, second), regions(rti, secondRegion)));
        return result;
    }
    private static AttributeSetRegionSetPairList pair(RTIambassador rti, AttributeHandle attribute, RegionHandle region) throws Exception {
        AttributeSetRegionSetPairList result = rti.getAttributeSetRegionSetPairListFactory().create(1);
        result.add(new AttributeRegionAssociation(attributes(rti, attribute), regions(rti, region)));
        return result;
    }
    private static RegionHandleSet regions(RTIambassador rti, RegionHandle... handles) throws Exception {
        RegionHandleSet result = rti.getRegionHandleSetFactory().create();
        for (RegionHandle handle : handles) result.add(handle);
        return result;
    }
    private static DimensionHandleSet dimensions(RTIambassador rti, DimensionHandle... handles) throws Exception {
        DimensionHandleSet result = rti.getDimensionHandleSetFactory().create();
        for (DimensionHandle handle : handles) result.add(handle);
        return result;
    }
    private static AttributeHandleSet attributes(RTIambassador rti, AttributeHandle... handles) throws Exception {
        AttributeHandleSet result = rti.getAttributeHandleSetFactory().create();
        for (AttributeHandle handle : handles) result.add(handle);
        return result;
    }
    private static byte[] tag(int value) { return new byte[]{(byte) value}; }
    private static void await(BooleanSupplier ready, Participant... participants) throws Exception {
        long deadline = System.nanoTime() + 10_000_000_000L;
        while (!ready.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant participant : participants) participant.rti.evokeMultipleCallbacks(0.01, 0.03);
        }
        assertTrue(ready.getAsBoolean(), "Expected regional callback did not arrive");
    }
    private static void cleanup(Cleanup action, List<Throwable> failures) {
        try { action.run(); } catch (Throwable failure) { failures.add(failure); }
    }
    @FunctionalInterface private interface Cleanup { void run() throws Exception; }

    private static final class TwoFederates implements AutoCloseable {
        final Participant sender = new Participant();
        final Participant receiver = new Participant();
        final String federation = "java-ddm-commit-" + UUID.randomUUID();
        boolean created;
        TwoFederates() throws Exception {
            try {
                String endpoint = System.getProperty("gorti.endpoint");
                sender.connect(endpoint); receiver.connect(endpoint);
                URL fom = LiveDdmTest.class.getResource("/ddm-fom.xml");
                assertNotNull(fom);
                sender.rti.createFederationExecution(federation, new URL[]{fom}); created = true;
                sender.join(federation); receiver.join(federation);
            } catch (Throwable failure) {
                try { close(); } catch (Throwable cleanup) { failure.addSuppressed(cleanup); }
                throw failure;
            }
        }
        public void close() throws Exception {
            List<Throwable> failures = new ArrayList<>();
            cleanup(sender::resign, failures); cleanup(receiver::resign, failures);
            if (created) cleanup(() -> sender.rti.destroyFederationExecution(federation), failures);
            cleanup(sender::disconnect, failures); cleanup(receiver::disconnect, failures);
            if (!failures.isEmpty()) {
                Exception failure = new Exception("Two-federate DDM cleanup failed");
                failures.forEach(failure::addSuppressed); throw failure;
            }
        }
    }

    private static final class Participant extends NullFederateAmbassador {
        final RTIambassador rti;
        final List<ObjectInstanceHandle> discovered = new ArrayList<>();
        final List<Integer> received = new ArrayList<>();
        final Map<Integer, Set<AttributeHandle>> reflections = new HashMap<>();
        final Map<Integer, Set<AttributeHandle>> provided = new HashMap<>();
        final Map<Integer, Integer> provideCalls = new HashMap<>();
        final Set<String> reserved = new java.util.HashSet<>();
        boolean connected;
        boolean joined;
        boolean regulating;
        Participant() throws Exception { rti = RtiFactoryFactory.getRtiFactory().getRtiAmbassador(); }
        void connect(String endpoint) throws Exception { rti.connect(this, CallbackModel.HLA_EVOKED, endpoint); connected = true; }
        void join(String federation) throws Exception { rti.joinFederationExecution("ddm-" + UUID.randomUUID(), "java-ddm", federation); joined = true; }
        void resign() throws Exception { if (joined) { rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); joined = false; } }
        void disconnect() throws Exception { if (connected) { rti.disconnect(); connected = false; } }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { discovered.add(object); }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name, FederateHandle producer) { discovered.add(object); }
        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag,
                OrderType sent, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            // Distinct publisher-region sets can produce separate callbacks for one update.
            reflections.computeIfAbsent(tag[0] & 255, ignored -> new java.util.HashSet<>()).addAll(values.keySet());
        }
        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values, byte[] tag,
                OrderType sent, TransportationTypeHandle transport, SupplementalReceiveInfo info) { received.add(tag[0] & 255); }
        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values, byte[] tag,
                OrderType sent, TransportationTypeHandle transport, LogicalTime time, OrderType receivedOrder, SupplementalReceiveInfo info) { received.add(tag[0] & 255); }
        @Override public void receiveInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values, byte[] tag,
                OrderType sent, TransportationTypeHandle transport, LogicalTime time, OrderType receivedOrder, MessageRetractionHandle handle,
                SupplementalReceiveInfo info) { received.add(tag[0] & 255); }
        @Override public void provideAttributeValueUpdate(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) {
            provideCalls.merge(tag[0] & 255, 1, Integer::sum);
            provided.computeIfAbsent(tag[0] & 255, ignored -> new java.util.HashSet<>()).addAll(attributes);
        }
        @Override public void timeRegulationEnabled(LogicalTime time) { regulating = true; }
        @Override public void objectInstanceNameReservationSucceeded(String name) { reserved.add(name); }
    }
}
