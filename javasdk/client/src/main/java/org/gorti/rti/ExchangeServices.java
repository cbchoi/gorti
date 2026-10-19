package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.function.Supplier;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import rti.v1.Common;
import rti.v1.Declaration;
import rti.v1.DeclarationServiceGrpc;
import rti.v1.Federation;
import rti.v1.FederationServiceGrpc;
import rti.v1.ObjectServiceGrpc;
import rti.v1.Support;
import rti.v1.SupportServiceGrpc;

/** Typed declaration, object-management, and basic support adapters. Serialized by the owning ambassador. */
final class ExchangeServices {
    private static final Common.WireVersion V1 = Common.WireVersion.WIRE_VERSION_V1;
    private final ServiceContext context;
    private final ExchangeKnowledge knowledge = new ExchangeKnowledge();
    private long nextRetraction;
    private long lastIssuedRetraction;
    private long clearedRetraction;

    public ExchangeServices(ServiceContext context) { this.context = Objects.requireNonNull(context, "context"); }

    public void publishObjectClassAttributes(ObjectClassHandle objectClass, AttributeHandleSet attributes) throws RTIexception {
        context.requireMember();
        Declaration.PubObjAttrsRequest request = Declaration.PubObjAttrsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(objectClass(objectClass)).addAllAttributeHandles(attributes(attributes)).build();
        WireErrors.call(() -> declarations().publishObjectClassAttributes(request));
    }

    public void unpublishObjectClass(ObjectClassHandle objectClass) throws RTIexception {
        unpublish(objectClass, Collections.emptyList(), false);
    }

    /** A supplied empty attribute set is a no-op, distinct from whole-class removal. */
    public void unpublishObjectClassAttributes(ObjectClassHandle objectClass, AttributeHandleSet attributes) throws RTIexception {
        context.requireMember();
        unpublish(objectClass, attributes(attributes), true);
    }

    private void unpublish(ObjectClassHandle objectClass, List<Long> attributes, boolean attributeSetPresent) throws RTIexception {
        context.requireMember();
        context.flushOwnershipCallbacks();
        Declaration.UnpubObjAttrsRequest.Builder request = Declaration.UnpubObjAttrsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(objectClass(objectClass)).addAllAttributeHandles(attributes);
        if (attributeSetPresent) request.setAttributeSetPresent(true);
        WireErrors.call(() -> declarations().unpublishObjectClassAttributes(request.build()));
    }

    public void subscribeObjectClassAttributes(ObjectClassHandle objectClass, AttributeHandleSet attributes) throws RTIexception {
        subscribeObjectClassAttributes(objectClass, attributes, "");
    }

    public void subscribeObjectClassAttributes(ObjectClassHandle objectClass, AttributeHandleSet attributes, String updateRateDesignator) throws RTIexception {
        subscribe(objectClass, attributes, true, updateRateDesignator);
    }

    public void subscribeObjectClassAttributesPassively(ObjectClassHandle objectClass, AttributeHandleSet attributes) throws RTIexception {
        subscribeObjectClassAttributesPassively(objectClass, attributes, "");
    }

    public void subscribeObjectClassAttributesPassively(ObjectClassHandle objectClass, AttributeHandleSet attributes, String updateRateDesignator) throws RTIexception {
        subscribe(objectClass, attributes, false, updateRateDesignator);
    }

    private void subscribe(ObjectClassHandle objectClass, AttributeHandleSet attributes, boolean active, String rate) throws RTIexception {
        context.requireMember();
        if (rate == null) throw new InvalidUpdateRateDesignator("Update rate designator is null");
        Declaration.SubObjAttrsRequest request = Declaration.SubObjAttrsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(objectClass(objectClass)).addAllAttributeHandles(attributes(attributes))
                .setActive(active).setUpdateRateDesignator(rate).build();
        WireErrors.call(() -> declarations().subscribeObjectClassAttributes(request));
    }

    public void unsubscribeObjectClass(ObjectClassHandle objectClass) throws RTIexception {
        unsubscribe(objectClass, Collections.emptyList(), false);
    }

    /** A supplied empty attribute set is a no-op, distinct from whole-class removal. */
    public void unsubscribeObjectClassAttributes(ObjectClassHandle objectClass, AttributeHandleSet attributes) throws RTIexception {
        context.requireMember();
        unsubscribe(objectClass, attributes(attributes), true);
    }

    private void unsubscribe(ObjectClassHandle objectClass, List<Long> attributes, boolean attributeSetPresent) throws RTIexception {
        context.requireMember();
        Declaration.UnsubObjAttrsRequest.Builder request = Declaration.UnsubObjAttrsRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(objectClass(objectClass)).addAllAttributeHandles(attributes);
        if (attributeSetPresent) request.setAttributeSetPresent(true);
        WireErrors.call(() -> declarations().unsubscribeObjectClassAttributes(request.build()));
    }

    public void publishInteractionClass(InteractionClassHandle interaction) throws RTIexception {
        context.requireMember();
        Declaration.PubInterRequest request = Declaration.PubInterRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).build();
        WireErrors.call(() -> declarations().publishInteractionClass(request));
    }

    public void unpublishInteractionClass(InteractionClassHandle interaction) throws RTIexception {
        context.requireMember();
        Declaration.UnpubInterRequest request = Declaration.UnpubInterRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).build();
        WireErrors.call(() -> declarations().unpublishInteractionClass(request));
    }

    public void subscribeInteractionClass(InteractionClassHandle interaction) throws RTIexception { subscribe(interaction, true); }

    public void subscribeInteractionClassPassively(InteractionClassHandle interaction) throws RTIexception { subscribe(interaction, false); }

    private void subscribe(InteractionClassHandle interaction, boolean active) throws RTIexception {
        context.requireMember();
        Declaration.SubInterRequest request = Declaration.SubInterRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).setActive(active).build();
        WireErrors.call(() -> declarations().subscribeInteractionClass(request));
    }

    public void unsubscribeInteractionClass(InteractionClassHandle interaction) throws RTIexception {
        context.requireMember();
        Declaration.UnsubInterRequest request = Declaration.UnsubInterRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).build();
        WireErrors.call(() -> declarations().unsubscribeInteractionClass(request));
    }

    public void reserveObjectInstanceName(String name) throws RTIexception {
        context.requireMember();
        if (name == null) throw new IllegalName("Object instance name is null");
        rti.v1.Object.ReserveObjectInstanceNameRequest request = rti.v1.Object.ReserveObjectInstanceNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).setObjectName(name).build();
        WireErrors.call(() -> objects().reserveObjectInstanceName(request));
    }

    public void releaseObjectInstanceName(String name) throws RTIexception {
        context.requireMember();
        if (name == null) throw new ObjectInstanceNameNotReserved("Object instance name is null");
        rti.v1.Object.ReleaseObjectInstanceNameRequest request = rti.v1.Object.ReleaseObjectInstanceNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).setObjectName(name).build();
        WireErrors.call(() -> objects().releaseObjectInstanceName(request));
    }

    public void reserveMultipleObjectInstanceName(Set<String> names) throws RTIexception {
        context.requireMember();
        if (names == null || names.isEmpty()) throw new NameSetWasEmpty("Object instance name set is empty");
        for (String name : names) if (name == null) throw new IllegalName("Object instance name is null");
        rti.v1.Object.ReserveMultipleObjectInstanceNamesRequest request = rti.v1.Object.ReserveMultipleObjectInstanceNamesRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).addAllObjectNames(names).build();
        WireErrors.call(() -> objects().reserveMultipleObjectInstanceNames(request));
    }

    public void releaseMultipleObjectInstanceName(Set<String> names) throws RTIexception {
        context.requireMember();
        if (names == null) throw new ObjectInstanceNameNotReserved("Object instance name set is null");
        for (String name : names) if (name == null) throw new ObjectInstanceNameNotReserved("Object instance name is null");
        rti.v1.Object.ReleaseMultipleObjectInstanceNamesRequest request = rti.v1.Object.ReleaseMultipleObjectInstanceNamesRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).addAllObjectNames(names).build();
        WireErrors.call(() -> objects().releaseMultipleObjectInstanceNames(request));
    }

    public ObjectInstanceHandle registerObjectInstance(ObjectClassHandle objectClass) throws RTIexception {
        return register(objectClass, "");
    }

    public ObjectInstanceHandle registerObjectInstance(ObjectClassHandle objectClass, String name) throws RTIexception {
        context.requireMember();
        if (name == null || name.isEmpty()) throw new ObjectInstanceNameNotReserved("Named registration requires a reserved name");
        return register(objectClass, name);
    }

    private ObjectInstanceHandle register(ObjectClassHandle objectClass, String name) throws RTIexception {
        context.requireMember();
        long classId = objectClass(objectClass);
        rti.v1.Object.RegisterObjectRequest request = rti.v1.Object.RegisterObjectRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(classId).setObjectName(name).build();
        rti.v1.Object.RegisterObjectResponse result = WireErrors.call(() -> objects().registerObjectInstance(request));
        discovered(result.getObjectHandle(), classId, result.getObjectName());
        return Handles.objectInstance(result.getObjectHandle());
    }

    public void updateAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag) throws RTIexception {
        context.requireMember();
        rti.v1.Object.UpdateAttributeValuesRequest request = update(object, values, tag).build();
        WireErrors.call(() -> objects().updateAttributeValues(request));
    }

    public MessageRetractionReturn updateAttributeValues(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag, LogicalTime time) throws RTIexception {
        context.requireMember();
        LogicalTime timestamp = time(time);
        rti.v1.Object.UpdateAttributeValuesRequest.Builder request = TimeWire.write(update(object, values, tag), "logical_time", timestamp, context.timeImplementation());
        long retraction = allocateRetraction();
        request.setMessageRetractionHandle(retraction);
        WireErrors.call(() -> objects().updateAttributeValues(request.build()));
        return issued(retraction);
    }

    private rti.v1.Object.UpdateAttributeValuesRequest.Builder update(ObjectInstanceHandle object, AttributeHandleValueMap values, byte[] tag) throws RTIexception {
        rti.v1.Object.UpdateAttributeValuesRequest.Builder request = rti.v1.Object.UpdateAttributeValuesRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(knownObject(object)).setUserSuppliedTag(bytes(tag));
        if (values == null) throw new RTIinternalError("Attribute value map is null");
        for (Map.Entry<AttributeHandle, byte[]> entry : values.entrySet()) {
            request.putAttributes(id(entry.getKey(), () -> new AttributeNotDefined("Expected a gorti attribute handle")), bytes(entry.getValue()));
        }
        return request;
    }

    public void sendInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values, byte[] tag) throws RTIexception {
        context.requireMember();
        rti.v1.Object.SendInteractionRequest request = send(interaction, values, tag).build();
        WireErrors.call(() -> objects().sendInteraction(request));
    }

    public MessageRetractionReturn sendInteraction(InteractionClassHandle interaction, ParameterHandleValueMap values, byte[] tag, LogicalTime time) throws RTIexception {
        context.requireMember();
        LogicalTime timestamp = time(time);
        rti.v1.Object.SendInteractionRequest.Builder request = TimeWire.write(send(interaction, values, tag), "logical_time", timestamp, context.timeImplementation());
        long retraction = allocateRetraction();
        request.setMessageRetractionHandle(retraction);
        WireErrors.call(() -> objects().sendInteraction(request.build()));
        return issued(retraction);
    }

    private rti.v1.Object.SendInteractionRequest.Builder send(InteractionClassHandle interaction, ParameterHandleValueMap values, byte[] tag) throws RTIexception {
        rti.v1.Object.SendInteractionRequest.Builder request = rti.v1.Object.SendInteractionRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).setUserSuppliedTag(bytes(tag));
        if (values == null) throw new RTIinternalError("Parameter value map is null");
        for (Map.Entry<ParameterHandle, byte[]> entry : values.entrySet()) {
            request.putParameters(id(entry.getKey(), () -> new InteractionParameterNotDefined("Expected a gorti parameter handle")), bytes(entry.getValue()));
        }
        return request;
    }

    public void deleteObjectInstance(ObjectInstanceHandle object, byte[] tag) throws RTIexception {
        context.requireMember();
        long objectId = knownObject(object);
        rti.v1.Object.DeleteObjectInstanceRequest request = deletion(objectId, tag).build();
        WireErrors.call(() -> objects().deleteObjectInstance(request));
        knowledge.forget(objectId);
    }

    public MessageRetractionReturn deleteObjectInstance(ObjectInstanceHandle object, byte[] tag, LogicalTime time) throws RTIexception {
        context.requireMember();
        LogicalTime timestamp = time(time);
        long objectId = knownObject(object);
        rti.v1.Object.DeleteObjectInstanceRequest.Builder request = TimeWire.write(deletion(objectId, tag), "logical_time", timestamp, context.timeImplementation());
        long retraction = allocateRetraction();
        request.setMessageRetractionHandle(retraction);
        WireErrors.call(() -> objects().deleteObjectInstance(request.build()));
        knowledge.deleted(objectId, context.federate(), retraction, timestamp);
        return issued(retraction);
    }

    private rti.v1.Object.DeleteObjectInstanceRequest.Builder deletion(long object, byte[] tag) throws RTIexception {
        return rti.v1.Object.DeleteObjectInstanceRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(object).setUserSuppliedTag(bytes(tag));
    }

    public void localDeleteObjectInstance(ObjectInstanceHandle object) throws RTIexception {
        context.requireMember();
        long objectId = knownObject(object);
        rti.v1.Object.LocalDeleteObjectInstanceRequest request = rti.v1.Object.LocalDeleteObjectInstanceRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).setObjectHandle(objectId).build();
        context.flushOwnershipCallbacks();
        if (context.ownershipCallbackReceiptsEnabled()) {
            rti.v1.Object.LocalDeleteObjectInstanceFence fence = WireErrors.call(() -> objects().localDeleteObjectInstanceFenced(request));
            if (fence.getFederationGeneration() != context.generation() || fence.getCallbackEpoch() != context.callbackEpoch())
                throw new RTIinternalError("Local deletion returned an invalid callback scope");
            context.retireObjectKnowledge(objectId, fence.getRetiredKnowledgeEpoch());
        } else {
            WireErrors.call(() -> objects().localDeleteObjectInstance(request));
        }
        knowledge.forget(objectId);
    }

    public void requestAttributeValueUpdate(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) throws RTIexception {
        context.requireMember();
        rti.v1.Object.RequestAttributeValueUpdateRequest request = rti.v1.Object.RequestAttributeValueUpdateRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(knownObject(object)).addAllAttributeHandles(attributes(attributes)).setUserSuppliedTag(bytes(tag)).build();
        WireErrors.call(() -> objects().requestAttributeValueUpdate(request));
    }

    public void requestAttributeValueUpdate(ObjectClassHandle objectClass, AttributeHandleSet attributes, byte[] tag) throws RTIexception {
        context.requireMember();
        rti.v1.Object.RequestClassAttributeValueUpdateRequest request = rti.v1.Object.RequestClassAttributeValueUpdateRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectClassHandle(objectClass(objectClass)).addAllAttributeHandles(attributes(attributes)).setUserSuppliedTag(bytes(tag)).build();
        WireErrors.call(() -> objects().requestClassAttributeValueUpdate(request));
    }

    public void retract(MessageRetractionHandle handle) throws RTIexception {
        context.requireMember();
        if (!context.regulating()) throw new TimeRegulationIsNotEnabled("Retraction requires time regulation");
        long value;
        try {
            value = Handles.value(handle);
            if (!Objects.equals(Handles.retractionFederation(handle), context.federation())
                    || Handles.retractionProducer(handle) != context.federate()
                    || Handles.retractionGeneration(handle) != context.generation()
                    || Long.compareUnsigned(value, clearedRetraction) <= 0
                    || Long.compareUnsigned(value, lastIssuedRetraction) > 0) {
                throw new InvalidMessageRetractionHandle("Retraction handle was not issued by this federate in this generation");
            }
        } catch (IllegalArgumentException invalid) {
            throw new InvalidMessageRetractionHandle("Expected a gorti retraction handle", invalid);
        }
        rti.v1.Object.RetractRequest request = rti.v1.Object.RetractRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate()).setMessageRetractionHandle(value).build();
        // The backend distinguishes failed-send gaps, already-retracted messages, and expiration.
        WireErrors.call(() -> objects().retract(request));
        knowledge.retracted(context.federate(), value);
    }

    public ObjectClassHandle getObjectClassHandle(String name) throws RTIexception {
        context.requireRestoreControl();
        Support.GetObjectClassHandleRequest request = Support.GetObjectClassHandleRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassName(name(name)).build();
        return Handles.objectClass(WireErrors.call(() -> support().getObjectClassHandle(request)).getClassHandle());
    }

    public FederateHandle getFederateHandle(String name) throws RTIexception {
        context.requireRestoreControl();
        if (name == null) throw new NameNotFound("Federate name is null");
        for (Federation.FederationMember member : members().getMembersList()) {
            if (name.equals(member.getFederateName())) return Handles.federate(member.getFederateHandle());
        }
        throw new NameNotFound("Federate name not found: " + name);
    }

    public String getFederateName(FederateHandle federate) throws RTIexception {
        context.requireRestoreControl();
        long value = id(federate, () -> new InvalidFederateHandle("Expected a gorti federate handle"));
        for (Federation.FederationMember member : members().getMembersList()) {
            if (value == member.getFederateHandle()) return member.getFederateName();
        }
        throw new InvalidFederateHandle("Federate handle not found: " + Long.toUnsignedString(value));
    }

    private Federation.ListFederationMembersResponse members() throws RTIexception {
        Federation.ListFederationMembersRequest request = Federation.ListFederationMembersRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).build();
        return WireErrors.call(() -> FederationServiceGrpc.newBlockingStub(context.channel()).listFederationMembers(request));
    }

    public String getObjectClassName(ObjectClassHandle objectClass) throws RTIexception {
        context.requireRestoreControl();
        Support.GetObjectClassNameRequest request = Support.GetObjectClassNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassHandle(supportObjectClass(objectClass)).build();
        return WireErrors.call(() -> support().getObjectClassName(request)).getClassName();
    }

    public AttributeHandle getAttributeHandle(ObjectClassHandle objectClass, String name) throws RTIexception {
        context.requireRestoreControl();
        Support.GetAttributeHandleRequest request = Support.GetAttributeHandleRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassHandle(supportObjectClass(objectClass)).setAttributeName(name(name)).build();
        return Handles.attribute(WireErrors.call(() -> support().getAttributeHandle(request)).getAttributeHandle());
    }

    public String getAttributeName(ObjectClassHandle objectClass, AttributeHandle attribute) throws RTIexception {
        context.requireRestoreControl();
        Support.GetAttributeNameRequest request = Support.GetAttributeNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassHandle(supportObjectClass(objectClass))
                .setAttributeHandle(id(attribute, () -> new InvalidAttributeHandle("Expected a gorti attribute handle"))).build();
        return WireErrors.call(() -> support().getAttributeName(request)).getAttributeName();
    }

    public InteractionClassHandle getInteractionClassHandle(String name) throws RTIexception {
        context.requireRestoreControl();
        Support.GetInteractionClassHandleRequest request = Support.GetInteractionClassHandleRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassName(name(name)).build();
        return Handles.interactionClass(WireErrors.call(() -> support().getInteractionClassHandle(request)).getClassHandle());
    }

    public String getInteractionClassName(InteractionClassHandle interaction) throws RTIexception {
        context.requireRestoreControl();
        Support.GetInteractionClassNameRequest request = Support.GetInteractionClassNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassHandle(supportInteraction(interaction)).build();
        return WireErrors.call(() -> support().getInteractionClassName(request)).getClassName();
    }

    public ParameterHandle getParameterHandle(InteractionClassHandle interaction, String name) throws RTIexception {
        context.requireRestoreControl();
        Support.GetParameterHandleRequest request = Support.GetParameterHandleRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassHandle(supportInteraction(interaction)).setParameterName(name(name)).build();
        return Handles.parameter(WireErrors.call(() -> support().getParameterHandle(request)).getParameterHandle());
    }

    public String getParameterName(InteractionClassHandle interaction, ParameterHandle parameter) throws RTIexception {
        context.requireRestoreControl();
        Support.GetParameterNameRequest request = Support.GetParameterNameRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setClassHandle(supportInteraction(interaction))
                .setParameterHandle(id(parameter, () -> new InvalidParameterHandle("Expected a gorti parameter handle"))).build();
        return WireErrors.call(() -> support().getParameterName(request)).getParameterName();
    }

    public ObjectInstanceHandle getObjectInstanceHandle(String name) throws RTIexception {
        context.requireRestoreControl();
        return Handles.objectInstance(knowledge.object(name));
    }

    public String getObjectInstanceName(ObjectInstanceHandle object) throws RTIexception {
        context.requireRestoreControl();
        return knowledge.object(knownObject(object)).name;
    }

    public ObjectClassHandle getKnownObjectClassHandle(ObjectInstanceHandle object) throws RTIexception {
        context.requireRestoreControl();
        return Handles.objectClass(knowledge.object(knownObject(object)).objectClass);
    }

    public OrderType getOrderType(String name) throws RTIexception {
        context.requireRestoreControl();
        if (name == null) throw new InvalidOrderName("Order name is null");
        Support.GetOrderTypeRequest request = Support.GetOrderTypeRequest.newBuilder().setWireVersion(V1).setOrderName(name).build();
        int value = WireErrors.call(() -> support().getOrderType(request)).getOrderType();
        switch (value) {
            case 1: return OrderType.RECEIVE;
            case 2: return OrderType.TIMESTAMP;
            default: throw new RTIinternalError("Server returned invalid order type: " + value);
        }
    }

    public String getOrderName(OrderType order) throws RTIexception {
        context.requireRestoreControl();
        if (order == null) throw new InvalidOrderType("Order type is null");
        Support.GetOrderNameRequest request = Support.GetOrderNameRequest.newBuilder()
                .setWireVersion(V1).setOrderType(order == OrderType.RECEIVE ? 1 : 2).build();
        return WireErrors.call(() -> support().getOrderName(request)).getOrderName();
    }

    public TransportationTypeHandle getTransportationTypeHandle(String name) throws RTIexception {
        context.requireRestoreControl();
        if (name == null) throw new InvalidTransportationName("Transportation name is null");
        Support.GetTransportationTypeRequest request = Support.GetTransportationTypeRequest.newBuilder()
                .setWireVersion(V1).setTransportationName(name).build();
        int value = WireErrors.call(() -> support().getTransportationType(request)).getTransportationType();
        try { return Handles.transportation(value); }
        catch (IllegalArgumentException invalid) { throw new RTIinternalError("Server returned invalid transportation type: " + value, invalid); }
    }

    public String getTransportationTypeName(TransportationTypeHandle transportation) throws RTIexception {
        context.requireRestoreControl();
        long value = id(transportation, () -> new InvalidTransportationType("Expected a gorti transportation handle"));
        Support.GetTransportationNameRequest request = Support.GetTransportationNameRequest.newBuilder()
                .setWireVersion(V1).setTransportationType((int) value).build();
        return WireErrors.call(() -> support().getTransportationName(request)).getTransportationName();
    }

    public void discovered(long object, long objectClass, String name) { knowledge.discovered(object, objectClass, name); }

    /** Called only after successful callback decoding; timestamp -1 denotes an untimed removal. */
    public void removed(long object, boolean receivedTimestampOrder, long retractionProducer, long retractionValue, double timestamp) {
        knowledge.removed(object, receivedTimestampOrder, retractionProducer, retractionValue, timestamp, context.logicalTime());
    }
    public void removed(long object, boolean receivedTimestampOrder, long producer, long value, LogicalTime timestamp) {
        knowledge.removed(object,receivedTimestampOrder,producer,value,timestamp,context.logicalTimeValue());
    }

    public void retracted(long producer, long value) { knowledge.retracted(producer, value); }

    public void advanceKnowledge(double time) { knowledge.advance(time); }
    public void advanceKnowledge(LogicalTime time) { knowledge.advance(time); }

    public void clearKnowledge() {
        knowledge.clear();
        // Retire this session's IDs without recycling them or retaining per-message history.
        clearedRetraction = nextRetraction;
        lastIssuedRetraction = nextRetraction;
    }

    public void constrainDisabled() { knowledge.constrainDisabled(); }

    State checkpoint() { return new State(knowledge.checkpoint(), clearedRetraction, lastIssuedRetraction); }

    void restore(State saved, long nextHint) {
        knowledge.restore(saved.knowledge);
        clearedRetraction = saved.cleared;
        if (Long.compareUnsigned(saved.lastIssued, lastIssuedRetraction) > 0) lastIssuedRetraction = saved.lastIssued;
        if (Long.compareUnsigned(saved.lastIssued, nextRetraction) > 0) nextRetraction = saved.lastIssued;
        if (nextHint != 0 && Long.compareUnsigned(nextHint - 1, nextRetraction) > 0) nextRetraction = nextHint - 1;
    }

    static final class State {
        private final ExchangeKnowledge knowledge;
        private final long cleared;
        private final long lastIssued;
        State(ExchangeKnowledge knowledge, long cleared, long lastIssued) {
            this.knowledge = knowledge; this.cleared = cleared; this.lastIssued = lastIssued;
        }

        void validateTime(String selected) { knowledge.validateTime(selected); }

        void writeTo(java.io.DataOutput output) throws java.io.IOException {
            knowledge.writeTo(output); output.writeLong(cleared); output.writeLong(lastIssued);
        }
        void writeTo(java.io.DataOutput output,boolean exact) throws java.io.IOException {
            knowledge.writeTo(output,exact);output.writeLong(cleared);output.writeLong(lastIssued);
        }

        static State readFrom(java.io.DataInput input) throws java.io.IOException {
            return readFrom(input,false);
        }
        static State readFrom(java.io.DataInput input,boolean exact) throws java.io.IOException {
            ExchangeKnowledge knowledge = ExchangeKnowledge.readFrom(input,exact);
            long cleared = input.readLong(), lastIssued = input.readLong();
            if (Long.compareUnsigned(cleared, lastIssued) > 0) throw new java.io.IOException("Invalid retraction checkpoint range");
            return new State(knowledge, cleared, lastIssued);
        }
    }

    long knownObject(ObjectInstanceHandle object) throws RTIexception {
        long value = id(object, () -> new ObjectInstanceNotKnown("Expected a gorti object instance handle"));
        knowledge.object(value);
        return value;
    }

    long allocateRetraction() throws RTIinternalError {
        if (!context.regulating()) return 0;
        if (nextRetraction == -1L) throw new RTIinternalError("Message retraction identifier space is exhausted");
        return ++nextRetraction;
    }

    MessageRetractionReturn issued(long value) {
        if (value == 0) return new MessageRetractionReturn(false, null);
        lastIssuedRetraction = value;
        return new MessageRetractionReturn(true, Handles.retraction(context.federation(), context.generation(), context.federate(), value));
    }

    private LogicalTime time(LogicalTime time) throws InvalidLogicalTime {
        try { return TimeWire.checked(time, context.timeImplementation()); }
        catch (IllegalArgumentException invalid) { throw new InvalidLogicalTime("Logical time does not match the selected factory", invalid); }
    }

    private static ByteString bytes(byte[] value) throws RTIinternalError {
        if (value == null) throw new RTIinternalError("Byte value or user-supplied tag is null");
        return ByteString.copyFrom(value);
    }

    private static String name(String value) throws NameNotFound {
        if (value == null) throw new NameNotFound("Name is null");
        return value;
    }

    private static long id(Object value, Supplier<? extends RTIexception> invalid) throws RTIexception {
        try { return Handles.value(value); }
        catch (IllegalArgumentException failure) { throw invalid.get(); }
    }

    private static long objectClass(ObjectClassHandle value) throws RTIexception {
        return id(value, () -> new ObjectClassNotDefined("Expected a gorti object class handle"));
    }

    private static long interaction(InteractionClassHandle value) throws RTIexception {
        return id(value, () -> new InteractionClassNotDefined("Expected a gorti interaction class handle"));
    }

    private static long supportObjectClass(ObjectClassHandle value) throws RTIexception {
        return id(value, () -> new InvalidObjectClassHandle("Expected a gorti object class handle"));
    }

    private static long supportInteraction(InteractionClassHandle value) throws RTIexception {
        return id(value, () -> new InvalidInteractionClassHandle("Expected a gorti interaction class handle"));
    }

    private static List<Long> attributes(AttributeHandleSet attributes) throws RTIexception {
        if (attributes == null) throw new AttributeNotDefined("Attribute handle set is null");
        List<Long> values = new ArrayList<>(attributes.size());
        for (AttributeHandle attribute : attributes) values.add(id(attribute, () -> new AttributeNotDefined("Expected a gorti attribute handle")));
        return values;
    }

    private DeclarationServiceGrpc.DeclarationServiceBlockingStub declarations() { return DeclarationServiceGrpc.newBlockingStub(context.channel()); }
    private ObjectServiceGrpc.ObjectServiceBlockingStub objects() { return ObjectServiceGrpc.newBlockingStub(context.channel()); }
    private SupportServiceGrpc.SupportServiceBlockingStub support() { return SupportServiceGrpc.newBlockingStub(context.channel()); }
}
