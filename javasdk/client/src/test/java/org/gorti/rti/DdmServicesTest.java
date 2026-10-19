package org.gorti.rti;

import static org.junit.jupiter.api.Assertions.*;

import com.google.protobuf.Message;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.*;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.StreamObserver;
import java.lang.reflect.Field;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Tag;
import rti.v1.Common;
import rti.v1.Ddm;
import rti.v1.DDMServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

class DdmServicesTest {
    private final RecordingChannel wire = new RecordingChannel();
    private final Context context = new Context(wire);
    private final ExchangeServices exchange = new ExchangeServices(context);
    private final DdmServices ddm = new DdmServices(context, exchange);
    private final RegionHandle region = Handles.region(11);
    private final DimensionHandle dimension = Handles.dimension(1);
    private final ObjectClassHandle objectClass = Handles.objectClass(21);
    private final InteractionClassHandle interaction = Handles.interactionClass(41);

    @Test void createLooksUpImplicitSpaceAndUsesExactDimensionHandles() throws Exception {
        assertEquals(region, ddm.createRegion(dimensions(1, 2)));
        Ddm.LookupRoutingSpaceRequest lookup = request(Ddm.LookupRoutingSpaceRequest.class);
        assertEquals("default", lookup.getName());
        Ddm.CreateRegionRequest create = request(Ddm.CreateRegionRequest.class);
        assertEquals(17, create.getRoutingSpaceHandle());
        assertEquals(Set.of(1L, 2L), Set.copyOf(create.getDimensionHandlesList()));
        assertEquals(7, create.getFederateHandle());
        assertEquals("fed", create.getFederationName());
    }

    @Test void unsignedBoundsRemainPendingUntilSuccessfulCommit() throws Exception {
        RangeBounds pending = new RangeBounds(Long.MIN_VALUE, -1L);
        ddm.setRangeBounds(region, dimension, pending);
        Ddm.SetRangeBoundsRequest set = request(Ddm.SetRangeBoundsRequest.class);
        assertEquals(Long.MIN_VALUE, set.getBounds().getLower());
        assertEquals(-1L, set.getBounds().getUpper());
        assertEquals(pending, ddm.getRangeBounds(region, dimension));
        wire.fail("InvalidRegion", Ddm.CommitRegionRequest.class);
        assertThrows(InvalidRegion.class, () -> ddm.commitRegionModifications(regions(11, 99)));
        wire.clearFailure();
        assertEquals(pending, ddm.getRangeBounds(region, dimension));
        ddm.commitRegionModifications(regions(11));
        assertEquals(new RangeBounds(0, -1L), ddm.getRangeBounds(region, dimension));
    }

    @Test void snapshotsAreDetachedAndClearAndDeleteDiscardPendingBounds() throws Exception {
        ddm.setRangeBounds(region, dimension, new RangeBounds(2, 8));
        DdmServices.State saved = ddm.checkpoint();
        ddm.setRangeBounds(region, dimension, new RangeBounds(4, 9));
        ddm.clear();
        assertEquals(new RangeBounds(0, -1L), ddm.getRangeBounds(region, dimension));
        ddm.restore(saved);
        assertEquals(new RangeBounds(2, 8), ddm.getRangeBounds(region, dimension));
        ddm.deleteRegion(region);
        assertEquals(new RangeBounds(0, -1L), ddm.getRangeBounds(region, dimension));
        ddm.restore(saved);
        assertEquals(new RangeBounds(2, 8), ddm.getRangeBounds(region, dimension));
    }

    @Test void boundFailuresKeepExactCheckedTypeAndNeverOverwriteLocalState() throws Exception {
        ddm.setRangeBounds(region, dimension, new RangeBounds(1, 9));
        for (String error : List.of("InvalidRangeBound", "RegionNotCreatedByThisFederate",
                "RegionDoesNotContainSpecifiedDimension", "InvalidRegion", "SaveInProgress", "RestoreInProgress")) {
            wire.fail(error, Ddm.SetRangeBoundsRequest.class);
            RTIexception failure = assertThrows(RTIexception.class,
                    () -> ddm.setRangeBounds(region, dimension, new RangeBounds(2, 8)));
            assertEquals(error, failure.getClass().getSimpleName());
        }
        wire.clearFailure();
        assertEquals(new RangeBounds(1, 9), ddm.getRangeBounds(region, dimension));
        assertThrows(InvalidRangeBound.class, () -> ddm.setRangeBounds(region, dimension, new RangeBounds(-1L, 1)));
        assertThrows(InvalidRangeBound.class, () -> ddm.setRangeBounds(region, dimension, new RangeBounds(2, 2)));
    }

    @Test void getRangeValidatesBackendRegionKnowledgeBeforeUsingPendingCache() throws Exception {
        ddm.setRangeBounds(region, dimension, new RangeBounds(1, 9));
        wire.fail("InvalidRegion", Support.GetDimensionHandleSetRequest.class);
        assertThrows(InvalidRegion.class, () -> ddm.getRangeBounds(region, dimension));
        wire.clearFailure();
        assertThrows(RegionDoesNotContainSpecifiedDimension.class,
                () -> ddm.getRangeBounds(region, Handles.dimension(99)));
    }

    @Test void allDimensionSupportUsesBackendAndPreservesUnsignedUpperBound() throws Exception {
        assertEquals(dimension, ddm.getDimensionHandle("X"));
        assertEquals("X", ddm.getDimensionName(dimension));
        assertEquals(-1L, ddm.getDimensionUpperBound(dimension));
        assertEquals(dimensions(1), ddm.getDimensionHandleSet(region));
        assertEquals(dimensions(1), ddm.getAvailableDimensionsForClassAttribute(objectClass, Handles.attribute(51)));
        assertEquals(dimensions(1), ddm.getAvailableDimensionsForInteractionClass(interaction));
        assertEquals(7, request(Support.GetDimensionHandleSetRequest.class).getFederateHandle());
        assertEquals(7, request(Support.GetAvailableDimensionsForClassAttributeRequest.class).getFederateHandle());
        wire.fail("NameNotFound", Support.GetDimensionHandleRequest.class);
        assertThrows(NameNotFound.class, () -> ddm.getDimensionHandle("missing"));
    }

    @Test void legacyUntaggedDimensionLookupErrorsMapToTheirDeclaredExceptions() {
        wire.failRaw(Status.NOT_FOUND.withDescription("dimension \"missing\" not found"), Support.GetDimensionHandleRequest.class);
        assertThrows(NameNotFound.class, () -> ddm.getDimensionHandle("missing"));
        wire.failRaw(Status.NOT_FOUND.withDescription("dimension handle 99 not found"), Support.GetDimensionNameRequest.class);
        assertThrows(InvalidDimensionHandle.class, () -> ddm.getDimensionName(Handles.dimension(99)));
        wire.failRaw(Status.NOT_FOUND.withDescription("dimension handle 99 not found"), Support.GetDimensionUpperBoundRequest.class);
        assertThrows(InvalidDimensionHandle.class, () -> ddm.getDimensionUpperBound(Handles.dimension(99)));
    }

    @Test void legacyDimensionFallbackDoesNotRewriteOtherServerFailures() {
        wire.failRaw(Status.NOT_FOUND.withDescription("federation \"fed\": missing"), Support.GetDimensionHandleRequest.class);
        assertThrows(RTIinternalError.class, () -> ddm.getDimensionHandle("missing"));
        wire.failRaw(Status.UNAVAILABLE.withDescription("dimension \"missing\" not found"), Support.GetDimensionHandleRequest.class);
        assertThrows(RTIinternalError.class, () -> ddm.getDimensionHandle("missing"));
        wire.fail("SaveInProgress", Support.GetDimensionHandleRequest.class);
        assertThrows(SaveInProgress.class, () -> ddm.getDimensionHandle("missing"));
    }

    @Test void registrationAndUnassociationUnionRegionsOnlyWithinEachAttribute() throws Exception {
        AttributeSetRegionSetPairList pairs = pairs();
        ObjectInstanceHandle object = ddm.registerObjectInstanceWithRegions(objectClass, pairs);
        assertEquals("assigned", exchange.getObjectInstanceName(object));
        assertEquals(objectClass, exchange.getKnownObjectClassHandle(object));
        assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(12L)), flatPairs(Ddm.RegisterObjectWithRegionsRequest.class));
        assertEquals(0, request(Ddm.RegisterObjectWithRegionsRequest.class).getAttributeRegionsCount());
        ddm.registerObjectInstanceWithRegions(objectClass, pairs, "reserved");
        assertEquals("reserved", request(Ddm.RegisterObjectWithRegionsRequest.class).getObjectName());
        ddm.unassociateRegionsForUpdates(object, pairs);
        assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(12L)), flatPairs(Ddm.UnassociateRegionsForUpdatesRequest.class));
        assertThrows(ObjectInstanceNotKnown.class,
                () -> ddm.associateRegionsForUpdates(Handles.objectInstance(99), pairs));
    }

    @Test void additiveAssociationUsesOneGroupedRequest() throws Exception {
        ObjectInstanceHandle object = ddm.registerObjectInstanceWithRegions(objectClass, pairs());
        wire.requests.clear();
        ddm.associateRegionsForUpdates(object, pairs());
        assertEquals(1, wire.requests.size());
        assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(12L)), flatPairs(Ddm.AssociateRegionsForUpdatesRequest.class));
    }

    @Test void registrationPreservesAllEmptyProductsInOneRequest() throws Exception {
        AttributeSetRegionSetPairList empty = HandleCollections.attributeRegionListFactory().create(0);
        ddm.registerObjectInstanceWithRegions(objectClass, empty);
        assertTrue(request(Ddm.RegisterObjectWithRegionsRequest.class).hasAttributeRegionGroups());
        assertEquals(0, request(Ddm.RegisterObjectWithRegionsRequest.class).getAttributeRegionGroups().getGroupsCount());
        wire.requests.clear();
        AttributeSetRegionSetPairList groups = HandleCollections.attributeRegionListFactory().create(3);
        groups.add(new AttributeRegionAssociation(attributes(51), regions()));
        groups.add(new AttributeRegionAssociation(attributes(), regions(11)));
        groups.add(new AttributeRegionAssociation(attributes(), regions()));
        ddm.registerObjectInstanceWithRegions(objectClass, groups, "reserved");
        assertEquals(1, wire.requests.size());
        Ddm.RegisterObjectWithRegionsRequest request = request(Ddm.RegisterObjectWithRegionsRequest.class);
        assertEquals("reserved", request.getObjectName());
        assertEquals(3, request.getAttributeRegionGroups().getGroupsCount());
        assertEquals(List.of(51L), request.getAttributeRegionGroups().getGroups(0).getAttributeHandlesList());
        assertEquals(0, request.getAttributeRegionGroups().getGroups(0).getRegionHandlesCount());
        assertEquals(0, request.getAttributeRegionGroups().getGroups(1).getAttributeHandlesCount());
        assertEquals(List.of(11L), request.getAttributeRegionGroups().getGroups(1).getRegionHandlesList());
    }

    @Test void heterogeneousPairsUseOneAtomicRpcPerService() throws Exception {
        ddm.subscribeObjectClassAttributesWithRegions(objectClass, pairs());
        ddm.subscribeObjectClassAttributesPassivelyWithRegions(objectClass, pairs(), "slow");
        ddm.unsubscribeObjectClassAttributesWithRegions(objectClass, pairs());
        ddm.requestAttributeValueUpdateWithRegions(objectClass, pairs(), new byte[0]);
        assertEquals(4, wire.requests.size());
        for (Class<? extends Message> type : List.of(Ddm.SubscribeOCAWithRegionsRequest.class,
                Ddm.UnsubscribeOCAWithRegionsRequest.class, Ddm.RequestAttributeValueUpdateWithRegionsRequest.class)) {
            assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(12L)), flatPairs(type));
        }
    }

    @Test void allSubscriptionOverloadsPreservePerAttributeRegionsAndOptions() throws Exception {
        ddm.subscribeObjectClassAttributesWithRegions(objectClass, rectangularPairs());
        assertSubscriptions(true, "");
        wire.requests.clear();
        ddm.subscribeObjectClassAttributesWithRegions(objectClass, rectangularPairs(), "fast");
        assertSubscriptions(true, "fast");
        wire.requests.clear();
        ddm.subscribeObjectClassAttributesPassivelyWithRegions(objectClass, rectangularPairs());
        assertSubscriptions(false, "");
        wire.requests.clear();
        ddm.subscribeObjectClassAttributesPassivelyWithRegions(objectClass, rectangularPairs(), "slow");
        assertSubscriptions(false, "slow");
        wire.requests.clear();
        ddm.unsubscribeObjectClassAttributesWithRegions(objectClass, rectangularPairs());
        assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(11L, 12L)), flatPairs(Ddm.UnsubscribeOCAWithRegionsRequest.class));
        wire.requests.clear();
        ddm.requestAttributeValueUpdateWithRegions(objectClass, rectangularPairs(), new byte[]{0, -1});
        assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(11L, 12L)), flatPairs(Ddm.RequestAttributeValueUpdateWithRegionsRequest.class));
        for (Message message : wire.requests) if (message instanceof Ddm.RequestAttributeValueUpdateWithRegionsRequest) {
            assertArrayEquals(new byte[]{0, -1}, ((Ddm.RequestAttributeValueUpdateWithRegionsRequest) message).getUserSuppliedTag().toByteArray());
        }
    }

    @Test void emptyGroupsPreservePresenceWithoutLegacyClearAll() throws Exception {
        AttributeSetRegionSetPairList emptyRegions = HandleCollections.attributeRegionListFactory().create(1);
        emptyRegions.add(new AttributeRegionAssociation(attributes(51), regions()));
        ddm.subscribeObjectClassAttributesWithRegions(objectClass, emptyRegions);
        assertEquals(0, request(Ddm.SubscribeOCAWithRegionsRequest.class).getAttributeRegionGroups().getGroups(0).getRegionHandlesCount());
        exchange.discovered(31, 21, "assigned");
        ddm.unassociateRegionsForUpdates(Handles.objectInstance(31), HandleCollections.attributeRegionListFactory().create(0));
        assertTrue(request(Ddm.UnassociateRegionsForUpdatesRequest.class).hasAttributeRegionGroups());
        assertEquals(0, request(Ddm.UnassociateRegionsForUpdatesRequest.class).getAttributeRegionGroups().getGroupsCount());
        AttributeSetRegionSetPairList noAttributes = HandleCollections.attributeRegionListFactory().create(1);
        noAttributes.add(new AttributeRegionAssociation(attributes(), regions(12)));
        ddm.subscribeObjectClassAttributesWithRegions(objectClass, noAttributes);
        Ddm.AttributeRegionGroup group = request(Ddm.SubscribeOCAWithRegionsRequest.class).getAttributeRegionGroups().getGroups(0);
        assertEquals(0, group.getAttributeHandlesCount());
        assertEquals(List.of(12L), group.getRegionHandlesList());
    }

    @Test void interactionSubscriptionsAndBothSendsUseSharedRetractionIdentity() throws Exception {
        ddm.subscribeInteractionClassWithRegions(interaction, regions(11));
        assertTrue(request(Ddm.SubscribeICWithRegionsRequest.class).getActive());
        ddm.subscribeInteractionClassPassivelyWithRegions(interaction, regions(12));
        assertFalse(request(Ddm.SubscribeICWithRegionsRequest.class).getActive());
        ddm.unsubscribeInteractionClassWithRegions(interaction, regions(12));
        assertEquals(List.of(12L), request(Ddm.UnsubscribeICWithRegionsRequest.class).getRegionHandlesList());
        ParameterHandleValueMap values = HandleCollections.parameterValueMapFactory().create(1);
        values.put(Handles.parameter(61), new byte[]{2, -1});
        ddm.sendInteractionWithRegions(interaction, values, regions(11), new byte[]{3});
        assertFalse(request(Ddm.SendInteractionWithRegionsRequest.class).hasLogicalTime());
        context.regulating = true;
        MessageRetractionReturn ordinary = exchange.sendInteraction(interaction, values, new byte[0], TimeValues.time(10));
        MessageRetractionReturn regional = ddm.sendInteractionWithRegions(interaction, values, regions(11), new byte[]{4}, TimeValues.time(11));
        assertEquals(Handles.value(ordinary.handle) + 1, Handles.value(regional.handle));
        assertEquals(7, Handles.retractionProducer(regional.handle));
        assertEquals(9, Handles.retractionGeneration(regional.handle));
        assertEquals("fed", Handles.retractionFederation(regional.handle));
        Ddm.SendInteractionWithRegionsRequest sent = request(Ddm.SendInteractionWithRegionsRequest.class);
        assertEquals(11, sent.getLogicalTime());
        assertEquals(Handles.value(regional.handle), sent.getMessageRetractionHandle());
        assertArrayEquals(new byte[]{2, -1}, sent.getParametersOrThrow(61).toByteArray());
        exchange.retract(regional.handle);
        context.regulating = false;
        assertFalse(ddm.sendInteractionWithRegions(interaction, values, regions(11), new byte[0], TimeValues.time(12)).retractionHandleIsValid);
        assertThrows(InvalidLogicalTime.class, () -> ddm.sendInteractionWithRegions(interaction, values, regions(11), new byte[0], null));
    }

    @Test void failedRegionalRegistrationDoesNotCreateLocalKnowledge() {
        wire.fail("InvalidRegionContext", Ddm.RegisterObjectWithRegionsRequest.class);
        assertThrows(InvalidRegionContext.class, () -> ddm.registerObjectInstanceWithRegions(objectClass, pairs()));
        assertThrows(ObjectInstanceNotKnown.class, () -> exchange.getObjectInstanceName(Handles.objectInstance(31)));
    }

    @Test void membershipAndLocalTypesAreCheckedBeforeTransport() {
        context.member = false;
        assertThrows(FederateNotExecutionMember.class, () -> ddm.createRegion(null));
        assertThrows(FederateNotExecutionMember.class, () -> ddm.getDimensionHandle(null));
        assertThrows(FederateNotExecutionMember.class, () -> ddm.setRangeBounds(null, null, null));
        assertTrue(wire.requests.isEmpty());
    }

    @Test @Tag("ddm-facade") void publicFacadeRoutesThroughInProcessGrpcAndKeepsCheckedErrors() throws Exception {
        String name = InProcessServerBuilder.generateName();
        List<Ddm.SubscribeICWithRegionsRequest> requests = new ArrayList<>();
        Server server = InProcessServerBuilder.forName(name).directExecutor()
                .addService(new DDMServiceGrpc.DDMServiceImplBase() {
                    @Override public void subscribeInteractionClassWithRegions(Ddm.SubscribeICWithRegionsRequest request, StreamObserver<Common.Empty> reply) {
                        requests.add(request);
                        if (request.getInteractionClassHandle() == 99) {
                            Metadata trailers = new Metadata();
                            trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), "InvalidRegionContext");
                            reply.onError(Status.INVALID_ARGUMENT.asRuntimeException(trailers));
                        } else { reply.onNext(Common.Empty.getDefaultInstance()); reply.onCompleted(); }
                    }
                }).build().start();
        ManagedChannel channel = InProcessChannelBuilder.forName(name).directExecutor().build();
        try {
            RTIambassador rti = RtiFactoryFactory.getRtiFactory().getRtiAmbassador();
            Field stateField = rti.getClass().getDeclaredField("services");
            stateField.setAccessible(true);
            Object state = stateField.get(rti);
            Field ddmField = state.getClass().getDeclaredField("ddm");
            ddmField.setAccessible(true);
            Context ctx = new Context(channel);
            ddmField.set(state, new DdmServices(ctx, new ExchangeServices(ctx)));
            rti.subscribeInteractionClassWithRegions(interaction, regions(11));
            assertEquals(7, requests.get(0).getFederateHandle());
            assertEquals(List.of(11L), requests.get(0).getRegionHandlesList());
            assertThrows(InvalidRegionContext.class, () -> rti.subscribeInteractionClassWithRegions(Handles.interactionClass(99), regions(11)));
        } finally { channel.shutdownNow(); server.shutdownNow(); }
    }

    private void assertSubscriptions(boolean active, String rate) {
        assertEquals(Map.of(51L, Set.of(11L, 12L), 52L, Set.of(11L, 12L)), flatPairs(Ddm.SubscribeOCAWithRegionsRequest.class));
        assertEquals(1, wire.requests.stream().filter(Ddm.SubscribeOCAWithRegionsRequest.class::isInstance).count());
        for (Message message : wire.requests) if (message instanceof Ddm.SubscribeOCAWithRegionsRequest) {
            Ddm.SubscribeOCAWithRegionsRequest request = (Ddm.SubscribeOCAWithRegionsRequest) message;
            assertTrue(request.hasActive()); assertEquals(active, request.getActive());
            assertEquals(rate, request.getUpdateRateDesignator());
        }
    }

    private Map<Long, Set<Long>> flatPairs(Class<? extends Message> type) {
        Map<Long, Set<Long>> result = new LinkedHashMap<>();
        for (Message request : wire.requests) if (type.isInstance(request)) {
            Ddm.AttributeRegionGroups groups = (Ddm.AttributeRegionGroups) request.getField(request.getDescriptorForType().findFieldByName("attribute_region_groups"));
            for (Ddm.AttributeRegionGroup group : groups.getGroupsList()) {
                for (Long attr : group.getAttributeHandlesList()) result.computeIfAbsent(attr, ignored -> new java.util.HashSet<>()).addAll(group.getRegionHandlesList());
            }
        }
        return result;
    }

    private <T> T request(Class<T> type) {
        for (int i = wire.requests.size() - 1; i >= 0; i--) if (type.isInstance(wire.requests.get(i))) return type.cast(wire.requests.get(i));
        throw new AssertionError("No " + type.getSimpleName());
    }

    private static DimensionHandleSet dimensions(long... ids) {
        DimensionHandleSet result = HandleCollections.dimensionSetFactory().create();
        for (long id : ids) result.add(Handles.dimension(id));
        return result;
    }
    private static RegionHandleSet regions(long... ids) {
        RegionHandleSet result = HandleCollections.regionSetFactory().create();
        for (long id : ids) result.add(Handles.region(id));
        return result;
    }
    private static AttributeHandleSet attributes(long... ids) {
        AttributeHandleSet result = HandleCollections.attributeSetFactory().create();
        for (long id : ids) result.add(Handles.attribute(id));
        return result;
    }
    private static AttributeSetRegionSetPairList pairs() {
        AttributeSetRegionSetPairList result = HandleCollections.attributeRegionListFactory().create(2);
        result.add(new AttributeRegionAssociation(attributes(51), regions(11)));
        result.add(new AttributeRegionAssociation(attributes(51, 52), regions(12)));
        return result;
    }

    private static AttributeSetRegionSetPairList rectangularPairs() {
        AttributeSetRegionSetPairList result = pairs();
        result.add(new AttributeRegionAssociation(attributes(52), regions(11)));
        return result;
    }

    private static final class Context implements ServiceContext {
        final Channel channel;
        boolean member = true;
        boolean regulating;
        Context(Channel channel) { this.channel = channel; }
        public void requireMember() throws RTIexception { if (!member) throw new FederateNotExecutionMember("not joined"); }
        public Channel channel() { return channel; }
        public String federation() { return "fed"; }
        public long federate() { return 7; }
        public long generation() { return 9; }
        public boolean regulating() { return regulating; }
        public double logicalTime() { return 0; }
    }

    private static final class RecordingChannel extends Channel {
        final List<Message> requests = new ArrayList<>();
        String failure;
        Status rawFailure;
        Class<?> failureType;
        void fail(String failure, Class<?> type) { this.failure = failure; this.failureType = type; rawFailure = null; }
        void failRaw(Status failure, Class<?> type) { rawFailure = failure; failureType = type; this.failure = null; }
        void clearFailure() { failure = null; rawFailure = null; }
        public String authority() { return "ddm-test"; }
        public <ReqT, RespT> ClientCall<ReqT, RespT> newCall(MethodDescriptor<ReqT, RespT> method, CallOptions options) {
            return new ClientCall<ReqT, RespT>() {
                Listener<RespT> listener;
                Message request;
                public void start(Listener<RespT> listener, Metadata headers) { this.listener = listener; }
                public void request(int count) { }
                public void cancel(String message, Throwable failure) { }
                public void sendMessage(ReqT request) { this.request = (Message) request; requests.add(this.request); }
                @SuppressWarnings("unchecked") public void halfClose() {
                    if (rawFailure != null && failureType.isInstance(request)) {
                        listener.onClose(rawFailure, new Metadata()); return;
                    }
                    if (failure != null && failureType.isInstance(request)) {
                        Metadata trailers = new Metadata();
                        trailers.put(Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER), failure);
                        listener.onClose(Status.FAILED_PRECONDITION, trailers); return;
                    }
                    Message result = Common.Empty.getDefaultInstance();
                    if (request instanceof Ddm.LookupRoutingSpaceRequest) result = Ddm.LookupRoutingSpaceResponse.newBuilder().setFound(true).setRoutingSpaceHandle(17).build();
                    if (request instanceof Ddm.CreateRegionRequest) result = Ddm.CreateRegionResponse.newBuilder().setRegionHandle(11).build();
                    if (request instanceof Ddm.QueryBoundsRequest) result = Ddm.QueryBoundsResponse.newBuilder().setFound(true).setBounds(Ddm.Range.newBuilder().setUpper(-1L)).build();
                    if (request instanceof Ddm.RegisterObjectWithRegionsRequest) result = Ddm.RegisterObjectWithRegionsResponse.newBuilder().setObjectHandle(31).setObjectName("assigned").build();
                    if (request instanceof Support.GetDimensionHandleRequest) result = Support.GetDimensionHandleResponse.newBuilder().setDimensionHandle(1).build();
                    if (request instanceof Support.GetDimensionNameRequest) result = Support.GetDimensionNameResponse.newBuilder().setDimensionName("X").build();
                    if (request instanceof Support.GetDimensionUpperBoundRequest) result = Support.GetDimensionUpperBoundResponse.newBuilder().setUpperBound(-1L).build();
                    if (request instanceof Support.GetDimensionHandleSetRequest) result = Support.GetDimensionHandleSetResponse.newBuilder().addDimensionHandles(1).build();
                    if (request instanceof Support.GetAvailableDimensionsForClassAttributeRequest) result = Support.GetAvailableDimensionsForClassAttributeResponse.newBuilder().addDimensionHandles(1).build();
                    if (request instanceof Support.GetAvailableDimensionsForInteractionClassRequest) result = Support.GetAvailableDimensionsForInteractionClassResponse.newBuilder().addDimensionHandles(1).build();
                    listener.onMessage((RespT) result); listener.onClose(Status.OK, new Metadata());
                }
            };
        }
    }
}
