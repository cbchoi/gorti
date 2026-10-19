package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.*;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Objects;
import org.gorti.rti.types.Handles;
import rti.v1.Common;
import rti.v1.ObjectServiceGrpc;

/** Standard ordering and transportation services, serialized by the owning ambassador. */
final class TransportationServices {
    private static final Common.WireVersion V1 = Common.WireVersion.WIRE_VERSION_V1;
    private final ServiceContext context;
    private final CallbackSink callbacks;
    private final ExchangeServices exchange;
    private Object lifecycle = new Object();

    public TransportationServices(ServiceContext context, CallbackSink callbacks, ExchangeServices exchange) {
        this.context = Objects.requireNonNull(context, "context");
        this.callbacks = Objects.requireNonNull(callbacks, "callbacks");
        this.exchange = Objects.requireNonNull(exchange, "exchange");
    }

    public void changeAttributeOrderType(ObjectInstanceHandle object, AttributeHandleSet attributes, OrderType order) throws RTIexception {
        long target = object(object);
        rti.v1.Object.ChangeAttributeOrderRequest request = rti.v1.Object.ChangeAttributeOrderRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).setOrderType(order(order)).build();
        WireErrors.call(() -> objects().changeAttributeOrderType(request));
    }

    public void changeInteractionOrderType(InteractionClassHandle interaction, OrderType order) throws RTIexception {
        context.requireMember();
        rti.v1.Object.ChangeInteractionOrderRequest request = rti.v1.Object.ChangeInteractionOrderRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).setOrderType(order(order)).build();
        WireErrors.call(() -> objects().changeInteractionOrderType(request));
    }

    public void requestAttributeTransportationTypeChange(ObjectInstanceHandle object, AttributeHandleSet attributes,
            TransportationTypeHandle transportation) throws RTIexception {
        long target = object(object);
        rti.v1.Object.ChangeAttributeTransportRequest request = rti.v1.Object.ChangeAttributeTransportRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).setTransportType(transportation(transportation)).build();
        Object state = lifecycle;
        long generation = context.generation();
        CallbackSink delivery = callbacks.capture();
        ObjectServiceGrpc.ObjectServiceBlockingStub wire = objects();
        rti.v1.Object.TransportationChange prepared = WireErrors.call(() -> wire.prepareAttributeTransportationTypeChange(request));
        validatePreparation(prepared, generation, target, request.getAttributeHandlesList(), 0);
        enqueueActivation(delivery, state, wire, request.getFederationName(), request.getFederateHandle(), prepared);
    }

    public void requestInteractionTransportationTypeChange(InteractionClassHandle interaction,
            TransportationTypeHandle transportation) throws RTIexception {
        context.requireMember();
        rti.v1.Object.ChangeInteractionTransportRequest request = rti.v1.Object.ChangeInteractionTransportRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setInteractionClassHandle(interaction(interaction)).setTransportType(transportation(transportation)).build();
        Object state = lifecycle;
        long generation = context.generation();
        long target = request.getInteractionClassHandle();
        CallbackSink delivery = callbacks.capture();
        ObjectServiceGrpc.ObjectServiceBlockingStub wire = objects();
        rti.v1.Object.TransportationChange prepared = WireErrors.call(() -> wire.prepareInteractionTransportationTypeChange(request));
        validatePreparation(prepared, generation, 0, List.of(), target);
        enqueueActivation(delivery, state, wire, request.getFederationName(), request.getFederateHandle(), prepared);
    }

    public void queryAttributeTransportationType(ObjectInstanceHandle object, AttributeHandle attribute) throws RTIexception {
        long target = object(object);
        rti.v1.Object.QueryAttributeTransportationTypeRequest request = rti.v1.Object.QueryAttributeTransportationTypeRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).setAttributeHandle(attribute(attribute)).build();
        CallbackSink delivery = callbacks.capture();
        rti.v1.Object.TransportationTypeResponse response = WireErrors.call(() -> objects().queryAttributeTransportationType(request));
        delivery.enqueue(TransportationCallbacks.reportAttribute(request, response));
    }

    public void queryInteractionTransportationType(FederateHandle federate, InteractionClassHandle interaction) throws RTIexception {
        context.requireMember();
        rti.v1.Object.QueryInteractionTransportationTypeRequest request = rti.v1.Object.QueryInteractionTransportationTypeRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setTargetFederateHandle(federate(federate)).setInteractionClassHandle(interaction(interaction)).build();
        CallbackSink delivery = callbacks.capture();
        rti.v1.Object.TransportationTypeResponse response = WireErrors.call(() -> objects().queryInteractionTransportationType(request));
        delivery.enqueue(TransportationCallbacks.reportInteraction(request, response));
    }

    /** Called with callback-queue lifecycle resets; old completions retain only the retired identity. */
    void clear() { lifecycle = new Object(); }

    rti.v1.Object.TransportationChange activateStreamConfirmation(rti.v1.Object.TransportationChange prepared) throws RTIexception {
        context.requireMember();
        if (prepared.getCallbackEpoch() != context.callbackEpoch()
                || (prepared.getObjectHandle() == 0) == (prepared.getInteractionClassHandle() == 0)
                || (prepared.getObjectHandle() == 0 && prepared.getAttributeHandlesCount() != 0)) {
            throw new RTIinternalError("Invalid or retired transportation confirmation");
        }
        validatePreparation(prepared, context.generation(), prepared.getObjectHandle(),
                prepared.getAttributeHandlesList(), prepared.getInteractionClassHandle());
        rti.v1.Object.TransportationChange result = activate(objects(), context.federation(), context.federate(), prepared);
        if (result.getTransportType() != prepared.getTransportType()
                || new HashSet<>(result.getAttributeHandlesList()).size() != result.getAttributeHandlesCount()) {
            throw new RTIinternalError("Mismatched transportation activation");
        }
        return result;
    }

    void confirmStream(rti.v1.Object.TransportationChange activated, FederateAmbassador ambassador) throws RTIexception {
        if (activated.getObjectHandle() != 0) TransportationCallbacks.confirmAttributes(activated).invoke(ambassador);
        else TransportationCallbacks.confirmInteraction(activated).invoke(ambassador);
    }

    private void enqueueActivation(CallbackSink delivery, Object state, ObjectServiceGrpc.ObjectServiceBlockingStub wire,
            String federation, long federate, rti.v1.Object.TransportationChange prepared) {
        delivery.enqueue(new CallbackSink.Invocation() {
            @Override public void invoke(FederateAmbassador ambassador) throws RTIexception {
                if (lifecycle != state) return;
                rti.v1.Object.TransportationChange activated;
                try { activated = activate(wire, federation, federate, prepared); }
                catch (SaveInProgress | RestoreInProgress deferred) {
                    delivery.enqueue(this);
                    return;
                } catch (RTIexception failure) {
                    io.grpc.Status.Code code = io.grpc.Status.fromThrowable(failure).getCode();
                    if (code == io.grpc.Status.Code.UNAVAILABLE || code == io.grpc.Status.Code.DEADLINE_EXCEEDED) {
                        // A lost reply may hide a committed activation. Retry this ticket,
                        // never issue a replacement change or infer successful confirmation.
                        delivery.enqueue(this);
                    }
                    throw failure;
                }
                if (activated.getObjectHandle() != 0) TransportationCallbacks.confirmAttributes(activated).invoke(ambassador);
                else TransportationCallbacks.confirmInteraction(activated).invoke(ambassador);
            }
        });
    }

    private static void validatePreparation(rti.v1.Object.TransportationChange change, long generation,
            long object, List<Long> attributes, long interaction) throws RTIinternalError {
        if (change.getTicket().size() != 32 || change.getFederationGeneration() != generation
                || change.getObjectHandle() != object || change.getInteractionClassHandle() != interaction
                || !new HashSet<>(change.getAttributeHandlesList()).equals(new HashSet<>(attributes))) {
            throw new RTIinternalError("Server returned a mismatched transportation preparation");
        }
        // Reject malformed transportation values before ever attempting activation.
        if (object != 0) TransportationCallbacks.confirmAttributes(change);
        else TransportationCallbacks.confirmInteraction(change);
    }

    private static rti.v1.Object.TransportationChange activate(ObjectServiceGrpc.ObjectServiceBlockingStub wire,
            String federation, long federate, rti.v1.Object.TransportationChange prepared) throws RTIexception {
        rti.v1.Object.ActivateTransportationTypeChangeRequest request = rti.v1.Object.ActivateTransportationTypeChangeRequest.newBuilder()
                .setWireVersion(V1).setFederationName(federation).setFederateHandle(federate).setTicket(prepared.getTicket())
                .setExpectedFederationGeneration(prepared.getFederationGeneration()).setExpectedCallbackEpoch(prepared.getCallbackEpoch()).build();
        rti.v1.Object.TransportationChange result = WireErrors.call(() -> wire.activateTransportationTypeChange(request));
        // Ownership loss can remove prepared attributes permanently, including
        // every attribute. The server must never add a new attribute to this ticket.
        if (!new HashSet<>(prepared.getAttributeHandlesList()).containsAll(result.getAttributeHandlesList())) {
            throw new RTIinternalError("Server activated attributes outside the transportation request");
        }
        validatePreparation(result, prepared.getFederationGeneration(), prepared.getObjectHandle(),
                result.getAttributeHandlesList(), prepared.getInteractionClassHandle());
        if (!result.getTicket().equals(prepared.getTicket()) || result.getCallbackEpoch() != prepared.getCallbackEpoch()) {
            throw new RTIinternalError("Server returned a mismatched transportation activation");
        }
        return result;
    }

    private long object(ObjectInstanceHandle object) throws RTIexception {
        context.requireMember();
        return exchange.knownObject(object);
    }

    private static long attribute(AttributeHandle attribute) throws AttributeNotDefined {
        try { return Handles.value(attribute); }
        catch (IllegalArgumentException invalid) { throw new AttributeNotDefined("Expected a gorti attribute handle", invalid); }
    }

    private static List<Long> attributes(AttributeHandleSet attributes) throws AttributeNotDefined {
        if (attributes == null) throw new AttributeNotDefined("Attribute handle set is null");
        List<Long> result = new ArrayList<>(attributes.size());
        for (AttributeHandle attribute : attributes) result.add(attribute(attribute));
        return result;
    }

    private static long interaction(InteractionClassHandle interaction) throws InteractionClassNotDefined {
        try { return Handles.value(interaction); }
        catch (IllegalArgumentException invalid) { throw new InteractionClassNotDefined("Expected a gorti interaction class handle", invalid); }
    }

    private static long federate(FederateHandle federate) throws RTIinternalError {
        try { return Handles.value(federate); }
        catch (IllegalArgumentException invalid) { throw new RTIinternalError("Expected a gorti federate handle", invalid); }
    }

    private static Common.TransportationType transportation(TransportationTypeHandle transportation) throws InvalidTransportationType {
        long value;
        try { value = Handles.value(transportation); }
        catch (IllegalArgumentException invalid) { throw new InvalidTransportationType("Expected a gorti transportation handle", invalid); }
        if (value == 1) return Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE;
        if (value == 2) return Common.TransportationType.TRANSPORTATION_TYPE_BEST_EFFORT;
        throw new InvalidTransportationType("Unknown transportation handle: " + value);
    }

    private static rti.v1.Object.OrderType order(OrderType order) throws RTIinternalError {
        if (order == OrderType.RECEIVE) return rti.v1.Object.OrderType.ORDER_TYPE_RECEIVE;
        if (order == OrderType.TIMESTAMP) return rti.v1.Object.OrderType.ORDER_TYPE_TIMESTAMP;
        throw new RTIinternalError("Order type is null");
    }

    private ObjectServiceGrpc.ObjectServiceBlockingStub objects() { return ObjectServiceGrpc.newBlockingStub(context.channel()); }

}
