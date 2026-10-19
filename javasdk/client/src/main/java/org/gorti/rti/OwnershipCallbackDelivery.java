package org.gorti.rti;

import hla.rti1516e.exceptions.RTIinternalError;
import hla.rti1516e.exceptions.RTIexception;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;
import rti.v1.Callback;
import rti.v1.Common;
import rti.v1.OwnershipCallbackServiceGrpc;
import rti.v1.Stream;

/** Invocation acknowledgments are independent of optional MOM telemetry. */
final class OwnershipCallbackDelivery {
    private final ServiceContext context;
    private boolean enabled;
    private final Set<String> invoked = new HashSet<>();
    private final Map<String, Callback.OwnershipCallbackReceipt> pending = new HashMap<>();
    private final Map<Long, Long> latest = new HashMap<>(), retired = new HashMap<>();

    OwnershipCallbackDelivery(ServiceContext context) { this.context = context; }

    void enable() throws RTIexception {
        WireErrors.call(() -> stub().enableOwnershipCallbackReceipts(scope()));
        enabled = true;
    }
    boolean enabled() { return enabled; }
    boolean enableIfSupported() throws RTIexception {
        try { stub().enableOwnershipCallbackReceipts(scope()); enabled = true; return true; }
        catch (io.grpc.StatusRuntimeException rejected) {
            if (rejected.getStatus().getCode() == io.grpc.Status.Code.UNIMPLEMENTED) return false;
            throw WireErrors.translate(rejected);
        }
    }
    void retry() throws RTIexception {
        flush();
        WireErrors.call(() -> stub().retryOwnershipCallbacks(scope()));
    }
    void observe(Stream.FederateEvent event) {
        if (event.hasDiscover() && event.getObjectKnowledgeEpoch() != 0) {
            long object = event.getDiscover().getObjectHandle(), epoch = event.getObjectKnowledgeEpoch();
            synchronized (this) {
                if (Long.compareUnsigned(epoch, latest.getOrDefault(object, 0L)) > 0) latest.put(object, epoch);
            }
        }
    }
    synchronized void retire(long object, long epoch) {
        if (Long.compareUnsigned(epoch, retired.getOrDefault(object, 0L)) > 0) retired.put(object, epoch);
    }
    synchronized boolean begin(Stream.FederateEvent event) throws RTIinternalError {
        long object = event.hasDiscover() ? event.getDiscover().getObjectHandle()
                : event.hasOwnershipAssumption() ? event.getOwnershipAssumption().getObjectHandle() : 0;
        long epoch = event.getObjectKnowledgeEpoch();
        if (object != 0 && epoch != 0 && (Long.compareUnsigned(epoch, retired.getOrDefault(object, 0L)) <= 0
                || Long.compareUnsigned(epoch, latest.getOrDefault(object, 0L)) < 0)) return false;
        if (!event.hasOwnershipReceipt()) return true;
        Callback.OwnershipCallbackReceipt receipt = event.getOwnershipReceipt();
        if (!enabled || receipt.getFederationGeneration() != context.generation()
                || receipt.getCallbackEpoch() != context.callbackEpoch()
                || receipt.getCallbackId() == 0 || receipt.getDeliveryAttempt() == 0
                || !(event.hasOwnershipAcquired() || event.hasOwnershipUnavailable() || event.hasOwnershipAcquisitionCancelled()))
            throw new RTIinternalError("Invalid terminal ownership callback receipt");
        String key = key(receipt);
        pending.put(key, receipt);
        return invoked.add(key);
    }
    void flush() throws RTIexception {
        Map<String, Callback.OwnershipCallbackReceipt> snapshot;
        synchronized (this) { snapshot = new HashMap<>(pending); }
        for (Map.Entry<String, Callback.OwnershipCallbackReceipt> entry : snapshot.entrySet()) {
            Callback.ReportOwnershipCallbackInvocationRequest report = Callback.ReportOwnershipCallbackInvocationRequest.newBuilder()
                    .setWireVersion(Common.WireVersion.WIRE_VERSION_V1).setFederationName(context.federation())
                    .setFederateHandle(context.federate()).setReceipt(entry.getValue()).build();
            WireErrors.call(() -> stub().reportOwnershipCallbackInvocation(report));
            synchronized (this) { pending.remove(entry.getKey(), entry.getValue()); }
        }
    }
    synchronized void entryFailed(Stream.FederateEvent event) {
        if (event.hasOwnershipReceipt()) {
            String key = key(event.getOwnershipReceipt());
            invoked.remove(key);
            pending.remove(key);
        }
    }
    synchronized void reset() { enabled = false; invoked.clear(); pending.clear(); latest.clear(); retired.clear(); }
    synchronized void restoreEpoch() { pending.clear(); invoked.clear(); latest.clear(); retired.clear(); }
    private static String key(Callback.OwnershipCallbackReceipt receipt) {
        return Long.toUnsignedString(receipt.getFederationGeneration()) + ":" + Long.toUnsignedString(receipt.getCallbackEpoch())
                + ":" + Long.toUnsignedString(receipt.getCallbackId());
    }
    private Callback.OwnershipCallbackScopeRequest scope() {
        return Callback.OwnershipCallbackScopeRequest.newBuilder().setWireVersion(Common.WireVersion.WIRE_VERSION_V1)
                .setFederationName(context.federation()).setFederateHandle(context.federate())
                .setFederationGeneration(context.generation()).setCallbackEpoch(context.callbackEpoch()).build();
    }
    private OwnershipCallbackServiceGrpc.OwnershipCallbackServiceBlockingStub stub() {
        return OwnershipCallbackServiceGrpc.newBlockingStub(context.channel());
    }
}
