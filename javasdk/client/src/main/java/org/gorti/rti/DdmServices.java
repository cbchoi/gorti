package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import java.io.DataInput;
import java.io.DataOutput;
import java.io.IOException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.function.Supplier;
import java.util.function.Function;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import rti.v1.Common;
import rti.v1.Ddm;
import rti.v1.DDMServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

/**
 * DDM adapters, serialized by the owning ambassador. Bounds retain unsigned long bits.
 * Grouped calls are sent in one request; the backend validates before mutation.
 */
final class DdmServices {
    private static final Common.WireVersion V1 = Common.WireVersion.WIRE_VERSION_V1;
    private static final Metadata.Key<String> SPEC_EXCEPTION =
            Metadata.Key.of("rti-spec-exception", Metadata.ASCII_STRING_MARSHALLER);
    private final ServiceContext context;
    private final ExchangeServices exchange;
    private final Map<Long, Map<Long, RangeBounds>> pending = new LinkedHashMap<>();

    DdmServices(ServiceContext context, ExchangeServices exchange) {
        this.context = Objects.requireNonNull(context, "context");
        this.exchange = Objects.requireNonNull(exchange, "exchange");
    }

    public RegionHandle createRegion(DimensionHandleSet dimensions) throws RTIexception {
        context.requireMember();
        if (dimensions == null) throw new InvalidDimensionHandle("Dimension handle set is null");
        List<Long> ids = new ArrayList<>();
        for (DimensionHandle dimension : dimensions) ids.add(dimension(dimension));
        Ddm.LookupRoutingSpaceRequest lookup = Ddm.LookupRoutingSpaceRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setName("default").build();
        Ddm.LookupRoutingSpaceResponse space = WireErrors.call(() -> ddm().lookupRoutingSpace(lookup));
        if (!space.getFound()) throw new RTIinternalError("The backend has no implicit IEEE routing space");
        Ddm.CreateRegionRequest request = Ddm.CreateRegionRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setRoutingSpaceHandle(space.getRoutingSpaceHandle()).addAllDimensionHandles(ids).build();
        try {
            return Handles.region(WireErrors.call(() -> ddm().createRegion(request)).getRegionHandle());
        } catch (RegionDoesNotContainSpecifiedDimension invalid) {
            // The common wire maps ErrDimensionNotFound for SetRangeBounds, not CreateRegion.
            throw new InvalidDimensionHandle(invalid.getMessage(), invalid);
        }
    }

    public void commitRegionModifications(RegionHandleSet regions) throws RTIexception {
        context.requireMember();
        List<Long> ids = regions(regions);
        Ddm.CommitRegionRequest request = Ddm.CommitRegionRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .addAllRegionHandles(ids).build();
        WireErrors.call(() -> ddm().commitRegionModifications(request));
        for (long id : ids) pending.remove(id);
    }

    public void deleteRegion(RegionHandle region) throws RTIexception {
        context.requireMember();
        long id = region(region);
        Ddm.DeleteRegionRequest request = Ddm.DeleteRegionRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setRegionHandle(id).build();
        WireErrors.call(() -> ddm().deleteRegion(request));
        pending.remove(id);
    }

    public void setRangeBounds(RegionHandle region, DimensionHandle dimension, RangeBounds bounds) throws RTIexception {
        context.requireMember();
        long regionId = region(region);
        long dimensionId = id(dimension, () -> new RegionDoesNotContainSpecifiedDimension("Expected a gorti dimension handle"));
        if (bounds == null || Long.compareUnsigned(bounds.lower, bounds.upper) >= 0) {
            throw new InvalidRangeBound("Range lower bound must be unsigned-less-than upper bound");
        }
        Ddm.SetRangeBoundsRequest request = Ddm.SetRangeBoundsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setRegionHandle(regionId).setDimensionHandle(dimensionId)
                .setBounds(Ddm.Range.newBuilder().setLower(bounds.lower).setUpper(bounds.upper)).build();
        WireErrors.call(() -> ddm().setRangeBounds(request));
        // QueryBounds exposes the committed realization, but this service reads the local specification.
        pending.computeIfAbsent(regionId, ignored -> new LinkedHashMap<>())
                .put(dimensionId, new RangeBounds(bounds.lower, bounds.upper));
    }

    public RangeBounds getRangeBounds(RegionHandle region, DimensionHandle dimension) throws RTIexception {
        context.requireMember();
        long regionId = region(region);
        long dimensionId = id(dimension, () -> new RegionDoesNotContainSpecifiedDimension("Expected a gorti dimension handle"));
        DimensionHandleSet dimensions = getDimensionHandleSet(region);
        if (!dimensions.contains(dimension)) throw new RegionDoesNotContainSpecifiedDimension("Region does not contain the dimension");
        RangeBounds staged = pending.getOrDefault(regionId, Collections.emptyMap()).get(dimensionId);
        if (staged != null) return new RangeBounds(staged.lower, staged.upper);
        Ddm.QueryBoundsRequest request = Ddm.QueryBoundsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setRegionHandle(regionId)
                .setDimensionHandle(dimensionId).build();
        Ddm.QueryBoundsResponse result = WireErrors.call(() -> ddm().queryBounds(request));
        if (!result.getFound()) throw new InvalidRegion("Region bounds are no longer available");
        return new RangeBounds(result.getBounds().getLower(), result.getBounds().getUpper());
    }

    public ObjectInstanceHandle registerObjectInstanceWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations) throws RTIexception {
        return register(objectClass, associations, "");
    }

    public ObjectInstanceHandle registerObjectInstanceWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations, String name) throws RTIexception {
        context.requireMember();
        if (name == null || name.isEmpty()) throw new ObjectInstanceNameNotReserved("Named registration requires a reserved name");
        return register(objectClass, associations, name);
    }

    private ObjectInstanceHandle register(ObjectClassHandle objectClass, AttributeSetRegionSetPairList associations,
            String name) throws RTIexception {
        context.requireMember();
        long classId = objectClass(objectClass);
        Ddm.RegisterObjectWithRegionsRequest request = Ddm.RegisterObjectWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(classId).setObjectName(name).setAttributeRegionGroups(groups(associations)).build();
        Ddm.RegisterObjectWithRegionsResponse result = WireErrors.call(() -> ddm().registerObjectInstanceWithRegions(request));
        exchange.discovered(result.getObjectHandle(), classId, result.getObjectName());
        return Handles.objectInstance(result.getObjectHandle());
    }

    public void associateRegionsForUpdates(ObjectInstanceHandle object, AttributeSetRegionSetPairList associations) throws RTIexception {
        context.requireMember();
        long objectId = exchange.knownObject(object);
        Ddm.AssociateRegionsForUpdatesRequest request = Ddm.AssociateRegionsForUpdatesRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(objectId).setAttributeRegionGroups(groups(associations)).build();
        WireErrors.call(() -> ddm().associateRegionsForUpdates(request));
    }

    public void unassociateRegionsForUpdates(ObjectInstanceHandle object, AttributeSetRegionSetPairList associations) throws RTIexception {
        context.requireMember();
        long objectId = exchange.knownObject(object);
        Ddm.UnassociateRegionsForUpdatesRequest request = Ddm.UnassociateRegionsForUpdatesRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(objectId).setAttributeRegionGroups(groups(associations)).build();
        WireErrors.call(() -> ddm().unassociateRegionsForUpdates(request));
    }

    public void subscribeObjectClassAttributesWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations) throws RTIexception {
        subscribe(objectClass, associations, true, "");
    }

    public void subscribeObjectClassAttributesWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations, String rate) throws RTIexception {
        subscribe(objectClass, associations, true, rate);
    }

    public void subscribeObjectClassAttributesPassivelyWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations) throws RTIexception {
        subscribe(objectClass, associations, false, "");
    }

    public void subscribeObjectClassAttributesPassivelyWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations, String rate) throws RTIexception {
        subscribe(objectClass, associations, false, rate);
    }

    private void subscribe(ObjectClassHandle objectClass, AttributeSetRegionSetPairList associations,
            boolean active, String rate) throws RTIexception {
        context.requireMember();
        long classId = objectClass(objectClass);
        Ddm.AttributeRegionGroups groups = groups(associations);
        if (rate == null) throw new InvalidUpdateRateDesignator("Update rate designator is null");
        Ddm.SubscribeOCAWithRegionsRequest request = Ddm.SubscribeOCAWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(classId).setAttributeRegionGroups(groups)
                .setActive(active).setUpdateRateDesignator(rate).build();
        WireErrors.call(() -> ddm().subscribeObjectClassAttributesWithRegions(request));
    }

    public void unsubscribeObjectClassAttributesWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations) throws RTIexception {
        context.requireMember();
        long classId = objectClass(objectClass);
        Ddm.UnsubscribeOCAWithRegionsRequest request = Ddm.UnsubscribeOCAWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(classId).setAttributeRegionGroups(groups(associations)).build();
        WireErrors.call(() -> ddm().unsubscribeObjectClassAttributesWithRegions(request));
    }

    public void subscribeInteractionClassWithRegions(InteractionClassHandle interaction, RegionHandleSet regions) throws RTIexception {
        subscribe(interaction, regions, true);
    }

    public void subscribeInteractionClassPassivelyWithRegions(InteractionClassHandle interaction, RegionHandleSet regions) throws RTIexception {
        subscribe(interaction, regions, false);
    }

    private void subscribe(InteractionClassHandle interaction, RegionHandleSet regions, boolean active) throws RTIexception {
        context.requireMember();
        Ddm.SubscribeICWithRegionsRequest request = Ddm.SubscribeICWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).addAllRegionHandles(regions(regions)).setActive(active).build();
        WireErrors.call(() -> ddm().subscribeInteractionClassWithRegions(request));
    }

    public void unsubscribeInteractionClassWithRegions(InteractionClassHandle interaction, RegionHandleSet regions) throws RTIexception {
        context.requireMember();
        Ddm.UnsubscribeICWithRegionsRequest request = Ddm.UnsubscribeICWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).addAllRegionHandles(regions(regions)).build();
        WireErrors.call(() -> ddm().unsubscribeInteractionClassWithRegions(request));
    }

    public void sendInteractionWithRegions(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
            RegionHandleSet regions, byte[] tag) throws RTIexception {
        context.requireMember();
        Ddm.SendInteractionWithRegionsRequest request = send(interaction, parameters, regions, tag).build();
        WireErrors.call(() -> ddm().sendInteractionWithRegions(request));
    }

    public MessageRetractionReturn sendInteractionWithRegions(InteractionClassHandle interaction, ParameterHandleValueMap parameters,
            RegionHandleSet regions, byte[] tag, LogicalTime time) throws RTIexception {
        context.requireMember();
        LogicalTime timestamp;
        try { timestamp = TimeWire.checked(time, context.timeImplementation()); }
        catch (IllegalArgumentException invalid) { throw new InvalidLogicalTime("Logical time does not match the selected factory", invalid); }
        Ddm.SendInteractionWithRegionsRequest.Builder request = TimeWire.write(send(interaction, parameters, regions, tag), "logical_time", timestamp, context.timeImplementation());
        long retraction = exchange.allocateRetraction();
        request.setMessageRetractionHandle(retraction);
        WireErrors.call(() -> ddm().sendInteractionWithRegions(request.build()));
        return exchange.issued(retraction);
    }

    private Ddm.SendInteractionWithRegionsRequest.Builder send(InteractionClassHandle interaction,
            ParameterHandleValueMap parameters, RegionHandleSet regions, byte[] tag) throws RTIexception {
        Ddm.SendInteractionWithRegionsRequest.Builder request = Ddm.SendInteractionWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).addAllRegionHandles(regions(regions)).setUserSuppliedTag(bytes(tag));
        if (parameters == null) throw new RTIinternalError("Parameter value map is null");
        for (Map.Entry<ParameterHandle, byte[]> entry : parameters.entrySet()) {
            request.putParameters(id(entry.getKey(), () -> new InteractionParameterNotDefined("Expected a gorti parameter handle")), bytes(entry.getValue()));
        }
        return request;
    }

    public void requestAttributeValueUpdateWithRegions(ObjectClassHandle objectClass,
            AttributeSetRegionSetPairList associations, byte[] tag) throws RTIexception {
        context.requireMember();
        long classId = objectClass(objectClass);
        ByteString copiedTag = bytes(tag);
        Ddm.RequestAttributeValueUpdateWithRegionsRequest request = Ddm.RequestAttributeValueUpdateWithRegionsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(classId).setAttributeRegionGroups(groups(associations))
                .setUserSuppliedTag(copiedTag).build();
        WireErrors.call(() -> ddm().requestAttributeValueUpdateWithRegions(request));
    }

    public DimensionHandle getDimensionHandle(String name) throws RTIexception {
        context.requireRestoreControl();
        if (name == null) throw new NameNotFound("Dimension name is null");
        Support.GetDimensionHandleRequest request = Support.GetDimensionHandleRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setDimensionName(name).build();
        return Handles.dimension(dimensionLookup(() -> support().getDimensionHandle(request), "dimension \"",
                failure -> new NameNotFound(failure.getMessage(), failure)).getDimensionHandle());
    }

    public String getDimensionName(DimensionHandle dimension) throws RTIexception {
        context.requireRestoreControl();
        Support.GetDimensionNameRequest request = Support.GetDimensionNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setDimensionHandle(dimension(dimension)).build();
        return dimensionLookup(() -> support().getDimensionName(request), "dimension handle ",
                failure -> new InvalidDimensionHandle(failure.getMessage(), failure)).getDimensionName();
    }

    public long getDimensionUpperBound(DimensionHandle dimension) throws RTIexception {
        context.requireRestoreControl();
        Support.GetDimensionUpperBoundRequest request = Support.GetDimensionUpperBoundRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setDimensionHandle(dimension(dimension)).build();
        return dimensionLookup(() -> support().getDimensionUpperBound(request), "dimension handle ",
                failure -> new InvalidDimensionHandle(failure.getMessage(), failure)).getUpperBound();
    }

    public DimensionHandleSet getDimensionHandleSet(RegionHandle region) throws RTIexception {
        context.requireMember();
        Support.GetDimensionHandleSetRequest request = Support.GetDimensionHandleSetRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setRegionHandle(region(region)).build();
        return dimensions(WireErrors.call(() -> support().getDimensionHandleSet(request)).getDimensionHandlesList());
    }

    public DimensionHandleSet getAvailableDimensionsForClassAttribute(ObjectClassHandle objectClass, AttributeHandle attribute) throws RTIexception {
        context.requireRestoreControl();
        Support.GetAvailableDimensionsForClassAttributeRequest request = Support.GetAvailableDimensionsForClassAttributeRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(id(objectClass, () -> new InvalidObjectClassHandle("Expected a gorti object class handle")))
                .setAttributeHandle(id(attribute, () -> new InvalidAttributeHandle("Expected a gorti attribute handle"))).build();
        return dimensions(WireErrors.call(() -> support().getAvailableDimensionsForClassAttribute(request)).getDimensionHandlesList());
    }

    public DimensionHandleSet getAvailableDimensionsForInteractionClass(InteractionClassHandle interaction) throws RTIexception {
        context.requireRestoreControl();
        Support.GetAvailableDimensionsForInteractionClassRequest request = Support.GetAvailableDimensionsForInteractionClassRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(id(interaction, () -> new InvalidInteractionClassHandle("Expected a gorti interaction class handle"))).build();
        return dimensions(WireErrors.call(() -> support().getAvailableDimensionsForInteractionClass(request)).getDimensionHandlesList());
    }

    void clear() { pending.clear(); }
    State checkpoint() { return new State(pending); }
    void restore(State state) {
        Objects.requireNonNull(state, "state");
        pending.clear();
        pending.putAll(copy(state.pending));
    }

    static final class State {
        private static final int MAX_ENTRIES = 100000;
        private final Map<Long, Map<Long, RangeBounds>> pending;
        private State(Map<Long, Map<Long, RangeBounds>> pending) { this.pending = copy(pending); }

        void writeTo(DataOutput output) throws IOException {
            checkCount(pending.size());
            int ranges = 0;
            for (Map.Entry<Long, Map<Long, RangeBounds>> region : pending.entrySet()) {
                checkHandle(region.getKey());
                checkCount(region.getValue().size());
                ranges += region.getValue().size();
                checkCount(ranges);
                for (Map.Entry<Long, RangeBounds> dimension : region.getValue().entrySet()) {
                    checkHandle(dimension.getKey());
                    checkBounds(dimension.getValue().lower, dimension.getValue().upper);
                }
            }
            output.writeInt(pending.size());
            for (Map.Entry<Long, Map<Long, RangeBounds>> region : pending.entrySet()) {
                output.writeLong(region.getKey());
                output.writeInt(region.getValue().size());
                for (Map.Entry<Long, RangeBounds> dimension : region.getValue().entrySet()) {
                    output.writeLong(dimension.getKey());
                    output.writeLong(dimension.getValue().lower);
                    output.writeLong(dimension.getValue().upper);
                }
            }
        }

        static State readFrom(DataInput input) throws IOException {
            int regionCount = checkCount(input.readInt());
            int ranges = 0;
            Map<Long, Map<Long, RangeBounds>> pending = new LinkedHashMap<>();
            for (int i = 0; i < regionCount; i++) {
                long region = checkHandle(input.readLong());
                if (pending.containsKey(region)) throw new IOException("Duplicate DDM region handle");
                int dimensionCount = checkCount(input.readInt());
                ranges += dimensionCount;
                checkCount(ranges);
                Map<Long, RangeBounds> dimensions = new LinkedHashMap<>();
                for (int j = 0; j < dimensionCount; j++) {
                    long dimension = checkHandle(input.readLong());
                    if (dimensions.containsKey(dimension)) throw new IOException("Duplicate DDM dimension handle");
                    long lower = input.readLong();
                    long upper = input.readLong();
                    checkBounds(lower, upper);
                    dimensions.put(dimension, new RangeBounds(lower, upper));
                }
                pending.put(region, dimensions);
            }
            return new State(pending);
        }

        private static int checkCount(int count) throws IOException {
            if (count < 0 || count > MAX_ENTRIES) throw new IOException("Invalid DDM checkpoint count");
            return count;
        }
        private static long checkHandle(long handle) throws IOException {
            if (handle == 0) throw new IOException("Invalid DDM checkpoint handle");
            return handle;
        }
        private static void checkBounds(long lower, long upper) throws IOException {
            // Range bounds denote nonnegative uint64 values, including signed Java bit patterns.
            if (Long.compareUnsigned(lower, upper) >= 0) throw new IOException("Invalid DDM checkpoint bounds");
        }
    }

    private static Map<Long, Map<Long, RangeBounds>> copy(Map<Long, Map<Long, RangeBounds>> source) {
        Map<Long, Map<Long, RangeBounds>> result = new LinkedHashMap<>();
        for (Map.Entry<Long, Map<Long, RangeBounds>> entry : source.entrySet()) {
            Map<Long, RangeBounds> dimensions = new LinkedHashMap<>();
            for (Map.Entry<Long, RangeBounds> dimension : entry.getValue().entrySet()) {
                RangeBounds bounds = dimension.getValue();
                dimensions.put(dimension.getKey(), new RangeBounds(bounds.lower, bounds.upper));
            }
            result.put(entry.getKey(), dimensions);
        }
        return result;
    }

    private static Ddm.AttributeRegionGroups groups(AttributeSetRegionSetPairList associations) throws RTIexception {
        if (associations == null) throw new RTIinternalError("Attribute/region pair list is null");
        Ddm.AttributeRegionGroups.Builder result = Ddm.AttributeRegionGroups.newBuilder();
        for (AttributeRegionAssociation association : associations) {
            if (association == null || association.ahset == null) throw new AttributeNotDefined("Attribute set is null");
            Ddm.AttributeRegionGroup.Builder group = Ddm.AttributeRegionGroup.newBuilder()
                    .addAllRegionHandles(regions(association.rhset));
            for (AttributeHandle attribute : association.ahset) {
                group.addAttributeHandles(id(attribute, () -> new AttributeNotDefined("Expected a gorti attribute handle")));
            }
            result.addGroups(group);
        }
        return result.build();
    }

    private static List<Long> regions(RegionHandleSet regions) throws RTIexception {
        if (regions == null) throw new InvalidRegion("Region handle set is null");
        List<Long> result = new ArrayList<>();
        for (RegionHandle region : regions) result.add(region(region));
        return result;
    }

    private static DimensionHandleSet dimensions(List<Long> ids) {
        DimensionHandleSet result = HandleCollections.dimensionSetFactory().create();
        for (long id : ids) result.add(Handles.dimension(id));
        return result;
    }

    private static long id(Object handle, Supplier<? extends RTIexception> error) throws RTIexception {
        try { return Handles.value(handle); }
        catch (IllegalArgumentException invalid) { throw error.get(); }
    }
    private static long region(RegionHandle region) throws RTIexception { return id(region, () -> new InvalidRegion("Expected a gorti region handle")); }
    private static long dimension(DimensionHandle dimension) throws RTIexception { return id(dimension, () -> new InvalidDimensionHandle("Expected a gorti dimension handle")); }
    private static long objectClass(ObjectClassHandle objectClass) throws RTIexception { return id(objectClass, () -> new ObjectClassNotDefined("Expected a gorti object class handle")); }
    private static long interaction(InteractionClassHandle interaction) throws RTIexception { return id(interaction, () -> new InteractionClassNotDefined("Expected a gorti interaction class handle")); }
    private static ByteString bytes(byte[] value) throws RTIinternalError {
        if (value == null) throw new RTIinternalError("Byte value or user-supplied tag is null");
        return ByteString.copyFrom(value);
    }

    private static <T> T dimensionLookup(Supplier<T> call, String missingPrefix,
            Function<RTIinternalError, ? extends RTIexception> missing) throws RTIexception {
        try { return WireErrors.call(call); }
        catch (RTIinternalError error) {
            // These three legacy support handlers omit the canonical exception trailer.
            // Do not reinterpret missing federations, transport failures, or tagged errors.
            if (error.getCause() instanceof StatusRuntimeException) {
                StatusRuntimeException failure = (StatusRuntimeException) error.getCause();
                String description = failure.getStatus().getDescription();
                if (failure.getStatus().getCode() == Status.Code.NOT_FOUND
                        && (failure.getTrailers() == null || !failure.getTrailers().containsKey(SPEC_EXCEPTION))
                        && description != null && description.startsWith(missingPrefix) && description.endsWith(" not found")) {
                    throw missing.apply(error);
                }
            }
            throw error;
        }
    }
    private DDMServiceGrpc.DDMServiceBlockingStub ddm() { return DDMServiceGrpc.newBlockingStub(context.channel()); }
    private SupportServiceGrpc.SupportServiceBlockingStub support() { return SupportServiceGrpc.newBlockingStub(context.channel()); }
}
