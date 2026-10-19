package org.gorti.rti;

import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.TransportationTypeHandle;
import hla.rti1516e.exceptions.RTIinternalError;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import rti.v1.Common;

/** Response-backed callbacks: the transportation RPCs do not emit stream events. */
final class TransportationCallbacks {
    private TransportationCallbacks() { }

    // Confirmation payloads come from successful activation, never request intent.
    static CallbackSink.Invocation confirmAttributes(rti.v1.Object.TransportationChange request) throws RTIinternalError {
        TransportationTypeHandle achieved = transportation(request.getTransportType());
        return ambassador -> {
            AttributeHandleSet attributes = HandleCollections.attributeSetFactory().create();
            for (long attribute : request.getAttributeHandlesList()) attributes.add(Handles.attribute(attribute));
            ambassador.confirmAttributeTransportationTypeChange(Handles.objectInstance(request.getObjectHandle()), attributes, achieved);
        };
    }

    static CallbackSink.Invocation confirmInteraction(rti.v1.Object.TransportationChange request) throws RTIinternalError {
        TransportationTypeHandle achieved = transportation(request.getTransportType());
        return ambassador -> ambassador.confirmInteractionTransportationTypeChange(
                Handles.interactionClass(request.getInteractionClassHandle()), achieved);
    }

    static CallbackSink.Invocation reportAttribute(rti.v1.Object.QueryAttributeTransportationTypeRequest request,
            rti.v1.Object.TransportationTypeResponse response) throws RTIinternalError {
        return LocalNotification.transport("reportAttributeTransportationType", request.getObjectHandle(), request.getAttributeHandle(), 0, 0, response.getTransportType());
    }

    static CallbackSink.Invocation reportInteraction(rti.v1.Object.QueryInteractionTransportationTypeRequest request,
            rti.v1.Object.TransportationTypeResponse response) throws RTIinternalError {
        return LocalNotification.transport("reportInteractionTransportationType", 0, 0, request.getTargetFederateHandle(), request.getInteractionClassHandle(), response.getTransportType());
    }

    private static TransportationTypeHandle transportation(Common.TransportationType value) throws RTIinternalError {
        if (value == Common.TransportationType.TRANSPORTATION_TYPE_RELIABLE) return Handles.transportation(1);
        if (value == Common.TransportationType.TRANSPORTATION_TYPE_BEST_EFFORT) return Handles.transportation(2);
        throw new RTIinternalError("Server returned an unspecified or unknown transportation type: " + value);
    }
}
