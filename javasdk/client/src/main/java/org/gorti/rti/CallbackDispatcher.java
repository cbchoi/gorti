package org.gorti.rti;

import com.google.protobuf.ByteString;
import hla.rti1516e.exceptions.RTIinternalError;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import rti.v1.Stream;

/** Phase-rejected entries remain pending, not running or completed callbacks. */
final class CallbackDispatcher {
    private final Map<ByteString, Pending> pending = new LinkedHashMap<>();
    private long evocation;
    private boolean deferred;

    static final class Pending {
        final Stream.FederateEvent event;
        final long restoreGate, joinedEpoch, callbackEpoch, attemptedEvocation;
        Pending(Stream.FederateEvent event, long restoreGate, long joinedEpoch, long callbackEpoch, long evocation) {
            this.event = event; this.restoreGate = restoreGate;
            this.joinedEpoch = joinedEpoch; this.callbackEpoch = callbackEpoch; this.attemptedEvocation = evocation;
        }
    }

    void beginEvocation() { evocation++; }
    void beginDispatch() { deferred = false; }
    boolean deferred() { return deferred; }

    boolean retained(Stream.FederateEvent event, long joinedEpoch, long callbackEpoch) {
        retire(joinedEpoch, callbackEpoch);
        if (!pending.containsKey(event.getCallbackReceipt())) return false;
        deferred = true;
        return true;
    }

    void defer(Stream.FederateEvent event, long restoreGate, long joinedEpoch, long callbackEpoch) throws RTIinternalError {
        retire(joinedEpoch, callbackEpoch);
        if (!pending.containsKey(event.getCallbackReceipt())) {
            if (pending.size() >= 16384) throw new RTIinternalError("Deferred callback capacity exceeded");
            pending.put(event.getCallbackReceipt(), new Pending(event, restoreGate, joinedEpoch, callbackEpoch, evocation));
        }
        deferred = true;
    }

    Pending poll(long joinedEpoch, long callbackEpoch) {
        retire(joinedEpoch, callbackEpoch);
        Iterator<Pending> entries = pending.values().iterator();
        while (entries.hasNext()) {
            Pending entry = entries.next();
            // A failed retry cannot spin ahead of queued checkpoint controls.
            if (entry.attemptedEvocation == evocation) continue;
            entries.remove();
            return entry;
        }
        return null;
    }

    boolean hasPending(long joinedEpoch, long callbackEpoch) {
        retire(joinedEpoch, callbackEpoch);
        return !pending.isEmpty();
    }

    List<Stream.FederateEvent> checkpointEvents(long joinedEpoch, long callbackEpoch) {
        retire(joinedEpoch, callbackEpoch);
        List<Stream.FederateEvent> events = new ArrayList<>();
        for (Pending entry : pending.values()) events.add(entry.event);
        return events;
    }

    void clear() { pending.clear(); }

    private void retire(long joinedEpoch, long callbackEpoch) {
        pending.values().removeIf(entry -> entry.joinedEpoch != joinedEpoch || entry.callbackEpoch != callbackEpoch);
    }
}
