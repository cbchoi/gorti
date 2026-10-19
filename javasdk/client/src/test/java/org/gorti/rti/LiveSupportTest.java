package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.Channel;
import io.grpc.StatusRuntimeException;
import java.net.URL;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.function.BooleanSupplier;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfSystemProperty;
import rti.v1.Common;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

/** Real authenticated GoRTI sessions; adapter coverage can run before facade integration. */
@EnabledIfSystemProperty(named = "gorti.endpoint", matches = ".+")
class LiveSupportTest {
    @Test @Timeout(90)
    void adaptersDriveAdvisoryCallbacksAndServerOwnedSupportState() throws Throwable {
        String federation = "java-support-" + UUID.randomUUID();
        Participant owner = new Participant();
        Participant peer = new Participant();
        boolean created = false;
        Throwable original = null;
        try {
            for (Participant p : List.of(owner, peer)) {
                assertThrows(NotConnected.class, () -> p.support.getUpdateRateValue("fast"));
                p.connect();
                assertThrows(FederateNotExecutionMember.class, p.support::getAutomaticResignDirective);
            }
            owner.core.createFederationExecution(federation, new URL[]{fom()});
            created = true;
            owner.join(federation); peer.join(federation);
            assertEquals(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST, owner.support.getAutomaticResignDirective());
            for (ResignAction action : ResignAction.values()) {
                owner.support.setAutomaticResignDirective(action);
                assertEquals(action, owner.support.getAutomaticResignDirective());
                assertEquals(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST, peer.support.getAutomaticResignDirective());
            }
            owner.support.setAutomaticResignDirective(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            assertEquals(2.5, owner.support.getUpdateRateValue("slow"));
            assertEquals(20, peer.support.getUpdateRateValue("fast"));
            assertThrows(InvalidUpdateRateDesignator.class, () -> owner.support.getUpdateRateValue("missing"));
            assertThrows(InvalidResignAction.class, () -> owner.support.setAutomaticResignDirective(null));
            Set<Long> groups = new HashSet<>();
            for (ServiceGroup group : ServiceGroup.values()) {
                long normalized = owner.support.normalizeServiceGroup(group);
                assertTrue(groups.add(normalized));
                assertEquals(normalized, peer.support.normalizeServiceGroup(group));
            }
            long ownerValue = owner.support.normalizeFederateHandle(owner.handle);
            long peerValue = owner.support.normalizeFederateHandle(peer.handle);
            assertNotEquals(ownerValue, peerValue);
            assertEquals(ownerValue, peer.support.normalizeFederateHandle(owner.handle));
            assertThrows(InvalidFederateHandle.class, () -> owner.support.normalizeFederateHandle(Handles.federate(Long.MAX_VALUE)));

            assertThrows(ObjectClassRelevanceAdvisorySwitchIsOff.class, owner.support::disableObjectClassRelevanceAdvisorySwitch);
            assertThrows(AttributeRelevanceAdvisorySwitchIsOff.class, owner.support::disableAttributeRelevanceAdvisorySwitch);
            assertThrows(InteractionRelevanceAdvisorySwitchIsOff.class, owner.support::disableInteractionRelevanceAdvisorySwitch);
            assertThrows(AttributeScopeAdvisorySwitchIsOff.class, peer.support::disableAttributeScopeAdvisorySwitch);
            owner.support.enableObjectClassRelevanceAdvisorySwitch();
            owner.support.enableAttributeRelevanceAdvisorySwitch();
            owner.support.enableInteractionRelevanceAdvisorySwitch();
            peer.support.enableAttributeScopeAdvisorySwitch();
            assertThrows(ObjectClassRelevanceAdvisorySwitchIsOn.class, owner.support::enableObjectClassRelevanceAdvisorySwitch);
            assertThrows(AttributeRelevanceAdvisorySwitchIsOn.class, owner.support::enableAttributeRelevanceAdvisorySwitch);
            assertThrows(InteractionRelevanceAdvisorySwitchIsOn.class, owner.support::enableInteractionRelevanceAdvisorySwitch);
            assertThrows(AttributeScopeAdvisorySwitchIsOn.class, peer.support::enableAttributeScopeAdvisorySwitch);

            ObjectClassHandle type = owner.exchange.getObjectClassHandle("HLAobjectRoot.SupportAsset");
            AttributeHandle attribute = owner.exchange.getAttributeHandle(type, "value");
            InteractionClassHandle pulse = owner.exchange.getInteractionClassHandle("HLAinteractionRoot.SupportPulse");
            AttributeHandleSet attributes = HandleCollections.attributeSetFactory().create();
            attributes.add(attribute);
            for (Participant p : List.of(owner, peer)) {
                p.callbacks.expectedAttributes = Set.of(attribute);
                p.callbacks.expectedClass = type;
                p.callbacks.expectedInteraction = pulse;
            }
            owner.exchange.publishObjectClassAttributes(type, attributes);
            owner.exchange.publishInteractionClass(pulse);
            peer.exchange.subscribeObjectClassAttributes(type, attributes, "fast");
            peer.exchange.subscribeInteractionClass(pulse);
            await(() -> owner.callbacks.count("start") == 1 && owner.callbacks.count("interactions-on") == 1, owner, peer);
            ObjectInstanceHandle object = owner.exchange.registerObjectInstance(type);
            owner.callbacks.expectedObject = object;
            peer.callbacks.expectedObject = object;
            assertThrows(ObjectInstanceNotKnown.class, () -> peer.support.getUpdateRateValueForAttribute(object, attribute));
            await(() -> object.equals(peer.callbacks.discovered) && owner.callbacks.count("updates-on:fast") == 1, owner, peer);
            assertEquals(20, owner.support.getUpdateRateValueForAttribute(object, attribute));
            assertEquals(20, peer.support.getUpdateRateValueForAttribute(object, attribute));
            assertThrows(AttributeNotDefined.class, () -> owner.support.getUpdateRateValueForAttribute(object, Handles.attribute(Long.MAX_VALUE)));
            AttributeHandleValueMap values = HandleCollections.attributeValueMapFactory().create(1);
            values.put(attribute, new byte[]{0, 0, 0, 1});
            owner.exchange.updateAttributeValues(object, values, new byte[0]);
            await(() -> peer.callbacks.count("reflect") == 1, owner, peer);

            peer.exchange.subscribeObjectClassAttributes(type, attributes, "slow");
            await(() -> owner.callbacks.count("updates-on:slow") == 1, owner, peer);
            assertEquals(2.5, owner.support.getUpdateRateValueForAttribute(object, attribute));
            peer.exchange.unsubscribeObjectClass(type);
            peer.exchange.unsubscribeInteractionClass(pulse);
            owner.exchange.updateAttributeValues(object, values, new byte[0]);
            await(() -> owner.callbacks.count("stop") == 1 && owner.callbacks.count("updates-off") == 1
                    && owner.callbacks.count("interactions-off") == 1, owner, peer);

            owner.support.disableObjectClassRelevanceAdvisorySwitch();
            owner.support.disableAttributeRelevanceAdvisorySwitch();
            owner.support.disableInteractionRelevanceAdvisorySwitch();
            peer.support.disableAttributeScopeAdvisorySwitch();
            int ownerCount = owner.callbacks.events.size();
            int peerScopeCount = peer.callbacks.count("scope-in") + peer.callbacks.count("scope-out");
            peer.exchange.subscribeObjectClassAttributes(type, attributes, "fast");
            peer.exchange.subscribeInteractionClass(pulse);
            owner.exchange.updateAttributeValues(object, values, new byte[0]);
            await(() -> peer.callbacks.count("reflect") == 2, owner, peer);
            peer.exchange.unsubscribeObjectClass(type);
            peer.exchange.unsubscribeInteractionClass(pulse);
            owner.exchange.updateAttributeValues(object, values, new byte[0]);
            barrier(owner, peer);
            assertEquals(ownerCount, owner.callbacks.events.size(), "Disabled publisher switches must suppress callbacks");
            assertEquals(peerScopeCount, peer.callbacks.count("scope-in") + peer.callbacks.count("scope-out"));

            Channel stale = peer.core.channel();
            long departed = peer.core.federate();
            FederateHandle oldHandle = peer.handle;
            peer.resign();
            assertThrows(FederateNotExecutionMember.class, () -> peer.support.getUpdateRateValue("fast"));
            assertThrows(InvalidFederateHandle.class, () -> owner.support.normalizeFederateHandle(oldHandle));
            peer.join(federation);
            assertNotEquals(oldHandle, peer.handle);
            assertThrows(StatusRuntimeException.class, () -> SupportServiceGrpc.newBlockingStub(stale).normalizeServiceGroup(
                    Support.NormalizeServiceGroupRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                            .setFederationName(federation).setFederateHandle(departed).setServiceGroup(0).build()));
            assertEquals(20, peer.support.getUpdateRateValue("fast"));
        } catch (Throwable failure) {
            original = failure;
            throw failure;
        } finally {
            List<Throwable> errors = new ArrayList<>();
            cleanup(peer::resign, errors); cleanup(owner::resign, errors);
            if (created) cleanup(() -> owner.core.destroyFederationExecution(federation), errors);
            cleanup(peer.core::disconnect, errors); cleanup(owner.core::disconnect, errors);
            if (original != null) errors.forEach(original::addSuppressed);
            else if (!errors.isEmpty()) {
                AssertionError failure = new AssertionError("Support test cleanup failed");
                errors.forEach(failure::addSuppressed);
                throw failure;
            }
        }
    }

    @Test @Timeout(60)
    void regionalScopeSwitchControlsInAndOutCallbacks() throws Exception { verifyScope(true); }

    @Test @Timeout(60)
    void ordinarySubscriptionsAlsoReceiveScopeAdvisories() throws Exception { verifyScope(false); }

    @Test @Timeout(60)
    void supportQueriesRemainAvailableDuringSaveWhileSwitchChangesAreRejected() throws Exception {
        verifyCheckpointQueries(false);
    }

    @Test @Timeout(60)
    void supportQueriesRemainAvailableDuringRestoreWhileSwitchChangesAreRejected() throws Exception {
        verifyCheckpointQueries(true);
    }

    private static void verifyCheckpointQueries(boolean restore) throws Exception {
        String federation = "java-support-save-" + UUID.randomUUID();
        Participant owner = new Participant();
        Participant peer = new Participant();
        boolean created = false;
        boolean saving = false;
        boolean restoring = false;
        try {
            owner.connect(); peer.connect();
            owner.core.createFederationExecution(federation, new URL[]{fom()});
            created = true;
            owner.join(federation); peer.join(federation);
            ObjectClassHandle type = owner.exchange.getObjectClassHandle("HLAobjectRoot.SupportAsset");
            AttributeHandle attribute = owner.exchange.getAttributeHandle(type, "value");
            AttributeHandleSet attributes = HandleCollections.attributeSetFactory().create();
            attributes.add(attribute);
            peer.callbacks.expectedClass = type;
            owner.exchange.publishObjectClassAttributes(type, attributes);
            peer.exchange.subscribeObjectClassAttributes(type, attributes, "fast");
            ObjectInstanceHandle object = owner.exchange.registerObjectInstance(type);
            await(() -> object.equals(peer.callbacks.discovered), owner, peer);
            List<Callable<Object>> lookups = checkpointLookups(owner, type, attribute, object);
            List<Object> expectedLookups = new ArrayList<>();
            for (Callable<Object> lookup : lookups) expectedLookups.add(lookup.call());
            DimensionHandle dimension = owner.core.ddm.getDimensionHandle("X");
            RegionHandle region = region(owner, dimension);
            owner.core.saveRestore.requestFederationSave("support-save");
            saving = true;
            await(() -> owner.callbacks.saving && peer.callbacks.saving, owner, peer);
            if (restore) {
                owner.core.saveRestore.federateSaveBegun(); peer.core.saveRestore.federateSaveBegun();
                owner.core.saveRestore.federateSaveComplete(); peer.core.saveRestore.federateSaveComplete();
                await(() -> owner.callbacks.saved && peer.callbacks.saved, owner, peer);
                saving = false;
                owner.core.saveRestore.requestFederationRestore("support-save");
                restoring = true;
                await(() -> owner.callbacks.restoring && peer.callbacks.restoring, owner, peer);
                assertThrows(RestoreInProgress.class, owner.support::enableObjectClassRelevanceAdvisorySwitch);
            } else {
                assertThrows(SaveInProgress.class, owner.support::enableObjectClassRelevanceAdvisorySwitch);
            }
            assertAll("Support query/directive methods remain available during checkpoints",
                    () -> assertEquals(20, owner.support.getUpdateRateValue("fast")),
                    () -> assertEquals(20, owner.support.getUpdateRateValueForAttribute(object, attribute)),
                    () -> assertEquals(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST, owner.support.getAutomaticResignDirective()),
                    () -> owner.support.setAutomaticResignDirective(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST),
                    () -> assertTrue(owner.support.normalizeFederateHandle(owner.handle) >= 0),
                    () -> assertTrue(owner.support.normalizeServiceGroup(ServiceGroup.SUPPORT_SERVICES) >= 0));
            assertAll("Legal lookup guards during checkpoints", java.util.stream.IntStream.range(0, lookups.size())
                    .mapToObj(index -> (org.junit.jupiter.api.function.Executable) () ->
                            assertEquals(expectedLookups.get(index), lookups.get(index).call(), "lookup " + index)));
            Class<? extends RTIexception> blocked = restore ? RestoreInProgress.class : SaveInProgress.class;
            assertThrows(blocked, () -> owner.core.ddm.getDimensionHandleSet(region));
            assertThrows(blocked, () -> owner.core.ddm.getRangeBounds(region, dimension));
        } finally {
            try {
                if (saving) owner.core.saveRestore.abortFederationSave();
                if (restoring) {
                    owner.core.saveRestore.abortFederationRestore();
                    await(() -> owner.callbacks.restoreAborted && peer.callbacks.restoreAborted, owner, peer);
                }
            }
            finally {
                try { peer.resign(); }
                finally {
                    try { owner.resign(); }
                    finally {
                        try { if (created) owner.core.destroyFederationExecution(federation); }
                        finally { try { peer.core.disconnect(); } finally { owner.core.disconnect(); } }
                    }
                }
            }
        }
    }

    private static List<Callable<Object>> checkpointLookups(Participant owner, ObjectClassHandle type,
            AttributeHandle attribute, ObjectInstanceHandle object) throws Exception {
        ExchangeServices exchange = owner.exchange;
        DdmServices ddm = owner.core.ddm;
        String federateName = exchange.getFederateName(owner.handle);
        String objectName = exchange.getObjectInstanceName(object);
        InteractionClassHandle interaction = exchange.getInteractionClassHandle("HLAinteractionRoot.SupportPulse");
        ParameterHandle parameter = exchange.getParameterHandle(interaction, "stamp");
        DimensionHandle dimension = ddm.getDimensionHandle("X");
        TransportationTypeHandle transportation = exchange.getTransportationTypeHandle("HLAreliable");
        return List.of(
                () -> exchange.getFederateHandle(federateName), () -> exchange.getFederateName(owner.handle),
                () -> exchange.getObjectClassHandle("HLAobjectRoot.SupportAsset"), () -> exchange.getObjectClassName(type),
                () -> exchange.getAttributeHandle(type, "value"), () -> exchange.getAttributeName(type, attribute),
                () -> exchange.getInteractionClassHandle("HLAinteractionRoot.SupportPulse"), () -> exchange.getInteractionClassName(interaction),
                () -> exchange.getParameterHandle(interaction, "stamp"), () -> exchange.getParameterName(interaction, parameter),
                () -> exchange.getObjectInstanceHandle(objectName), () -> exchange.getObjectInstanceName(object),
                () -> exchange.getKnownObjectClassHandle(object),
                () -> exchange.getOrderType("Receive"), () -> exchange.getOrderName(OrderType.RECEIVE),
                () -> exchange.getTransportationTypeHandle("HLAreliable"), () -> exchange.getTransportationTypeName(transportation),
                () -> ddm.getDimensionHandle("X"), () -> ddm.getDimensionName(dimension), () -> ddm.getDimensionUpperBound(dimension),
                () -> ddm.getAvailableDimensionsForClassAttribute(type, attribute),
                () -> ddm.getAvailableDimensionsForInteractionClass(interaction));
    }

    private static void verifyScope(boolean regional) throws Exception {
        String federation = "java-support-scope-" + UUID.randomUUID();
        Participant owner = new Participant();
        Participant peer = new Participant();
        boolean created = false;
        try {
            owner.connect(); peer.connect();
            owner.core.createFederationExecution(federation, new URL[]{fom()});
            created = true;
            owner.join(federation); peer.join(federation);
            ObjectClassHandle type = owner.exchange.getObjectClassHandle("HLAobjectRoot.SupportAsset");
            AttributeHandle attribute = owner.exchange.getAttributeHandle(type, "value");
            AttributeHandleSet attributes = HandleCollections.attributeSetFactory().create();
            attributes.add(attribute);
            peer.callbacks.expectedAttributes = Set.of(attribute);
            peer.callbacks.expectedClass = type;
            owner.exchange.publishObjectClassAttributes(type, attributes);
            peer.support.enableAttributeScopeAdvisorySwitch();
            DimensionHandle dimension = owner.core.ddm.getDimensionHandle("X");
            RegionHandle subscription = null;
            if (regional) {
                subscription = region(peer, dimension);
                peer.core.ddm.subscribeObjectClassAttributesWithRegions(type, association(attributes, subscription), "fast");
            } else {
                peer.exchange.subscribeObjectClassAttributes(type, attributes, "fast");
            }
            ObjectInstanceHandle object = regional
                    ? owner.core.ddm.registerObjectInstanceWithRegions(type, association(attributes, region(owner, dimension)))
                    : owner.exchange.registerObjectInstance(type);
            peer.callbacks.expectedObject = object;
            AttributeHandleValueMap values = HandleCollections.attributeValueMapFactory().create(1);
            values.put(attribute, new byte[]{0, 0, 0, 1});
            owner.exchange.updateAttributeValues(object, values, new byte[0]);
            await(() -> object.equals(peer.callbacks.discovered)
                    && peer.callbacks.count("scope-in") == 1 && peer.callbacks.count("reflect") == 1, owner, peer);
            if (regional) {
                bounds(peer, subscription, dimension, 80, 100);
                owner.exchange.updateAttributeValues(object, values, new byte[0]);
                await(() -> peer.callbacks.count("scope-out") == 1, owner, peer);
                assertEquals(1, peer.callbacks.count("reflect"));
                peer.support.disableAttributeScopeAdvisorySwitch();
                bounds(peer, subscription, dimension, 0, 20);
                owner.exchange.updateAttributeValues(object, values, new byte[0]);
                await(() -> peer.callbacks.count("reflect") == 2, owner, peer);
                bounds(peer, subscription, dimension, 80, 100);
                owner.exchange.updateAttributeValues(object, values, new byte[0]);
                barrier(owner, peer);
                assertEquals(1, peer.callbacks.count("scope-in"));
                assertEquals(1, peer.callbacks.count("scope-out"));
                peer.support.enableAttributeScopeAdvisorySwitch();
                bounds(peer, subscription, dimension, 0, 20);
                owner.exchange.updateAttributeValues(object, values, new byte[0]);
                await(() -> peer.callbacks.count("scope-in") == 2 && peer.callbacks.count("reflect") == 3, owner, peer);
            }
        } finally {
            try { peer.resign(); }
            finally {
                try { owner.resign(); }
                finally {
                    try { if (created) owner.core.destroyFederationExecution(federation); }
                    finally { try { peer.core.disconnect(); } finally { owner.core.disconnect(); } }
                }
            }
        }
    }

    private static AttributeSetRegionSetPairList association(AttributeHandleSet attributes, RegionHandle region) {
        RegionHandleSet regions = HandleCollections.regionSetFactory().create();
        regions.add(region);
        AttributeSetRegionSetPairList pairs = HandleCollections.attributeRegionListFactory().create(1);
        pairs.add(new AttributeRegionAssociation(attributes, regions));
        return pairs;
    }
    private static RegionHandle region(Participant p, DimensionHandle dimension) throws Exception {
        DimensionHandleSet dimensions = HandleCollections.dimensionSetFactory().create();
        dimensions.add(dimension);
        RegionHandle region = p.core.ddm.createRegion(dimensions);
        bounds(p, region, dimension, 0, 20);
        return region;
    }
    private static void bounds(Participant p, RegionHandle region, DimensionHandle dimension, long lower, long upper) throws Exception {
        p.core.ddm.setRangeBounds(region, dimension, new RangeBounds(lower, upper));
        RegionHandleSet regions = HandleCollections.regionSetFactory().create();
        regions.add(region);
        p.core.ddm.commitRegionModifications(regions);
    }

    @Test @Timeout(60)
    void publicFacadeRoutesAllFourteenSupportMethods() throws Exception {
        RTIambassador rti = RtiFactoryFactory.getRtiFactory("gorti").getRtiAmbassador();
        String federation = "java-support-facade-" + UUID.randomUUID();
        boolean created = false;
        boolean joined = false;
        try {
            rti.connect(new NullFederateAmbassador(), CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint"));
            rti.createFederationExecution(federation, new URL[]{fom()});
            created = true;
            FederateHandle self = rti.joinFederationExecution("facade", "support", federation);
            joined = true;
            assertEquals(20, rti.getUpdateRateValue("fast"));
            assertEquals(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST, rti.getAutomaticResignDirective());
            rti.setAutomaticResignDirective(ResignAction.DELETE_OBJECTS);
            assertEquals(ResignAction.DELETE_OBJECTS, rti.getAutomaticResignDirective());
            assertTrue(rti.normalizeFederateHandle(self) >= 0);
            for (ServiceGroup group : ServiceGroup.values()) assertTrue(rti.normalizeServiceGroup(group) >= 0);
            rti.enableObjectClassRelevanceAdvisorySwitch(); rti.disableObjectClassRelevanceAdvisorySwitch();
            rti.enableAttributeRelevanceAdvisorySwitch(); rti.disableAttributeRelevanceAdvisorySwitch();
            rti.enableAttributeScopeAdvisorySwitch(); rti.disableAttributeScopeAdvisorySwitch();
            rti.enableInteractionRelevanceAdvisorySwitch(); rti.disableInteractionRelevanceAdvisorySwitch();
            assertThrows(ObjectInstanceNotKnown.class, () -> rti.getUpdateRateValueForAttribute(Handles.objectInstance(Long.MAX_VALUE), Handles.attribute(1)));
        } finally {
            try {
                if (joined) rti.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST);
            } finally {
                try { if (created) rti.destroyFederationExecution(federation); }
                finally { rti.disconnect(); }
            }
        }
    }

    private static URL fom() { URL fom = LiveSupportTest.class.getResource("/support-fom.xml"); assertNotNull(fom); return fom; }
    private static void await(BooleanSupplier done, Participant... participants) throws Exception {
        long deadline = System.nanoTime() + 10_000_000_000L;
        while (!done.getAsBoolean() && System.nanoTime() < deadline) {
            for (Participant p : participants) {
                p.callbacks.evoking = true;
                try { p.core.evokeMultipleCallbacks(0.001, 0.05); }
                finally { p.callbacks.evoking = false; }
                assertNull(p.callbacks.failure);
            }
        }
        assertTrue(done.getAsBoolean(), "Timed out: " + participants[0].callbacks.events + " / " + participants[1].callbacks.events);
    }
    private static void barrier(Participant owner, Participant peer) throws Exception {
        String label = "barrier-" + UUID.randomUUID();
        owner.core.synchronization.registerFederationSynchronizationPoint(label, new byte[0]);
        await(() -> owner.callbacks.announced.contains(label) && peer.callbacks.announced.contains(label), owner, peer);
        owner.core.synchronization.synchronizationPointAchieved(label);
        peer.core.synchronization.synchronizationPointAchieved(label);
        await(() -> owner.callbacks.synchronizedLabels.contains(label) && peer.callbacks.synchronizedLabels.contains(label), owner, peer);
    }
    @FunctionalInterface private interface Cleanup { void run() throws Exception; }
    private static void cleanup(Cleanup operation, List<Throwable> errors) { try { operation.run(); } catch (Throwable failure) { errors.add(failure); } }

    private static final class Participant {
        final AmbassadorServices core = new AmbassadorServices();
        final ExchangeServices exchange = core.exchange;
        final SupportServices support = new SupportServices(core, exchange);
        final Callbacks callbacks = new Callbacks();
        FederateHandle handle;
        boolean joined;
        void connect() throws Exception { core.connect(callbacks, CallbackModel.HLA_EVOKED, System.getProperty("gorti.endpoint")); }
        void join(String federation) throws Exception { handle = core.joinFederationExecution(UUID.randomUUID().toString(), "support", federation); joined = true; }
        void resign() throws Exception { if (joined) { core.resignFederationExecution(ResignAction.CANCEL_THEN_DELETE_THEN_DIVEST); joined = false; } }
    }

    private static final class Callbacks extends NullFederateAmbassador {
        final Thread caller = Thread.currentThread();
        final List<String> events = new ArrayList<>();
        final Set<String> announced = new HashSet<>();
        final Set<String> synchronizedLabels = new HashSet<>();
        Set<AttributeHandle> expectedAttributes;
        ObjectClassHandle expectedClass;
        InteractionClassHandle expectedInteraction;
        ObjectInstanceHandle expectedObject;
        ObjectInstanceHandle discovered;
        boolean evoking;
        boolean saving;
        boolean saved;
        boolean restoring;
        boolean restoreAborted;
        String failure;
        int count(String kind) { return (int) events.stream().filter(kind::equals).count(); }
        void guard() { if (!evoking || Thread.currentThread() != caller) failure = "Callback outside caller's evoke"; }
        void record(String kind) { guard(); events.add(kind); }
        void attributes(String kind, ObjectInstanceHandle object, AttributeHandleSet attributes) {
            assertEquals(expectedObject, object); assertEquals(expectedAttributes, attributes); record(kind);
        }
        @Override public void connectionLost(String message) { failure = message; }
        @Override public void initiateFederateSave(String label) { guard(); saving = true; }
        @Override public void federationSaved() { guard(); saved = true; }
        @Override public void initiateFederateRestore(String label, String name, FederateHandle handle) { guard(); restoring = true; }
        @Override public void federationNotRestored(RestoreFailureReason reason) { guard(); restoreAborted = true; }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name) { guard(); assertEquals(expectedClass, type); discovered = object; }
        @Override public void discoverObjectInstance(ObjectInstanceHandle object, ObjectClassHandle type, String name, FederateHandle producer) { discoverObjectInstance(object, type, name); }
        @Override public void startRegistrationForObjectClass(ObjectClassHandle type) { assertEquals(expectedClass, type); record("start"); }
        @Override public void stopRegistrationForObjectClass(ObjectClassHandle type) { assertEquals(expectedClass, type); record("stop"); }
        @Override public void turnInteractionsOn(InteractionClassHandle type) { assertEquals(expectedInteraction, type); record("interactions-on"); }
        @Override public void turnInteractionsOff(InteractionClassHandle type) { assertEquals(expectedInteraction, type); record("interactions-off"); }
        @Override public void turnUpdatesOnForObjectInstance(ObjectInstanceHandle object, AttributeHandleSet attributes) { attributes("updates-on", object, attributes); }
        @Override public void turnUpdatesOnForObjectInstance(ObjectInstanceHandle object, AttributeHandleSet attributes, String rate) { attributes("updates-on:" + rate, object, attributes); }
        @Override public void turnUpdatesOffForObjectInstance(ObjectInstanceHandle object, AttributeHandleSet attributes) { attributes("updates-off", object, attributes); }
        @Override public void attributesInScope(ObjectInstanceHandle object, AttributeHandleSet attributes) { attributes("scope-in", object, attributes); }
        @Override public void attributesOutOfScope(ObjectInstanceHandle object, AttributeHandleSet attributes) { attributes("scope-out", object, attributes); }
        @Override public void reflectAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag, OrderType order, TransportationTypeHandle transport, SupplementalReflectInfo info) {
            assertEquals(expectedObject, object); assertEquals(expectedAttributes, values.keySet()); record("reflect");
        }
        @Override public void announceSynchronizationPoint(String label, byte[] tag) { guard(); announced.add(label); }
        @Override public void federationSynchronized(String label, FederateHandleSet failed) { guard(); assertTrue(failed.isEmpty()); synchronizedLabels.add(label); }
    }
}
