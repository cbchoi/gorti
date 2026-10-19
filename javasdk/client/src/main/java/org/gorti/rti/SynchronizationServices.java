package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.FederateHandle;
import hla.rti1516e.FederateHandleSet;
import hla.rti1516e.exceptions.*;
import java.util.HashSet;
import java.util.Set;
import org.gorti.rti.types.Handles;
import rti.v1.Common;
import rti.v1.Sync;
import rti.v1.SyncServiceGrpc;

/** Synchronization point state changes at callback invocation, not stream admission. */
final class SynchronizationServices {
    private final ServiceContext context;
    private final Set<String> announced = new HashSet<>();

    SynchronizationServices(ServiceContext context) { this.context = context; }

    public void registerFederationSynchronizationPoint(String label, byte[] tag) throws RTIexception {
        register(label, tag, null);
    }

    public void registerFederationSynchronizationPoint(String label, byte[] tag, FederateHandleSet participants)
            throws RTIexception {
        context.requireMember();
        if (participants == null) {
            throw new RTIinternalError("Explicit synchronization set is null");
        }
        register(label, tag, participants);
    }

    private void register(String label, byte[] tag, FederateHandleSet participants) throws RTIexception {
        context.requireMember();
        if (label == null) throw new RTIinternalError("Synchronization label is null");
        Sync.RegisterSyncPointRequest.Builder request = Sync.RegisterSyncPointRequest.newBuilder()
                .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation())
                .setFederateHandle(context.federate()).setLabel(label)
                .setTag(tag == null ? ByteString.EMPTY : ByteString.copyFrom(tag));
        if (participants != null) {
            for (FederateHandle participant : participants) {
                try { request.addRequiredFederates(Handles.value(participant)); }
                catch (IllegalArgumentException failure) { throw new InvalidFederateHandle("Foreign federate handle", failure); }
            }
        }
        WireErrors.call(() -> SyncServiceGrpc.newBlockingStub(context.channel())
                .registerFederationSynchronizationPoint(request.build()));
    }

    public void synchronizationPointAchieved(String label) throws RTIexception {
        synchronizationPointAchieved(label, true);
    }

    public void synchronizationPointAchieved(String label, boolean successful) throws RTIexception {
        context.requireMember();
        if (!announced.contains(label)) throw new SynchronizationPointLabelNotAnnounced("Point has not been announced: " + label);
        WireErrors.call(() -> SyncServiceGrpc.newBlockingStub(context.channel()).synchronizationPointAchieved(
                Sync.AchieveSyncPointRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                        .setFederationName(context.federation()).setFederateHandle(context.federate())
                        .setLabel(label).setSuccessfully(successful).build()));
        announced.remove(label);
    }

    void announced(String label) { announced.add(label); }
    void completed(String label) { announced.remove(label); }
    void clear() { announced.clear(); }
    Set<String> checkpoint() { return new HashSet<>(announced); }
    void restore(Set<String> saved) { announced.clear(); announced.addAll(saved); }
}
