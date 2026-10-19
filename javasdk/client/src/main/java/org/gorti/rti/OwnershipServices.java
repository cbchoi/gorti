package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.AttributeHandle;
import hla.rti1516e.AttributeHandleSet;
import hla.rti1516e.ObjectInstanceHandle;
import hla.rti1516e.exceptions.AttributeNotDefined;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import org.gorti.rti.types.Handles;
import rti.v1.Common;
import rti.v1.Ownership;
import rti.v1.OwnershipServiceGrpc;

/** Standard ownership services; serialized by the owning ambassador. */
final class OwnershipServices {
    private static final Common.WireVersion V1 = Common.WireVersion.WIRE_VERSION_V1;
    // QueryOwnershipResponse reserves UINT64_MAX for the backend's MOM owner.
    private static final long RTI_OWNER = -1L;
    private final ServiceContext context;
    private final CallbackSink callbacks;
    private final ExchangeServices exchange;

    public OwnershipServices(ServiceContext context, CallbackSink callbacks, ExchangeServices exchange) {
        this.context = Objects.requireNonNull(context, "context");
        this.callbacks = Objects.requireNonNull(callbacks, "callbacks");
        this.exchange = Objects.requireNonNull(exchange, "exchange");
    }

    public void unconditionalAttributeOwnershipDivestiture(ObjectInstanceHandle object, AttributeHandleSet attributes) throws RTIexception {
        long target = object(object);
        Ownership.UnconditionalDivestRequest request = Ownership.UnconditionalDivestRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).build();
        WireErrors.call(() -> ownership().unconditionalAttributeOwnershipDivestiture(request));
    }

    public void negotiatedAttributeOwnershipDivestiture(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) throws RTIexception {
        long target = object(object);
        Ownership.NegotiatedDivestRequest request = Ownership.NegotiatedDivestRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).setTag(bytes(tag)).setTwoPhase(true).build();
        WireErrors.call(() -> ownership().negotiatedAttributeOwnershipDivestiture(request));
    }

    public void confirmDivestiture(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) throws RTIexception {
        long target = object(object);
        Ownership.ConfirmDivestitureRequest request = Ownership.ConfirmDivestitureRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).setTag(bytes(tag)).build();
        WireErrors.call(() -> ownership().confirmDivestiture(request));
    }

    public void attributeOwnershipAcquisition(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag) throws RTIexception {
        acquire(object, attributes, tag, false);
    }

    public void attributeOwnershipAcquisitionIfAvailable(ObjectInstanceHandle object, AttributeHandleSet attributes) throws RTIexception {
        acquire(object, attributes, new byte[0], true);
    }

    private void acquire(ObjectInstanceHandle object, AttributeHandleSet attributes, byte[] tag, boolean ifAvailable) throws RTIexception {
        long target = object(object);
        Ownership.AcquireRequest request = Ownership.AcquireRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).setTag(bytes(tag)).setIfAvailable(ifAvailable).build();
        WireErrors.call(() -> ownership().attributeOwnershipAcquisition(request));
    }

    public void attributeOwnershipReleaseDenied(ObjectInstanceHandle object, AttributeHandleSet attributes) throws RTIexception {
        long target = object(object);
        Ownership.ReleaseDeniedRequest request = Ownership.ReleaseDeniedRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).build();
        WireErrors.call(() -> ownership().attributeOwnershipReleaseDenied(request));
    }

    public AttributeHandleSet attributeOwnershipDivestitureIfWanted(ObjectInstanceHandle object, AttributeHandleSet attributes) throws RTIexception {
        long target = object(object);
        Ownership.DivestIfWantedRequest request = Ownership.DivestIfWantedRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).build();
        Ownership.DivestIfWantedResponse result = WireErrors.call(() -> ownership().attributeOwnershipDivestitureIfWantedWithResult(request));
        return OwnershipCallbacks.attributes(result.getDivestedAttributeHandlesList());
    }

    public void cancelNegotiatedAttributeOwnershipDivestiture(ObjectInstanceHandle object, AttributeHandleSet attributes) throws RTIexception {
        long target = object(object);
        Ownership.CancelDivestRequest request = Ownership.CancelDivestRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).build();
        WireErrors.call(() -> ownership().cancelNegotiatedAttributeOwnershipDivestiture(request));
    }

    public void cancelAttributeOwnershipAcquisition(ObjectInstanceHandle object, AttributeHandleSet attributes) throws RTIexception {
        long target = object(object);
        Ownership.CancelAcquireRequest request = Ownership.CancelAcquireRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).addAllAttributeHandles(attributes(attributes)).build();
        // Negotiated servers deliver the mandatory terminal through the stream.
        CallbackSink delivery = callbacks.capture();
        WireErrors.call(() -> ownership().cancelAttributeOwnershipAcquisition(request));
        if (!context.ownershipCallbackReceiptsEnabled()) {
            delivery.enqueue(LocalNotification.cancelled(target, request.getAttributeHandlesList()));
        }
    }

    public void queryAttributeOwnership(ObjectInstanceHandle object, AttributeHandle attribute) throws RTIexception {
        long target = object(object);
        long requested = attribute(attribute);
        Ownership.QueryOwnershipRequest request = Ownership.QueryOwnershipRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).setAttributeHandle(requested).build();
        CallbackSink delivery = callbacks.capture();
        Ownership.QueryOwnershipResponse result = WireErrors.call(() -> ownership().queryAttributeOwnership(request));
        if (result.getOwned() != (result.getOwnerFederateHandle() != 0)) {
            throw new RTIinternalError("Contradictory ownership result");
        }
        if (result.getOwnerFederateHandle() == RTI_OWNER) {
            delivery.enqueue(LocalNotification.ownership("attributeIsOwnedByRTI", target, requested, 0));
        } else if (result.getOwned()) {
            delivery.enqueue(LocalNotification.ownership("informAttributeOwnership", target, requested, result.getOwnerFederateHandle()));
        } else {
            delivery.enqueue(LocalNotification.ownership("attributeIsNotOwned", target, requested, 0));
        }
    }

    public boolean isAttributeOwnedByFederate(ObjectInstanceHandle object, AttributeHandle attribute) throws RTIexception {
        long target = object(object);
        Ownership.IsOwnedRequest request = Ownership.IsOwnedRequest.newBuilder()
                .setWireVersion(V1).setFederationName(context.federation()).setFederateHandle(context.federate())
                .setObjectHandle(target).setAttributeHandle(attribute(attribute)).build();
        return WireErrors.call(() -> ownership().isAttributeOwnedByFederate(request)).getOwned();
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

    private static ByteString bytes(byte[] tag) throws RTIinternalError {
        if (tag == null) throw new RTIinternalError("User-supplied tag is null");
        return ByteString.copyFrom(tag);
    }

    private OwnershipServiceGrpc.OwnershipServiceBlockingStub ownership() {
        return OwnershipServiceGrpc.newBlockingStub(context.channel());
    }
}
