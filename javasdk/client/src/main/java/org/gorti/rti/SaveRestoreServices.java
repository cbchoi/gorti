package org.gorti.rti;

import hla.rti1516e.FederateHandleSaveStatusPair;
import hla.rti1516e.FederateRestoreStatus;
import hla.rti1516e.LogicalTime;
import hla.rti1516e.exceptions.InvalidLogicalTime;
import hla.rti1516e.exceptions.RTIexception;
import hla.rti1516e.exceptions.RTIinternalError;
import org.gorti.rti.types.TimeValues;
import rti.v1.Common;
import rti.v1.Savepoint;
import rti.v1.SavepointServiceGrpc;

/** Save/restore RPCs; the facade owns checkpoint state and restore transitions. */
final class SaveRestoreServices {
    private final ServiceContext context;
    private final CallbackSink callbacks;
    private final CheckpointSource checkpoint;
    private final RestoreReadiness restoreReadiness;

    @FunctionalInterface interface CheckpointSource { byte[] read() throws RTIexception; }
    @FunctionalInterface interface RestoreReadiness { void check() throws RTIexception; }

    SaveRestoreServices(ServiceContext context, CallbackSink callbacks) {
        this(context, callbacks, () -> new byte[0], () -> { });
    }

    SaveRestoreServices(ServiceContext context, CallbackSink callbacks, CheckpointSource checkpoint, RestoreReadiness restoreReadiness) {
        this.context = context;
        this.callbacks = callbacks;
        this.checkpoint = checkpoint;
        this.restoreReadiness = restoreReadiness;
    }

    public void requestFederationSave(String label) throws RTIexception {
        context.requireMember();
        Savepoint.RequestFederationSaveRequest request = saveRequest(label).build();
        WireErrors.call(() -> stub().requestFederationSave(request));
    }

    public void requestFederationSave(String label, LogicalTime time) throws RTIexception {
        context.requireMember();
        LogicalTime value;
        try { value = TimeWire.checked(time, context.timeImplementation()); }
        catch (IllegalArgumentException failure) { throw new InvalidLogicalTime("Logical time does not match the selected factory", failure); }
        // The server validates regulating/GALT, pending advances, and lookahead atomically.
        Savepoint.RequestFederationSaveRequest request = TimeWire.write(saveRequest(label), "save_time", value, context.timeImplementation()).build();
        WireErrors.call(() -> stub().requestFederationSave(request));
    }

    public void federateSaveBegun() throws RTIexception {
        context.requireMember();
        Savepoint.FederateSaveResponseRequest request = saveResponse();
        WireErrors.call(() -> stub().federateSaveBegun(request));
    }

    public void federateSaveComplete() throws RTIexception {
        context.requireMember();
        Savepoint.FederateSaveResponseRequest request = saveResponse().toBuilder()
                .setClientState(com.google.protobuf.ByteString.copyFrom(checkpoint.read())).build();
        WireErrors.call(() -> stub().federateSaveComplete(request));
    }

    public void federateSaveNotComplete() throws RTIexception {
        context.requireMember();
        Savepoint.FederateSaveResponseRequest request = saveResponse();
        WireErrors.call(() -> stub().federateSaveNotComplete(request));
    }

    public void abortFederationSave() throws RTIexception {
        context.requireMember();
        Savepoint.AbortFederationSaveRequest request = Savepoint.AbortFederationSaveRequest.newBuilder()
                .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation())
                .setFederateHandle(context.federate()).build();
        WireErrors.call(() -> stub().abortFederationSave(request));
    }

    public void queryFederationSaveStatus() throws RTIexception {
        context.requireMember();
        Savepoint.QuerySaveStateRequest request = Savepoint.QuerySaveStateRequest.newBuilder()
                .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation()).build();
        CallbackSink delivery = callbacks.capture();
        Savepoint.QuerySaveStateResponse response = WireErrors.call(() -> stub().querySaveState(request));
        delivery.enqueue(LocalNotification.saveStatus(response));
    }

    public void requestFederationRestore(String label) throws RTIexception {
        context.requireMember();
        requireLabel(label);
        Savepoint.RequestFederationRestoreRequest request = Savepoint.RequestFederationRestoreRequest.newBuilder()
                .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation())
                .setFederateHandle(context.federate()).setLabel(label).build();
        WireErrors.call(() -> stub().requestFederationRestore(request));
    }

    public void federateRestoreComplete() throws RTIexception {
        context.requireRestoreResponse();
        restoreReadiness.check();
        Savepoint.FederateRestoreResponseRequest request = restoreResponse();
        WireErrors.call(() -> stub().federateRestoreComplete(request));
    }

    public void federateRestoreNotComplete() throws RTIexception {
        context.requireRestoreResponse();
        Savepoint.FederateRestoreResponseRequest request = restoreResponse();
        WireErrors.call(() -> stub().federateRestoreNotComplete(request));
    }

    public void abortFederationRestore() throws RTIexception {
        context.requireRestoreControl();
        Savepoint.AbortFederationRestoreRequest request = Savepoint.AbortFederationRestoreRequest.newBuilder()
                .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation())
                .setFederateHandle(context.federate()).build();
        WireErrors.call(() -> stub().abortFederationRestore(request));
    }

    public void queryFederationRestoreStatus() throws RTIexception {
        context.requireRestoreControl();
        Savepoint.QueryRestoreStateRequest request = Savepoint.QueryRestoreStateRequest.newBuilder()
                .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation()).build();
        CallbackSink delivery = callbacks.capture();
        Savepoint.QueryRestoreStateResponse response = WireErrors.call(() -> stub().queryRestoreState(request));
        delivery.enqueue(LocalNotification.restoreStatus(response));
    }

    private Savepoint.RequestFederationSaveRequest.Builder saveRequest(String label) throws RTIinternalError {
        requireLabel(label);
        return Savepoint.RequestFederationSaveRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                .setFederationName(context.federation()).setFederateHandle(context.federate()).setLabel(label);
    }

    private Savepoint.FederateSaveResponseRequest saveResponse() {
        return Savepoint.FederateSaveResponseRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                .setFederationName(context.federation()).setFederateHandle(context.federate()).build();
    }

    private Savepoint.FederateRestoreResponseRequest restoreResponse() {
        return Savepoint.FederateRestoreResponseRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                .setFederationName(context.federation()).setFederateHandle(context.federate()).build();
    }

    private SavepointServiceGrpc.SavepointServiceBlockingStub stub() {
        return SavepointServiceGrpc.newBlockingStub(context.channel());
    }

    private static void requireLabel(String label) throws RTIinternalError {
        if (label == null) throw new RTIinternalError("Save/restore label is null");
    }
}
