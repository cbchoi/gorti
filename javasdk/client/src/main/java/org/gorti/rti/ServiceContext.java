package org.gorti.rti;

import hla.rti1516e.exceptions.RTIexception;
import io.grpc.Channel;

/** Joined identity shared by the standard service adapters; guarded by the facade. */
interface ServiceContext {
    void requireMember() throws RTIexception;
    default void requireRestoreControl() throws RTIexception { requireMember(); }
    default void requireRestoreResponse() throws RTIexception { requireRestoreControl(); }
    Channel channel();
    String federation();
    long federate();
    long generation();
    boolean regulating();
    double logicalTime();
    default String timeImplementation() { return TimeWire.FLOAT; }
    default hla.rti1516e.LogicalTime logicalTimeValue() { return org.gorti.rti.types.TimeValues.time(logicalTime()); }
    default long callbackEpoch() { return 0; }
    default void flushOwnershipCallbacks() throws RTIexception { }
    default boolean ownershipCallbackReceiptsEnabled() { return false; }
    default void retireObjectKnowledge(long object, long epoch) { }
}
