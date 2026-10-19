package org.gorti.rti;

import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.FederateAmbassador;
import hla.rti1516e.exceptions.RTIexception;
import java.util.List;
import org.gorti.rti.types.HandleCollections;
import org.gorti.rti.types.Handles;
import rti.v1.Stream;

/** Ownership stream decoding, invoked by the parent's evoked callback queue. */
final class OwnershipCallbacks {
    private OwnershipCallbacks() { }

    static boolean deliver(Stream.FederateEvent event, FederateAmbassador ambassador) throws RTIexception {
        switch (event.getEventCase()) {
            case OWNERSHIP_ASSUMPTION: {
                Stream.RequestAttributeOwnershipAssumption value = event.getOwnershipAssumption();
                ambassador.requestAttributeOwnershipAssumption(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()), value.getTag().toByteArray());
                return true;
            }
            case OWNERSHIP_ACQUIRED: {
                Stream.AttributeOwnershipAcquisitionNotification value = event.getOwnershipAcquired();
                ambassador.attributeOwnershipAcquisitionNotification(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()), value.getTag().toByteArray());
                return true;
            }
            case OWNERSHIP_DIVEST_CONFIRMED: {
                Stream.RequestDivestitureConfirmation value = event.getOwnershipDivestConfirmed();
                ambassador.requestDivestitureConfirmation(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()));
                return true;
            }
            case OWNERSHIP_RELEASE_REQUESTED: {
                Stream.RequestAttributeOwnershipRelease value = event.getOwnershipReleaseRequested();
                ambassador.requestAttributeOwnershipRelease(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()), value.getTag().toByteArray());
                return true;
            }
            case OWNERSHIP_UNAVAILABLE: {
                Stream.AttributeOwnershipUnavailable value = event.getOwnershipUnavailable();
                ambassador.attributeOwnershipUnavailable(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()));
                return true;
            }
            case OWNERSHIP_ACQUISITION_CANCELLED: {
                Stream.ConfirmAttributeOwnershipAcquisitionCancellation value = event.getOwnershipAcquisitionCancelled();
                ambassador.confirmAttributeOwnershipAcquisitionCancellation(Handles.objectInstance(value.getObjectHandle()),
                        attributes(value.getAttributeHandlesList()));
                return true;
            }
            default: return false;
        }
    }

    static AttributeHandleSet attributes(List<Long> values) {
        AttributeHandleSet result = HandleCollections.attributeSetFactory().create();
        for (long value : values) result.add(Handles.attribute(value));
        return result;
    }
}
