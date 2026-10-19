package org.gorti.rti;

import hla.rti1516e.FederateAmbassador;
import hla.rti1516e.exceptions.RTIexception;

/** Schedules callbacks derived from authoritative service responses for evoked delivery. */
interface CallbackSink {
    void enqueue(Invocation callback);

    /** Capture lifecycle identity before starting a response-producing request. */
    default CallbackSink capture() { return this; }

    @FunctionalInterface
    interface Invocation {
        void invoke(FederateAmbassador ambassador) throws RTIexception;
    }
}
