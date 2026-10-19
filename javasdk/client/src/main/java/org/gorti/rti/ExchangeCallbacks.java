package org.gorti.rti;

import hla.rti1516e.*;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import java.util.LinkedHashSet;
import java.util.List;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import org.gorti.rti.types.TimeValues;
import rti.v1.Common;
import rti.v1.Stream;

/** Converts wire metadata into the corresponding standard callback overload. */
final class ExchangeCallbacks {
    private ExchangeCallbacks() { }

    static boolean deliver(Stream.FederateEvent event, FederateAmbassador target, String federation,
            long generation, Runnable afterRemoval) throws RTIexception {
        return deliver(event, target, federation, generation, afterRemoval, TimeWire.FLOAT);
    }

    static boolean deliver(Stream.FederateEvent event, FederateAmbassador target, String federation,
            long generation, Runnable afterRemoval, String selected) throws RTIexception {
        switch (event.getEventCase()) {
            case DISCOVER: {
                Stream.DiscoverObjectInstance value = event.getDiscover();
                if (value.hasProducingFederate()) {
                    target.discoverObjectInstance(Handles.objectInstance(value.getObjectHandle()),
                            Handles.objectClass(value.getObjectClassHandle()), value.getObjectName(),
                            Handles.federate(value.getProducingFederate()));
                } else {
                    target.discoverObjectInstance(Handles.objectInstance(value.getObjectHandle()),
                            Handles.objectClass(value.getObjectClassHandle()), value.getObjectName());
                }
                return true;
            }
            case REFLECT: reflect(event.getReflect(), target, federation, generation, selected); return true;
            case RECEIVE: receive(event.getReceive(), target, federation, generation, selected); return true;
            case REMOVE: remove(event.getRemove(), target, federation, generation, afterRemoval, selected); return true;
            case PROVIDE_UPDATE: {
                Stream.ProvideAttributeValueUpdate value = event.getProvideUpdate();
                target.provideAttributeValueUpdate(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()), value.getUserSuppliedTag().toByteArray());
                return true;
            }
            case RESERVATION_SUCCEEDED:
                target.objectInstanceNameReservationSucceeded(event.getReservationSucceeded().getObjectName()); return true;
            case RESERVATION_FAILED:
                target.objectInstanceNameReservationFailed(event.getReservationFailed().getObjectName()); return true;
            case RESERVATION_MULTI_SUCCEEDED:
                target.multipleObjectInstanceNameReservationSucceeded(new LinkedHashSet<>(event.getReservationMultiSucceeded().getObjectNamesList())); return true;
            case RESERVATION_MULTI_FAILED:
                target.multipleObjectInstanceNameReservationFailed(new LinkedHashSet<>(event.getReservationMultiFailed().getRequestedNamesList())); return true;
            case SYNC_REGISTRATION_SUCCEEDED:
                target.synchronizationPointRegistrationSucceeded(event.getSyncRegistrationSucceeded().getLabel()); return true;
            case SYNC_REGISTRATION_FAILED:
                target.synchronizationPointRegistrationFailed(event.getSyncRegistrationFailed().getLabel(),
                        syncFailure(event.getSyncRegistrationFailed().getReason())); return true;
            case SYNC_ANNOUNCED:
                target.announceSynchronizationPoint(event.getSyncAnnounced().getLabel(), event.getSyncAnnounced().getTag().toByteArray()); return true;
            case SYNC_SYNCHRONIZED: {
                FederateHandleSet failed = HandleCollections.federateSetFactory().create();
                for (long handle : event.getSyncSynchronized().getFailedToSyncList()) failed.add(Handles.federate(handle));
                target.federationSynchronized(event.getSyncSynchronized().getLabel(), failed); return true;
            }
            case START_REGISTRATION:
                target.startRegistrationForObjectClass(Handles.objectClass(event.getStartRegistration().getObjectClassHandle())); return true;
            case STOP_REGISTRATION:
                target.stopRegistrationForObjectClass(Handles.objectClass(event.getStopRegistration().getObjectClassHandle())); return true;
            case TURN_INTERACTIONS_ON:
                target.turnInteractionsOn(Handles.interactionClass(event.getTurnInteractionsOn().getInteractionClassHandle())); return true;
            case TURN_INTERACTIONS_OFF:
                target.turnInteractionsOff(Handles.interactionClass(event.getTurnInteractionsOff().getInteractionClassHandle())); return true;
            case ATTRIBUTES_IN_SCOPE:
                target.attributesInScope(Handles.objectInstance(event.getAttributesInScope().getObjectHandle()),
                        attributes(event.getAttributesInScope().getAttributeHandlesList())); return true;
            case ATTRIBUTES_OUT_OF_SCOPE:
                target.attributesOutOfScope(Handles.objectInstance(event.getAttributesOutOfScope().getObjectHandle()),
                        attributes(event.getAttributesOutOfScope().getAttributeHandlesList())); return true;
            case TURN_UPDATES_ON: {
                Stream.TurnUpdatesOnForObjectInstance value = event.getTurnUpdatesOn();
                if (value.hasUpdateRateDesignator()) target.turnUpdatesOnForObjectInstance(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()), value.getUpdateRateDesignator());
                else target.turnUpdatesOnForObjectInstance(Handles.objectInstance(value.getObjectHandle()), attributes(value.getAttributeHandlesList()));
                return true;
            }
            case TURN_UPDATES_OFF:
                target.turnUpdatesOffForObjectInstance(Handles.objectInstance(event.getTurnUpdatesOff().getObjectHandle()),
                        attributes(event.getTurnUpdatesOff().getAttributeHandlesList())); return true;
            case RETRACTION_REQUESTED:
                target.requestRetraction(Handles.retraction(federation, generation, event.getRetractionRequested().getSenderFederate(),
                        event.getRetractionRequested().getMessageRetractionHandle())); return true;
            default: return false;
        }
    }

    private static void reflect(Stream.ReflectAttributeValues value, FederateAmbassador target, String federation, long generation, String selected) throws RTIexception {
        AttributeHandleValueMap attributes = HandleCollections.attributeValueMapFactory().create(value.getAttributesCount());
        value.getAttributesMap().forEach((handle, bytes) -> attributes.put(Handles.attribute(handle), bytes.toByteArray()));
        Stream.ObjectCallbackMetadata metadata = value.getMetadata();
        LogicalTime timestamp = timestamp(value, selected);
        OrderType sent = sent(metadata, timestamp != null);
        TransportationTypeHandle transport = transport(metadata);
        ObjectInstanceHandle object = Handles.objectInstance(value.getObjectHandle());
        byte[] tag = value.getUserSuppliedTag().toByteArray();
        Supplemental info = new Supplemental(metadata);
        if (timestamp == null) {
            target.reflectAttributeValues(object, attributes, tag, sent, transport, info);
        } else if (received(metadata) == OrderType.RECEIVE) {
            target.reflectAttributeValues(object, attributes, tag, sent, transport,
                    timestamp, OrderType.RECEIVE, info);
        } else {
            target.reflectAttributeValues(object, attributes, tag, sent, transport,
                    timestamp, OrderType.TIMESTAMP, retraction(metadata, federation, generation), info);
        }
    }

    private static void receive(Stream.ReceiveInteraction value, FederateAmbassador target, String federation, long generation, String selected) throws RTIexception {
        ParameterHandleValueMap parameters = HandleCollections.parameterValueMapFactory().create(value.getParametersCount());
        value.getParametersMap().forEach((handle, bytes) -> parameters.put(Handles.parameter(handle), bytes.toByteArray()));
        Stream.ObjectCallbackMetadata metadata = value.getMetadata();
        LogicalTime timestamp = timestamp(value, selected);
        OrderType sent = sent(metadata, timestamp != null);
        TransportationTypeHandle transport = transport(metadata);
        InteractionClassHandle interaction = Handles.interactionClass(value.getInteractionClassHandle());
        byte[] tag = value.getUserSuppliedTag().toByteArray();
        Supplemental info = new Supplemental(metadata);
        if (timestamp == null) {
            target.receiveInteraction(interaction, parameters, tag, sent, transport, info);
        } else if (received(metadata) == OrderType.RECEIVE) {
            target.receiveInteraction(interaction, parameters, tag, sent, transport,
                    timestamp, OrderType.RECEIVE, info);
        } else {
            target.receiveInteraction(interaction, parameters, tag, sent, transport,
                    timestamp, OrderType.TIMESTAMP, retraction(metadata, federation, generation), info);
        }
    }

    private static void remove(Stream.RemoveObjectInstance value, FederateAmbassador target, String federation,
            long generation, Runnable afterRemoval, String selected) throws RTIexception {
        Stream.ObjectCallbackMetadata metadata = value.getMetadata();
        LogicalTime time = timestamp(value, selected);
        OrderType sent = sent(metadata, time != null);
        ObjectInstanceHandle object = Handles.objectInstance(value.getObjectHandle());
        byte[] tag = value.getUserSuppliedTag().toByteArray();
        Supplemental info = new Supplemental(metadata);
        OrderType received = received(metadata);
        MessageRetractionHandle handle = received == OrderType.TIMESTAMP ? retraction(metadata, federation, generation) : null;
        // Commit local knowledge after an actual invocation, including a throwing user callback.
        try {
            if (time == null) target.removeObjectInstance(object, tag, sent, info);
            else if (received == OrderType.RECEIVE) target.removeObjectInstance(object, tag, sent, time, received, info);
            else target.removeObjectInstance(object, tag, sent, time, received, handle, info);
        } finally {
            afterRemoval.run();
        }
    }

    private static LogicalTime timestamp(com.google.protobuf.Message value, String selected) throws RTIinternalError {
        try { return TimeWire.hasTime(value, "logical_time") ? TimeWire.read(value, "logical_time", selected) : null; }
        catch (IllegalArgumentException invalid) { throw new RTIinternalError("Invalid callback logical time", invalid); }
    }

    private static AttributeHandleSet attributes(List<Long> values) {
        AttributeHandleSet result = HandleCollections.attributeSetFactory().create();
        for (long value : values) result.add(Handles.attribute(value));
        return result;
    }

    private static OrderType sent(Stream.ObjectCallbackMetadata metadata, boolean timestamped) throws RTIinternalError {
        if (!metadata.hasSentOrder() || !metadata.hasReceivedOrder()) throw new RTIinternalError("Callback order metadata is absent");
        OrderType sent = order(metadata.getSentOrder());
        OrderType received = received(metadata);
        if (received == OrderType.TIMESTAMP && sent != OrderType.TIMESTAMP) {
            throw new RTIinternalError("Receive-order send cannot become a timestamp-order callback");
        }
        if (!timestamped && (sent == OrderType.TIMESTAMP || received == OrderType.TIMESTAMP)) {
            throw new RTIinternalError("Timestamp-ordered callback has no logical time");
        }
        return sent;
    }
    private static OrderType received(Stream.ObjectCallbackMetadata metadata) throws RTIinternalError {
        if (!metadata.hasReceivedOrder()) throw new RTIinternalError("Callback received order is absent");
        return order(metadata.getReceivedOrder());
    }
    private static OrderType order(rti.v1.Object.OrderType value) throws RTIinternalError {
        switch (value) {
            case ORDER_TYPE_RECEIVE: return OrderType.RECEIVE;
            case ORDER_TYPE_TIMESTAMP: return OrderType.TIMESTAMP;
            default: throw new RTIinternalError("Unknown callback order type: " + value);
        }
    }
    private static TransportationTypeHandle transport(Stream.ObjectCallbackMetadata metadata) throws RTIinternalError {
        if (!metadata.hasTransportationType()) throw new RTIinternalError("Callback transportation metadata is absent");
        Common.TransportationType value = metadata.getTransportationType();
        if (value != Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE
                && value != Common.TransportationType.TRANSPORTATION_TYPE_BEST_EFFORT) {
            throw new RTIinternalError("Unknown callback transportation type: " + value);
        }
        return Handles.transportation(value.getNumber());
    }
    private static MessageRetractionHandle retraction(Stream.ObjectCallbackMetadata metadata, String federation, long generation) throws RTIinternalError {
        if (!metadata.hasProducingFederate()) throw new RTIinternalError("TSO callback has no producing identity");
        if (!metadata.hasMessageRetractionHandle() || metadata.getMessageRetractionHandle() == 0) {
            return Handles.nonRetractableMessage();
        }
        return Handles.retraction(federation, generation, metadata.getProducingFederate(), metadata.getMessageRetractionHandle());
    }
    private static SynchronizationPointFailureReason syncFailure(Stream.SyncPointFailureReason value) throws RTIinternalError {
        switch (value) {
            case SYNC_POINT_FAILURE_REASON_LABEL_NOT_UNIQUE: return SynchronizationPointFailureReason.SYNCHRONIZATION_POINT_LABEL_NOT_UNIQUE;
            case SYNC_POINT_FAILURE_REASON_SET_MEMBER_NOT_JOINED: return SynchronizationPointFailureReason.SYNCHRONIZATION_SET_MEMBER_NOT_JOINED;
            default: throw new RTIinternalError("Unknown synchronization failure reason: " + value);
        }
    }

    private static final class Supplemental implements FederateAmbassador.SupplementalReflectInfo,
            FederateAmbassador.SupplementalReceiveInfo, FederateAmbassador.SupplementalRemoveInfo {
        private final Stream.ObjectCallbackMetadata metadata;
        Supplemental(Stream.ObjectCallbackMetadata metadata) { this.metadata = metadata; }
        @Override public boolean hasProducingFederate() { return metadata.hasProducingFederate(); }
        @Override public FederateHandle getProducingFederate() { return hasProducingFederate() ? Handles.federate(metadata.getProducingFederate()) : null; }
        @Override public boolean hasSentRegions() { return metadata.hasReceivedRegions(); }
        @Override public RegionHandleSet getSentRegions() {
            if (!hasSentRegions()) return null;
            RegionHandleSet result = HandleCollections.regionSetFactory().create();
            for (long value : metadata.getReceivedRegions().getRegionHandlesList()) result.add(Handles.region(value));
            return result;
        }
    }
}
